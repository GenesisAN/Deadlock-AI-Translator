//go:build !windows

package localcert

import "crypto/tls"

func Prepare(dir string) (tls.Certificate, bool, error) {
	pair, err := Load(dir)
	return pair, false, err
}
