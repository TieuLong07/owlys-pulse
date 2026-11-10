package main

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
)

var errProcessIdentityMismatch = errors.New("process creation identity mismatch")

// mutateVerifiedProcess binds identity verification and mutation to one kernel object.
func mutateVerifiedProcess(pid int, identity string, mutate func(windows.Handle) error) error {
	h, err := windows.OpenProcess(processSuspendResume|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var created, exit, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(h, &created, &exit, &kernel, &user); err != nil {
		return err
	}
	actual := fmt.Sprintf("%d:%d", created.HighDateTime, created.LowDateTime)
	if identity == "" || actual != identity {
		return errProcessIdentityMismatch
	}
	var code uint32
	if err = windows.GetExitCodeProcess(h, &code); err != nil {
		return err
	}
	if code != 259 {
		return windows.ERROR_INVALID_PARAMETER
	}
	return mutate(h)
}
func ntSuspendVerified(pid int, identity string) error {
	return mutateVerifiedProcess(pid, identity, func(h windows.Handle) error {
		r, _, err := procNtSuspend.Call(uintptr(h))
		if r != 0 {
			return fmt.Errorf("NtSuspendProcess status 0x%x (%v)", r, err)
		}
		return nil
	})
}
func ntResumeVerified(pid int, identity string) error {
	err := mutateVerifiedProcess(pid, identity, func(h windows.Handle) error {
		r, _, callErr := procNtResume.Call(uintptr(h))
		if r != 0 {
			return fmt.Errorf("NtResumeProcess status 0x%x (%v)", r, callErr)
		}
		return nil
	})
	if errors.Is(err, errProcessIdentityMismatch) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_NOT_FOUND) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	return err
}
