package main

import (
	"path/filepath"
	"testing"
)

func TestBoostSessionSuspendsSelectedProcessesAndRestoresAllState(t *testing.T) {
	var suspended []int
	var resumed []int
	var activatedScheme string
	var stoppedServices []string
	var startedServices []string

	snapshotFile := filepath.Join(t.TempDir(), "session_snapshot.json")

	engine := &BoostEngine{
		SnapshotPath: snapshotFile,
		SuspendPID: func(pid int) error {
			suspended = append(suspended, pid)
			return nil
		},
		ResumePID: func(pid int) error {
			resumed = append(resumed, pid)
			return nil
		},
		GetActivePowerScheme: func() (string, error) {
			return "381b4222-f694-41f0-9685-ff5bb260df2e", nil // Balanced
		},
		SetActivePowerScheme: func(guid string) error {
			activatedScheme = guid
			return nil
		},
		GetServiceRunning: func(name string) (bool, error) {
			if name == "WSearch" {
				return true, nil
			}
			return false, nil
		},
		StopService: func(name string) error {
			stoppedServices = append(stoppedServices, name)
			return nil
		},
		StartService: func(name string) error {
			startedServices = append(startedServices, name)
			return nil
		},
	}

	req := BoostRequest{
		PauseApps:          true,
		SelectedPIDs:       []int{1234, 5678},
		ReduceWindowsTasks: true,
		ServiceNames:       []string{"WSearch", "SysMain"},
		HighPerformance:    true,
	}

	processes := []RunningProcess{
		{Name: "chrome.exe", PID: 1234, MemoryKB: 320000},
		{Name: "Discord.exe", PID: 5678, MemoryKB: 180000},
	}

	status, err := engine.StartBoost(req, processes)
	if err != nil {
		t.Fatalf("StartBoost returned error: %v", err)
	}
	if !status.Active {
		t.Fatalf("expected active boost status")
	}
	if len(status.SuspendedApps) != 2 {
		t.Fatalf("expected 2 suspended apps, got %#v", status.SuspendedApps)
	}
	if status.SavedMemoryKB != 500000 {
		t.Fatalf("expected 500000 KB saved memory, got %d", status.SavedMemoryKB)
	}
	if activatedScheme != HighPerformancePowerGUID {
		t.Fatalf("expected high performance power scheme %s, got %s", HighPerformancePowerGUID, activatedScheme)
	}
	if len(stoppedServices) != 1 || stoppedServices[0] != "WSearch" {
		t.Fatalf("expected only running service WSearch to be stopped, got %#v", stoppedServices)
	}

	// Resume a single app mid-session
	status, err = engine.ResumeSingleProcess(1234)
	if err != nil {
		t.Fatalf("ResumeSingleProcess returned error: %v", err)
	}
	if len(status.SuspendedApps) != 1 || status.SuspendedApps[0].PID != 5678 {
		t.Fatalf("expected only PID 5678 remaining suspended, got %#v", status.SuspendedApps)
	}

	// Stop boost & restore everything remaining
	finalStatus, err := engine.StopBoost()
	if err != nil {
		t.Fatalf("StopBoost returned error: %v", err)
	}
	if finalStatus.Active {
		t.Fatalf("expected inactive status after StopBoost")
	}
	if len(resumed) != 2 || resumed[0] != 1234 || resumed[1] != 5678 {
		t.Fatalf("expected both processes resumed in order, got %#v", resumed)
	}
	if activatedScheme != "381b4222-f694-41f0-9685-ff5bb260df2e" {
		t.Fatalf("expected original power scheme restored, got %s", activatedScheme)
	}
	if len(startedServices) != 1 || startedServices[0] != "WSearch" {
		t.Fatalf("expected WSearch restarted, got %#v", startedServices)
	}
}

