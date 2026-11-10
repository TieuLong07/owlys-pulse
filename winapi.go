package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
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

// settingValue retains the registry type and bytes, including nonexistence.
type settingValue struct {
	Present bool
	Type    uint32
	Data    []byte
}
type savedSetting struct {
	Key   string
	Value settingValue
}
type settingStore interface {
	read(string) (settingValue, error)
	write(string, settingValue) error
}

func snapshotSettings(s settingStore, keys []string) ([]savedSetting, error) {
	out := make([]savedSetting, 0, len(keys))
	for _, key := range keys {
		value, err := s.read(key)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", key, err)
		}
		out = append(out, savedSetting{key, value})
	}
	return out, nil
}
func restoreSettings(s settingStore, saved []savedSetting) error {
	var errs []error
	for _, entry := range saved {
		if err := s.write(entry.Key, entry.Value); err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", entry.Key, err))
		}
	}
	return errors.Join(errs...)
}

const mmcssProfile = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile`

var mmcssNames = []string{"Games/GPU Priority", "Games/Priority", "Games/Scheduling Category", "Games/SFIO Priority", "NetworkThrottlingIndex", "SystemResponsiveness"}

func canWriteMMCSS() error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, mmcssProfile, registry.SET_VALUE)
	if err != nil {
		return err
	}
	return k.Close()
}

type registrySettings struct{}

func (registrySettings) open(name string, access uint32) (registry.Key, string, error) {
	path := mmcssProfile
	if strings.HasPrefix(name, "Games/") {
		path += `\Tasks\Games`
		name = strings.TrimPrefix(name, "Games/")
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, access)
	return k, name, err
}
func (r registrySettings) read(name string) (settingValue, error) {
	k, value, err := r.open(name, registry.QUERY_VALUE)
	if err != nil {
		return settingValue{}, err
	}
	defer k.Close()
	n, typ, err := k.GetValue(value, nil)
	if errors.Is(err, registry.ErrNotExist) {
		return settingValue{}, nil
	}
	if err != nil && !errors.Is(err, registry.ErrShortBuffer) {
		return settingValue{}, err
	}
	buf := make([]byte, n)
	for {
		n, typ, err = k.GetValue(value, buf)
		if errors.Is(err, registry.ErrShortBuffer) {
			buf = make([]byte, n)
			continue
		}
		if err != nil {
			return settingValue{}, err
		}
		return settingValue{true, typ, buf[:n]}, nil
	}
}
func (r registrySettings) write(name string, v settingValue) error {
	k, value, err := r.open(name, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !v.Present {
		err = k.DeleteValue(value)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	p, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	return setRawRegistryValue(k, p, v)
}

var procRegSetValueEx = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegSetValueExW")

func setRawRegistryValue(k registry.Key, p *uint16, v settingValue) error {
	var b uintptr
	if len(v.Data) > 0 {
		b = uintptr(unsafe.Pointer(&v.Data[0]))
	}
	status, _, _ := procRegSetValueEx.Call(uintptr(k), uintptr(unsafe.Pointer(p)), 0, uintptr(v.Type), b, uintptr(len(v.Data)))
	if status != 0 {
		return windows.Errno(status)
	}
	return nil
}
func mmcssBoostValue(name string) settingValue {
	var v uint32
	switch name {
	case "Games/GPU Priority":
		v = 8
	case "Games/Priority":
		v = 6
	case "NetworkThrottlingIndex":
		v = 0xffffffff
	case "SystemResponsiveness":
		v = 0
	default:
		data := windows.StringToUTF16("High")
		raw := make([]byte, len(data)*2)
		for i, c := range data {
			raw[i*2] = byte(c)
			raw[i*2+1] = byte(c >> 8)
		}
		return settingValue{true, registry.SZ, raw}
	}
	return settingValue{true, registry.DWORD, []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}}
}
func applyMMCSSSettings(s settingStore) ([]savedSetting, error) {
	saved, err := snapshotSettings(s, mmcssNames)
	if err != nil {
		return nil, err
	}
	for _, name := range mmcssNames {
		if err = s.write(name, mmcssBoostValue(name)); err != nil {
			return nil, errors.Join(fmt.Errorf("boost %s: %w", name, err), restoreSettings(s, saved))
		}
	}
	return saved, nil
}

// Registry, power scheme and foreground process state live in memory until restoration.
// A durable booster snapshot must serialize these if crash recovery is required.
var mmcssSaved []savedSetting

// enableMMCSSGamePriority snapshots every touched value (including absent values) before writing.
func enableMMCSSGamePriority() error {
	if mmcssSaved != nil {
		return nil
	}
	saved, err := applyMMCSSSettings(registrySettings{})
	if err == nil {
		mmcssSaved = saved
	}
	return err
}
func restoreMMCSSGamePriority() error {
	if mmcssSaved == nil {
		return nil
	}
	err := restoreSettings(registrySettings{}, mmcssSaved)
	if err == nil {
		mmcssSaved = nil
	}
	return err
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

// extremePowerState captures the exact active scheme and its AC settings.
type extremePowerState struct {
	Scheme               string
	CoreMin, ThrottleMin uint32
}
type powerSettings interface {
	currentScheme() (string, error)
	readAC(string, string) (uint32, error)
	writeAC(string, string, uint32) error
	activate(string) error
}

func applyExtremePower(p powerSettings) (extremePowerState, error) {
	scheme, err := p.currentScheme()
	if err != nil {
		return extremePowerState{}, err
	}
	core, err := p.readAC(scheme, "CPMINCORES")
	if err != nil {
		return extremePowerState{}, err
	}
	throttle, err := p.readAC(scheme, "PROCTHROTTLEMIN")
	if err != nil {
		return extremePowerState{}, err
	}
	saved := extremePowerState{scheme, core, throttle}
	if err = p.writeAC(scheme, "CPMINCORES", 100); err != nil {
		return saved, err
	}
	if err = p.writeAC(scheme, "PROCTHROTTLEMIN", 100); err != nil {
		return saved, errors.Join(err, restoreExtremePower(p, saved))
	}
	if err = p.activate(scheme); err != nil {
		return saved, errors.Join(err, restoreExtremePower(p, saved))
	}
	return saved, nil
}
func restoreExtremePower(p powerSettings, s extremePowerState) error {
	if s.Scheme == "" {
		return nil
	}
	err1 := p.writeAC(s.Scheme, "CPMINCORES", s.CoreMin)
	err2 := p.writeAC(s.Scheme, "PROCTHROTTLEMIN", s.ThrottleMin)
	err3 := p.activate(s.Scheme)
	return errors.Join(err1, err2, err3)
}

type windowsPowerSettings struct{}

func (windowsPowerSettings) currentScheme() (string, error) {
	b, err := hiddenWindowsCommand("powercfg", "-getactivescheme").Output()
	if err != nil {
		return "", err
	}
	text := string(b)
	start := strings.Index(text, "(")
	if start < 0 {
		return "", fmt.Errorf("powercfg active scheme: %q", text)
	}
	text = text[:start]
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", fmt.Errorf("powercfg active scheme: empty")
	}
	return parts[len(parts)-1], nil
}
func (windowsPowerSettings) readAC(scheme, key string) (uint32, error) {
	b, err := hiddenWindowsCommand("powercfg", "-query", scheme, "sub_processor", key).Output()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(strings.ToLower(line), "current ac power setting index") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				v, e := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(parts[len(parts)-1]), "0x"), 16, 32)
				return uint32(v), e
			}
		}
	}
	return 0, fmt.Errorf("powercfg AC index not found for %s", key)
}
func (windowsPowerSettings) writeAC(scheme, key string, v uint32) error {
	return hiddenWindowsCommand("powercfg", "-setacvalueindex", scheme, "sub_processor", key, strconv.FormatUint(uint64(v), 10)).Run()
}
func (windowsPowerSettings) activate(scheme string) error {
	return hiddenWindowsCommand("powercfg", "-setactive", scheme).Run()
}

// foregroundPriorityState records PID and each independent priority setting.
type foregroundPriorityState struct{ PID, Class, IO, Page uint32 }
type prioritySettings interface {
	foregroundPID() (uint32, error)
	readPriorities(uint32) (uint32, uint32, uint32, error)
	writeClass(uint32, uint32) error
	writeIO(uint32, uint32) error
	writePage(uint32, uint32) error
}

func applyForegroundPriority(p prioritySettings) (foregroundPriorityState, error) {
	pid, err := p.foregroundPID()
	if err != nil || pid <= 4 || int(pid) == os.Getpid() {
		return foregroundPriorityState{}, err
	}
	class, io, page, err := p.readPriorities(pid)
	if err != nil {
		return foregroundPriorityState{}, err
	}
	saved := foregroundPriorityState{pid, class, io, page}
	if err = p.writeClass(pid, highPriorityClass); err != nil {
		return saved, err
	}
	if err = p.writeIO(pid, 3); err != nil {
		return saved, errors.Join(err, restoreForegroundPriority(p, saved))
	}
	if err = p.writePage(pid, 5); err != nil {
		return saved, errors.Join(err, restoreForegroundPriority(p, saved))
	}
	return saved, nil
}
func restoreForegroundPriority(p prioritySettings, s foregroundPriorityState) error {
	if s.PID == 0 {
		return nil
	}
	return errors.Join(p.writeClass(s.PID, s.Class), p.writeIO(s.PID, s.IO), p.writePage(s.PID, s.Page))
}

var procGetPriorityClass = kernel32.NewProc("GetPriorityClass")
var procNtQueryInfoProcess = ntdll.NewProc("NtQueryInformationProcess")

type windowsPrioritySettings struct{}

func (windowsPrioritySettings) foregroundPID() (uint32, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return 0, nil
	}
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid, nil
}
func openPriorityProcess(pid uint32) (windows.Handle, error) {
	return windows.OpenProcess(processSetInformation|processQueryLimitedInfo, false, pid)
}
func (windowsPrioritySettings) readPriorities(pid uint32) (uint32, uint32, uint32, error) {
	h, err := openPriorityProcess(pid)
	if err != nil {
		return 0, 0, 0, err
	}
	defer windows.CloseHandle(h)
	class, _, callErr := procGetPriorityClass.Call(uintptr(h))
	if class == 0 {
		return 0, 0, 0, callErr
	}
	var io, page uint32
	for _, entry := range []struct {
		kind uintptr
		out  *uint32
	}{{processIoPriorityClass, &io}, {processPagePriorityClass, &page}} {
		status, _, e := procNtQueryInfoProcess.Call(uintptr(h), entry.kind, uintptr(unsafe.Pointer(entry.out)), unsafe.Sizeof(io), 0)
		if status != 0 {
			return 0, 0, 0, fmt.Errorf("NtQueryInformationProcess(%d): 0x%x: %v", entry.kind, status, e)
		}
	}
	return uint32(class), io, page, nil
}
func (windowsPrioritySettings) writeClass(pid, v uint32) error {
	h, err := openPriorityProcess(pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	ok, _, e := procSetPriorityClass.Call(uintptr(h), uintptr(v))
	if ok == 0 {
		return e
	}
	return nil
}
func writeProcessPriority(pid uint32, kind uintptr, v uint32) error {
	h, err := openPriorityProcess(pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	status, _, e := procNtSetInfoProcess.Call(uintptr(h), kind, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	if status != 0 {
		return fmt.Errorf("NtSetInformationProcess(%d): 0x%x: %v", kind, status, e)
	}
	return nil
}
func (windowsPrioritySettings) writeIO(pid, v uint32) error {
	return writeProcessPriority(pid, processIoPriorityClass, v)
}
func (windowsPrioritySettings) writePage(pid, v uint32) error {
	return writeProcessPriority(pid, processPagePriorityClass, v)
}

// Keep the legacy booster API; typed state is also available to durable snapshots.
var extremePowerSaved extremePowerState
var foregroundSaved foregroundPriorityState

func applyExtremeModeII() ([]string, error) {
	power, err := applyExtremePower(windowsPowerSettings{})
	if err != nil {
		return nil, err
	}
	extremePowerSaved = power
	fg, err := applyForegroundPriority(windowsPrioritySettings{})
	if err != nil {
		_ = restoreExtremePower(windowsPowerSettings{}, power)
		extremePowerSaved = extremePowerState{}
		return nil, err
	}
	foregroundSaved = fg
	var stopped []string
	for _, svc := range extremeIIServices {
		running, e := isWindowsServiceRunning(svc)
		if e == nil && running {
			if stopWindowsService(svc) == nil {
				stopped = append(stopped, svc)
			}
		}
	}
	return stopped, nil
}
func restoreExtremeModeII(stoppedServices []string) error {
	var errs []error
	for _, svc := range stoppedServices {
		if err := startWindowsService(svc); err != nil {
			errs = append(errs, err)
		}
	}
	if err := restoreForegroundPriority(windowsPrioritySettings{}, foregroundSaved); err != nil {
		errs = append(errs, err)
	} else {
		foregroundSaved = foregroundPriorityState{}
	}
	if err := restoreExtremePower(windowsPowerSettings{}, extremePowerSaved); err != nil {
		errs = append(errs, err)
	} else {
		extremePowerSaved = extremePowerState{}
	}
	return errors.Join(errs...)
}

// selfTrimPulseMemory trims Owlys Pulse's own working set so the booster itself consumes minimal physical RAM while gaming.
func selfTrimPulseMemory() error {
	return trimProcessWorkingSet(os.Getpid())
}
