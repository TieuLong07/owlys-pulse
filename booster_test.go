package main

import (
	"encoding/json"
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestPowerWriteAheadAndExplicitRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	const original = "00000000-0000-0000-0000-000000000000"
	e := &BoostEngine{SnapshotPath: path, GetActivePowerScheme: func() (string, error) { return original, nil }, SetActivePowerScheme: func(guid string) error {
		if guid == HighPerformancePowerGUID {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var snap sessionSnapshot
			if err := json.Unmarshal(data, &snap); err != nil {
				t.Fatal(err)
			}
			if !snap.PowerPlanSwitched || snap.OriginalPowerScheme != original {
				t.Fatalf("power not journaled: %+v", snap)
			}
		}
		return nil
	}}
	if _, err := e.StartBoost(BoostRequest{HighPerformance: true}, nil); err != nil {
		t.Fatal(err)
	}
	restored := ""
	r := &BoostEngine{SnapshotPath: path, SetActivePowerScheme: func(guid string) error { restored = guid; return nil }}
	if err := r.RecoverPreviousSession(); err != nil {
		t.Fatal(err)
	}
	if restored != original {
		t.Fatalf("recovered %q", restored)
	}
	if err := r.RecoverPreviousSession(); err != nil {
		t.Fatalf("empty recovery: %v", err)
	}
}

func TestUnwritableSnapshotPreventsMutation(t *testing.T) {
	called := false
	e := &BoostEngine{SnapshotPath: t.TempDir(), GetActivePowerScheme: func() (string, error) { return "original", nil }, SetActivePowerScheme: func(string) error { called = true; return nil }}
	if _, err := e.StartBoost(BoostRequest{HighPerformance: true}, nil); err == nil {
		t.Fatal("missing journal error")
	}
	if called {
		t.Fatal("mutated without journal")
	}
}

func TestPIDReuseRecoveryDoesNotResumeDifferentProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "original", nil }, SuspendPID: func(int) error { return nil }}
	if _, err := e.StartBoost(BoostRequest{PauseApps: true, SelectedPIDs: []int{1234}}, []RunningProcess{{Name: "chrome.exe", PID: 1234}}); err != nil {
		t.Fatal(err)
	}
	resumed := false
	r := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "reused", nil }, ResumePID: func(int) error { resumed = true; return nil }}
	if err := r.RecoverPreviousSession(); err != nil {
		t.Fatalf("reused PID should be resolved: %v", err)
	}
	if resumed {
		t.Fatal("resumed recycled PID")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("resolved snapshot retained: %v", err)
	}
}

func TestExitedPIDRecoveryResolvesWithoutResuming(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "", windows.ERROR_INVALID_PARAMETER }, ResumePID: func(int) error { t.Fatal("resumed exited PID"); return nil }, status: BoostStatus{SuspendedApps: []RunningProcess{{PID: 1234}}}, snapshot: sessionSnapshot{ProcessIDs: map[int]string{1234: "original"}}}
	if err := e.saveSnapshotLocked(); err != nil {
		t.Fatal(err)
	}
	if err := e.RecoverPreviousSession(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("resolved snapshot retained: %v", err)
	}
}

func TestAmbiguousPIDIdentityFailureRetainsRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "", windows.ERROR_ACCESS_DENIED }, ResumePID: func(int) error { t.Fatal("resumed unverified PID"); return nil }, status: BoostStatus{SuspendedApps: []RunningProcess{{PID: 1234}}}, snapshot: sessionSnapshot{ProcessIDs: map[int]string{1234: "original"}}}
	if err := e.saveSnapshotLocked(); err != nil {
		t.Fatal(err)
	}
	if err := e.RecoverPreviousSession(); err == nil {
		t.Fatal("ambiguous access denied lost")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot lost: %v", err)
	}
}

func TestStopBoostDoesNotResumeReusedPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "reused", nil }, ResumePID: func(int) error { t.Fatal("resumed reused PID"); return nil }, status: BoostStatus{Active: true, SuspendedApps: []RunningProcess{{PID: 1234}}}, snapshot: sessionSnapshot{ProcessIDs: map[int]string{1234: "original"}}}
	if err := e.saveSnapshotLocked(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StopBoost(); err != nil {
		t.Fatal(err)
	}
}

func TestResumeSinglePreservesFailedPIDAndJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	errDenied := errors.New("resume denied")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "original", nil }, ResumePID: func(int) error { return errDenied }, status: BoostStatus{Active: true, SuspendedApps: []RunningProcess{{PID: 1234, PIDs: []int{1234}, MemoryKB: 10}}, SavedMemoryKB: 10}, snapshot: sessionSnapshot{ProcessIDs: map[int]string{1234: "original"}}}
	if err := e.saveSnapshotLocked(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ResumeSingleProcess(1234); !errors.Is(err, errDenied) {
		t.Fatalf("resume failure discarded: %v", err)
	}
	if len(e.Status().SuspendedApps) != 1 {
		t.Fatal("failed PID discarded")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snap sessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.SuspendedApps) != 1 {
		t.Fatal("failed PID lost from journal")
	}
}

func TestStopBoostRetainsUnknownVersionTwoIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "anything", nil }, ResumePID: func(int) error { t.Fatal("resumed PID without journal identity"); return nil }, status: BoostStatus{Active: true, SuspendedApps: []RunningProcess{{PID: 1234}}}, snapshot: sessionSnapshot{Version: 2, SuspendedApps: []RunningProcess{{PID: 1234}}}}
	if _, err := e.StopBoost(); err == nil {
		t.Fatal("missing identity was treated as restored")
	}
}

func TestInactiveStopDoesNotDeleteUnloadedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"suspendedApps":[{"pid":1234}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &BoostEngine{SnapshotPath: path, ResumePID: func(int) error { t.Fatal("resumed without loading snapshot"); return nil }}
	if _, err := e.StopBoost(); err == nil {
		t.Fatal("unresolved snapshot silently deleted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("journal lost: %v", err)
	}
}

func TestLegacySnapshotNeverResumesUnverifiedPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"suspendedApps":[{"pid":1234}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "reused", nil }, ResumePID: func(int) error { t.Fatal("resumed reused legacy PID"); return nil }}
	if err := e.RecoverPreviousSession(); err == nil {
		t.Fatal("legacy PID without identity treated as resolved")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("journal lost: %v", err)
	}
}

func TestFailedMutationIsOnlyJournaledNotConfirmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "original", nil }, SuspendPID: func(int) error { return errors.New("uncertain") }, GetServiceRunning: func(string) (bool, error) { return true, nil }, StopService: func(string) error { return errors.New("uncertain") }, GetActivePowerScheme: func() (string, error) { return "original", nil }, SetActivePowerScheme: func(string) error { return errors.New("uncertain") }}
	status, err := e.StartBoost(BoostRequest{PauseApps: true, SelectedPIDs: []int{1234}, ReduceWindowsTasks: true, ServiceNames: []string{"WSearch"}, HighPerformance: true}, []RunningProcess{{Name: "chrome.exe", PID: 1234}})
	if err != nil {
		t.Fatal(err)
	}
	if len(status.SuspendedApps) != 0 || len(status.StoppedServices) != 0 || status.PowerPlanSwitched || status.SavedMemoryKB != 0 {
		t.Fatalf("intent counted as confirmed: %+v", status)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snap sessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.SuspendedApps) != 1 || len(snap.StoppedServices) != 1 || !snap.PowerPlanSwitched {
		t.Fatalf("intents not retained: %+v", snap)
	}
}

func TestResumeSingleReportsJournalFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "original", nil }, ResumePID: func(int) error { return nil }, status: BoostStatus{Active: true, SuspendedApps: []RunningProcess{{PID: 1234}}}, snapshot: sessionSnapshot{ProcessIDs: map[int]string{1234: "original"}}}
	if err := e.saveSnapshotLocked(); err != nil {
		t.Fatal(err)
	}
	e.SnapshotPath = t.TempDir() // Rename onto a directory fails after resume.
	if _, err := e.ResumeSingleProcess(1234); err == nil {
		t.Fatal("journal failure swallowed")
	}
	if len(e.Status().SuspendedApps) != 1 {
		t.Fatal("retry state discarded despite journal failure")
	}
}

