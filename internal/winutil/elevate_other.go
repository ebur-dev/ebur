//go:build !windows

package winutil

import "errors"

var ErrDeclined = errors.New("the administrator prompt was declined")

// TODO(v2): pkexec / osascript "with administrator privileges".
func RunElevated(string, string) error {
	return errors.New("running as administrator is only implemented on Windows so far")
}
