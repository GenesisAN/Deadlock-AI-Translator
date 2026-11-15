//go:build !windows

package settings

import "os"

func protect(data []byte) ([]byte, error)   { return data, nil }
func unprotect(data []byte) ([]byte, error) { return data, nil }
func replace(old, new string) error         { return os.Rename(old, new) }
func syncDirectory(dir string) {
	if f, e := os.Open(dir); e == nil {
		defer f.Close()
		_ = f.Sync()
	}
}
