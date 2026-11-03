package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcessKBEntry holds human-friendly Vietnamese knowledge about a common Windows process.
type ProcessKBEntry struct {
	FriendlyName string
	Publisher    string
	Category     string
	Description  string
	Recommended  bool
	Caution      string
}

// UnknownProcessItem is the strictly anonymized record exported when a user opts in to share unknown processes.
// It never includes file paths, command-line arguments, usernames, or window titles.
type UnknownProcessItem struct {
	ExeName         string `json:"exeName"`
	Publisher       string `json:"publisher"`
	FileDescription string `json:"fileDescription"`
	InstanceCount   int    `json:"instanceCount"`
	MemoryMB        int64  `json:"memoryMB"`
}

// UnknownProcessReport is the previewable & exportable JSON payload for unknown processes.
type UnknownProcessReport struct {
	GeneratedAt  string               `json:"generatedAt"`
	PrivacyNote  string               `json:"privacyNote"`
	TotalUnknown int                  `json:"totalUnknown"`
	SavedPath    string               `json:"savedPath"`
	PrettyJSON   string               `json:"prettyJSON"`
	Items        []UnknownProcessItem `json:"items"`
}

var (
	versionDLL                 = windows.NewLazySystemDLL("version.dll")
	procGetFileVersionInfoSize = versionDLL.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfo     = versionDLL.NewProc("GetFileVersionInfoW")
	procVerQueryValue          = versionDLL.NewProc("VerQueryValueW")
	procQueryFullProcessImage  = kernel32.NewProc("QueryFullProcessImageNameW")

	peMetaCacheMu sync.Mutex
	peMetaCache   = map[string][2]string{} // lower exe -> [FileDescription, CompanyName]
)

