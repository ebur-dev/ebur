// Package ports checks whether a TCP port is free before a service starts
// and, when it is not, names the process holding it (SVC-03).
package ports

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// Owner is the process listening on a port.
type Owner struct {
	PID  int
	Name string // executable name, or a description for system processes
	Path string // full path when it can be read
}

// Conflict describes a port that is already taken.
type Conflict struct {
	Port  int
	Owner Owner // zero PID when the owner could not be found
}

func (c *Conflict) Error() string {
	if c.Owner.PID == 0 {
		return fmt.Sprintf("port %d is already in use by another program", c.Port)
	}
	return fmt.Sprintf("port %d is already in use by %s (PID %d)", c.Port, c.Owner.Name, c.Owner.PID)
}

// Hint suggests a next step, following NFR-14.
func (c *Conflict) Hint() string {
	switch {
	case c.Owner.PID == 4:
		return "Windows' HTTP service (http.sys) holds the port, usually for IIS, Skype for Business or Web Deploy. Stop that service or choose another port in Settings."
	case c.Owner.PID == 0:
		return "Close the other program, or choose another port in Settings."
	}
	return fmt.Sprintf("Close %s, or choose another port in Settings.", c.Owner.Name)
}

// Check returns nil if port is free on 127.0.0.1 and on all interfaces,
// or a *Conflict naming the owner.
func Check(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}
	if owner, found, err := findListener(port); err == nil && found {
		return &Conflict{Port: port, Owner: owner}
	}
	// Fallback (and the only check off Windows): try to bind.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return &Conflict{Port: port}
	}
	ln.Close()
	return nil
}

// CheckAll checks several ports and returns every conflict.
func CheckAll(ports ...int) []*Conflict {
	var out []*Conflict
	for _, p := range ports {
		var c *Conflict
		if err := Check(p); errors.As(err, &c) {
			out = append(out, c)
		}
	}
	return out
}
