// Package config loads and saves ebur.json, the global settings file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ebur-dev/ebur/internal/fsutil"
)

// Settings is the content of ebur.json. Fields left out of the file take
// their default values.
type Settings struct {
	// Active maps a runtime name to the version used when a project does
	// not pin one, e.g. {"php": "8.4.28"}.
	Active map[string]string `json:"active"`
	// WebServer is "nginx" (MVP) or "apache" (v1, SVC-02).
	WebServer string `json:"web_server"`
	// AutoStart lists services started when Ebur launches (SVC-06).
	AutoStart []string `json:"auto_start,omitempty"`
	// PHPPoolSize is the number of php-cgi workers per PHP version.
	PHPPoolSize int `json:"php_pool_size"`
	// TLD is the site domain suffix without the dot (SITE-04).
	TLD string `json:"tld"`
	// ManifestURL is where the runtime manifest is fetched from.
	ManifestURL string `json:"manifest_url,omitempty"`
	// SetupDone is set when the first-run setup has finished, so the
	// window stops showing the wizard.
	SetupDone bool `json:"setup_done,omitempty"`
	// ManualHosts stops Ebur from updating the hosts file after a start;
	// `ebur hosts sync` still works (SITE-02).
	ManualHosts bool `json:"manual_hosts,omitempty"`
	// Theme is the window appearance: "system" (follow Windows), "light"
	// or "dark" (GUI-05).
	Theme string `json:"theme,omitempty"`
	// DBPort is the MariaDB port. Change it when another server (e.g.
	// Laragon's MySQL) already uses 3306.
	DBPort int `json:"db_port"`
	// DBRootPassword is generated when the first database is set up
	// (NFR-09: never blank). Local-only; shown by `ebur db`. MariaDB and
	// MySQL share it.
	DBRootPassword string `json:"db_root_password,omitempty"`
	// MySQLPort is MySQL's port when installed next to MariaDB (SVC-01:
	// both can run at once). 0 until MySQL is installed, which picks a
	// free one.
	MySQLPort int `json:"mysql_port,omitempty"`
	// DefaultDB is the server new projects use when both are installed:
	// "mariadb" or "mysql". Empty means MariaDB if installed.
	DefaultDB string `json:"default_db,omitempty"`
}

// SelfResolving reports whether names under tld reach this computer
// without the hosts file: browsers send every *.localhost name to
// 127.0.0.1 themselves (SITE-04). Windows' own resolver, which PHP uses,
// does not, so a site cannot call its own address with it.
func SelfResolving(tld string) bool { return tld == "localhost" }

// Defaults returns the settings used for a fresh install.
func Defaults() Settings {
	return Settings{
		Active:      map[string]string{},
		WebServer:   "nginx",
		PHPPoolSize: 4,
		TLD:         "test",
		DBPort:      3306,
	}
}

func (s *Settings) fillDefaults() {
	d := Defaults()
	if s.Active == nil {
		s.Active = d.Active
	}
	if s.WebServer == "" {
		s.WebServer = d.WebServer
	}
	if s.PHPPoolSize <= 0 {
		s.PHPPoolSize = d.PHPPoolSize
	}
	if s.TLD == "" {
		s.TLD = d.TLD
	}
	if s.DBPort == 0 {
		s.DBPort = d.DBPort
	}
}

// Validate reports settings Ebur cannot run with.
func (s Settings) Validate() error {
	switch s.WebServer {
	case "nginx", "apache":
	default:
		return fmt.Errorf("config: unknown web_server %q (use nginx or apache)", s.WebServer)
	}
	if s.PHPPoolSize > 64 {
		return fmt.Errorf("config: php_pool_size %d is too large (max 64)", s.PHPPoolSize)
	}
	switch s.Theme {
	case "", "system", "light", "dark":
	default:
		return fmt.Errorf("config: unknown theme %q (use system, light or dark)", s.Theme)
	}
	// SITE-04: the elevated hosts helper only accepts these two endings.
	switch s.TLD {
	case "test", "localhost":
	default:
		return fmt.Errorf("config: tld %q is not supported (use test or localhost)", s.TLD)
	}
	if s.DBPort < 1024 || s.DBPort > 65535 {
		return fmt.Errorf("config: db_port %d must be between 1024 and 65535", s.DBPort)
	}
	// mysql_port may equal db_port when MariaDB is not installed (MySQL
	// alone); when both run, the start-up port check reports a clash.
	if s.MySQLPort != 0 && (s.MySQLPort < 1024 || s.MySQLPort > 65535) {
		return fmt.Errorf("config: mysql_port %d must be between 1024 and 65535", s.MySQLPort)
	}
	switch s.DefaultDB {
	case "", "mariadb", "mysql":
	default:
		return fmt.Errorf("config: unknown default_db %q (use mariadb or mysql)", s.DefaultDB)
	}
	return nil
}

// Load reads path. A missing file is not an error: it returns defaults.
func Load(path string) (Settings, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return Settings{}, fmt.Errorf("config: %s is not valid JSON: %w", filepath.Base(path), err)
	}
	s.fillDefaults()
	return s, s.Validate()
}

// Save writes path atomically, so a crash never leaves a half-written file.
func Save(path string, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, append(b, '\n'), 0o644)
}
