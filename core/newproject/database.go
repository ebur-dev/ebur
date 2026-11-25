package newproject

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/ebur-dev/ebur/core/dbclient"
	"github.com/ebur-dev/ebur/core/paths"
)

// database wraps the shared MariaDB client with new-project wording.
type database struct{ c *dbclient.Client }

func openDatabase(root paths.Root) (*database, error) {
	c, err := dbclient.Open(root)
	if errors.Is(err, dbclient.ErrNotInstalled) {
		return nil, errors.New("MariaDB is not installed; run `ebur install mariadb`, or create a blank project")
	}
	if err != nil {
		return nil, err
	}
	return &database{c}, nil
}

var dbNameRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

func (d *database) ping(ctx context.Context) error {
	if err := d.c.Ping(ctx); err != nil && !errors.Is(err, dbclient.ErrNotRunning) {
		return fmt.Errorf("cannot reach the database: %w", err)
	} else if err != nil {
		return err
	}
	return nil
}

func (d *database) create(ctx context.Context, name string) error {
	if !dbNameRe.MatchString(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	err := d.c.Create(ctx, name)
	if errors.Is(err, dbclient.ErrExists) {
		return fmt.Errorf("a database named %s already exists; Ebur will not reuse or delete it. Drop it in Adminer if it is unwanted, or choose another name", name)
	}
	if err != nil {
		return fmt.Errorf("create database %s: %w", name, err)
	}
	return nil
}

// drop removes a database Create made moments ago, when a later step failed.
func (d *database) drop(ctx context.Context, name string) {
	if dbNameRe.MatchString(name) {
		d.c.Drop(ctx, name)
	}
}
