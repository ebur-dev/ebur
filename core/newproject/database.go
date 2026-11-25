package newproject

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/ebur-dev/ebur/core/dbclient"
	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/paths"
)

// database wraps the shared client with new-project wording.
type database struct{ c *dbclient.Client }

// openDatabase opens the server a new project uses: kind ("mariadb" or
// "mysql") when given, otherwise the default server (SVC-01).
func openDatabase(root paths.Root, kind string) (*database, error) {
	var c *dbclient.Client
	var err error
	if kind == "" {
		c, err = dbclient.Open(root)
	} else {
		k, ok := dbserver.ParseKind(kind)
		if !ok {
			return nil, fmt.Errorf("unknown database server %q; use mariadb or mysql", kind)
		}
		c, err = dbclient.OpenKind(root, k)
	}
	if errors.Is(err, dbclient.ErrNotInstalled) {
		return nil, errors.New("no database server is installed; run `ebur install mariadb` (or `ebur install mysql 8.4`), or create a blank project")
	}
	if err != nil {
		return nil, err
	}
	return &database{c}, nil
}

var dbNameRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

func (d *database) ping(ctx context.Context) error {
	if err := d.c.Ping(ctx); errors.Is(err, dbclient.ErrNotRunning) {
		return fmt.Errorf("%s is not running; start Ebur first (`ebur start`)", d.c.Server.Label())
	} else if err != nil {
		return fmt.Errorf("cannot reach %s: %w", d.c.Server.Label(), err)
	}
	return nil
}

func (d *database) create(ctx context.Context, name string) error {
	if !dbNameRe.MatchString(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	err := d.c.Create(ctx, name)
	if errors.Is(err, dbclient.ErrExists) {
		return fmt.Errorf("a database named %s already exists on %s; Ebur will not reuse or delete it. Drop it in Adminer if it is unwanted, or choose another name", name, d.c.Server.Kind.Name())
	}
	if err != nil {
		return fmt.Errorf("create database %s on %s: %w", name, d.c.Server.Kind.Name(), err)
	}
	return nil
}

// drop removes a database Create made moments ago, when a later step failed.
func (d *database) drop(ctx context.Context, name string) {
	if dbNameRe.MatchString(name) {
		d.c.Drop(ctx, name)
	}
}
