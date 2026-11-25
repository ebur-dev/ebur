package php

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebur-dev/ebur/internal/fsutil"
)

// Extensions enabled on first install: what Laravel, WordPress and
// Composer need. Users can change them later (PHP-05, v1).
var DefaultExtensions = []string{"curl", "fileinfo", "gd", "intl", "mbstring", "mysqli", "openssl", "pdo_mysql", "pdo_sqlite", "sqlite3", "zip"}

const marker = "; ---- Ebur settings"

// oldNames maps an extension to the file name older PHP builds used:
// PHP 7.4 ships gd as php_gd2.dll (renamed php_gd.dll in 8.0).
var oldNames = map[string]string{"gd": "gd2"}

// extensionFile returns the name to load ext by in this PHP build, if the
// build has it.
func extensionFile(phpDir, ext string) (string, bool) {
	for _, name := range []string{ext, oldNames[ext]} {
		if name == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(phpDir, "ext", "php_"+name+".dll")); err == nil {
			return name, true
		}
	}
	return "", false
}

// iniPath quotes a path for php.ini with forward slashes, which PHP accepts
// on Windows and which need no escaping.
func iniPath(p string) string { return `"` + filepath.ToSlash(p) + `"` }

// EnsureINI creates etc\php\<version>\php.ini from PHP's own
// php.ini-development on first use and returns its path. An existing
// file is never rewritten, so user edits survive version switches (PHP-03).
func EnsureINI(etcDir, phpDir, version, tmpDir string) (string, error) {
	path := filepath.Join(etcDir, "php", version, "php.ini")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	base, err := os.ReadFile(filepath.Join(phpDir, "php.ini-development"))
	if err != nil {
		return "", fmt.Errorf("php %s has no php.ini-development to start from: %w", version, err)
	}
	var b strings.Builder
	b.Write(base)
	// Directives later in the file win, so appending overrides the template
	// without editing it line by line.
	fmt.Fprintf(&b, "\n\n%s (added on first install; safe to edit) ----\n", marker)
	fmt.Fprintf(&b, "extension_dir = %s\n", iniPath(filepath.Join(phpDir, "ext")))
	for _, ext := range DefaultExtensions {
		if name, ok := extensionFile(phpDir, ext); ok {
			fmt.Fprintf(&b, "extension = %s\n", name)
		}
	}
	fmt.Fprintf(&b, "upload_tmp_dir = %s\n", iniPath(tmpDir))
	fmt.Fprintf(&b, "sys_temp_dir = %s\n", iniPath(tmpDir))
	b.WriteString("memory_limit = 512M\nupload_max_filesize = 512M\npost_max_size = 512M\nmax_execution_time = 300\n")
	b.WriteString("cgi.fix_pathinfo = 1\n")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", err
	}
	return path, fsutil.WriteFileAtomic(path, []byte(b.String()), 0o644)
}
