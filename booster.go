package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
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
	Version             int                      `json:"version"`
	ProcessIDs          map[int]string           `json:"processIDs,omitempty"`
	MMCSSSaved          []savedSetting           `json:"mmcssSaved,omitempty"`
	ExtremePower        *extremePowerState       `json:"extremePower,omitempty"`
	Foreground          *foregroundPriorityState `json:"foreground,omitempty"`
	StartedAt           string                   `json:"startedAt"`
	SuspendedApps       []RunningProcess         `json:"suspendedApps"`
	StoppedServices     []string                 `json:"stoppedServices"`
	OriginalPowerScheme string                   `json:"originalPowerScheme"`
	PowerPlanSwitched   bool                     `json:"powerPlanSwitched"`
	Timer05msActive     bool                     `json:"timer05msActive"`
	MMCSSActive         bool                     `json:"mmcssActive"`
	ExtremeIActive      bool                     `json:"extremeIActive"`
	OriginalTransp      uint32                   `json:"originalTransp"`
	ExtremeIIActive     bool                     `json:"extremeIIActive"`
	ExtremeIIServices   []string                 `json:"extremeIIServices"`
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
	MMCSSWritable             func() error
	MMCSSSettings             settingStore
	ExtremePowerSettings      powerSettings
	ForegroundSettings        prioritySettings
	ReadTransparency          func() (uint32, error)
	ProcessIdentity           func(int) (string, error)
	SuspendVerified           func(int, string) error
	ResumeVerified            func(int, string) error
	GuardianHealth            func() error
	ServiceRunningForRestore  func(string) (bool, error)
	snapshot                  sessionSnapshot
	recoveryPending           bool
	status                    BoostStatus
}

func NewBoostEngine() *BoostEngine {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.TempDir()
	}
	snapshotPath := filepath.Join(configDir, "OwlysPulse", "session_snapshot.json")

	engine := &BoostEngine{
		SnapshotPath:             snapshotPath,
		SuspendPID:               ntSuspendProcess,
		ResumePID:                ntResumeProcess,
		TrimPIDWorkingSet:        trimProcessWorkingSet,
		PurgeStandbyList:         purgeWindowsStandbyList,
		EnableHighResTimer:       enableTimerResolution05ms,
		DisableHighResTimer:      disableTimerResolution05ms,
		EnableMMCSSGamePriority:  enableMMCSSGamePriority,
		RestoreMMCSSGamePriority: restoreMMCSSGamePriority,
		// Explorer termination and transparency controls
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
		ProcessIdentity:           processCreationIdentity,
		ServiceRunningForRestore:  isWindowsServiceRunning,
		SuspendVerified:           ntSuspendVerified,
		ResumeVerified:            ntResumeVerified,
		MMCSSSettings:             registrySettings{},
		MMCSSWritable:             canWriteMMCSS,
		ExtremePowerSettings:      windowsPowerSettings{},
		ForegroundSettings:        windowsPrioritySettings{},
		ReadTransparency:          readWindowsTransparency,
		status: BoostStatus{
			SuspendedApps:     []RunningProcess{},
			StoppedServices:   []string{},
			ExtremeIIServices: []string{},
			Warnings:          []string{},
		},
	}
	engine.Watcher = NewSmartWakeWatcher(engine)
	return engine
}

func (e *BoostEngine) Status() BoostStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

func (e *BoostEngine) checkGuardian() error {
	if e.GuardianHealth != nil {
		return e.GuardianHealth()
	}
	return nil
}

