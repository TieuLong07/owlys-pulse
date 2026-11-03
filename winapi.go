package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	processQueryLimitedInfo     = 0x1000
	processSetQuota             = 0x0100
	processSetInformation       = 0x0200
	systemMemoryListInformation = 80
	memoryFlushModifiedList     = 3
	memoryPurgeStandbyList      = 4
	memoryPurgeLowPriorityList  = 5
	processIoPriorityClass      = 33
	processPagePriorityClass    = 39
	highPriorityClass           = 0x00000080
)

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	user32                   = windows.NewLazySystemDLL("user32.dll")
	procK32EmptyWorkingSet   = kernel32.NewProc("K32EmptyWorkingSet")
	procSetPriorityClass     = kernel32.NewProc("SetPriorityClass")
	procNtSetTimerResolution = ntdll.NewProc("NtSetTimerResolution")
	procNtSetSystemInfo      = ntdll.NewProc("NtSetSystemInformation")
	procNtSetInfoProcess     = ntdll.NewProc("NtSetInformationProcess")
	procGetForegroundWindow  = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")

	extremeIIServices = []string{
		"Spooler", "Fax", "MapsBroker", "lfsvc", "WbioSrvc",
		"TabletInputService", "StiSvc", "WerSvc", "PcaSvc",
		"DPS", "WdiSystemHost", "wisvc", "RetailDemo", "PhoneSvc",
	}
)

// trimProcessWorkingSet calls K32EmptyWorkingSet on a suspended process so its physical RAM pages move out immediately.
func trimProcessWorkingSet(pid int) error {
	handle, err := windows.OpenProcess(processQueryLimitedInfo|processSetQuota, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	r1, _, callErr := procK32EmptyWorkingSet.Call(uintptr(handle))
	if r1 == 0 {
		return fmt.Errorf("K32EmptyWorkingSet failed: %v", callErr)
	}
	return nil
}

// enableTimerResolution05ms requests 0.5ms (5000 * 100ns) kernel timer resolution via NtSetTimerResolution.
func enableTimerResolution05ms() error {
	var current uint32
	r1, _, err := procNtSetTimerResolution.Call(5000, 1, uintptr(unsafe.Pointer(&current)))
	if r1 != 0 {
		return fmt.Errorf("NtSetTimerResolution(0.5ms) status 0x%x: %v", r1, err)
	}
	return nil
}

// disableTimerResolution05ms releases Owlys Pulse's 0.5ms timer resolution vote.
func disableTimerResolution05ms() error {
	var current uint32
	r1, _, err := procNtSetTimerResolution.Call(5000, 0, uintptr(unsafe.Pointer(&current)))
	if r1 != 0 {
		return fmt.Errorf("NtSetTimerResolution(restore) status 0x%x: %v", r1, err)
	}
	return nil
}

func enableProcessPrivilege(privilegeName string) error {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return err
	}
	defer token.Close()

	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(privilegeName), &luid); err != nil {
		return err
	}

	tp := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]windows.LUIDAndAttributes{
			{
				Luid:       luid,
				Attributes: windows.SE_PRIVILEGE_ENABLED,
			},
		},
	}
	return windows.AdjustTokenPrivileges(token, false, &tp, 0, nil, nil)
}

// purgeWindowsStandbyList flushes modified pages and purges the Windows Standby List (ISLC mechanism).
func purgeWindowsStandbyList() error {
	_ = enableProcessPrivilege("SeProfileSingleProcessPrivilege")
	_ = enableProcessPrivilege("SeIncreaseQuotaPrivilege")

	commands := []uint32{memoryFlushModifiedList, memoryPurgeStandbyList, memoryPurgeLowPriorityList}
	var lastErr error
	var succeeded bool
	for _, cmd := range commands {
		command := cmd
		r1, _, err := procNtSetSystemInfo.Call(
			uintptr(systemMemoryListInformation),
			uintptr(unsafe.Pointer(&command)),
			unsafe.Sizeof(command),
		)
		if r1 == 0 {
			succeeded = true
		} else {
			lastErr = fmt.Errorf("NtSetSystemInformation(%d) status 0x%x (%v)", cmd, r1, err)
		}
	}
	if !succeeded && lastErr != nil {
		return lastErr
	}
	return nil
}

// enableMMCSSGamePriority boosts Multimedia Class Scheduler Service (MMCSS) Games profile & disables network throttling.
func enableMMCSSGamePriority() error {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile\Tasks\Games`,
		registry.SET_VALUE,
	)
	if err == nil {
		_ = k.SetDWordValue("GPU Priority", 8)
		_ = k.SetDWordValue("Priority", 6)
		_ = k.SetStringValue("Scheduling Category", "High")
		_ = k.SetStringValue("SFIO Priority", "High")
		_ = k.Close()
	}

	sysProfile, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile`,
		registry.SET_VALUE,
	)
	if err == nil {
		_ = sysProfile.SetDWordValue("NetworkThrottlingIndex", 0xFFFFFFFF)
		_ = sysProfile.SetDWordValue("SystemResponsiveness", 0)
		_ = sysProfile.Close()
	}
	return nil
}

