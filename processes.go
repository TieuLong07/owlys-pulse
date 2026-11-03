package main

import (
	"encoding/csv"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// RunningProcess is a user-visible process sampled from Windows Task Manager.
type RunningProcess struct {
	Name          string `json:"name"`
	FriendlyName  string `json:"friendlyName"`
	Publisher     string `json:"publisher"`
	Description   string `json:"description"`
	KnownInKB     bool   `json:"knownInKB"`
	PID           int    `json:"pid"`
	PIDs          []int  `json:"pids"`
	InstanceCount int    `json:"instanceCount"`
	MemoryKB      int64  `json:"memoryKB"`
	SessionName   string `json:"sessionName"`
	Category      string `json:"category"`
	Recommended   bool   `json:"recommended"`
	Caution       string `json:"caution"`
}

func parseTasklistCSV(input string) ([]RunningProcess, error) {
	if !strings.Contains(input, "\n") || strings.HasPrefix(strings.TrimSpace(input), "INFO:") {
		return []RunningProcess{}, nil
	}

	reader := csv.NewReader(strings.NewReader(input))
	reader.FieldsPerRecord = -1
	_, err := reader.Read() // header
	if err != nil {
		return []RunningProcess{}, nil
	}

	selfPID := os.Getpid()
	byName := make(map[string]*RunningProcess)
	var order []string
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(record) < 5 {
			continue
		}
		sessionName := strings.TrimSpace(record[2])
		if strings.EqualFold(sessionName, "Services") || !eligibleProcess(record[0], record[1]) {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil || pid == selfPID {
			continue
		}
		memory := strings.NewReplacer(",", "", ".", "", "K", "", "k", "", " ", "").Replace(record[4])
		memoryKB, err := strconv.ParseInt(memory, 10, 64)
		if err != nil {
			memoryKB = 0
		}

		key := strings.ToLower(strings.TrimSpace(record[0]))
		if existing, ok := byName[key]; ok {
			existing.MemoryKB += memoryKB
			existing.PIDs = append(existing.PIDs, pid)
			existing.InstanceCount = len(existing.PIDs)
			category, recommended, caution := classifyUserProcess(existing.Name, existing.MemoryKB)
			existing.Category = category
			existing.Recommended = recommended
			existing.Caution = caution
			enrichProcessFromKBAndPE(existing)
			continue
		}

		category, recommended, caution := classifyUserProcess(record[0], memoryKB)
		entry := &RunningProcess{
			Name:          record[0],
			PID:           pid,
			PIDs:          []int{pid},
			InstanceCount: 1,
			MemoryKB:      memoryKB,
			SessionName:   sessionName,
			Category:      category,
			Recommended:   recommended,
			Caution:       caution,
		}
		enrichProcessFromKBAndPE(entry)
		byName[key] = entry
		order = append(order, key)
	}

	processes := make([]RunningProcess, 0, len(order))
	for _, key := range order {
		processes = append(processes, *byName[key])
	}

	sort.SliceStable(processes, func(i, j int) bool {
		return processes[i].MemoryKB > processes[j].MemoryKB
	})

	return processes, nil
}

func eligibleProcess(name, pid string) bool {
	if pid == "0" || pid == "4" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	criticalNames := map[string]bool{
		"system": true, "system idle process": true, "registry": true, "memory compression": true,
		"secure system": true, "smss.exe": true, "csrss.exe": true, "wininit.exe": true,
		"winlogon.exe": true, "services.exe": true, "lsass.exe": true, "lsaiso.exe": true,
		"svchost.exe": true, "fontdrvhost.exe": true, "wudfhost.exe": true, "dwm.exe": true,
		"explorer.exe": true, "sihost.exe": true, "taskhostw.exe": true, "ctfmon.exe": true,
		"runtimebroker.exe": true, "applicationframehost.exe": true, "textinputhost.exe": true,
		"shellexperiencehost.exe": true, "startmenuexperiencehost.exe": true, "searchhost.exe": true,
		"dllhost.exe": true, "conhost.exe": true, "openconsole.exe": true, "audiodg.exe": true,
		"spoolsv.exe": true, "wmiprvse.exe": true, "owlyspulse.exe": true, "owlys-pulse.exe": true,
		"hermes.exe": true, "cua-driver.exe": true, "msedgewebview2.exe": true, // NEVER suspend WebView2 (Wails/Hermes host)
	}
	if criticalNames[lower] {
		return false
	}

	blockedFragments := []string{
		// Display & GPU Drivers
		"nvdisplay", "nvidia", "amd", "radeon", "atiesrxx", "atieclxx", "igfx",
		// Antivirus & Anti-Cheat
		"defender", "msmpeng", "securityhealth", "antimalware", "vanguard", "vgc", "vgtray",
		"easyanticheat", "battleye", "faceit",
		// Input & Mouse/Keyboard Hook processes (WH_MOUSE_LL / WH_KEYBOARD_LL deadlock prevention)
		"steelseries", "cboard", "windhawk", "unikey", "evkey", "gotv",
		"lghub", "logi", "touchpad", "synaptics", "elan", "wacom", "huion",
		"fnhotkey", "corsair", "icue", "rzsynapse", "razer synapse",
		// Developer tools & CLI hosts (Hermes & background shells)
		"python", "node", "powershell", "bash", "cmd",
		// VPN / Network drivers
		"tailscale", "zerotier", "vpnsvc",
		// Laptop OEM hardware controllers
		"lenovovantage", "lenovoutility", "smartsense",
	}
	for _, frag := range blockedFragments {
		if strings.Contains(lower, frag) {
			return false
		}
	}
	return true
}

func classifyUserProcess(name string, memoryKB int64) (category string, recommended bool, caution string) {
	lower := strings.ToLower(strings.TrimSpace(name))

	switch {
	case strings.Contains(lower, "chrome") || strings.Contains(lower, "firefox") || strings.Contains(lower, "brave") || strings.Contains(lower, "coccoc") || strings.Contains(lower, "opera"):
		return "Trình duyệt", true, ""
	case strings.EqualFold(lower, "msedge.exe"):
		return "Trình duyệt", true, ""
	case strings.Contains(lower, "onedrive") || strings.Contains(lower, "dropbox") || strings.Contains(lower, "googledrivesync"):
		return "Đồng bộ đám mây", true, ""
	case strings.Contains(lower, "discord") || strings.Contains(lower, "teams") || strings.Contains(lower, "zalo") || strings.Contains(lower, "telegram") || strings.Contains(lower, "slack"):
		return "Giao tiếp & Voice", false, "Tạm dừng có thể ngắt voice chat hoặc tin nhắn"
	case strings.Contains(lower, "obs") || strings.Contains(lower, "streamlabs") || strings.Contains(lower, "medal") || strings.Contains(lower, "gamebar"):
		return "Quay & Stream", false, "Đang dùng cho quay màn hình hoặc overlay"
	case strings.Contains(lower, "steam") || strings.Contains(lower, "epicgames") || strings.Contains(lower, "riotclient") || strings.Contains(lower, "battle.net"):
		return "Game Launcher", false, "Giữ chạy nếu game đang cần launcher hoặc overlay"
	case strings.Contains(lower, "spotify") || strings.Contains(lower, "itunes"):
		return "Đa phương tiện", false, "Có thể tạm dừng nếu không nghe nhạc khi chơi game"
	case strings.Contains(lower, "figma") || strings.Contains(lower, "notion") || strings.Contains(lower, "office") || strings.Contains(lower, "winword") || strings.Contains(lower, "excel"):
		return "Ứng dụng làm việc", false, "Tạm dừng sẽ thu hồi RAM làm việc"
	default:
		// Safe by default: NEVER recommend unknown processes automatically!
		return "Ứng dụng nền", false, ""
	}
}