func TestBoostEngineExtremeModesExecuteDeepHardwareOptimizationsAndRestore(t *testing.T) {
	var trimmedPIDs []int
	var standbyPurged bool
	var timerSet bool
	var timerRestored bool
	var mmcssBoosted bool
	var mmcssRestored bool
	var explorerKilled bool
	var explorerRestarted bool
	var transparencyDisabled bool
	var transparencyRestored bool
	var extreme2Applied bool
	var extreme2Restored bool
	var selfTrimmed bool

	snapshotFile := filepath.Join(t.TempDir(), "extreme_snapshot.json")

	engine := &BoostEngine{
		SnapshotPath: snapshotFile,
		SuspendPID:   func(pid int) error { return nil },
		ResumePID:    func(pid int) error { return nil },
		TrimPIDWorkingSet: func(pid int) error {
			trimmedPIDs = append(trimmedPIDs, pid)
			return nil
		},
		PurgeStandbyList: func() error {
			standbyPurged = true
			return nil
		},
		EnableHighResTimer: func() error {
			timerSet = true
			return nil
		},
		DisableHighResTimer: func() error {
			timerRestored = true
			return nil
		},
		EnableMMCSSGamePriority: func() error {
			mmcssBoosted = true
			return nil
		},
		RestoreMMCSSGamePriority: func() error {
			mmcssRestored = true
			return nil
		},
		KillExplorerShell: func() error {
			explorerKilled = true
			return nil
		},
		RestartExplorerShell: func() error {
			explorerRestarted = true
			return nil
		},
		DisableVisualTransparency: func() (uint32, error) {
			transparencyDisabled = true
			return 1, nil
		},
		RestoreVisualTransparency: func(orig uint32) error {
			if orig == 1 {
				transparencyRestored = true
			}
			return nil
		},
		ApplyExtremeModeII: func() ([]string, error) {
			extreme2Applied = true
			return []string{"Spooler", "WerSvc", "SysMain"}, nil
		},
		RestoreExtremeModeII: func(services []string) error {
			if len(services) == 3 {
				extreme2Restored = true
			}
			return nil
		},
		SelfTrimPulseMemory: func() error {
			selfTrimmed = true
			return nil
		},
	}

	req := BoostRequest{
		PauseApps:           true,
		SelectedPIDs:        []int{1234},
		TrimWorkingSet:      true,
		PurgeStandby:        true,
		TimerResolution05ms: true,
		MMCSSGpuPriority:    true,
		ExtremeModeI:        true,
		ExtremeModeII:       true,
	}

	running := []RunningProcess{
		{Name: "chrome.exe", PID: 1234, PIDs: []int{1234, 1235}, MemoryKB: 500000},
	}

	status, err := engine.StartBoost(req, running)
	if err != nil {
		t.Fatalf("StartBoost failed: %v", err)
	}
	if len(trimmedPIDs) != 2 {
		t.Fatalf("expected both suspended PIDs to have working sets trimmed, got %#v", trimmedPIDs)
	}
	if !standbyPurged || !status.StandbyPurged {
		t.Fatalf("expected standby list purge")
	}
	if !timerSet || !status.Timer05msActive {
		t.Fatalf("expected 0.5ms timer resolution enabled")
	}
	if !mmcssBoosted || !status.MMCSSActive {
		t.Fatalf("expected MMCSS GPU/CPU priority enabled")
	}
	if !explorerKilled || !transparencyDisabled || !status.ExtremeIActive {
		t.Fatalf("expected Extreme Mode I (Explorer + Transparency) applied")
	}
	if !extreme2Applied || !selfTrimmed || !status.ExtremeIIActive {
		t.Fatalf("expected Extreme Mode II applied and self memory trimmed")
	}

	finalStatus, err := engine.StopBoost()
	if err != nil {
		t.Fatalf("StopBoost failed: %v", err)
	}
	if finalStatus.Active {
		t.Fatalf("expected boost inactive after StopBoost")
	}
	if !timerRestored || !mmcssRestored || !explorerRestarted || !transparencyRestored || !extreme2Restored {
		t.Fatalf("expected all Extreme I & II states restored: timer=%v mmcss=%v explorer=%v transp=%v ext2=%v",
			timerRestored, mmcssRestored, explorerRestarted, transparencyRestored, extreme2Restored)
	}
}
