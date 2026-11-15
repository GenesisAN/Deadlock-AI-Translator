package localcert

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const rootName = "Deadlock Translator Go Local Root"

func RootPath(dir string) string   { return filepath.Join(dir, "root-ca.pem") }
func bundlePath(dir string) string { return filepath.Join(dir, "localhost.pem") }

func Load(dir string) (tls.Certificate, error) {
	pair, _, err := load(dir)
	return pair, err
}

func load(dir string) (tls.Certificate, *x509.Certificate, error) {
	name := bundlePath(dir)
	pair, err := tls.LoadX509KeyPair(name, name)
	if err != nil {
		return pair, nil, err
	}
	if pair.Leaf == nil {
		pair.Leaf, err = x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return pair, nil, err
		}
	}
	b, err := os.ReadFile(RootPath(dir))
	if err != nil {
		return pair, nil, err
	}
	block, rest := pem.Decode(b)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return pair, nil, errors.New("根证书无效")
	}
	root, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return pair, nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	_, err = pair.Leaf.Verify(x509.VerifyOptions{DNSName: "localhost", Roots: pool})
	return pair, root, err
}

func isBridgeRoot(root *x509.Certificate) bool {
	if root.Subject.CommonName != rootName || !root.IsCA || !root.BasicConstraintsValid || !root.MaxPathLenZero ||
		!root.PermittedDNSDomainsCritical || len(root.PermittedDNSDomains) != 1 || root.PermittedDNSDomains[0] != "localhost" ||
		len(root.PermittedIPRanges) != 2 || !bytes.Equal(root.RawIssuer, root.RawSubject) || root.CheckSignatureFrom(root) != nil {
		return false
	}
	return root.PermittedIPRanges[0].String() == "127.0.0.1/32" && root.PermittedIPRanges[1].String() == "::1/128"
}

func Init(dir string) error {
	if p, e := Load(dir); e == nil && time.Until(p.Leaf.NotAfter) > 30*24*time.Hour {
		return nil
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	root, key, e := rootCA(dir)
	if e != nil {
		return e
	}
	now := time.Now()
	serial, e := serial()
	if e != nil {
		return e
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "localhost"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return e
	}
	der, e := x509.CreateCertificate(rand.Reader, leaf, root, &leafKey.PublicKey, key)
	if e != nil {
		return e
	}
	return save(bundlePath(dir), append(encode("CERTIFICATE", der), encode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(leafKey))...))
}

func rootCA(dir string) (*x509.Certificate, *rsa.PrivateKey, error) {
	private := filepath.Join(dir, "root-private.pem")
	b, e := os.ReadFile(private)
	if errors.Is(e, os.ErrNotExist) {
		if _, e = os.Stat(RootPath(dir)); !errors.Is(e, os.ErrNotExist) {
			return nil, nil, errors.New("找到根证书，但根私钥不可用")
		}
		key, e := rsa.GenerateKey(rand.Reader, 3072)
		if e != nil {
			return nil, nil, e
		}
		serial, e := serial()
		if e != nil {
			return nil, nil, e
		}
		now := time.Now()
		root := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: rootName}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
			PermittedDNSDomainsCritical: true, PermittedDNSDomains: []string{"localhost"}, PermittedIPRanges: []*net.IPNet{{IP: net.IP{127, 0, 0, 1}, Mask: net.CIDRMask(32, 32)}, {IP: net.IPv6loopback, Mask: net.CIDRMask(128, 128)}}}
		der, e := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
		if e != nil {
			return nil, nil, e
		}
		b = append(encode("CERTIFICATE", der), encode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))...)
		if e = save(private, b); e != nil {
			return nil, nil, e
		}
	} else if e != nil {
		return nil, nil, e
	}
	cert, rest := pem.Decode(b)
	key, _ := pem.Decode(rest)
	if cert == nil || key == nil {
		return nil, nil, errors.New("无法读取本机 CA")
	}
	ca, e := x509.ParseCertificate(cert.Bytes)
	if e != nil {
		return nil, nil, e
	}
	rsaKey, e := x509.ParsePKCS1PrivateKey(key.Bytes)
	if e != nil {
		return nil, nil, e
	}
	if time.Until(ca.NotAfter) < 365*24*time.Hour {
		return nil, nil, errors.New("本机 CA 即将到期，请使用新证书目录并更新信任")
	}
	if e = save(RootPath(dir), encode("CERTIFICATE", cert.Bytes)); e != nil {
		return nil, nil, e
	}
	return ca, rsaKey, nil
}
func serial() (*big.Int, error)        { return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128)) }
func encode(t string, b []byte) []byte { return pem.EncodeToMemory(&pem.Block{Type: t, Bytes: b}) }
func save(name string, data []byte) error {
	f, e := os.CreateTemp(filepath.Dir(name), ".cert-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e == nil {
		e = f.Sync()
	}
	if err := f.Close(); e == nil {
		e = err
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), name)
}
