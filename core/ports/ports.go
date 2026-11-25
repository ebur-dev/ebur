// Package ports checks whether a TCP port is free before a service starts
// and, when it is not, names the process holding it (SVC-03).
package ports

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
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

// Hint suggests a next step, following NFR-14. It recognises the local
// development stacks people most often have installed already, because
// "close httpd.exe" does not tell anyone where httpd.exe came from.
func (c *Conflict) Hint() string {
	switch {
	case c.Owner.PID == 4:
		return "Windows' HTTP service (http.sys) holds the port, usually for IIS, Skype for Business or Web Deploy. Stop that service (for IIS: the World Wide Web Publishing Service, W3SVC, in services.msc), then try again."
	case c.Owner.PID == 0:
		return "Close the other program, then try again."
	}
	if h := knownStack(c.Owner); h != "" {
		return h
	}
	where := c.Owner.Name
	if c.Owner.Path != "" {
		where = c.Owner.Path
	}
	return fmt.Sprintf("Close %s, then try again.", where)
}

// knownStack explains how to stop a program that belongs to another local
// web stack, or returns "".
func knownStack(o Owner) string {
	p := strings.ToLower(strings.ReplaceAll(o.Path, "/", `\`))
	name := strings.ToLower(o.Name)
	what := "web server"
	switch {
	case strings.HasPrefix(name, "mysqld"), strings.HasPrefix(name, "mariadbd"):
		what = "database"
	case strings.HasPrefix(name, "httpd"):
		what = "Apache web server"
	case strings.HasPrefix(name, "nginx"):
		what = "Nginx web server"
	}
	const after = " Ebur can use the port once it is stopped; the two cannot serve on the same port at the same time."
	switch name {
	case "wslrelay.exe", "wslhost.exe":
		// WSL forwards ports opened inside Linux to Windows; closing the
		// relay does nothing useful.
		return "A program inside WSL (Linux on Windows) is using this port, usually a server in a Docker container or a WSL distro; wslrelay.exe only forwards it to Windows. Stop that server (in Docker Desktop, or for example `wsl -e sudo service redis-server stop`), or run `wsl --shutdown` to stop WSL entirely." + after
	case "com.docker.backend.exe", "vpnkit.exe", "com.docker.proxy.exe", "docker-proxy.exe":
		return "A Docker container publishes this port. Stop it in Docker Desktop (Containers), or with `docker ps` and `docker stop <name>`." + after
	}
	switch {
	case strings.Contains(p, `\xampp`):
		return "This is XAMPP's " + what + ". Stop it in the XAMPP Control Panel, and untick its Autostart there so it does not come back after a restart." + after
	case strings.Contains(p, `\wamp`):
		return "This is WampServer's " + what + ". Right-click the WampServer icon in the tray and choose Stop All Services, or quit WampServer." + after
	case strings.Contains(p, `\laragon`):
		return "This is Laragon's " + what + ". Click Stop All in Laragon, or quit it." + after
	case strings.Contains(p, `\ampps`):
		return "This is AMPPS's " + what + ". Stop it in the AMPPS control panel." + after
	case strings.HasPrefix(name, "httpd"):
		return fmt.Sprintf("This is an Apache web server (%s). If it runs as a Windows service (often named Apache2.4), stop it in Services (services.msc) and set its Startup type to Manual; otherwise close the program that started it.%s", o.Path, after)
	}
	return ""
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
