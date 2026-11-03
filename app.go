package main

import (
	"context"
	"os/exec"
	"syscall"
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
}

func NewApp() *App {
	return &App{
		commandOutput: tasklistOutput,
		engine:        NewBoostEngine(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	if a.engine != nil && a.engine.Status().Active {
		_, _ = a.engine.StopBoost()
	}
}

// ListRunningProcesses reads processes currently reported by Windows Task Manager.
// It neither uses demo process data nor uploads process information.
func (a *App) ListRunningProcesses() ([]RunningProcess, error) {
	output, err := a.commandOutput()
	if err != nil {
		return nil, err
	}
	return parseTasklistCSV(output)
}

// GetBoostStatus returns the current boost session state.
func (a *App) GetBoostStatus() BoostStatus {
	return a.engine.Status()
}

// StartBoost activates a game boost session using the user's selected options and PIDs.
func (a *App) StartBoost(req BoostRequest) (BoostStatus, error) {
	running, err := a.ListRunningProcesses()
	if err != nil {
		return a.engine.Status(), err
	}
	return a.engine.StartBoost(req, running)
}

// StopBoost restores all suspended processes, power schemes, and stopped services.
func (a *App) StopBoost() (BoostStatus, error) {
	return a.engine.StopBoost()
}

// ResumeProcess immediately resumes a single suspended process by PID during an active boost session.
func (a *App) ResumeProcess(pid int) (BoostStatus, error) {
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
