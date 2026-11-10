package main

import (
	"testing"
	"time"
)

func TestSmartWakeRefreezeDoesNotAcquireEngineLock(t *testing.T) {
	e := &BoostEngine{SuspendPID: func(int) error { return nil }}
	w := NewSmartWakeWatcher(e)
	w.DwellRequired = 0
	w.ReFreezeCooldown = 0
	w.SetSuspended([]RunningProcess{{PID: 1234, PIDs: []int{1234}}})
	w.checkForegroundTarget(1234, "", false)
	w.checkForegroundTarget(1234, "", false)
	w.checkForegroundTarget(5678, "", false)
	e.mu.Lock()
	done := make(chan struct{})
	go func() {
		w.checkForegroundTarget(5678, "", false)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		e.mu.Unlock()
		t.Fatal("SmartWake acquired engine lock under watcher lock")
	}
	e.mu.Unlock()
}

func TestSmartWakeWatcherResumesForegroundProcessSafely(t *testing.T) {
	var resumedPIDs []int
	var trimmedPIDs []int

	mockResume := func(pid int) error {
		resumedPIDs = append(resumedPIDs, pid)
		return nil
	}
	mockTrim := func(pid int) error {
		trimmedPIDs = append(trimmedPIDs, pid)
		return nil
	}

	engine := &BoostEngine{
		ResumePID:         mockResume,
		TrimPIDWorkingSet: mockTrim,
	}

	watcher := NewSmartWakeWatcher(engine)
	watcher.PollInterval = 10 * time.Millisecond
	watcher.DwellRequired = 20 * time.Millisecond
	watcher.ReFreezeCooldown = 20 * time.Millisecond

	// Setup suspended processes
	suspended := []RunningProcess{
		{Name: "msedge.exe", PID: 1000, PIDs: []int{1000, 1001}},
		{Name: "Discord.exe", PID: 2000, PIDs: []int{2000}},
	}
	watcher.SetSuspended(suspended)

	// Case 1: An unrelated background app pops up or fake-calls (PID 9999) -> must NOT trigger wake
	watcher.checkForegroundTarget(9999, "notepad.exe", true)
	if len(resumedPIDs) != 0 {
		t.Fatalf("unrelated PID should not wake anything, got %#v", resumedPIDs)
	}

	// Case 2: A suspended app triggers a momentary flicker (< 20ms) -> must NOT wake immediately (debounced)
	watcher.checkForegroundTarget(1001, "msedge.exe", true)
	if len(resumedPIDs) != 0 {
		t.Fatalf("flicker under dwell time must not wake, got %#v", resumedPIDs)
	}

	// Case 3: Genuine user focus: user stays focused on Edge (> 20ms) -> WAKES Edge!
	time.Sleep(30 * time.Millisecond)
	watcher.checkForegroundTarget(1001, "msedge.exe", true)
	if len(resumedPIDs) != 2 || resumedPIDs[0] != 1000 || resumedPIDs[1] != 1001 {
		t.Fatalf("expected both edge PIDs resumed after genuine dwell, got %#v", resumedPIDs)
	}

	// Edge is now in TemporarilyWoken state. User switches back to Game (PID 5555)
	watcher.checkForegroundTarget(5555, "game.exe", true)
	// Dwell time on game passes -> Edge should be re-frozen automatically!
	time.Sleep(30 * time.Millisecond)
	var suspendedAgain []int
	engine.SuspendPID = func(pid int) error {
		suspendedAgain = append(suspendedAgain, pid)
		return nil
	}
	watcher.checkForegroundTarget(5555, "game.exe", true)

	if len(suspendedAgain) != 2 || suspendedAgain[0] != 1000 || suspendedAgain[1] != 1001 {
		t.Fatalf("expected Edge re-frozen after user returned to game, got %#v", suspendedAgain)
	}
	if len(trimmedPIDs) != 2 {
		t.Fatalf("expected Edge working set re-trimmed on re-freeze, got %#v", trimmedPIDs)
	}
}
