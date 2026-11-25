// Package dbclient runs SQL, dumps and loads against Ebur's database
// servers (MariaDB, and MySQL next to it) through each server's own
// command-line tools, as root on 127.0.0.1.
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
	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/provision"
	"github.com/ebur-dev/ebur/internal/proc"
)

// ErrNotInstalled means no database server is installed.
var ErrNotInstalled = errors.New("no database server is installed; run `ebur install mariadb` or `ebur install mysql 8.4`")

// ErrNotRunning means the server did not answer.
var ErrNotRunning = errors.New("the database is not running; start Ebur first (`ebur start`)")

// Client talks to one database server.
type Client struct {
	Server   dbserver.Server
	Version  string
	password string
	cnf      string // MySQL: client option file holding the login
}

// Open finds the default server (see dbserver.Default). It does not connect.
func Open(root paths.Root) (*Client, error) {
	cfg, err := config.Load(root.Config())
	if err != nil {
		return nil, err
	}
	s, ok := dbserver.Default(root, cfg)
	if !ok {
		return nil, ErrNotInstalled
	}
	return OpenServer(root, cfg, s)
}

// OpenKind opens the installed server of kind.
func OpenKind(root paths.Root, kind dbserver.Kind) (*Client, error) {
	cfg, err := config.Load(root.Config())
	if err != nil {
		return nil, err
	}
	s, ok := dbserver.Get(root, cfg, kind)
	if !ok {
		return nil, fmt.Errorf("%s is not installed; run `ebur install %s`", kind.Name(), kind)
	}
	return OpenServer(root, cfg, s)
}

// OpenServer opens a known server.
func OpenServer(root paths.Root, cfg config.Settings, s dbserver.Server) (*Client, error) {
	c := &Client{Server: s, Version: s.Version, password: cfg.DBRootPassword}
	if s.Kind == dbserver.MySQL {
		// MYSQL_PWD is deprecated in MySQL 8: its tools read the login
		// from an option file instead.
		cnf, err := provision.WriteClientCnf(root, s.Kind, s.Version, s.Port, cfg.DBRootPassword)
		if err != nil {
			return nil, err
		}
		c.cnf = cnf
	}
	return c, nil
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_$-]{1,64}$`)

// ValidName reports whether name is a database name Ebur will handle
// (letters, digits, _, $ and -), so it is always safe inside backticks.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// System databases are never listed, dumped over or restored.
var system = map[string]bool{"mysql": true, "information_schema": true, "performance_schema": true, "sys": true}

// IsSystem reports whether name is one of MariaDB's own databases.
func IsSystem(name string) bool { return system[strings.ToLower(name)] }

func (c *Client) command(ctx context.Context, tool dbserver.Tool, args ...string) *exec.Cmd {
	var base []string
	if c.cnf != "" {
		base = []string{"--defaults-extra-file=" + filepath.ToSlash(c.cnf)} // must be the first option
	} else {
		base = []string{"--host=127.0.0.1", fmt.Sprintf("--port=%d", c.Server.Port), "--user=root"}
	}
	cmd := exec.CommandContext(ctx, c.Server.ToolPath(tool), append(base, args...)...)
	cmd.Env = os.Environ()
	if c.cnf == "" {
		cmd.Env = append(cmd.Env, "MYSQL_PWD="+c.password) // keeps it off the command line
	}
	proc.Hide(cmd)
	return cmd
}

// Exec runs SQL and returns its tab-separated output.
func (c *Client) Exec(ctx context.Context, sql string) (string, error) {
	cmd := c.command(ctx, dbserver.Client, "--batch", "--skip-column-names", "-e", sql)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", c.explain(stderr.String(), err)
	}
	// MySQL's client ends lines with \r\n on Windows, MariaDB's with \n.
	return strings.TrimSpace(strings.ReplaceAll(out.String(), "\r\n", "\n")), nil
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
	cmd := c.command(ctx, dbserver.Dump, "--single-transaction", "--routines", "--triggers", "--events",
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
	cmd := c.command(ctx, dbserver.Client, "--default-character-set=utf8mb4", "--database="+name)
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
