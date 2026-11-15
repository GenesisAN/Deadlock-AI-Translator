package settings

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	crypt32    = syscall.NewLazyDLL("crypt32.dll")
	dpapiWrite = crypt32.NewProc("CryptProtectData")
	dpapiRead  = crypt32.NewProc("CryptUnprotectData")
	kernel32   = syscall.NewLazyDLL("kernel32.dll")
	localFree  = kernel32.NewProc("LocalFree")
	moveFileEx = kernel32.NewProc("MoveFileExW")
)

type blob struct {
	Size uint32
	Data *byte
}

func dpapi(proc *syscall.LazyProc, b []byte) ([]byte, error) {
	in := blob{uint32(len(b)), unsafe.SliceData(b)}
	var out blob
	const uiForbidden = 1
	r, _, e := proc.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, uiForbidden, uintptr(unsafe.Pointer(&out)))
	runtime.KeepAlive(b)
	if r == 0 {
		return nil, e
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
func protect(b []byte) ([]byte, error)   { return dpapi(dpapiWrite, b) }
func unprotect(b []byte) ([]byte, error) { return dpapi(dpapiRead, b) }
func replace(old, new string) error {
	a, e := syscall.UTF16PtrFromString(old)
	if e != nil {
		return e
	}
	b, e := syscall.UTF16PtrFromString(new)
	if e != nil {
		return e
	}
	const replaceExistingAndWriteThrough = 9
	r, _, e := moveFileEx.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), replaceExistingAndWriteThrough)
	if r == 0 {
		return e
	}
	return nil
}
func syncDirectory(string) {}
