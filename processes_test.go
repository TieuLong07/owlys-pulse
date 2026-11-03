package main

import "testing"

func TestParseTasklistCSVReturnsRunningProcesses(t *testing.T) {
	csv := `"Image Name","PID","Session Name","Session#","Mem Usage"
"chrome.exe","1234","Console","1","256,000 K"
"Discord.exe","5678","Console","1","512,000 K"
"System Idle Process","0","Services","0","8 K"
`

	processes, err := parseTasklistCSV(csv)
	if err != nil {
		t.Fatalf("parseTasklistCSV returned an error: %v", err)
	}
	if len(processes) != 2 {
		t.Fatalf("expected 2 eligible processes, got %d: %#v", len(processes), processes)
	}
	if processes[0].Name != "Discord.exe" || processes[0].PID != 5678 || processes[0].MemoryKB != 512000 {
		t.Fatalf("unexpected first process: %#v", processes[0])
	}
	if processes[1].Name != "chrome.exe" || processes[1].PID != 1234 || processes[1].MemoryKB != 256000 {
		t.Fatalf("unexpected second process: %#v", processes[1])
	}
}

func TestParseTasklistCSVExcludesCriticalWindowsProcesses(t *testing.T) {
	csv := `"Image Name","PID","Session Name","Session#","Mem Usage"
"Secure System","188","Services","0","66,000 K"
"smss.exe","812","Services","0","2,000 K"
"csrss.exe","1140","Services","0","6,000 K"
"wininit.exe","1324","Services","0","8,000 K"
"winlogon.exe","1420","Console","1","15,000 K"
"services.exe","1452","Services","0","10,000 K"
"lsass.exe","1492","Services","0","32,000 K"
"LsaIso.exe","1484","Services","0","3,000 K"
"svchost.exe","1632","Services","0","50,000 K"
"SteelSeriesEngine.exe","17172","Console","1","79,000 K"
"CBoard.Windows.exe","20336","Console","1","206,000 K"
"msedgewebview2.exe","12788","Console","1","125,000 K"
"chrome.exe","1234","Console","1","256,000 K"
`

	processes, err := parseTasklistCSV(csv)
	if err != nil {
		t.Fatalf("parseTasklistCSV returned an error: %v", err)
	}
	if len(processes) != 1 || processes[0].Name != "chrome.exe" {
		t.Fatalf("critical Windows and input hook processes must not be selectable: %#v", processes)
	}
}

func TestParseTasklistCSVIgnoresHeaderAndSystemProcesses(t *testing.T) {
	csv := `INFO: No tasks are running which match the specified criteria.`
	processes, err := parseTasklistCSV(csv)
	if err != nil {
		t.Fatalf("parseTasklistCSV returned an error: %v", err)
	}
	if len(processes) != 0 {
		t.Fatalf("expected no processes, got %#v", processes)
	}
}

func TestParseTasklistCSVSortsByMemoryAndMarksRecommendedUserApps(t *testing.T) {
	csv := `"Image Name","PID","Session Name","Session#","Mem Usage"
"dwm.exe","1972","Console","1","126,000 K"
"NVDisplay.Container.exe","3068","Services","0","56,000 K"
"OwlysPulse.exe","28604","Console","1","42,000 K"
"OneDrive.exe","4100","Console","1","110,000 K"
"chrome.exe","1234","Console","1","640,000 K"
"Discord.exe","5678","Console","1","380,000 K"
`

	processes, err := parseTasklistCSV(csv)
	if err != nil {
		t.Fatalf("parseTasklistCSV returned an error: %v", err)
	}
	if len(processes) != 3 {
		t.Fatalf("expected 3 user apps after filtering dwm/NVDisplay/OwlysPulse, got %d: %#v", len(processes), processes)
	}
	if processes[0].Name != "chrome.exe" || !processes[0].Recommended || processes[0].Category != "Trình duyệt" {
		t.Fatalf("expected chrome.exe first and recommended, got %#v", processes[0])
	}
	if processes[1].Name != "Discord.exe" || processes[1].Recommended || processes[1].Caution == "" {
		t.Fatalf("expected Discord.exe second with voice caution and not auto-recommended, got %#v", processes[1])
	}
	if processes[2].Name != "OneDrive.exe" || !processes[2].Recommended {
		t.Fatalf("expected OneDrive.exe third and recommended, got %#v", processes[2])
	}
}

func TestKnowledgeBaseEnrichesKnownProcessesAndGeneratesPrivacySafeUnknownReport(t *testing.T) {
	csv := `"Image Name","PID","Session Name","Session#","Mem Usage"
"msedge.exe","1000","Console","1","450,000 K"
"Figma.exe","2000","Console","1","320,000 K"
"MysteryVendorTool.exe","3000","Console","1","90,000 K"
`

	processes, err := parseTasklistCSV(csv)
	if err != nil {
		t.Fatalf("parseTasklistCSV failed: %v", err)
	}
	if len(processes) != 3 {
		t.Fatalf("expected 3 processes, got %d", len(processes))
	}
	if !processes[0].KnownInKB || processes[0].FriendlyName == "" || processes[0].Description == "" {
		t.Fatalf("expected msedge.exe to be enriched by Knowledge Base, got %#v", processes[0])
	}
	if !processes[1].KnownInKB || processes[1].FriendlyName == "" {
		t.Fatalf("expected Figma.exe to be in Knowledge Base, got %#v", processes[1])
	}
	if processes[2].KnownInKB {
		t.Fatalf("expected MysteryVendorTool.exe to be marked unknown in KB, got %#v", processes[2])
	}

	report := buildUnknownProcessReport(processes)
	if report.TotalUnknown != 1 || len(report.Items) != 1 || report.Items[0].ExeName != "MysteryVendorTool.exe" {
		t.Fatalf("unexpected unknown process report: %#v", report)
	}
}