var builtinProcessKB = map[string]ProcessKBEntry{
	"chrome.exe": {
		FriendlyName: "Google Chrome",
		Publisher:    "Google LLC",
		Category:     "Trình duyệt",
		Description:  "Trình duyệt web ngốn nhiều RAM và CPU nền khi mở nhiều tab.",
		Recommended:  true,
	},
	"msedge.exe": {
		FriendlyName: "Microsoft Edge",
		Publisher:    "Microsoft Corporation",
		Category:     "Trình duyệt",
		Description:  "Trình duyệt Edge và các tiện ích chạy ngầm của Windows.",
		Recommended:  true,
	},
	"msedgewebview2.exe": {
		FriendlyName: "Microsoft Edge WebView2",
		Publisher:    "Microsoft Corporation",
		Category:     "Hệ thống nhúng",
		Description:  "Tiến trình render của Wails/Hermes/Widgets. TUYỆT ĐỐI KHÔNG TẠM DỪNG.",
		Recommended:  false,
		Caution:      "CẤM TẠM DỪNG: Đây là tiến trình giao diện của chính Owlys Pulse và Hermes",
	},
	"firefox.exe": {
		FriendlyName: "Mozilla Firefox",
		Publisher:    "Mozilla Corporation",
		Category:     "Trình duyệt",
		Description:  "Trình duyệt web Firefox chạy ngầm.",
		Recommended:  true,
	},
	"brave.exe": {
		FriendlyName: "Brave Browser",
		Publisher:    "Brave Software",
		Category:     "Trình duyệt",
		Description:  "Trình duyệt nhân Chromium ngốn nhiều bộ nhớ RAM.",
		Recommended:  true,
	},
	"coccoc.exe": {
		FriendlyName: "Trình duyệt Cốc Cốc",
		Publisher:    "Coc Coc",
		Category:     "Trình duyệt",
		Description:  "Trình duyệt Cốc Cốc và trình tải xuống chạy nền.",
		Recommended:  true,
	},
	"opera.exe": {
		FriendlyName: "Opera / Opera GX",
		Publisher:    "Opera Software",
		Category:     "Trình duyệt",
		Description:  "Trình duyệt web Opera chạy nền.",
		Recommended:  true,
	},
	"discord.exe": {
		FriendlyName: "Discord",
		Publisher:    "Discord Inc.",
		Category:     "Giao tiếp & Voice",
		Description:  "Ứng dụng chat voice và overlay cho game thủ.",
		Recommended:  false,
		Caution:      "Tạm dừng sẽ ngắt kết nối đàm thoại Voice Chat và Discord Overlay",
	},
	"zalo.exe": {
		FriendlyName: "Zalo PC",
		Publisher:    "VNG Corporation",
		Category:     "Giao tiếp & Nhắn tin",
		Description:  "Ứng dụng nhắn tin Zalo trên máy tính, gồm nhiều tiến trình con.",
		Recommended:  false,
		Caution:      "Tạm dừng sẽ không nhận được thông báo tin nhắn hoặc cuộc gọi Zalo",
	},
	"zalocall.exe": {
		FriendlyName: "Zalo Video/Voice Call",
		Publisher:    "VNG Corporation",
		Category:     "Giao tiếp & Voice",
		Description:  "Tiến trình xử lý cuộc gọi âm thanh/hình ảnh của Zalo PC.",
		Recommended:  false,
		Caution:      "Giữ chạy nếu bạn đang gọi Zalo",
	},
	"telegram.exe": {
		FriendlyName: "Telegram Desktop",
		Publisher:    "Telegram FZ-LLC",
		Category:     "Giao tiếp & Nhắn tin",
		Description:  "Ứng dụng nhắn tin Telegram chạy nền.",
		Recommended:  false,
		Caution:      "Tạm dừng sẽ ngưng nhận thông báo tin nhắn trong lúc chơi",
	},
	"teams.exe": {
		FriendlyName: "Microsoft Teams",
		Publisher:    "Microsoft Corporation",
		Category:     "Giao tiếp & Họp",
		Description:  "Ứng dụng họp trực tuyến và nhắn tin công việc Microsoft Teams.",
		Recommended:  true,
	},
	"ms-teams.exe": {
		FriendlyName: "Microsoft Teams (New)",
		Publisher:    "Microsoft Corporation",
		Category:     "Giao tiếp & Họp",
		Description:  "Tiến trình chạy ngầm của Microsoft Teams bản mới.",
		Recommended:  true,
	},
	"slack.exe": {
		FriendlyName: "Slack Desktop",
		Publisher:    "Slack Technologies",
		Category:     "Giao tiếp & Làm việc",
		Description:  "Ứng dụng chat công việc ngốn nhiều RAM Electron.",
		Recommended:  true,
	},
	"steelseriesggclient.exe": {
		FriendlyName: "SteelSeries GG Client",
		Publisher:    "SteelSeries ApS",
		Category:     "Phần mềm Gear",
		Description:  "Phần mềm điều khiển chuột và gear SteelSeries.",
		Recommended:  false,
		Caution:      "KHÔNG NÊN TẠM DỪNG: Có thể làm treo luồng chuột hoặc mất DPI/macro chuột",
	},
	"steelseriesgg.exe": {
		FriendlyName: "SteelSeries GG Launcher",
		Publisher:    "SteelSeries ApS",
		Category:     "Phần mềm Gear",
		Description:  "Trình khởi chạy phần cứng SteelSeries.",
		Recommended:  false,
		Caution:      "KHÔNG NÊN TẠM DỪNG: Liên kết với driver chuột",
	},
	"steelseriessonar.exe": {
		FriendlyName: "SteelSeries Sonar Audio",
		Publisher:    "SteelSeries ApS",
		Category:     "Âm thanh Gaming",
		Description:  "Bộ xử lý âm thanh vòm ảo SteelSeries Sonar.",
		Recommended:  false,
		Caution:      "Tạm dừng có thể làm mất tiếng nếu bạn đang chọn đầu ra âm thanh Sonar",
	},
	"razer central.exe": {
		FriendlyName: "Razer Central",
		Publisher:    "Razer Inc.",
		Category:     "Phần mềm Gear / Bloat",
		Description:  "Trình quản lý tài khoản và đồng bộ đám mây của Razer.",
		Recommended:  true,
	},
	"razercortex.exe": {
		FriendlyName: "Razer Cortex",
		Publisher:    "Razer Inc.",
		Category:     "Phần mềm Gear / Bloat",
		Description:  "Ứng dụng quảng cáo/boost của Razer ngốn RAM nền.",
		Recommended:  true,
	},
	"lghub_agent.exe": {
		FriendlyName: "Logitech G HUB Agent",
		Publisher:    "Logitech Inc.",
		Category:     "Phần mềm Gear",
		Description:  "Tiến trình quản lý macro và DPI chuột/bàn phím Logitech.",
		Recommended:  false,
		Caution:      "Tạm dừng có thể làm mất nút phụ hoặc mức DPI chuột Logitech",
	},
	"armourycrate.usersessionhelper.exe": {
		FriendlyName: "ASUS Armoury Crate Helper",
		Publisher:    "ASUSTeK Computer Inc.",
		Category:     "Phần mềm Hãng / Bloat",
		Description:  "Tiến trình phụ trợ đèn LED và thông số của ASUS Armoury Crate.",
		Recommended:  true,
	},
	"wallpaper64.exe": {
		FriendlyName: "Wallpaper Engine (64-bit)",
		Publisher:    "Wallpaper Engine Team",
		Category:     "Hình nền động",
		Description:  "Trình phát hình nền động ngốn RAM và GPU nền.",
		Recommended:  true,
	},
	"wallpaper32.exe": {
		FriendlyName: "Wallpaper Engine (32-bit)",
		Publisher:    "Wallpaper Engine Team",
		Category:     "Hình nền động",
		Description:  "Trình phát hình nền động ngốn RAM và GPU nền.",
		Recommended:  true,
	},
	"cboard.windows.exe": {
		FriendlyName: "Ciiboard for Windows",
		Publisher:    "Ciiboard",
		Category:     "Tiện ích Clipboard & Kéo thả",
		Description:  "Thanh đảo nổi và quản lý kéo thả nhanh trên màn hình.",
		Recommended:  true,
	},
	"yasb.exe": {
		FriendlyName: "YASB Status Bar",
		Publisher:    "Open Source",
		Category:     "Tiện ích Giao diện",
		Description:  "Thanh trạng thái tùy biến trên màn hình nền Windows.",
		Recommended:  true,
	},
	"unikeynt.exe": {
		FriendlyName: "Bộ gõ tiếng Việt UniKey",
		Publisher:    "Pham Kim Long",
		Category:     "Bộ gõ tiếng Việt",
		Description:  "Trình gõ tiếng Việt siêu nhẹ.",
		Recommended:  false,
		Caution:      "Giữ chạy nếu bạn cần chat tiếng Việt có dấu trong game",
	},
	"evkey64.exe": {
		FriendlyName: "Bộ gõ tiếng Việt EVKey",
		Publisher:    "Lam Quang Minh",
		Category:     "Bộ gõ tiếng Việt",
		Description:  "Trình gõ tiếng Việt có tính năng tự tắt dấu khi vào game.",
		Recommended:  false,
		Caution:      "Giữ chạy để dùng phím tắt hoặc gõ tiếng Việt",
	},
	"figma.exe": {
		FriendlyName: "Figma Desktop App",
		Publisher:    "Figma Inc.",
		Category:     "Ứng dụng Thiết kế",
		Description:  "Ứng dụng thiết kế giao diện đồ họa tiêu thụ nhiều RAM/GPU.",
		Recommended:  true,
	},
	"code.exe": {
		FriendlyName: "Visual Studio Code",
		Publisher:    "Microsoft Corporation",
		Category:     "Ứng dụng Lập trình",
		Description:  "Trình soạn thảo mã nguồn VS Code và các Language Server chạy ngầm.",
		Recommended:  true,
	},
	"node.exe": {
		FriendlyName: "Node.js Runtime",
		Publisher:    "OpenJS Foundation",
		Category:     "Môi trường Dev / Nền",
		Description:  "Tiến trình chạy mã JavaScript/TypeScript hoặc công cụ lập trình ngầm.",
		Recommended:  true,
	},
	"python.exe": {
		FriendlyName: "Python Interpreter",
		Publisher:    "Python Software Foundation",
		Category:     "Môi trường Dev / AI",
		Description:  "Tiến trình thực thi script Python hoặc dịch vụ cục bộ.",
		Recommended:  true,
	},
	"onedrive.exe": {
		FriendlyName: "Microsoft OneDrive",
		Publisher:    "Microsoft Corporation",
		Category:     "Đồng bộ đám mây",
		Description:  "Đồng bộ tệp tin lên đám mây, thường gây nghẽn mạng và ổ cứng khi chơi game.",
		Recommended:  true,
	},
	"dropbox.exe": {
		FriendlyName: "Dropbox Client",
		Publisher:    "Dropbox, Inc.",
		Category:     "Đồng bộ đám mây",
		Description:  "Dịch vụ đồng bộ hóa tệp đám mây chạy nền.",
		Recommended:  true,
	},
	"googledrivesync.exe": {
		FriendlyName: "Google Drive for Desktop",
		Publisher:    "Google LLC",
		Category:     "Đồng bộ đám mây",
		Description:  "Trình đồng bộ hóa dữ liệu Google Drive ngầm.",
		Recommended:  true,
	},
	"spotify.exe": {
		FriendlyName: "Spotify Music",
		Publisher:    "Spotify AB",
		Category:     "Đa phương tiện",
		Description:  "Ứng dụng nghe nhạc trực tuyến ngốn RAM trình duyệt nhúng.",
		Recommended:  true,
	},
	"steam.exe": {
		FriendlyName: "Steam Client",
		Publisher:    "Valve Corporation",
		Category:     "Game Launcher",
		Description:  "Nền tảng quản lý thư viện và xác thực bản quyền game Steam.",
		Recommended:  false,
		Caution:      "Không tạm dừng nếu bạn đang chơi game khởi chạy từ Steam",
	},
	"steamwebhelper.exe": {
		FriendlyName: "Steam Client WebHelper",
		Publisher:    "Valve Corporation",
		Category:     "Game Launcher",
		Description:  "Trình duyệt nhúng hiển thị cửa hàng, danh sách bạn bè và Steam Overlay.",
		Recommended:  false,
		Caution:      "Giữ chạy nếu game đang cần Shift+Tab Steam Overlay hoặc mời bạn bè",
	},
	"epicgameslauncher.exe": {
		FriendlyName: "Epic Games Launcher",
		Publisher:    "Epic Games, Inc.",
		Category:     "Game Launcher",
		Description:  "Trình khởi chạy game Epic Games.",
		Recommended:  false,
		Caution:      "Giữ chạy nếu bạn đang chơi game từ Epic Games",
	},
	"riotclientservices.exe": {
		FriendlyName: "Riot Client Services",
		Publisher:    "Riot Games, Inc.",
		Category:     "Game Launcher",
		Description:  "Dịch vụ xác thực đăng nhập cho Liên Minh Huyền Thoại / VALORANT.",
		Recommended:  false,
		Caution:      "Bắt buộc giữ chạy khi đang chơi game của Riot Games",
	},
	"obs64.exe": {
		FriendlyName: "OBS Studio",
		Publisher:    "OBS Project",
		Category:     "Quay & Stream",
		Description:  "Phần mềm quay màn hình và livestream.",
		Recommended:  false,
		Caution:      "Đang dùng để quay video hoặc phát trực tiếp",
	},
	"phoneexperiencehost.exe": {
		FriendlyName: "Windows Phone Link",
		Publisher:    "Microsoft Corporation",
		Category:     "Tiện ích Windows nền",
		Description:  "Ứng dụng đồng bộ điện thoại với máy tính chạy ngầm.",
		Recommended:  true,
	},
	"crossdeviceresume.exe": {
		FriendlyName: "Windows Cross-Device Experience",
		Publisher:    "Microsoft Corporation",
		Category:     "Tiện ích Windows nền",
		Description:  "Tiến trình đồng bộ tác vụ giữa điện thoại và Windows.",
		Recommended:  true,
	},
	"widgets.exe": {
		FriendlyName: "Windows 11 Widgets Board",
		Publisher:    "Microsoft Corporation",
		Category:     "Tiện ích Windows nền",
		Description:  "Bảng tin tức và thời tiết chạy ngầm của Windows 11.",
		Recommended:  true,
	},
	"gamebar.exe": {
		FriendlyName: "Xbox Game Bar",
		Publisher:    "Microsoft Corporation",
		Category:     "Quay & Overlay",
		Description:  "Thanh công cụ chụp màn hình và overlay Win+G của Windows.",
		Recommended:  false,
		Caution:      "Giữ chạy nếu bạn dùng tổ hợp Win+G",
	},
}

