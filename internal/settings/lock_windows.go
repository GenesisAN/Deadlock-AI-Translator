package settings

import (
	"os"
	"syscall"
	"unsafe"
)

var lockFileEx = kernel32.NewProc("LockFileEx")

func lock(f *os.File) error {
	var at syscall.Overlapped
	const exclusiveAndImmediate = 3
	ok, _, err := lockFileEx.Call(f.Fd(), exclusiveAndImmediate, 0, 1, 0, uintptr(unsafe.Pointer(&at)))
	if ok == 0 {
		return err
	}
	return nil
}
