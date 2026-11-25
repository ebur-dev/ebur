// Package dbclient runs SQL, dumps and loads against Ebur's MariaDB
// through MariaDB's own command-line tools, as root on 127.0.0.1.
package dbclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/internal/proc"
)

// ErrNotInstalled means no MariaDB is installed.
var ErrNotInstalled = errors.New("MariaDB is not installed; run `ebur install mariadb`")

// ErrNotRunning means the server did not answer.
var ErrNotRunning = errors.New("the database is not running; start Ebur first (`ebur start`)")

// Client talks to the active MariaDB.
type Client struct {
	bin      string // MariaDB's bin folder
	Version  string
	port     int
	password string
}

// Open finds the active MariaDB. It does not connect.
func Open(root paths.Root) (*Client, error) {
	cfg, err := config.Load(root.Config())
	if err != nil {
		return nil, err
	}
	v := cfg.Active["mariadb"]
	if v == "" {
		if inst, _ := runtimes.Installed(root, "mariadb"); len(inst) > 0 {
			v = inst[0]
		}
	}
	if v == "" {
		return nil, ErrNotInstalled
	}
	return &Client{bin: filepath.Join(root.RuntimeDir("mariadb", v), "bin"), Version: v, port: cfg.DBPort, password: cfg.DBRootPassword}, nil
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_$-]{1,64}$`)

// ValidName reports whether name is a database name Ebur will handle
// (letters, digits, _, $ and -), so it is always safe inside backticks.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// System databases are never listed, dumped over or restored.
var system = map[string]bool{"mysql": true, "information_schema": true, "performance_schema": true, "sys": true}

// IsSystem reports whether name is one of MariaDB's own databases.
func IsSystem(name string) bool { return system[strings.ToLower(name)] }

func (c *Client) command(ctx context.Context, tool string, args ...string) *exec.Cmd {
	base := []string{"--host=127.0.0.1", fmt.Sprintf("--port=%d", c.port), "--user=root"}
	cmd := exec.CommandContext(ctx, filepath.Join(c.bin, proc.Exe(tool)), append(base, args...)...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+c.password) // keeps it off the command line
	proc.Hide(cmd)
	return cmd
}

// Exec runs SQL and returns its tab-separated output.
func (c *Client) Exec(ctx context.Context, sql string) (string, error) {
	cmd := c.command(ctx, "mariadb", "--batch", "--skip-column-names", "-e", sql)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", c.explain(stderr.String(), err)
	}
	return strings.TrimSpace(out.String()), nil
}

// explain turns client output into an error, recognising "not running".
func (c *Client) explain(stderr string, err error) error {
	if strings.Contains(stderr, "2002") || strings.Contains(stderr, "2003") || strings.Contains(stderr, "Can't connect") {
		return ErrNotRunning
	}
	return errors.New(FirstLine(stderr, err))
}

// Ping checks that the server answers.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Exec(ctx, "SELECT 1")
	return err
}

// Database describes one user database.
type Database struct {
	Name   string `json:"name"`
	Tables int    `json:"tables"`
	Bytes  int64  `json:"bytes"`
}

// Databases lists user databases with their table count and size.
func (c *Client) Databases(ctx context.Context) ([]Database, error) {
	out, err := c.Exec(ctx, `SELECT s.SCHEMA_NAME, COUNT(t.TABLE_NAME), COALESCE(SUM(t.DATA_LENGTH + t.INDEX_LENGTH), 0)
FROM information_schema.SCHEMATA s LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = s.SCHEMA_NAME
GROUP BY s.SCHEMA_NAME ORDER BY s.SCHEMA_NAME`)
	if err != nil {
		return nil, err
	}
	dbs := []Database{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 || IsSystem(f[0]) {
			continue
		}
		n, _ := strconv.Atoi(f[1])
		b, _ := strconv.ParseInt(f[2], 10, 64)
		dbs = append(dbs, Database{Name: f[0], Tables: n, Bytes: b})
	}
	return dbs, nil
}

// Exists reports whether a database exists.
func (c *Client) Exists(ctx context.Context, name string) (bool, error) {
	dbs, err := c.Databases(ctx)
	if err != nil {
		return false, err
	}
	for _, d := range dbs {
		if d.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// ErrExists is returned by Create when the database already exists.
var ErrExists = errors.New("database exists")

// Create makes an empty utf8mb4 database.
func (c *Client) Create(ctx context.Context, name string) error {
	if !ValidName(name) || IsSystem(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	_, err := c.Exec(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	if err != nil && strings.Contains(err.Error(), "1007") {
		return fmt.Errorf("%w: %s", ErrExists, name)
	}
	return err
}

// Drop deletes a database. It refuses MariaDB's system databases.
func (c *Client) Drop(ctx context.Context, name string) error {
	if !ValidName(name) || IsSystem(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	_, err := c.Exec(ctx, "DROP DATABASE IF EXISTS `"+name+"`")
	return err
}

// Dump writes a consistent SQL dump of one database to w: tables, data,
// routines, triggers and events, without CREATE DATABASE, so it can be
// loaded into a database of any name.
func (c *Client) Dump(ctx context.Context, name string, w io.Writer) error {
	if !ValidName(name) || IsSystem(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	cmd := c.command(ctx, "mariadb-dump", "--single-transaction", "--routines", "--triggers", "--events",
		"--hex-blob", "--default-character-set=utf8mb4", "--skip-dump-date", "--", name)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = w, &stderr
	if err := cmd.Run(); err != nil {
		return c.explain(stderr.String(), err)
	}
	return nil
}

// Load runs the SQL from r inside database name.
func (c *Client) Load(ctx context.Context, name string, r io.Reader) error {
	if !ValidName(name) || IsSystem(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	cmd := c.command(ctx, "mariadb", "--default-character-set=utf8mb4", "--database="+name)
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stderr = r, &stderr
	if err := cmd.Run(); err != nil {
		return c.explain(stderr.String(), err)
	}
	return nil
}

// FirstLine is the client's first error line, skipping its warnings
// (e.g. about TLS on a loopback connection).
// The client may echo the failing statement between "-----" lines first,
// so an "ERROR ..." line wins when there is one.
func FirstLine(out string, err error) string {
	first := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ERROR") {
			return line
		}
		if first == "" && line != "" && !strings.HasPrefix(line, "WARNING:") && strings.Trim(line, "-") != "" {
			first = line
		}
	}
	if first != "" {
		return first
	}
	return err.Error()
}
