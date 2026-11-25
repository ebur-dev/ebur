// Package certs runs Ebur's local certificate authority (SSL-01 to SSL-04).
//
// The CA is created in Go rather than with mkcert, and is name-constrained
// to the local TLD, localhost and loopback addresses: even if its private
// key leaked, it could not be used to impersonate real websites.
//
// Leaf certificates carry a CRL distribution point on 127.0.0.1, served
// by Ebur's Nginx, because Windows' Schannel (used by curl.exe) rejects
// certificates whose revocation status it cannot check.
package certs

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ebur-dev/ebur/internal/fsutil"
)

const (
	caValidity   = 10 * 365 * 24 * time.Hour
	leafValidity = 397 * 24 * time.Hour // under the 398-day limit browsers apply to public CAs
	renewBefore  = 30 * 24 * time.Hour
	crlValidity  = 90 * 24 * time.Hour
	// CRLPath is where Nginx serves the CRL on the default server.
	CRLPath = "/.well-known/ebur/ca.crl"
)

// Dir layout under the Ebur root's ssl folder.
type Dir string

func (d Dir) CACert() string { return filepath.Join(string(d), "ca", "ebur-ca.pem") }
func (d Dir) CAKey() string  { return filepath.Join(string(d), "ca", "ebur-ca-key.pem") }
func (d Dir) CRL() string    { return filepath.Join(string(d), "ca", "ebur-ca.crl") }
func (d Dir) Bundle() string { return filepath.Join(string(d), "php-ca-bundle.pem") }
func (d Dir) SiteCert(host string) string {
	return filepath.Join(string(d), "sites", host+".pem")
}
func (d Dir) SiteKey(host string) string {
	return filepath.Join(string(d), "sites", host+"-key.pem")
}

// CA is a loaded certificate authority.
type CA struct {
	Cert *x509.Certificate
	Key  crypto.Signer
	PEM  []byte
}

// Fingerprint is the SHA-256 of the CA certificate, for display.
func (ca *CA) Fingerprint() string {
	h := sha256.Sum256(ca.Cert.Raw)
	return strings.ToUpper(hex.EncodeToString(h[:]))
}

var ErrNoCA = errors.New("no local CA yet")

// Exists reports whether a CA has been created.
func (d Dir) Exists() bool {
	_, err := os.Stat(d.CACert())
	return err == nil
}

// LoadCA reads the CA, or returns ErrNoCA.
func (d Dir) LoadCA() (*CA, error) {
	certPEM, err := os.ReadFile(d.CACert())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoCA
	}
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(d.CAKey())
	if err != nil {
		return nil, fmt.Errorf("CA key missing: %w", err)
	}
	cert, err := parseCert(certPEM)
	if err != nil {
		return nil, err
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil {
		return nil, errors.New("CA key file is not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, err
	}
	signer, ok := k.(crypto.Signer)
	if !ok {
		return nil, errors.New("CA key cannot sign")
	}
	return &CA{Cert: cert, Key: signer, PEM: certPEM}, nil
}

func parseCert(b []byte) (*x509.Certificate, error) {
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "CERTIFICATE" {
		return nil, errors.New("not a PEM certificate")
	}
	return x509.ParseCertificate(blk.Bytes)
}

func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

func writeKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
}

