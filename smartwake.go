package main

import (
	"os"
	"sync"
	"time"
	"unsafe"
)

// SmartWakeWatcher monitors user-initiated window focus changes and safely resumes suspended apps
// ONLY when the user intentionally clicks or Alt+Tabs into them, and re-freezes them when returning to the game.
type SmartWakeWatcher struct {
	mu                 sync.Mutex
	engine             *BoostEngine
	PollInterval       time.Duration
	DwellRequired      time.Duration
	ReFreezeCooldown   time.Duration
	stopChan           chan struct{}
	running            bool
	suspendedMap       map[int]RunningProcess // primary PID -> RunningProcess
	pidToPrimary       map[int]int            // child PID -> primary PID
	temporarilyWoken   map[int]time.Time      // primary PID -> woken timestamp
	candidatePID       int
	candidateStartTime time.Time
}

func NewSmartWakeWatcher(engine *BoostEngine) *SmartWakeWatcher {
	return &SmartWakeWatcher{
		engine:           engine,
		PollInterval:     150 * time.Millisecond,
		DwellRequired:    250 * time.Millisecond, // User must dwell > 250ms to prevent background IPC flickers
		ReFreezeCooldown: 2 * time.Second,
		suspendedMap:     make(map[int]RunningProcess),
		pidToPrimary:     make(map[int]int),
		temporarilyWoken: make(map[int]time.Time),
	}
}

func (w *SmartWakeWatcher) SetSuspended(list []RunningProcess) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.suspendedMap = make(map[int]RunningProcess)
	w.pidToPrimary = make(map[int]int)
	for _, proc := range list {
		w.suspendedMap[proc.PID] = proc
		targets := proc.PIDs
		if len(targets) == 0 {
			targets = []int{proc.PID}
		}
		for _, pid := range targets {
			w.pidToPrimary[pid] = proc.PID
		}
	}
}

func (w *SmartWakeWatcher) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.stopChan = make(chan struct{})
	w.mu.Unlock()

	go w.loop()
}

func (w *SmartWakeWatcher) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	close(w.stopChan)
	w.mu.Unlock()
}

func (w *SmartWakeWatcher) loop() {
	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.pollForeground()
		}
	}
}

func (w *SmartWakeWatcher) pollForeground() {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return
	}
	var fgPID uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&fgPID)))
	if fgPID <= 4 || int(fgPID) == os.Getpid() {
		return
	}

	w.checkForegroundTarget(int(fgPID), "", false)
}

func (w *SmartWakeWatcher) checkForegroundTarget(pid int, _ string, _ bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()

	// Track dwell time on the currently focused PID
	if pid != w.candidatePID {
		w.candidatePID = pid
		w.candidateStartTime = now
		return
	}

	// Guard against background triggers: must dwell at least DwellRequired to confirm genuine user intent
	if now.Sub(w.candidateStartTime) < w.DwellRequired {
		return
	}

	primaryPID, isSuspended := w.pidToPrimary[pid]

	// Branch 1: User genuinely focused a suspended application
	if isSuspended {
		proc, exists := w.suspendedMap[primaryPID]
		if exists {
			// Resume all threads for this application
			targets := proc.PIDs
			if len(targets) == 0 {
				targets = []int{proc.PID}
			}
			if w.engine != nil && w.engine.ResumePID != nil {
				for _, targetPID := range targets {
					_ = w.engine.ResumePID(targetPID)
				}
			}
			w.temporarilyWoken[primaryPID] = now
		}
		return
	}

	// Branch 2: User is focused on an external window (e.g. Game or another app)
	// Check if any temporarily-woken apps should now be re-frozen
	for primaryPID, wokenAt := range w.temporarilyWoken {
		// Allow cooldown after wake before considering re-freeze
		if now.Sub(wokenAt) < w.ReFreezeCooldown {
			continue
		}

		// Re-freeze the woken app to restore maximum resources for the game!
		if w.engine != nil && w.engine.SuspendPID != nil {
			status := w.engine.Status()
			var foundProc *RunningProcess
			for _, p := range status.SuspendedApps {
				if p.PID == primaryPID {
					foundProc = &p
					break
				}
			}
			targets := []int{primaryPID}
			if foundProc != nil && len(foundProc.PIDs) > 0 {
				targets = foundProc.PIDs
			} else if proc, ok := w.suspendedMap[primaryPID]; ok && len(proc.PIDs) > 0 {
				targets = proc.PIDs
			}

			for _, targetPID := range targets {
				if err := w.engine.SuspendPID(targetPID); err == nil {
					if w.engine.TrimPIDWorkingSet != nil {
						_ = w.engine.TrimPIDWorkingSet(targetPID)
					}
				}
			}
			if foundProc != nil {
				w.suspendedMap[primaryPID] = *foundProc
			}
		}
		delete(w.temporarilyWoken, primaryPID)
	}
}
