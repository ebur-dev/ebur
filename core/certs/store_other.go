//go:build !windows

package certs

import "errors"

// TODO(v2): macOS keychain (security add-trusted-cert) and Linux
// (update-ca-certificates / NSS) trust stores.
var errUnsupported = errors.New("trusting the local CA is only implemented on Windows so far")

var ErrDeclined = errors.New("the certificate change was declined")

func (ca *CA) IsTrusted() (bool, error) { return false, errUnsupported }
func (ca *CA) Trust() error             { return errUnsupported }
func (ca *CA) Untrust() error           { return errUnsupported }
