package main

import (
	"testing"
)

func TestListRunningProcessesUsesTasklistOutput(t *testing.T) {
	app := NewApp()
	app.commandOutput = func() (string, error) {
		return `"Image Name","PID","Session Name","Session#","Mem Usage"
"chrome.exe","1234","Console","1","256,000 K"
`, nil
	}

	processes, err := app.ListRunningProcesses()
	if err != nil {
		t.Fatalf("ListRunningProcesses returned an error: %v", err)
	}
	if len(processes) != 1 || processes[0].Name != "chrome.exe" {
		t.Fatalf("unexpected processes: %#v", processes)
	}
}

func TestHiddenWindowsCommandSetsHideWindowAndNoWindowFlags(t *testing.T) {
	cmd := hiddenWindowsCommand("tasklist", "/FO", "CSV")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("expected SysProcAttr.HideWindow=true, got %#v", cmd.SysProcAttr)
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindowFlag == 0 {
		t.Fatalf("expected CREATE_NO_WINDOW (0x08000000) in CreationFlags, got 0x%x", cmd.SysProcAttr.CreationFlags)
	}
}
