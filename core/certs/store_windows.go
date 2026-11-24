package certs

import (
	"bytes"
	"crypto/x509"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The CA goes into the current user's Root store: no admin rights
// needed, but Windows shows its own confirmation dialog (SRS §2.4).

func openUserRoot() (windows.Handle, error) {
	name, _ := windows.UTF16PtrFromString("Root")
	return windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM, 0, 0,
		windows.CERT_SYSTEM_STORE_CURRENT_USER, uintptr(unsafe.Pointer(name)))
}

// findInStore returns the store's context for cert, or nil.
func findInStore(store windows.Handle, cert *x509.Certificate) *windows.CertContext {
	var prev *windows.CertContext
	for {
		ctx, err := windows.CertEnumCertificatesInStore(store, prev)
		if err != nil || ctx == nil {
			return nil
		}
		raw := unsafe.Slice(ctx.EncodedCert, ctx.Length)
		if bytes.Equal(raw, cert.Raw) {
			return ctx
		}
		prev = ctx
	}
}

// IsTrusted reports whether the CA is in the current user's Root store.
func (ca *CA) IsTrusted() (bool, error) {
	store, err := openUserRoot()
	if err != nil {
		return false, err
	}
	defer windows.CertCloseStore(store, 0)
	ctx := findInStore(store, ca.Cert)
	if ctx != nil {
		windows.CertFreeCertificateContext(ctx)
		return true, nil
	}
	return false, nil
}

// ErrDeclined means the user clicked No in Windows' confirmation dialog.
var ErrDeclined = errors.New("the certificate change was declined in the Windows dialog")

// Trust adds the CA to the current user's Root store. Windows asks the
// user to confirm; declining returns ErrDeclined.
func (ca *CA) Trust() error {
	if ok, _ := ca.IsTrusted(); ok {
		return nil
	}
	store, err := openUserRoot()
	if err != nil {
		return err
	}
	defer windows.CertCloseStore(store, 0)
	ctx, err := windows.CertCreateCertificateContext(windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING, &ca.Cert.Raw[0], uint32(len(ca.Cert.Raw)))
	if err != nil {
		return err
	}
	defer windows.CertFreeCertificateContext(ctx)
	if err := windows.CertAddCertificateContextToStore(store, ctx, windows.CERT_STORE_ADD_REPLACE_EXISTING, nil); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return ErrDeclined
		}
		return err
	}
	return nil
}

// Untrust removes the CA from the current user's Root store (SSL-03).
func (ca *CA) Untrust() error {
	store, err := openUserRoot()
	if err != nil {
		return err
	}
	defer windows.CertCloseStore(store, 0)
	ctx := findInStore(store, ca.Cert)
	if ctx == nil {
		return nil
	}
	// CertDeleteCertificateFromStore frees ctx, even on failure.
	if err := windows.CertDeleteCertificateFromStore(ctx); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return ErrDeclined
		}
		return err
	}
	return nil
}
