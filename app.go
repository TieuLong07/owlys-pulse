package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const createNoWindowFlag = 0x08000000

func hiddenWindowsCommand(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindowFlag,
	}
	return cmd
}

// App exposes the clean local-only backend to the Wails frontend.
type App struct {
	ctx           context.Context
	commandOutput func() (string, error)
	engine        *BoostEngine
	guardianMu    sync.Mutex
	recoveryError string
}

func NewApp() *App {
	return &App{
		commandOutput: tasklistOutput,
		engine:        NewBoostEngine(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startGlobalHotkey()
	// Auto-recover if previous session was abruptly closed
	if a.engine != nil {
		_ = a.engine.RecoverPreviousSession()
	}
}

func (a *App) shutdown(ctx context.Context) {
	if a.engine != nil && a.engine.Status().Active {
		_, _ = a.StopBoost()
	}
}

// startExplorerRescueWatchdog launches a detached minimal watchdog if Explorer was closed.
// If Pulse process unexpectedly terminates, the watchdog immediately starts explorer.exe!
func (a *App) startExplorerRescueWatchdog() {
	pid := os.Getpid()
	cmd := hiddenWindowsCommand("cmd.exe", "/C", fmt.Sprintf(
		"timeout /t 2 /nobreak >nul & :loop & tasklist /FI \"PID eq %d\" 2>nul | find \"%d\" >nul || (start explorer.exe & exit) & timeout /t 2 /nobreak >nul & goto loop",
		pid, pid,
	))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindowFlag | 0x00000008, // DETACHED_PROCESS
	}
	_ = cmd.Start()
}

func (a *App) startGlobalHotkey() {
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		user32 := syscall.NewLazyDLL("user32.dll")
		regHotKey := user32.NewProc("RegisterHotKey")
		unregHotKey := user32.NewProc("UnregisterHotKey")
		getMsg := user32.NewProc("GetMessageW")

		// Hotkey 1: Ctrl + Shift + F12 (0x7B)
		regHotKey.Call(0, 0x4F01, 0x0002|0x0004, 0x7B)
		// Hotkey 2: Ctrl + Alt + Shift + R (0x52)
		regHotKey.Call(0, 0x4F02, 0x0002|0x0001|0x0004, 0x52)
		defer unregHotKey.Call(0, 0x4F01)
		defer unregHotKey.Call(0, 0x4F02)

		var msg struct {
			Hwnd     uintptr
			Message  uint32
			Wparam   uintptr
			Lparam   uintptr
			Time     uint32
			Pt       struct{ X, Y int32 }
			LPrivate uint32
		}
		for {
			r, _, _ := getMsg.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if r == 0 || r == ^uintptr(0) {
				return
			}
			if msg.Message == 0x0312 { // WM_HOTKEY
				if a.engine != nil && a.engine.Status().Active {
					_, _ = a.StopBoost()
				}
			}
		}
	}()
}

// ListRunningProcesses reads processes currently reported by Windows Task Manager.
func (a *App) ListRunningProcesses() ([]RunningProcess, error) {
	output, err := a.commandOutput()
	if err != nil {
		return nil, err
	}
	return parseTasklistCSV(output)
}

func (a *App) GetRecoveryError() string {
	return a.recoveryError
}

func (a *App) RetryRecovery() error {
	a.guardianMu.Lock()
	defer a.guardianMu.Unlock()
	if a.engine == nil {
		return nil
	}
	err := a.engine.RecoverPreviousSession()
	if err != nil {
		a.recoveryError = err.Error()
	} else {
		a.recoveryError = ""
	}
	return err
}

// GetBoostStatus returns the current boost session state.
func (a *App) GetBoostStatus() BoostStatus {
	return a.engine.Status()
}

// StartBoost activates a game boost session using the user's selected options and PIDs.
func (a *App) StartBoost(req BoostRequest) (BoostStatus, error) {
	a.guardianMu.Lock()
	defer a.guardianMu.Unlock()

	running, err := a.ListRunningProcesses()
	if err != nil {
		return a.engine.Status(), err
	}

	status, err := a.engine.StartBoost(req, running)
	if err != nil {
		return status, err
	}

	if status.ExtremeIActive {
		a.startExplorerRescueWatchdog()
	}

	return status, nil
}

// StopBoost restores all suspended processes, power schemes, and stopped services.
func (a *App) StopBoost() (BoostStatus, error) {
	a.guardianMu.Lock()
	defer a.guardianMu.Unlock()

	status, err := a.engine.StopBoost()
	if err != nil {
		a.recoveryError = err.Error()
		return status, err
	}
	a.recoveryError = ""
	return status, nil
}

// ResumeProcess immediately resumes a single suspended process by PID during an active boost session.
func (a *App) ResumeProcess(pid int) (BoostStatus, error) {
	a.guardianMu.Lock()
	defer a.guardianMu.Unlock()
	return a.engine.ResumeSingleProcess(pid)
}

// GetUnknownProcessesReport returns a privacy-safe preview of unknown processes not yet in the Owlys Knowledge Base.
func (a *App) GetUnknownProcessesReport() (UnknownProcessReport, error) {
	running, err := a.ListRunningProcesses()
	if err != nil {
		return UnknownProcessReport{}, err
	}
	return buildUnknownProcessReport(running), nil
}

// ExportUnknownProcessesReport saves the anonymized unknown process report to disk for opt-in contribution.
func (a *App) ExportUnknownProcessesReport() (UnknownProcessReport, error) {
	running, err := a.ListRunningProcesses()
	if err != nil {
		return UnknownProcessReport{}, err
	}
	return saveUnknownProcessReportToDisk(running)
}

func tasklistOutput() (string, error) {
	output, err := hiddenWindowsCommand("tasklist", "/FO", "CSV").Output()
	return string(output), err
}