func TestFinalSaveKeepsFailedSuspensionIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "original", nil }, SuspendPID: func(int) error { return errors.New("unknown mutation outcome") }}
	if _, err := e.StartBoost(BoostRequest{PauseApps: true, SelectedPIDs: []int{1234}}, []RunningProcess{{PID: 1234}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snap sessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.SuspendedApps) != 1 || snap.ProcessIDs[1234] != "original" {
		t.Fatalf("write-ahead intent overwritten: %+v", snap)
	}
}

func TestFinalSaveKeepsFailedServiceIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, GetServiceRunning: func(string) (bool, error) { return true, nil }, StopService: func(string) error { return errors.New("unknown mutation outcome") }}
	if _, err := e.StartBoost(BoostRequest{ReduceWindowsTasks: true, ServiceNames: []string{"WSearch"}}, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snap sessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.StoppedServices) != 1 || snap.StoppedServices[0] != "WSearch" {
		t.Fatalf("write-ahead service intent overwritten: %+v", snap)
	}
}

func TestRestoreFailureKeepsSnapshotForRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	errResume := errors.New("access denied")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "original", nil }, ResumePID: func(int) error { return errResume }, status: BoostStatus{Active: true, SuspendedApps: []RunningProcess{{Name: "chrome.exe", PID: 1234, PIDs: []int{1234}}}}, snapshot: sessionSnapshot{ProcessIDs: map[int]string{1234: "original"}}}
	e.saveSnapshotLocked()
	if _, err := e.StopBoost(); !errors.Is(err, errResume) {
		t.Fatalf("expected restore failure, got %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot lost on failure: %v", err)
	}
	if !e.Status().Active {
		t.Fatal("UI must not claim fully restored")
	}
	e.ResumePID = func(int) error { return nil }
	if _, err := e.StopBoost(); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("snapshot not cleared after success: %v", err)
	}
}

func TestBoostDoesNotChangeAlreadyActiveHighPerformanceScheme(t *testing.T) {
	calls := 0
	e := &BoostEngine{SnapshotPath: filepath.Join(t.TempDir(), "state.json"), GetActivePowerScheme: func() (string, error) { return HighPerformancePowerGUID, nil }, SetActivePowerScheme: func(string) error { calls++; return nil }}
	if _, err := e.StartBoost(BoostRequest{HighPerformance: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StopBoost(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("already high scheme changed %d times", calls)
	}
}

func TestSnapshotExistsBeforeSuspendingFirstProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, SuspendPID: func(int) error {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("snapshot missing before mutation: %v", err)
		}
		return nil
	}}
	_, err := e.StartBoost(BoostRequest{PauseApps: true, SelectedPIDs: []int{1234}}, []RunningProcess{{Name: "chrome.exe", PID: 1234}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotContainsPendingProcessBeforeSuspension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, SuspendPID: func(int) error {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var snap sessionSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			t.Fatal(err)
		}
		if len(snap.SuspendedApps) == 0 || snap.SuspendedApps[0].PID != 1234 {
			t.Fatalf("PID not journaled ahead of mutation: %+v", snap)
		}
		return nil
	}}
	_, err := e.StartBoost(BoostRequest{PauseApps: true, SelectedPIDs: []int{1234}}, []RunningProcess{{Name: "chrome.exe", PID: 1234}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryFailurePreservesSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, ProcessIdentity: func(int) (string, error) { return "original", nil }, ResumePID: func(int) error { return errors.New("access denied") }, status: BoostStatus{SuspendedApps: []RunningProcess{{Name: "chrome.exe", PID: 1234}}}, snapshot: sessionSnapshot{ProcessIDs: map[int]string{1234: "original"}}}
	e.saveSnapshotLocked()
	if err := e.recoverPreviousSession(); err == nil {
		t.Fatal("failure swallowed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot lost: %v", err)
	}
}

