// Package config loads and saves ebur.json, the global settings file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ebur/ebur/internal/fsutil"
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
	// DBPort is the MariaDB port. Change it when another server (e.g.
	// Laragon's MySQL) already uses 3306.
	DBPort int `json:"db_port"`
	// DBRootPassword is generated when the first database is set up
	// (NFR-09: never blank). Local-only; shown by `ebur db`.
	DBRootPassword string `json:"db_root_password,omitempty"`
}

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
	if s.DBPort < 1024 || s.DBPort > 65535 {
		return fmt.Errorf("config: db_port %d must be between 1024 and 65535", s.DBPort)
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