// CreateCA makes a new CA limited to tld (e.g. "test"), localhost and
// loopback addresses. It refuses to overwrite an existing CA.
func (d Dir) CreateCA(tld string) (*CA, error) {
	if d.Exists() {
		return nil, errors.New("a local CA already exists; uninstall it first")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	sn, err := serial()
	if err != nil {
		return nil, err
	}
	id := hex.EncodeToString(sn.Bytes()[:4])
	now := time.Now()
	_, lo4, _ := net.ParseCIDR("127.0.0.0/8")
	_, lo6, _ := net.ParseCIDR("::1/128")
	tmpl := &x509.Certificate{
		SerialNumber:          sn,
		Subject:               pkix.Name{CommonName: "Ebur Local Development CA " + id, Organization: []string{"Ebur local development"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		// Name constraints: this CA can only vouch for local names.
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{tld, "localhost"},
		PermittedIPRanges:           []*net.IPNet{lo4, lo6},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := writeKey(d.CAKey(), key); err != nil {
		return nil, err
	}
	if err := fsutil.WriteFileAtomic(d.CACert(), certPEM, 0o644); err != nil {
		return nil, err
	}
	return d.LoadCA()
}

// Permits reports whether the CA's name constraints allow host.
func (ca *CA) Permits(host string) bool {
	for _, dom := range ca.Cert.PermittedDNSDomains {
		if host == dom || strings.HasSuffix(host, "."+dom) {
			return true
		}
	}
	return false
}

// EnsureSite returns cert and key paths for host, issuing or renewing the
// certificate when it is missing, expiring, or from another CA.
func (d Dir) EnsureSite(ca *CA, host string, crlURL string) (certPath, keyPath string, err error) {
	certPath, keyPath = d.SiteCert(host), d.SiteKey(host)
	if !ca.Permits(host) {
		return "", "", fmt.Errorf("%s is outside the names the Ebur CA may sign (%s); recreate the CA after changing the TLD", host, strings.Join(ca.Cert.PermittedDNSDomains, ", "))
	}
	if b, err := os.ReadFile(certPath); err == nil {
		if c, err := parseCert(b); err == nil && c.CheckSignatureFrom(ca.Cert) == nil && time.Until(c.NotAfter) > renewBefore {
			if _, err := os.Stat(keyPath); err == nil {
				return certPath, keyPath, nil
			}
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	sn, err := serial()
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: sn,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"Ebur local development"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host},
	}
	if crlURL != "" {
		tmpl.CRLDistributionPoints = []string{crlURL}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return "", "", err
	}
	if err := writeKey(keyPath, key); err != nil {
		return "", "", err
	}
	// Leaf followed by the CA, so clients that lack the root still see the chain.
	chain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), ca.PEM...)
	if err := fsutil.WriteFileAtomic(certPath, chain, 0o644); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

// EnsureCRL writes an empty, signed CRL if missing or close to expiry.
func (d Dir) EnsureCRL(ca *CA) (string, error) {
	path := d.CRL()
	if b, err := os.ReadFile(path); err == nil {
		if crl, err := x509.ParseRevocationList(b); err == nil && crl.CheckSignatureFrom(ca.Cert) == nil && time.Until(crl.NextUpdate) > renewBefore {
			return path, nil
		}
	}
	now := time.Now()
	der, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number:     big.NewInt(now.Unix()),
		ThisUpdate: now.Add(-time.Hour),
		NextUpdate: now.Add(crlValidity),
	}, ca.Cert, ca.Key)
	if err != nil {
		return "", err
	}
	return path, fsutil.WriteFileAtomic(path, der, 0o644)
}

// WriteBundle writes public roots (e.g. curl's cacert.pem) followed by the
// Ebur CA, for PHP's curl.cainfo and openssl.cafile (SSL-04).
func (d Dir) WriteBundle(ca *CA, publicRoots []byte) (string, error) {
	var b bytes.Buffer
	b.Write(bytes.TrimRight(publicRoots, "\r\n"))
	b.WriteString("\n\nEbur Local Development CA\n=========================\n")
	b.Write(ca.PEM)
	path := d.Bundle()
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b.Bytes()) {
		return path, nil
	}
	return path, fsutil.WriteFileAtomic(path, b.Bytes(), 0o644)
}

// Remove deletes the CA, CRL, bundle and every site certificate. Call it
// after removing the CA from the trust store (SSL-03).
func (d Dir) Remove() error {
	for _, p := range []string{filepath.Join(string(d), "ca"), filepath.Join(string(d), "sites"), d.Bundle()} {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}