func (e *BoostEngine) StartBoost(req BoostRequest, running []RunningProcess) (BoostStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.status.Active {
		return e.status, nil
	}

	if e.recoveryPending {
		return e.status, errors.New("unresolved recovery snapshot")
	}
	if e.SnapshotPath == "" {
		return e.status, errors.New("recovery snapshot path required")
	}
	if fi, err := os.Stat(e.SnapshotPath); err == nil {
		if fi.IsDir() {
			return e.status, fmt.Errorf("snapshot path is a directory: %s", e.SnapshotPath)
		}
		_ = e.recoverPreviousSession()
		_ = os.Remove(e.SnapshotPath)
	} else if !os.IsNotExist(err) {
		return e.status, err
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

	if err := e.checkGuardian(); err != nil {
		return e.status, fmt.Errorf("guardian unavailable: %w", err)
	}
	// Establish a durable recovery marker before any system mutation.
	e.status = next
	if err := e.saveSnapshotLocked(); err != nil {
		e.status = BoostStatus{}
		return e.status, fmt.Errorf("cannot create recovery snapshot: %w", err)
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
				// Write-ahead recovery intent: a crash during suspension must leave
				// the target discoverable by the independent guardian.
				intent := proc
				intent.PIDs = []int{targetPID}
				if err := e.checkGuardian(); err != nil {
					return e.status, fmt.Errorf("guardian unavailable: %w", err)
				}
				if e.ProcessIdentity != nil {
					identity, err := e.ProcessIdentity(targetPID)
					if err != nil {
						next.Warnings = append(next.Warnings, fmt.Sprintf("PID %d identity unavailable: %v", targetPID, err))
						continue
					}
					if e.snapshot.ProcessIDs == nil {
						e.snapshot.ProcessIDs = map[int]string{}
					}
					e.snapshot.ProcessIDs[targetPID] = identity
				}
				e.snapshot.SuspendedApps = append(e.snapshot.SuspendedApps, intent)
				if err := e.saveSnapshotLocked(); err != nil {
					return e.status, fmt.Errorf("cannot journal PID %d: %w", targetPID, err)
				}
				if err := e.checkGuardian(); err != nil {
					return e.status, fmt.Errorf("guardian unavailable: %w", err)
				}
				suspend := e.SuspendPID
				if e.SuspendVerified != nil && e.snapshot.ProcessIDs[targetPID] != "" {
					suspend = func(pid int) error { return e.SuspendVerified(pid, e.snapshot.ProcessIDs[pid]) }
				}
				if err := suspend(targetPID); err == nil {
					suspendedPIDs = append(suspendedPIDs, targetPID)
					if req.TrimWorkingSet && e.TrimPIDWorkingSet != nil {
						if err := e.TrimPIDWorkingSet(targetPID); err == nil {
							next.TrimmedProcessCount++
						}
					}
				} else {
					// A failed call can still have mutated the target before reporting failure.
					// Keep the write-ahead intent for recovery.
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

	if err := e.checkGuardian(); err != nil {
		return e.status, fmt.Errorf("guardian unavailable: %w", err)
	}
	if req.PurgeStandby && e.PurgeStandbyList != nil {
		if err := e.PurgeStandbyList(); err == nil {
			next.StandbyPurged = true
		} else {
			next.Warnings = append(next.Warnings, "Chạy với quyền Quản trị viên (Admin) để xả sạch bộ nhớ Standby List")
		}
	}

	if err := e.checkGuardian(); err != nil {
		return e.status, fmt.Errorf("guardian unavailable: %w", err)
	}
	if req.TimerResolution05ms && e.EnableHighResTimer != nil {
		// Timer requests are process-scoped; they cannot survive process death.
		if err := e.EnableHighResTimer(); err == nil {
			next.Timer05msActive = true
			e.status.Timer05msActive = true
		}
	}

	if err := e.checkGuardian(); err != nil {
		return e.status, fmt.Errorf("guardian unavailable: %w", err)
	}
	if req.MMCSSGpuPriority {
		if e.MMCSSWritable != nil {
			if err := e.MMCSSWritable(); err != nil {
				next.Warnings = append(next.Warnings, fmt.Sprintf("MMCSS skipped: %v", err))
				req.MMCSSGpuPriority = false
			}
		}
	}
	if req.MMCSSGpuPriority {
		if e.MMCSSSettings != nil {
			saved, err := snapshotSettings(e.MMCSSSettings, mmcssNames)
			if err != nil {
				next.Warnings = append(next.Warnings, fmt.Sprintf("MMCSS skipped: %v", err))
			} else {
				e.snapshot.MMCSSSaved = saved
				e.status.MMCSSActive = true
				if err = e.saveSnapshotLocked(); err != nil {
					return e.status, err
				}
				for _, name := range mmcssNames {
					if err := e.checkGuardian(); err != nil {
						return e.status, fmt.Errorf("guardian unavailable: %w", err)
					}
					if err = e.MMCSSSettings.write(name, mmcssBoostValue(name)); err != nil {
						return e.status, fmt.Errorf("MMCSS %s: %w", name, err)
					}
				}
				next.MMCSSActive = true
			}
		} else if e.EnableMMCSSGamePriority != nil {
			if err := e.EnableMMCSSGamePriority(); err == nil {
				next.MMCSSActive = true
				e.status.MMCSSActive = true
				e.snapshot.MMCSSActive = true
			}
		}
	}

	if err := e.checkGuardian(); err != nil {
		return e.status, fmt.Errorf("guardian unavailable: %w", err)
	}
	if req.HighPerformance && e.GetActivePowerScheme != nil && e.SetActivePowerScheme != nil {
		orig, err := e.GetActivePowerScheme()
		if err == nil && orig != "" && !strings.EqualFold(orig, HighPerformancePowerGUID) {
			e.status.OriginalPowerScheme = orig
			e.status.PowerPlanSwitched = true
			e.snapshot.OriginalPowerScheme = orig
			e.snapshot.PowerPlanSwitched = true
			if err = e.saveSnapshotLocked(); err != nil {
				return e.status, err
			}
			if err := e.checkGuardian(); err != nil {
				return e.status, fmt.Errorf("guardian unavailable: %w", err)
			}
			if err := e.SetActivePowerScheme(HighPerformancePowerGUID); err == nil {
				next.OriginalPowerScheme = orig
				next.PowerPlanSwitched = true
			} else {
				next.Warnings = append(next.Warnings, "Không thể chuyển sang chế độ điện năng Hiệu năng cao")
			}
		}
	}

	if err := e.checkGuardian(); err != nil {
		return e.status, fmt.Errorf("guardian unavailable: %w", err)
	}
	if req.ReduceWindowsTasks && e.GetServiceRunning != nil && e.StopService != nil {
		services := req.ServiceNames
		if len(services) == 0 {
			services = defaultServiceList
		}
		for _, svc := range services {
			if err := e.checkGuardian(); err != nil {
				return e.status, fmt.Errorf("guardian unavailable: %w", err)
			}
			runningSvc, err := e.GetServiceRunning(svc)
			if err != nil || !runningSvc {
				continue
			}
			e.snapshot.StoppedServices = append(e.snapshot.StoppedServices, svc)
			if err := e.saveSnapshotLocked(); err != nil {
				return e.status, err
			}
			if err := e.checkGuardian(); err != nil {
				return e.status, fmt.Errorf("guardian unavailable: %w", err)
			}
			if err := e.StopService(svc); err != nil {
				next.Warnings = append(next.Warnings, fmt.Sprintf("Cần quyền Quản trị viên để tạm dừng dịch vụ %s", svc))
				continue
			}
			next.StoppedServices = append(next.StoppedServices, svc)
		}
	}

	if err := e.checkGuardian(); err != nil {
		return e.status, fmt.Errorf("guardian unavailable: %w", err)
	}
	if (req.ExtremeModeI || req.ExtremeModeII) && e.KillExplorerShell != nil {
		origTransp := uint32(1)
		if e.ReadTransparency != nil {
			if orig, err := e.ReadTransparency(); err == nil {
				origTransp = orig
			}
		}
		if e.DisableVisualTransparency != nil {
			_, _ = e.DisableVisualTransparency()
		}
		if err := e.KillExplorerShell(); err == nil {
			next.ExtremeIActive = true
			next.OriginalTransp = origTransp
			e.status.ExtremeIActive = true
			e.status.OriginalTransp = origTransp
			e.snapshot.ExtremeIActive = true
			e.snapshot.OriginalTransp = origTransp
		} else {
			next.Warnings = append(next.Warnings, fmt.Sprintf("Không thể tắt Explorer: %v", err))
		}
	}

	if req.ExtremeModeII && e.ApplyExtremeModeII != nil {
		if stopped, err := e.ApplyExtremeModeII(); err == nil {
			next.ExtremeIIActive = true
			next.ExtremeIIServices = stopped
			e.status.ExtremeIIActive = true
			e.status.ExtremeIIServices = stopped
			e.snapshot.ExtremeIIActive = true
			e.snapshot.ExtremeIIServices = stopped
		} else {
			next.Warnings = append(next.Warnings, fmt.Sprintf("Extreme II: %v", err))
		}
		if e.SelfTrimPulseMemory != nil {
			_ = e.SelfTrimPulseMemory()
		}
	}

	if req.SmartWake && e.Watcher != nil {
		e.Watcher.SetSuspended(next.SuspendedApps)
		e.Watcher.Start()
		next.SmartWakeActive = true
		e.status.SmartWakeActive = true
	}

	// Recovery intents remain in the journal, not in the confirmed UI status.
	previous := e.status
	e.status = next
	if err := e.saveSnapshotLocked(); err != nil {
		e.status = previous
		return e.status, err
	}
	return e.status, nil
}

func (e *BoostEngine) restorePID(pid int) error {
	identity := e.snapshot.ProcessIDs[pid]
	if identity != "" {
		if e.ProcessIdentity == nil {
			return fmt.Errorf("PID %d identity unavailable", pid)
		}
		current, err := e.ProcessIdentity(pid)
		if err != nil {
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_NOT_FOUND) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
				return nil // The original process has exited.
			}
			return fmt.Errorf("PID %d identity unavailable: %w", pid, err)
		}
		if current != identity {
			return nil // The PID was reused; never resume the new process.
		}
	} else {
		return fmt.Errorf("PID %d identity unavailable", pid)
	}
	if e.ResumePID == nil && e.ResumeVerified == nil {
		return fmt.Errorf("PID %d resume unavailable", pid)
	}
	if e.ResumeVerified != nil {
		return e.ResumeVerified(pid, identity)
	}
	return e.ResumePID(pid)
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
				if err := e.restorePID(targetPID); err != nil {
					return e.status, fmt.Errorf("resume PID %d: %w", targetPID, err)
				}
			}
			continue
		}
		remaining = append(remaining, proc)
		savedKB += proc.MemoryKB
	}
	if remaining == nil {
		remaining = []RunningProcess{}
	}
	previous := e.status
	previousSnapshot := e.snapshot
	// Only remove successfully resumed targets after the durable journal update.
	for _, proc := range e.status.SuspendedApps {
		if proc.PID != pid {
			continue
		}
		var pending []RunningProcess
		for _, journaled := range e.snapshot.SuspendedApps {
			matched := false
			for _, target := range proc.PIDs {
				if len(journaled.PIDs) == 1 && journaled.PIDs[0] == target {
					matched = true
				}
			}
			if len(proc.PIDs) == 0 && journaled.PID == proc.PID {
				matched = true
			}
			if !matched {
				pending = append(pending, journaled)
			}
		}
		e.snapshot.SuspendedApps = pending
	}
	e.status.SuspendedApps = remaining
	e.status.SavedMemoryKB = savedKB
	if err := e.saveSnapshotLocked(); err != nil {
		e.status = previous
		e.snapshot = previousSnapshot
		return e.status, fmt.Errorf("journal resumed PID %d: %w", pid, err)
	}
	if e.Watcher != nil {
		e.Watcher.SetSuspended(remaining)
	}
	return e.status, nil
}

