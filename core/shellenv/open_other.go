//go:build !windows

package shellenv

import (
	"errors"

	"github.com/ebur/ebur/core/paths"
)

// TODO(v2): open the user's terminal app with the environment set.
func Open(paths.Root, string) error {
	return errors.New("opening a terminal is only implemented on Windows so far; use `ebur exec`")
}