// restoreMMCSSGamePriority restores default Windows MMCSS values after boost stops.
func restoreMMCSSGamePriority() error {
	sysProfile, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile`,
		registry.SET_VALUE,
	)
	if err == nil {
		_ = sysProfile.SetDWordValue("NetworkThrottlingIndex", 10)
		_ = sysProfile.SetDWordValue("SystemResponsiveness", 20)
		_ = sysProfile.Close()
	}
	return nil
}

// disableWindowsTransparency turns off Windows shell transparency effects and returns the previous value.
func disableWindowsTransparency() (uint32, error) {
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE|registry.SET_VALUE,
	)
	if err != nil {
		return 1, err
	}
	defer k.Close()

	val, _, err := k.GetIntegerValue("EnableTransparency")
	if err != nil {
		val = 1
	}
	if err := k.SetDWordValue("EnableTransparency", 0); err != nil {
		return uint32(val), err
	}
	return uint32(val), nil
}

// restoreWindowsTransparency restores the user's original EnableTransparency setting.
func restoreWindowsTransparency(orig uint32) error {
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.SET_VALUE,
	)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetDWordValue("EnableTransparency", orig)
}

func killExplorerShell() error {
	// Disable auto-restart briefly via taskkill /F /IM explorer.exe
	return hiddenWindowsCommand("taskkill", "/F", "/IM", "explorer.exe").Run()
}

func restartExplorerShell() error {
	return hiddenWindowsCommand("explorer.exe").Start()
}

// applyExtremeModeII executes deep service blackout, CPU unparking, and foreground game I/O + Page priority elevation.
func applyExtremeModeII() ([]string, error) {
	var stopped []string
	for _, svc := range extremeIIServices {
		running, err := isWindowsServiceRunning(svc)
		if err != nil || !running {
			continue
		}
		if err := stopWindowsService(svc); err == nil {
			stopped = append(stopped, svc)
		}
	}

	// Unpark all CPU cores in current power scheme (PROCTHROTTLEMIN = 100%, CPMINCORES = 100%)
	_ = hiddenWindowsCommand("powercfg", "-setacvalueindex", "scheme_current", "sub_processor", "CPMINCORES", "100").Run()
	_ = hiddenWindowsCommand("powercfg", "-setacvalueindex", "scheme_current", "sub_processor", "PROCTHROTTLEMIN", "100").Run()
	_ = hiddenWindowsCommand("powercfg", "-setactive", "scheme_current").Run()

	// Boost foreground process priority if it is not Owlys Pulse or Explorer
	boostForegroundProcessPriority()

	return stopped, nil
}

func restoreExtremeModeII(stoppedServices []string) error {
	for _, svc := range stoppedServices {
		_ = startWindowsService(svc)
	}
	// Restore balanced processor minimum state (5%)
	_ = hiddenWindowsCommand("powercfg", "-setacvalueindex", "scheme_current", "sub_processor", "PROCTHROTTLEMIN", "5").Run()
	_ = hiddenWindowsCommand("powercfg", "-setactive", "scheme_current").Run()
	return nil
}

func boostForegroundProcessPriority() {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return
	}
	var fgPID uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&fgPID)))
	if fgPID <= 4 || int(fgPID) == os.Getpid() {
		return
	}

	handle, err := windows.OpenProcess(processSetInformation|processQueryLimitedInfo, false, fgPID)
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)

	// Set HIGH_PRIORITY_CLASS
	procSetPriorityClass.Call(uintptr(handle), uintptr(highPriorityClass))

	// Set IoPriority = High (3) and PagePriority = 5
	ioPrio := uint32(3)
	procNtSetInfoProcess.Call(uintptr(handle), uintptr(processIoPriorityClass), uintptr(unsafe.Pointer(&ioPrio)), unsafe.Sizeof(ioPrio))
	pagePrio := uint32(5)
	procNtSetInfoProcess.Call(uintptr(handle), uintptr(processPagePriorityClass), uintptr(unsafe.Pointer(&pagePrio)), unsafe.Sizeof(pagePrio))
}

// selfTrimPulseMemory trims Owlys Pulse's own working set so the booster itself consumes minimal physical RAM while gaming.
func selfTrimPulseMemory() error {
	return trimProcessWorkingSet(os.Getpid())
}
