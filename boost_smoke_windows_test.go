package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestBoostSmokeWindowsChild(t *testing.T) {
	path := os.Getenv("OWLYS_BOOST_SMOKE_CHILD_COUNTER")
	if path == "" {
		return
	}
	for n := 1; ; n++ {
		if err := os.WriteFile(path, []byte(strconv.Itoa(n)), 0600); err != nil {
			os.Exit(2)
		}
		time.Sleep(30 * time.Millisecond)
	}
}
func smokeCount(path string) (int, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return 0, e
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
func smokeWaitAdvance(path string, prior int) (int, error) {
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		n, e := smokeCount(path)
		if e == nil && n > prior {
			return n, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return 0, fmt.Errorf("child counter did not advance past %d", prior)
}

// TestBoostSmokeWindowsSuspendsAndRestoresOwnChild touches only its test-created child.
func TestBoostSmokeWindowsSuspendsAndRestoresOwnChild(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")
	snapshot := filepath.Join(dir, "snapshot.json")
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		t.Skipf("cannot create child safety job: %v", e)
	}
	defer windows.CloseHandle(job)
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); e != nil {
		t.Skipf("cannot set safety job: %v", e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(exe, "-test.run=^TestBoostSmokeWindowsChild$")
	cmd.Env = append(os.Environ(), "OWLYS_BOOST_SMOKE_CHILD_COUNTER="+counter)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	pid := cmd.Process.Pid
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }() // Own child only.
	h, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if e != nil {
		t.Fatalf("open own child: %v", e)
	}
	e = windows.AssignProcessToJobObject(job, h)
	windows.CloseHandle(h)
	if e != nil {
		t.Skipf("cannot assign own child to safety job: %v", e)
	}
	before, e := smokeWaitAdvance(counter, 0)
	if e != nil {
		t.Fatal(e)
	}
	identity, e := processCreationIdentity(pid)
	if e != nil {
		t.Skipf("cannot identify own child: %v", e)
	}
	probe, e := windows.OpenProcess(processSuspendResume|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if e != nil {
		t.Skipf("no suspend privilege for own child: %v", e)
	}
	windows.CloseHandle(probe)
	engine := NewBoostEngine()
	engine.SnapshotPath = snapshot
	// No App, Wails, guardian or global hotkey; every other option is zero/false.
	req := BoostRequest{PauseApps: true, SelectedPIDs: []int{pid}}
	started := false
	defer func() {
		if started {
			if _, e := engine.StopBoost(); e != nil {
				t.Errorf("deferred child restore: %v", e)
				if current, ie := processCreationIdentity(pid); ie == nil && current == identity {
					if re := ntResumeVerified(pid, identity); re != nil {
						t.Errorf("direct own-child recovery: %v", re)
					}
				}
			}
		}
	}()
	started = true // StartBoost may mutate before returning error.
	active, e := engine.StartBoost(req, []RunningProcess{{Name: "boost-smoke-child.exe", PID: pid, PIDs: []int{pid}}})
	if e != nil {
		t.Fatalf("start isolated boost: %v", e)
	}
	if !active.Active || len(active.SuspendedApps) != 1 || active.SuspendedApps[0].PID != pid {
		t.Fatalf("child not confirmed suspended: %+v", active)
	}
	if active.PowerPlanSwitched || active.MMCSSActive || active.Timer05msActive || active.ExtremeIActive || active.ExtremeIIActive || len(active.StoppedServices) != 0 {
		t.Fatalf("unexpected unrelated state: %+v", active)
	}
	data, e := os.ReadFile(snapshot)
	if e != nil {
		t.Fatal(e)
	}
	var journal sessionSnapshot
	if e = json.Unmarshal(data, &journal); e != nil {
		t.Fatal(e)
	}
	if journal.ProcessIDs[pid] != identity || len(journal.SuspendedApps) != 1 || journal.SuspendedApps[0].PID != pid || journal.PowerPlanSwitched || len(journal.StoppedServices) != 0 {
		t.Fatalf("unexpected journal: %+v", journal)
	}
	time.Sleep(150 * time.Millisecond) // Drain in-flight write before checking stasis.
	frozen, e := smokeCount(counter)
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(300 * time.Millisecond)
	still, e := smokeCount(counter)
	if e != nil {
		t.Fatal(e)
	}
	if still != frozen {
		t.Fatalf("child progressed while suspended: %d -> %d", frozen, still)
	}
	stopped, e := engine.StopBoost()
	if e != nil {
		t.Fatalf("stop boost: %v", e)
	}
	started = false
	if stopped.Active || len(stopped.SuspendedApps) != 0 {
		t.Fatalf("not restored: %+v", stopped)
	}
	after, e := smokeWaitAdvance(counter, still)
	if e != nil {
		t.Fatal(e)
	}
	if current, ie := processCreationIdentity(pid); ie != nil || current != identity {
		t.Fatalf("child identity changed: %q %v", current, ie)
	}
	if _, e = os.Stat(snapshot); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("snapshot remains: %v", e)
	}
	t.Logf("own child PID %d: before=%d, frozen=%d, still=%d, resumed=%d; journal removed", pid, before, frozen, still, after)
}
