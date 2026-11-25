package newproject

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/internal/proc"
)

// database runs SQL through MariaDB's own client as root.
type database struct {
	client   string
	port     int
	password string
}

func openDatabase(root paths.Root, cfg config.Settings) (*database, error) {
	v := activeOf(root, cfg, "mariadb")
	if v == "" {
		return nil, errors.New("MariaDB is not installed; run `ebur install mariadb`, or create a blank project")
	}
	return &database{
		client:   filepath.Join(root.RuntimeDir("mariadb", v), "bin", proc.Exe("mariadb")),
		port:     cfg.DBPort,
		password: cfg.DBRootPassword,
	}, nil
}

var dbNameRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

func (d *database) exec(ctx context.Context, sql string) (string, error) {
	cmd := exec.CommandContext(ctx, d.client, "--host=127.0.0.1", fmt.Sprintf("--port=%d", d.port),
		"--user=root", "--batch", "--skip-column-names", "-e", sql)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+d.password) // keeps it off the command line
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	proc.Hide(cmd)
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func (d *database) ping(ctx context.Context) error {
	if out, err := d.exec(ctx, "SELECT 1"); err != nil {
		if strings.Contains(out, "2002") || strings.Contains(out, "2003") || strings.Contains(out, "Can't connect") {
			return errors.New("the database is not running; start Ebur first (`ebur start`)")
		}
		return fmt.Errorf("cannot reach the database: %s", firstLine(out, err))
	}
	return nil
}

func (d *database) create(ctx context.Context, name string) error {
	if !dbNameRe.MatchString(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	out, err := d.exec(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	if err != nil {
		if strings.Contains(out, "1007") {
			return fmt.Errorf("a database named %s already exists; Ebur will not reuse or delete it. Drop it in Adminer if it is unwanted, or choose another name", name)
		}
		return fmt.Errorf("create database %s: %s", name, firstLine(out, err))
	}
	return nil
}

// drop removes a database Create made moments ago, when a later step failed.
func (d *database) drop(ctx context.Context, name string) {
	if dbNameRe.MatchString(name) {
		d.exec(ctx, "DROP DATABASE IF EXISTS `"+name+"`")
	}
}

// firstLine is the client's first error line, skipping its warnings
// (e.g. about TLS on a loopback connection).
func firstLine(out string, err error) string {
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "WARNING:") {
			return line
		}
	}
	return err.Error()
}