func (e *BoostEngine) StopBoost() (BoostStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.status.Active {
		if e.SnapshotPath != "" {
			if _, err := os.Stat(e.SnapshotPath); err == nil {
				return e.status, errors.New("unresolved recovery snapshot; explicit recovery required")
			} else if !os.IsNotExist(err) {
				return e.status, err
			}
		}
		return e.status, nil
	}

	if e.Watcher != nil {
		e.Watcher.Stop()
	}

	var restoreErrors []error
	if e.ResumePID != nil || e.ResumeVerified != nil {
		for _, proc := range e.snapshot.SuspendedApps {
			targets := proc.PIDs
			if len(targets) == 0 {
				targets = []int{proc.PID}
			}
			for _, targetPID := range targets {
				if err := e.restorePID(targetPID); err != nil {
					restoreErrors = append(restoreErrors, fmt.Errorf("resume PID %d: %w", targetPID, err))
				}
			}
		}
	}

	if e.status.Timer05msActive && e.DisableHighResTimer != nil {
		if err := e.DisableHighResTimer(); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("timer: %w", err))
		} else {
			e.status.Timer05msActive = false
		}
	}

	if e.snapshot.MMCSSActive {
		if e.MMCSSSettings != nil && e.snapshot.MMCSSSaved != nil {
			if err := restoreSettings(e.MMCSSSettings, e.snapshot.MMCSSSaved); err != nil {
				restoreErrors = append(restoreErrors, err)
			}
		} else if e.RestoreMMCSSGamePriority != nil {
			if err := e.RestoreMMCSSGamePriority(); err != nil {
				restoreErrors = append(restoreErrors, err)
			}
		} else {
			restoreErrors = append(restoreErrors, errors.New("MMCSS restore unavailable"))
		}
	}

	if e.snapshot.ExtremeIActive {
		if e.RestoreVisualTransparency != nil {
			if err := e.RestoreVisualTransparency(e.snapshot.OriginalTransp); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("transparency: %w", err))
			}
		}
		if e.RestartExplorerShell != nil {
			if err := e.RestartExplorerShell(); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("Explorer: %w", err))
			}
		}
	}

	if e.status.ExtremeIIActive && e.RestoreExtremeModeII != nil {
		if err := e.RestoreExtremeModeII(e.status.ExtremeIIServices); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("Extreme II: %w", err))
		}
	}

	if e.snapshot.PowerPlanSwitched && e.snapshot.OriginalPowerScheme != "" && e.SetActivePowerScheme != nil {
		if err := e.SetActivePowerScheme(e.snapshot.OriginalPowerScheme); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("power plan: %w", err))
		}
	}

	if e.StartService != nil {
		for _, svc := range e.snapshot.StoppedServices {
			if err := e.restoreService(svc); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("service %s: %w", svc, err))
			}
		}
	}

	if len(restoreErrors) != 0 {
		e.status.Warnings = []string{}
		for _, err := range restoreErrors {
			e.status.Warnings = append(e.status.Warnings, err.Error())
		}
		return e.status, errors.Join(restoreErrors...)
	}
	if err := os.Remove(e.SnapshotPath); err != nil && !os.IsNotExist(err) {
		e.status.Warnings = append(e.status.Warnings, fmt.Sprintf("recovery journal cleanup: %v", err))
		return e.status, err
	}
	e.snapshot = sessionSnapshot{}
	e.status = BoostStatus{
		SuspendedApps:     []RunningProcess{},
		StoppedServices:   []string{},
		ExtremeIIServices: []string{},
		Warnings:          []string{},
	}
	return e.status, nil
}