func TestBoostSessionSuspendsSelectedProcessesAndRestoresAllState(t *testing.T) {
	var suspended []int
	var resumed []int
	var activatedScheme string
	var stoppedServices []string
	var startedServices []string

	snapshotFile := filepath.Join(t.TempDir(), "session_snapshot.json")

	engine := &BoostEngine{
		SnapshotPath:    snapshotFile,
		ProcessIdentity: func(int) (string, error) { return "original", nil },
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
		SnapshotPath:    snapshotFile,
		ProcessIdentity: func(int) (string, error) { return "original", nil },
		SuspendPID:      func(pid int) error { return nil },
		ResumePID:       func(pid int) error { return nil },
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
	if !mmcssBoosted || !status.MMCSSActive || !explorerKilled || !transparencyDisabled || !status.ExtremeIActive || !extreme2Applied || !selfTrimmed || !status.ExtremeIIActive {
		t.Fatal("expected all extreme mode optimizations to activate")
	}

	finalStatus, err := engine.StopBoost()
	if err != nil {
		t.Fatalf("StopBoost failed: %v", err)
	}
	if finalStatus.Active {
		t.Fatalf("expected boost inactive after StopBoost")
	}
	if !timerRestored || !mmcssRestored || !explorerRestarted || !transparencyRestored || !extreme2Restored {
		t.Fatalf("expected all options to restore: timer=%v mmcss=%v explorer=%v transp=%v ext2=%v",
			timerRestored, mmcssRestored, explorerRestarted, transparencyRestored, extreme2Restored)
	}
}

func TestReleaseSkipsRunningServiceOnRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	starts := 0
	e := &BoostEngine{SnapshotPath: path, status: BoostStatus{Active: true}, snapshot: sessionSnapshot{StoppedServices: []string{"WSearch"}}, ServiceRunningForRestore: func(string) (bool, error) { return true, nil }, StartService: func(string) error { starts++; return nil }}
	if _, err := e.StopBoost(); err != nil {
		t.Fatal(err)
	}
	if starts != 0 {
		t.Fatalf("restarted already running service %d times", starts)
	}
}
func TestRecoverySkipsRunningService(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, snapshot: sessionSnapshot{StoppedServices: []string{"WSearch"}}, ServiceRunningForRestore: func(string) (bool, error) { return true, nil }, StartService: func(string) error { t.Fatal("restarted running service"); return nil }}
	if err := e.saveSnapshotLocked(); err != nil {
		t.Fatal(err)
	}
	if err := e.RecoverPreviousSession(); err != nil {
		t.Fatal(err)
	}
}
func TestGuardianFailureDuringStartPreventsLaterMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	calls := 0
	e := &BoostEngine{SnapshotPath: path, GuardianHealth: func() error {
		calls++
		if calls > 2 {
			return errors.New("guardian lost")
		}
		return nil
	}, GetActivePowerScheme: func() (string, error) { return "original", nil }, SetActivePowerScheme: func(string) error { t.Fatal("mutated after guardian loss"); return nil }}
	_, err := e.StartBoost(BoostRequest{HighPerformance: true}, nil)
	if err == nil {
		t.Fatal("guardian loss not reported")
	}
}
func TestProductionExtremeIEnabled(t *testing.T) {
	e := NewBoostEngine()
	e.SnapshotPath = filepath.Join(t.TempDir(), "state.json")
	if e.KillExplorerShell == nil {
		t.Fatal("Explorer shell capability should be present")
	}
}

func TestVerifiedProcessRefusesMismatchedCreationBeforeMutation(t *testing.T) {
	called := false
	err := mutateVerifiedProcess(os.Getpid(), "wrong", func(windows.Handle) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("mismatched process mutated: called=%v err=%v", called, err)
	}
}
func TestGuardianRecoveryNeverReleasesParentTimerVote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e := &BoostEngine{SnapshotPath: path, status: BoostStatus{Timer05msActive: true}, DisableHighResTimer: func() error { t.Fatal("guardian released its own timer vote"); return nil }}
	if err := e.saveSnapshotLocked(); err != nil {
		t.Fatal(err)
	}
	if err := e.RecoverPreviousSession(); err != nil {
		t.Fatal(err)
	}
}

func TestServiceStatePendingIsNotMistakenForStopped(t *testing.T) {
	for _, state := range []string{"START_PENDING", "STOP_PENDING"} {
		running, err := parseServiceRunning("STATE : 2 " + state)
		if err == nil || running {
			t.Fatalf("%s must fail closed: running=%v err=%v", state, running, err)
		}
	}
}
