package localcert

import (
	"bytes"
	"crypto/rand"
	"crypto/x509"
	"os"
	"testing"
	"time"
)

func TestInitRenewsLeafWithoutChangingBridgeRoot(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	pair, root, err := load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !isBridgeRoot(root) {
		t.Fatal("generated CA is ineligible for automatic installation")
	}
	broader := *root
	broader.PermittedDNSDomains = []string{"localhost", "example.com"}
	if isBridgeRoot(&broader) {
		t.Fatal("CA with an additional DNS name is eligible for automatic installation")
	}
	_, signer, err := rootCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	shortLived := *pair.Leaf
	shortLived.NotAfter = time.Now().Add(time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, &shortLived, root, pair.Leaf.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundlePath(dir), append(encode("CERTIFICATE", der), encode("PRIVATE KEY", key)...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	renewed, existingRoot, err := load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(existingRoot.Raw, root.Raw) || time.Until(renewed.Leaf.NotAfter) <= 30*24*time.Hour {
		t.Fatal("expected a renewed leaf signed by the existing CA")
	}
}