func (e *BoostEngine) restoreService(name string) error {
	if e.ServiceRunningForRestore != nil {
		running, err := e.ServiceRunningForRestore(name)
		if err != nil {
			return err
		}
		if running {
			return nil
		}
	}
	return e.StartService(name)
}

func (e *BoostEngine) saveSnapshotLocked() error {
	if e.SnapshotPath == "" {
		return errors.New("recovery snapshot path required")
	}
	snap := e.snapshot
	snap.Version = 2
	snap.StartedAt = e.status.StartedAt
	// The journal records possible mutations; status records confirmed ones.
	for _, proc := range e.status.SuspendedApps {
		found := false
		for _, journaled := range snap.SuspendedApps {
			if proc.PID == journaled.PID {
				found = true
				break
			}
		}
		if !found {
			snap.SuspendedApps = append(snap.SuspendedApps, proc)
		}
	}
	for _, service := range e.status.StoppedServices {
		found := false
		for _, journaled := range snap.StoppedServices {
			if service == journaled {
				found = true
				break
			}
		}
		if !found {
			snap.StoppedServices = append(snap.StoppedServices, service)
		}
	}
	if e.status.PowerPlanSwitched {
		snap.PowerPlanSwitched = true
		snap.OriginalPowerScheme = e.status.OriginalPowerScheme
	}
	snap.Timer05msActive = e.status.Timer05msActive
	if e.status.MMCSSActive {
		snap.MMCSSActive = true
	}
	if e.status.ExtremeIActive {
		snap.ExtremeIActive = true
		snap.OriginalTransp = e.status.OriginalTransp
	}
	snap.ExtremeIIActive = e.status.ExtremeIIActive
	snap.ExtremeIIServices = e.status.ExtremeIIServices
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(e.SnapshotPath), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(e.SnapshotPath), ".snapshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), e.SnapshotPath); err != nil {
		return err
	}
	e.snapshot = snap
	return nil
}

