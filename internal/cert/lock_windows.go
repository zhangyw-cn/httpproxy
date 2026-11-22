//go:build windows

package cert

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const lockfileExclusiveLock = 0x00000002

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

func lockFile(f *os.File) error {
	var overlapped syscall.Overlapped
	r1, _, err := procLockFileEx.Call(
		f.Fd(),
		lockfileExclusiveLock,
		0,
		0xffffffff,
		0xffffffff,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		if err != nil && err != syscall.Errno(0) {
			return fmt.Errorf("LockFileEx: %w", err)
		}
		return fmt.Errorf("LockFileEx failed")
	}
	return nil
}

func unlockFile(f *os.File) error {
	var overlapped syscall.Overlapped
	r1, _, err := procUnlockFileEx.Call(
		f.Fd(),
		0,
		0xffffffff,
		0xffffffff,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		if err != nil && err != syscall.Errno(0) {
			return fmt.Errorf("UnlockFileEx: %w", err)
		}
		return fmt.Errorf("UnlockFileEx failed")
	}
	return nil
}
