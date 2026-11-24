package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

func newCA(t *testing.T) (Dir, *CA) {
	t.Helper()
	d := Dir(t.TempDir())
	ca, err := d.CreateCA("test")
	if err != nil {
		t.Fatal(err)
	}
	return d, ca
}

func pool(ca *CA) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.Cert)
	return p
}

func TestSiteCertVerifies(t *testing.T) {
	d, ca := newCA(t)
	certPath, keyPath, err := d.EnsureSite(ca, "shop.test", "http://127.0.0.1"+CRLPath)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.Certificate) != 2 {
		t.Fatalf("chain has %d certs, want leaf + CA", len(pair.Certificate))
	}
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: "shop.test", Roots: pool(ca)}); err != nil {
		t.Fatal(err)
	}
	if len(leaf.CRLDistributionPoints) != 1 || !strings.HasSuffix(leaf.CRLDistributionPoints[0], CRLPath) {
		t.Fatalf("CRL DP: %v", leaf.CRLDistributionPoints)
	}
	if time.Until(leaf.NotAfter) > 398*24*time.Hour {
		t.Fatal("leaf validity over 398 days")
	}
	if fi, _ := os.Stat(keyPath); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatal("site key readable by others")
	}
}

// The name constraint is the safety net: a leaked CA key must not be able
// to mint trusted certificates for real domains.
func TestNameConstraintsBlockPublicNames(t *testing.T) {
	d, ca := newCA(t)
	if _, _, err := d.EnsureSite(ca, "bank.com", ""); err == nil {
		t.Fatal("EnsureSite accepted a public name")
	}
	// Forge one directly with the CA key, as an attacker with the key would.
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "bank.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"bank.com"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := x509.ParseCertificate(der)
	if _, err := forged.Verify(x509.VerifyOptions{DNSName: "bank.com", Roots: pool(ca)}); err == nil {
		t.Fatal("forged bank.com certificate verified; name constraints not enforced")
	}
	for _, ok := range []string{"localhost", "a.b.test"} {
		if !ca.Permits(ok) {
			t.Errorf("%s should be permitted", ok)
		}
	}
	if ca.Permits("test.com") || ca.Permits("nottest") {
		t.Error("constraint matched a non-local name")
	}
}

func TestReuseAndRenew(t *testing.T) {
	d, ca := newCA(t)
	p1, _, _ := d.EnsureSite(ca, "blog.test", "")
	b1, _ := os.ReadFile(p1)
	p2, _, _ := d.EnsureSite(ca, "blog.test", "")
	b2, _ := os.ReadFile(p2)
	if string(b1) != string(b2) {
		t.Fatal("valid certificate was reissued")
	}
	// A certificate from another CA is replaced.
	d2, other := newCA(t)
	foreign, _, _ := d2.EnsureSite(other, "blog.test", "")
	fb, _ := os.ReadFile(foreign)
	os.WriteFile(d.SiteCert("blog.test"), fb, 0o644)
	p3, _, _ := d.EnsureSite(ca, "blog.test", "")
	b3, _ := os.ReadFile(p3)
	if string(b3) == string(fb) {
		t.Fatal("certificate from another CA was kept")
	}
}

func TestLoadAndRefuseOverwrite(t *testing.T) {
	d, ca := newCA(t)
	loaded, err := d.LoadCA()
	if err != nil || loaded.Fingerprint() != ca.Fingerprint() || len(ca.Fingerprint()) != 64 {
		t.Fatal(err)
	}
	if _, err := d.CreateCA("test"); err == nil {
		t.Fatal("existing CA overwritten")
	}
	if _, err := Dir(t.TempDir()).LoadCA(); err != ErrNoCA {
		t.Fatalf("want ErrNoCA, got %v", err)
	}
	if !loaded.Cert.IsCA || !loaded.Cert.MaxPathLenZero {
		t.Fatal("CA flags wrong")
	}
}

func TestCRLAndBundle(t *testing.T) {
	d, ca := newCA(t)
	p, err := d.EnsureCRL(ca)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	crl, err := x509.ParseRevocationList(b)
	if err != nil || crl.CheckSignatureFrom(ca.Cert) != nil || len(crl.RevokedCertificateEntries) != 0 {
		t.Fatal("bad CRL", err)
	}
	if p2, _ := d.EnsureCRL(ca); p2 != p {
		t.Fatal(p2)
	}
	bundle, err := d.WriteBundle(ca, []byte("PUBLIC ROOTS\n"))
	if err != nil {
		t.Fatal(err)
	}
	bb, _ := os.ReadFile(bundle)
	if !strings.HasPrefix(string(bb), "PUBLIC ROOTS") || !strings.Contains(string(bb), "BEGIN CERTIFICATE") {
		t.Fatalf("%s", bb)
	}
	if err := d.Remove(); err != nil || d.Exists() {
		t.Fatal("Remove left the CA", err)
	}
}
