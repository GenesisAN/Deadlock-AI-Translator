package localcert

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// Prepare creates or renews the local server certificate and requests trust in
// the current user's Windows root store on initial setup.
func Prepare(dir string) (tls.Certificate, bool, error) {
	if err := Init(dir); err != nil {
		return tls.Certificate{}, false, err
	}
	pair, root, err := load(dir)
	if err != nil {
		return pair, false, err
	}
	trusted, err := rootInUserStore(root.Raw)
	if err != nil || trusted {
		return pair, false, err
	}
	if !isBridgeRoot(root) {
		return pair, false, errors.New("只会自动安装本程序生成的 localhost 根证书")
	}
	if err := installRoot(dir, root.Raw); err != nil {
		return pair, false, err
	}
	trusted, err = rootInUserStore(root.Raw)
	if err != nil {
		return pair, false, err
	}
	if !trusted {
		return pair, false, errors.New("当前用户尚未信任本机根证书")
	}
	return pair, true, nil
}

func rootInUserStore(der []byte) (bool, error) {
	const (
		certStoreProvSystemW  = 10
		certSystemCurrentUser = 0x10000
		certStoreOpenExisting = 0x4000
		certStoreReadOnly     = 0x8000
		cryptENotFound        = syscall.Errno(0x80092004)
	)
	name := [...]uint16{'R', 'o', 'o', 't', 0}
	store, err := syscall.CertOpenStore(certStoreProvSystemW, 0, 0,
		certSystemCurrentUser|certStoreOpenExisting|certStoreReadOnly, uintptr(unsafe.Pointer(&name[0])))
	runtime.KeepAlive(&name)
	if err != nil {
		return false, fmt.Errorf("读取 Windows 当前用户根证书库: %w", err)
	}
	defer syscall.CertCloseStore(store, 0)
	var previous *syscall.CertContext
	for {
		// Enumeration frees the previous context, even when it reaches the end.
		cert, err := syscall.CertEnumCertificatesInStore(store, previous)
		if errors.Is(err, cryptENotFound) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("检查 Windows 当前用户根证书库: %w", err)
		}
		if bytes.Equal(der, unsafe.Slice(cert.EncodedCert, cert.Length)) {
			_ = syscall.CertFreeCertificateContext(cert)
			return true, nil
		}
		previous = cert
	}
}

func installRoot(dir string, der []byte) error {
	var buf [syscall.MAX_PATH]uint16
	getSystemDirectory := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW")
	n, _, err := getSystemDirectory.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return fmt.Errorf("定位 Windows certutil: %w", err)
	}
	if n >= uintptr(len(buf)) {
		return errors.New("Windows 系统目录路径过长")
	}
	certutil := filepath.Join(syscall.UTF16ToString(buf[:n]), "certutil.exe")
	file, err := os.CreateTemp(dir, "root-*.cer")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(der)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "游戏翻译需要信任本机根证书。请在 Windows 提示中核对 Deadlock Translator Go Local Root 并确认。")
	cmd := exec.Command(certutil, "-user", "-addstore", "Root", file.Name())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Windows 未完成根证书安装: %w", err)
	}
	return nil
}