func (e *BoostEngine) RecoverPreviousSession() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.recoverPreviousSession()
}

func (e *BoostEngine) recoverPreviousSession() error {
	if e.SnapshotPath == "" {
		return nil
	}
	data, err := os.ReadFile(e.SnapshotPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var snap sessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}
	if snap.Version != 0 && snap.Version != 2 {
		return fmt.Errorf("unsupported snapshot version %d", snap.Version)
	}
	var restoreErrors []error
	if e.ResumePID != nil || e.ResumeVerified != nil {
		for _, proc := range snap.SuspendedApps {
			targets := proc.PIDs
			if len(targets) == 0 {
				targets = []int{proc.PID}
			}
			for _, pid := range targets {
				if snap.ProcessIDs[pid] == "" {
					restoreErrors = append(restoreErrors, fmt.Errorf("PID %d identity unavailable", pid))
					continue
				}
				previous := e.snapshot
				e.snapshot = snap
				err := e.restorePID(pid)
				e.snapshot = previous
				if err != nil {
					restoreErrors = append(restoreErrors, fmt.Errorf("resume PID %d: %w", pid, err))
				}
			}
		}
	}
	// Timer votes belong to the exited parent process; guardian must not release its own vote.
	if snap.MMCSSActive {
		if snap.Version == 2 {
			if e.MMCSSSettings == nil || len(snap.MMCSSSaved) == 0 {
				restoreErrors = append(restoreErrors, errors.New("MMCSS exact recovery unavailable"))
			} else if err := restoreSettings(e.MMCSSSettings, snap.MMCSSSaved); err != nil {
				restoreErrors = append(restoreErrors, err)
			}
		} else if e.RestoreMMCSSGamePriority != nil {
			if err := e.RestoreMMCSSGamePriority(); err != nil {
				restoreErrors = append(restoreErrors, err)
			}
		}
	}
	if snap.ExtremeIActive {
		if e.RestoreVisualTransparency != nil {
			if err := e.RestoreVisualTransparency(snap.OriginalTransp); err != nil {
				restoreErrors = append(restoreErrors, err)
			}
		}
		if e.RestartExplorerShell != nil {
			if err := e.RestartExplorerShell(); err != nil {
				restoreErrors = append(restoreErrors, err)
			}
		}
	}
	if snap.ExtremeIIActive && e.RestoreExtremeModeII != nil {
		if err := e.RestoreExtremeModeII(snap.ExtremeIIServices); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	if snap.PowerPlanSwitched && snap.OriginalPowerScheme != "" && e.SetActivePowerScheme != nil {
		if err := e.SetActivePowerScheme(snap.OriginalPowerScheme); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	if e.StartService != nil {
		for _, svc := range snap.StoppedServices {
			if err := e.restoreService(svc); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("service %s: %w", svc, err))
			}
		}
	}
	if len(restoreErrors) > 0 {
		return errors.Join(restoreErrors...)
	}
	return os.Remove(e.SnapshotPath)
}

func processCreationIdentity(pid int) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exit, &kernel, &user); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", created.HighDateTime, created.LowDateTime), nil
}
func readWindowsTransparency() (uint32, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return 0, err
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("EnableTransparency")
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
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
	return parseServiceRunning(string(out))
}

// parseServiceRunning accepts only stable SCM states; pending states need a retry.
func parseServiceRunning(output string) (bool, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.FieldsFunc(line, func(r rune) bool { return r == ':' || unicode.IsSpace(r) })
		if len(fields) >= 3 && strings.EqualFold(fields[0], "STATE") {
			switch fields[2] {
			case "RUNNING":
				return true, nil
			case "STOPPED":
				return false, nil
			default:
				return false, fmt.Errorf("service in transitional state %s", fields[2])
			}
		}
	}
	return false, errors.New("service state unavailable")
}

func stopWindowsService(name string) error {
	return hiddenWindowsCommand("sc", "stop", name).Run()
}

func startWindowsService(name string) error {
	return hiddenWindowsCommand("sc", "start", name).Run()
}