func enrichProcessFromKBAndPE(proc *RunningProcess) {
	lower := strings.ToLower(strings.TrimSpace(proc.Name))
	if kb, ok := builtinProcessKB[lower]; ok {
		proc.KnownInKB = true
		proc.FriendlyName = kb.FriendlyName
		proc.Publisher = kb.Publisher
		proc.Category = kb.Category
		proc.Description = kb.Description
		proc.Recommended = kb.Recommended
		if kb.Caution != "" {
			proc.Caution = kb.Caution
		}
		return
	}

	// Fallback: inspect local PE FileDescription & CompanyName directly from disk (100% offline)
	desc, company := lookupWindowsPEMetadata(lower, proc.PID)
	proc.KnownInKB = false
	if desc != "" {
		proc.FriendlyName = desc
		proc.Description = fmt.Sprintf("Tiến trình từ %s (Đọc trực tiếp từ chữ ký tệp Windows).", nonEmpty(company, "nhà phát triển bên thứ 3"))
	} else {
		proc.FriendlyName = proc.Name
		proc.Description = "Tiến trình chưa có trong từ điển Owlys Knowledge Base."
	}
	proc.Publisher = nonEmpty(company, "Chưa rõ nhà phát hành")
}

func nonEmpty(val, fallback string) string {
	if strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return fallback
}

