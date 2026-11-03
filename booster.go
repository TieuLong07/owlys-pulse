package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	HighPerformancePowerGUID = "8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c"
	processSuspendResume     = 0x0800
)

var (
	ntdll              = syscall.NewLazyDLL("ntdll.dll")
	procNtSuspend      = ntdll.NewProc("NtSuspendProcess")
	procNtResume       = ntdll.NewProc("NtResumeProcess")
	powerSchemeRegex   = regexp.MustCompile(`(?i)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)
	defaultServiceList = []string{"WSearch", "SysMain", "DiagTrack"}
)

// BoostRequest defines what the user chose to optimize for the current session.
type BoostRequest struct {
	PauseApps           bool     `json:"pauseApps"`
	SelectedPIDs        []int    `json:"selectedPIDs"`
	SmartWake           bool     `json:"smartWake"`
	TrimWorkingSet      bool     `json:"trimWorkingSet"`
	PurgeStandby        bool     `json:"purgeStandby"`
	TimerResolution05ms bool     `json:"timerResolution05ms"`
	MMCSSGpuPriority    bool     `json:"mmcssGpuPriority"`
	ReduceWindowsTasks  bool     `json:"reduceWindowsTasks"`
	ServiceNames        []string `json:"serviceNames"`
	HighPerformance     bool     `json:"highPerformance"`
	ExtremeModeI        bool     `json:"extremeModeI"`
	ExtremeModeII       bool     `json:"extremeModeII"`
}

// BoostStatus represents the live state of Owlys Pulse.
type BoostStatus struct {
	Active              bool             `json:"active"`
	StartedAt           string           `json:"startedAt"`
	SuspendedApps       []RunningProcess `json:"suspendedApps"`
	SavedMemoryKB       int64            `json:"savedMemoryKB"`
	TrimmedProcessCount int              `json:"trimmedProcessCount"`
	SmartWakeActive     bool             `json:"smartWakeActive"`
	StandbyPurged       bool             `json:"standbyPurged"`
	Timer05msActive     bool             `json:"timer05msActive"`
	MMCSSActive         bool             `json:"mmcssActive"`
	StoppedServices     []string         `json:"stoppedServices"`
	PowerPlanSwitched   bool             `json:"powerPlanSwitched"`
	OriginalPowerScheme string           `json:"originalPowerScheme"`
	ExtremeIActive      bool             `json:"extremeIActive"`
	OriginalTransp      uint32           `json:"originalTransp"`
	ExtremeIIActive     bool             `json:"extremeIIActive"`
	ExtremeIIServices   []string         `json:"extremeIIServices"`
	Warnings            []string         `json:"warnings"`
}

type sessionSnapshot struct {
	StartedAt           string           `json:"startedAt"`
	SuspendedApps       []RunningProcess `json:"suspendedApps"`
	StoppedServices     []string         `json:"stoppedServices"`
	OriginalPowerScheme string           `json:"originalPowerScheme"`
	PowerPlanSwitched   bool             `json:"powerPlanSwitched"`
	Timer05msActive     bool             `json:"timer05msActive"`
	MMCSSActive         bool             `json:"mmcssActive"`
	ExtremeIActive      bool             `json:"extremeIActive"`
	OriginalTransp      uint32           `json:"originalTransp"`
	ExtremeIIActive     bool             `json:"extremeIIActive"`
	ExtremeIIServices   []string         `json:"extremeIIServices"`
}

// BoostEngine coordinates reversible process suspension, power scheme switching, and service pausing.
type BoostEngine struct {
	mu                        sync.Mutex
	SnapshotPath              string
	Watcher                   *SmartWakeWatcher
	SuspendPID                func(pid int) error
	ResumePID                 func(pid int) error
	TrimPIDWorkingSet         func(pid int) error
	PurgeStandbyList          func() error
	EnableHighResTimer        func() error
	DisableHighResTimer       func() error
	EnableMMCSSGamePriority   func() error
	RestoreMMCSSGamePriority  func() error
	KillExplorerShell         func() error
	RestartExplorerShell      func() error
	DisableVisualTransparency func() (uint32, error)
	RestoreVisualTransparency func(orig uint32) error
	ApplyExtremeModeII        func() ([]string, error)
	RestoreExtremeModeII      func(services []string) error
	SelfTrimPulseMemory       func() error
	GetActivePowerScheme      func() (string, error)
	SetActivePowerScheme      func(guid string) error
	GetServiceRunning         func(name string) (bool, error)
	StopService               func(name string) error
	StartService              func(name string) error
	status                    BoostStatus
}

func NewBoostEngine() *BoostEngine {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.TempDir()
	}
	snapshotPath := filepath.Join(configDir, "OwlysPulse", "session_snapshot.json")

	engine := &BoostEngine{
		SnapshotPath:              snapshotPath,
		SuspendPID:                ntSuspendProcess,
		ResumePID:                 ntResumeProcess,
		TrimPIDWorkingSet:         trimProcessWorkingSet,
		PurgeStandbyList:          purgeWindowsStandbyList,
		EnableHighResTimer:        enableTimerResolution05ms,
		DisableHighResTimer:       disableTimerResolution05ms,
		EnableMMCSSGamePriority:   enableMMCSSGamePriority,
		RestoreMMCSSGamePriority:  restoreMMCSSGamePriority,
		KillExplorerShell:         killExplorerShell,
		RestartExplorerShell:      restartExplorerShell,
		DisableVisualTransparency: disableWindowsTransparency,
		RestoreVisualTransparency: restoreWindowsTransparency,
		ApplyExtremeModeII:        applyExtremeModeII,
		RestoreExtremeModeII:      restoreExtremeModeII,
		SelfTrimPulseMemory:       selfTrimPulseMemory,
		GetActivePowerScheme:      getWindowsPowerScheme,
		SetActivePowerScheme:      setWindowsPowerScheme,
		GetServiceRunning:         isWindowsServiceRunning,
		StopService:               stopWindowsService,
		StartService:              startWindowsService,
		status: BoostStatus{
			SuspendedApps:     []RunningProcess{},
			StoppedServices:   []string{},
			ExtremeIIServices: []string{},
			Warnings:          []string{},
		},
	}
	engine.Watcher = NewSmartWakeWatcher(engine)
	engine.recoverPreviousSession()
	return engine
}

func (e *BoostEngine) Status() BoostStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

func (e *BoostEngine) StartBoost(req BoostRequest, running []RunningProcess) (BoostStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.status.Active {
		return e.status, nil
	}

	selfPID := os.Getpid()
	byPID := make(map[int]RunningProcess, len(running))
	for _, proc := range running {
		byPID[proc.PID] = proc
	}

	next := BoostStatus{
		Active:            true,
		StartedAt:         time.Now().Format(time.RFC3339),
		SuspendedApps:     []RunningProcess{},
		StoppedServices:   []string{},
		ExtremeIIServices: []string{},
		Warnings:          []string{},
	}

	if req.PauseApps && e.SuspendPID != nil {
		for _, pid := range req.SelectedPIDs {
			if pid <= 4 || pid == selfPID {
				continue
			}
			proc, ok := byPID[pid]
			if !ok || !eligibleProcess(proc.Name, fmt.Sprintf("%d", proc.PID)) {
				continue
			}
			targets := proc.PIDs
			if len(targets) == 0 {
				targets = []int{proc.PID}
			}
			var suspendedPIDs []int
			for _, targetPID := range targets {
				if targetPID <= 4 || targetPID == selfPID {
					continue
				}
				if err := e.SuspendPID(targetPID); err == nil {
					suspendedPIDs = append(suspendedPIDs, targetPID)
					if req.TrimWorkingSet && e.TrimPIDWorkingSet != nil {
						if err := e.TrimPIDWorkingSet(targetPID); err == nil {
							next.TrimmedProcessCount++
						}
					}
				}
			}
			if len(suspendedPIDs) == 0 {
				next.Warnings = append(next.Warnings, fmt.Sprintf("Không thể tạm dừng %s (PID %d)", proc.Name, pid))
				continue
			}
			proc.PIDs = suspendedPIDs
			proc.InstanceCount = len(suspendedPIDs)
			next.SuspendedApps = append(next.SuspendedApps, proc)
			next.SavedMemoryKB += proc.MemoryKB
		}
	}

	if req.PurgeStandby && e.PurgeStandbyList != nil {
		if err := e.PurgeStandbyList(); err == nil {
			next.StandbyPurged = true
		} else {
			next.Warnings = append(next.Warnings, "Chạy với quyền Quản trị viên (Admin) để xả sạch bộ nhớ Standby List")
		}
	}

	if req.TimerResolution05ms && e.EnableHighResTimer != nil {
		if err := e.EnableHighResTimer(); err == nil {
			next.Timer05msActive = true
		}
	}

	if req.MMCSSGpuPriority && e.EnableMMCSSGamePriority != nil {
		if err := e.EnableMMCSSGamePriority(); err == nil {
			next.MMCSSActive = true
		}
	}

	if req.HighPerformance && e.GetActivePowerScheme != nil && e.SetActivePowerScheme != nil {
		orig, err := e.GetActivePowerScheme()
		if err == nil && orig != "" && !strings.EqualFold(orig, HighPerformancePowerGUID) {
			if err := e.SetActivePowerScheme(HighPerformancePowerGUID); err == nil {
				next.OriginalPowerScheme = orig
				next.PowerPlanSwitched = true
			} else {
				next.Warnings = append(next.Warnings, "Không thể chuyển sang chế độ điện năng Hiệu năng cao")
			}
		}
	}

	if req.ReduceWindowsTasks && e.GetServiceRunning != nil && e.StopService != nil {
		services := req.ServiceNames
		if len(services) == 0 {
			services = defaultServiceList
		}
		for _, svc := range services {
			runningSvc, err := e.GetServiceRunning(svc)
			if err != nil || !runningSvc {
				continue
			}
			if err := e.StopService(svc); err != nil {
				next.Warnings = append(next.Warnings, fmt.Sprintf("Cần quyền Quản trị viên để tạm dừng dịch vụ %s", svc))
				continue
			}
			next.StoppedServices = append(next.StoppedServices, svc)
		}
	}

	if (req.ExtremeModeI || req.ExtremeModeII) && e.KillExplorerShell != nil {
		origTransp := uint32(1)
		if e.DisableVisualTransparency != nil {
			if val, err := e.DisableVisualTransparency(); err == nil {
				origTransp = val
			}
		}
		if err := e.KillExplorerShell(); err == nil {
			next.ExtremeIActive = true
			next.OriginalTransp = origTransp
		}
	}

	if req.ExtremeModeII && e.ApplyExtremeModeII != nil {
		if stopped, err := e.ApplyExtremeModeII(); err == nil {
			next.ExtremeIIActive = true
			next.ExtremeIIServices = stopped
		}
		if e.SelfTrimPulseMemory != nil {
			_ = e.SelfTrimPulseMemory()
		}
	}

	if req.SmartWake && e.Watcher != nil {
		e.Watcher.SetSuspended(next.SuspendedApps)
		e.Watcher.Start()
		next.SmartWakeActive = true
	}

	e.status = next
	e.saveSnapshotLocked()
	return e.status, nil
}

func (e *BoostEngine) ResumeSingleProcess(pid int) (BoostStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var remaining []RunningProcess
	var savedKB int64
	for _, proc := range e.status.SuspendedApps {
		if proc.PID == pid {
			targets := proc.PIDs
			if len(targets) == 0 {
				targets = []int{proc.PID}
			}
			for _, targetPID := range targets {
				_ = e.ResumePID(targetPID)
			}
			continue
		}
		remaining = append(remaining, proc)
		savedKB += proc.MemoryKB
	}
	if remaining == nil {
		remaining = []RunningProcess{}
	}
	e.status.SuspendedApps = remaining
	e.status.SavedMemoryKB = savedKB
	if e.Watcher != nil {
		e.Watcher.SetSuspended(remaining)
	}
	e.saveSnapshotLocked()
	return e.status, nil
}

func (e *BoostEngine) StopBoost() (BoostStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.Watcher != nil {
		e.Watcher.Stop()
	}

	if e.ResumePID != nil {
		for _, proc := range e.status.SuspendedApps {
			targets := proc.PIDs
			if len(targets) == 0 {
				targets = []int{proc.PID}
			}
			for _, targetPID := range targets {
				_ = e.ResumePID(targetPID)
			}
		}
	}

	if e.status.Timer05msActive && e.DisableHighResTimer != nil {
		_ = e.DisableHighResTimer()
	}

	if e.status.MMCSSActive && e.RestoreMMCSSGamePriority != nil {
		_ = e.RestoreMMCSSGamePriority()
	}

	if e.status.ExtremeIActive {
		if e.RestoreVisualTransparency != nil {
			_ = e.RestoreVisualTransparency(e.status.OriginalTransp)
		}
		if e.RestartExplorerShell != nil {
			_ = e.RestartExplorerShell()
		}
	}

	if e.status.ExtremeIIActive && e.RestoreExtremeModeII != nil {
		_ = e.RestoreExtremeModeII(e.status.ExtremeIIServices)
	}

	if e.status.PowerPlanSwitched && e.status.OriginalPowerScheme != "" && e.SetActivePowerScheme != nil {
		_ = e.SetActivePowerScheme(e.status.OriginalPowerScheme)
	}

	if e.StartService != nil {
		for _, svc := range e.status.StoppedServices {
			_ = e.StartService(svc)
		}
	}

	e.status = BoostStatus{
		Active:            false,
		SuspendedApps:     []RunningProcess{},
		StoppedServices:   []string{},
		ExtremeIIServices: []string{},
		Warnings:          []string{},
	}
	_ = os.Remove(e.SnapshotPath)
	return e.status, nil
}

func (e *BoostEngine) saveSnapshotLocked() {
	if e.SnapshotPath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(e.SnapshotPath), 0o755)
	snap := sessionSnapshot{
		StartedAt:           e.status.StartedAt,
		SuspendedApps:       e.status.SuspendedApps,
		StoppedServices:     e.status.StoppedServices,
		OriginalPowerScheme: e.status.OriginalPowerScheme,
		PowerPlanSwitched:   e.status.PowerPlanSwitched,
		Timer05msActive:     e.status.Timer05msActive,
		MMCSSActive:         e.status.MMCSSActive,
		ExtremeIActive:      e.status.ExtremeIActive,
		OriginalTransp:      e.status.OriginalTransp,
		ExtremeIIActive:     e.status.ExtremeIIActive,
		ExtremeIIServices:   e.status.ExtremeIIServices,
	}
	if data, err := json.MarshalIndent(snap, "", "  "); err == nil {
		_ = os.WriteFile(e.SnapshotPath, data, 0o644)
	}
}

func (e *BoostEngine) recoverPreviousSession() {
	if e.SnapshotPath == "" {
		return
	}
	data, err := os.ReadFile(e.SnapshotPath)
	if err != nil {
		return
	}
	var snap sessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	if e.ResumePID != nil {
		for _, proc := range snap.SuspendedApps {
			targets := proc.PIDs
			if len(targets) == 0 {
				targets = []int{proc.PID}
			}
			for _, targetPID := range targets {
				_ = e.ResumePID(targetPID)
			}
		}
	}
	if snap.Timer05msActive && e.DisableHighResTimer != nil {
		_ = e.DisableHighResTimer()
	}
	if snap.MMCSSActive && e.RestoreMMCSSGamePriority != nil {
		_ = e.RestoreMMCSSGamePriority()
	}
	if snap.ExtremeIActive {
		if e.RestoreVisualTransparency != nil {
			_ = e.RestoreVisualTransparency(snap.OriginalTransp)
		}
		if e.RestartExplorerShell != nil {
			_ = e.RestartExplorerShell()
		}
	}
	if snap.ExtremeIIActive && e.RestoreExtremeModeII != nil {
		_ = e.RestoreExtremeModeII(snap.ExtremeIIServices)
	}
	if snap.PowerPlanSwitched && snap.OriginalPowerScheme != "" && e.SetActivePowerScheme != nil {
		_ = e.SetActivePowerScheme(snap.OriginalPowerScheme)
	}
	if e.StartService != nil {
		for _, svc := range snap.StoppedServices {
			_ = e.StartService(svc)
		}
	}
	_ = os.Remove(e.SnapshotPath)
}

func ntSuspendProcess(pid int) error {
	handle, err := syscall.OpenProcess(processSuspendResume, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)
	r1, _, callErr := procNtSuspend.Call(uintptr(handle))
	if r1 != 0 {
		return fmt.Errorf("NtSuspendProcess status 0x%x (%v)", r1, callErr)
	}
	return nil
}

func ntResumeProcess(pid int) error {
	handle, err := syscall.OpenProcess(processSuspendResume, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)
	r1, _, callErr := procNtResume.Call(uintptr(handle))
	if r1 != 0 {
		return fmt.Errorf("NtResumeProcess status 0x%x (%v)", r1, callErr)
	}
	return nil
}

func getWindowsPowerScheme() (string, error) {
	out, err := hiddenWindowsCommand("powercfg", "/getactivescheme").Output()
	if err != nil {
		return "", err
	}
	match := powerSchemeRegex.FindString(string(out))
	if match == "" {
		return "", fmt.Errorf("power scheme GUID not found")
	}
	return strings.ToLower(match), nil
}

func setWindowsPowerScheme(guid string) error {
	return hiddenWindowsCommand("powercfg", "/setactive", guid).Run()
}

func isWindowsServiceRunning(name string) (bool, error) {
	out, err := hiddenWindowsCommand("sc", "query", name).Output()
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "RUNNING"), nil
}

func stopWindowsService(name string) error {
	return hiddenWindowsCommand("sc", "stop", name).Run()
}

func startWindowsService(name string) error {
	return hiddenWindowsCommand("sc", "start", name).Run()
}