func lookupWindowsPEMetadata(lowerName string, pid int) (description string, company string) {
	peMetaCacheMu.Lock()
	if cached, ok := peMetaCache[lowerName]; ok {
		peMetaCacheMu.Unlock()
		return cached[0], cached[1]
	}
	peMetaCacheMu.Unlock()

	exePath := queryProcessImagePath(pid)
	if exePath == "" {
		return "", ""
	}
	description, company = readPEVersionStrings(exePath)

	peMetaCacheMu.Lock()
	peMetaCache[lowerName] = [2]string{description, company}
	peMetaCacheMu.Unlock()
	return description, company
}

func queryProcessImagePath(pid int) string {
	if pid <= 4 {
		return ""
	}
	handle, err := windows.OpenProcess(processQueryLimitedInfo, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	r1, _, _ := procQueryFullProcessImage.Call(
		uintptr(handle),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func readPEVersionStrings(exePath string) (description string, company string) {
	pathPtr, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return "", ""
	}
	var dummy uint32
	size, _, _ := procGetFileVersionInfoSize.Call(uintptr(unsafe.Pointer(pathPtr)), uintptr(unsafe.Pointer(&dummy)))
	if size == 0 || size > 1024*1024 {
		return "", ""
	}

	data := make([]byte, size)
	r1, _, _ := procGetFileVersionInfo.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		size,
		uintptr(unsafe.Pointer(&data[0])),
	)
	if r1 == 0 {
		return "", ""
	}

	// Query translation table \VarFileInfo\Translation
	transPath, _ := windows.UTF16PtrFromString(`\VarFileInfo\Translation`)
	var transPtr uintptr
	var transLen uint32
	var langCodepage = "040904b0"
	r2, _, _ := procVerQueryValue.Call(
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(unsafe.Pointer(transPath)),
		uintptr(unsafe.Pointer(&transPtr)),
		uintptr(unsafe.Pointer(&transLen)),
	)
	if r2 != 0 && transLen >= 4 && transPtr != 0 {
		lang := *(*uint16)(unsafe.Pointer(transPtr))
		cp := *(*uint16)(unsafe.Pointer(transPtr + 2))
		langCodepage = fmt.Sprintf("%04x%04x", lang, cp)
	}

	description = queryVerString(data, langCodepage, "FileDescription")
	if description == "" {
		description = queryVerString(data, "040904b0", "FileDescription")
	}
	company = queryVerString(data, langCodepage, "CompanyName")
	if company == "" {
		company = queryVerString(data, "040904b0", "CompanyName")
	}
	return strings.TrimSpace(description), strings.TrimSpace(company)
}

func queryVerString(data []byte, langCP string, key string) string {
	subBlock := fmt.Sprintf(`\StringFileInfo\%s\%s`, langCP, key)
	subPtr, err := windows.UTF16PtrFromString(subBlock)
	if err != nil {
		return ""
	}
	var valPtr uintptr
	var valLen uint32
	r1, _, _ := procVerQueryValue.Call(
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(unsafe.Pointer(subPtr)),
		uintptr(unsafe.Pointer(&valPtr)),
		uintptr(unsafe.Pointer(&valLen)),
	)
	if r1 == 0 || valPtr == 0 || valLen == 0 {
		return ""
	}
	slice := unsafe.Slice((*uint16)(unsafe.Pointer(valPtr)), valLen)
	return windows.UTF16ToString(slice)
}

func buildUnknownProcessReport(processes []RunningProcess) UnknownProcessReport {
	var items []UnknownProcessItem
	for _, proc := range processes {
		if proc.KnownInKB {
			continue
		}
		items = append(items, UnknownProcessItem{
			ExeName:         proc.Name,
			Publisher:       proc.Publisher,
			FileDescription: proc.FriendlyName,
			InstanceCount:   proc.InstanceCount,
			MemoryMB:        proc.MemoryKB / 1024,
		})
	}
	if items == nil {
		items = []UnknownProcessItem{}
	}

	report := UnknownProcessReport{
		GeneratedAt:  time.Now().Format(time.RFC3339),
		PrivacyNote:  "Chỉ chứa tên file .exe, nhà phát hành và mô tả chung. Không chứa đường dẫn thư mục cá nhân, tên người dùng hay tiêu đề cửa sổ.",
		TotalUnknown: len(items),
		Items:        items,
	}

	if pretty, err := json.MarshalIndent(report, "", "  "); err == nil {
		report.PrettyJSON = string(pretty)
	}
	return report
}

func saveUnknownProcessReportToDisk(processes []RunningProcess) (UnknownProcessReport, error) {
	report := buildUnknownProcessReport(processes)
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.TempDir()
	}
	outPath := filepath.Join(configDir, "OwlysPulse", "unknown_processes_contribution.json")
	_ = os.MkdirAll(filepath.Dir(outPath), 0o755)

	report.SavedPath = outPath
	if pretty, err := json.MarshalIndent(report, "", "  "); err == nil {
		report.PrettyJSON = string(pretty)
		_ = os.WriteFile(outPath, pretty, 0o644)
	}
	return report, nil
}
