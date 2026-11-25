// Package dbsnap saves and restores named database snapshots (TOOL-05):
// gzip-compressed mariadb-dump files in data\snapshots\<database>\.
//
// A restore first saves the current data as a "before-restore" snapshot,
// and puts it back by itself if loading the chosen snapshot fails, so a
// restore never loses data.
package dbsnap

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ebur-dev/ebur/core/dbclient"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/internal/fsutil"
)

// Snapshot is one saved copy of a database.
type Snapshot struct {
	Database string    `json:"database"`
	Name     string    `json:"name"`  // e.g. 2026-11-25_143012_before-upgrade
	Label    string    `json:"label"` // e.g. before-upgrade; "" if none
	Time     time.Time `json:"time"`
	Bytes    int64     `json:"bytes"` // compressed size on disk
	Auto     bool      `json:"auto"`  // saved by Ebur before a restore
	Path     string    `json:"path"`
}

const (
	ext        = ".sql.gz"
	timeLayout = "2006-01-02_150405"
	// AutoLabel marks the copy saved before each restore.
	AutoLabel = "before-restore"
)

var (
	labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
	nameRe  = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}_\d{6})(?:_([a-z0-9-]{1,40}))?$`)
)

// Dir is where snapshots are kept.
func Dir(root paths.Root) string { return filepath.Join(root.Data(), "snapshots") }

func dbDir(root paths.Root, db string) (string, error) {
	if !dbclient.ValidName(db) || dbclient.IsSystem(db) {
		return "", fmt.Errorf("invalid database name %q", db)
	}
	return filepath.Join(Dir(root), db), nil
}

// CleanLabel turns free text ("Before upgrade!") into a label
// ("before-upgrade"), or reports why it cannot.
func CleanLabel(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	if s != "" && !labelRe.MatchString(out) {
		return "", fmt.Errorf("label %q has no letters or digits", s)
	}
	return out, nil
}

// Save dumps db into a new snapshot with an optional label.
func Save(ctx context.Context, c *dbclient.Client, root paths.Root, db, label string) (Snapshot, error) {
	dir, err := dbDir(root, db)
	if err != nil {
		return Snapshot{}, err
	}
	if label, err = CleanLabel(label); err != nil {
		return Snapshot{}, err
	}
	if ok, err := c.Exists(ctx, db); err != nil {
		return Snapshot{}, err
	} else if !ok {
		return Snapshot{}, fmt.Errorf("there is no database named %s", db)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Snapshot{}, err
	}
	now := time.Now()
	name := now.Format(timeLayout)
	if label != "" {
		name += "_" + label
	}
	path := filepath.Join(dir, name+ext)
	if _, err := os.Stat(path); err == nil { // two snapshots in the same second
		time.Sleep(time.Second)
		return Save(ctx, c, root, db, label)
	}
	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return Snapshot{}, err
	}
	zw := gzip.NewWriter(f)
	zw.Name = db + ".sql"
	bw := bufio.NewWriterSize(zw, 1<<16)
	err = c.Dump(ctx, db, bw)
	err = errors.Join(err, bw.Flush(), zw.Close(), f.Sync(), f.Close())
	if err != nil {
		os.Remove(part)
		return Snapshot{}, fmt.Errorf("snapshot %s: %w", db, err)
	}
	if err := fsutil.Rename(part, path); err != nil {
		os.Remove(part)
		return Snapshot{}, err
	}
	return describe(db, path)
}

func describe(db, path string) (Snapshot, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Snapshot{}, err
	}
	name := strings.TrimSuffix(filepath.Base(path), ext)
	m := nameRe.FindStringSubmatch(name)
	if m == nil {
		return Snapshot{}, fmt.Errorf("not a snapshot name: %s", name)
	}
	t, err := time.ParseInLocation(timeLayout, m[1], time.Local)
	if err != nil {
		t = fi.ModTime()
	}
	return Snapshot{Database: db, Name: name, Label: m[2], Time: t, Bytes: fi.Size(), Auto: m[2] == AutoLabel, Path: path}, nil
}

// List returns db's snapshots, newest first. db "" lists every database's.
func List(root paths.Root, db string) ([]Snapshot, error) {
	var dbs []string
	if db != "" {
		if _, err := dbDir(root, db); err != nil {
			return nil, err
		}
		dbs = []string{db}
	} else {
		entries, err := os.ReadDir(Dir(root))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && dbclient.ValidName(e.Name()) {
				dbs = append(dbs, e.Name())
			}
		}
	}
	out := []Snapshot{}
	for _, d := range dbs {
		files, err := filepath.Glob(filepath.Join(Dir(root), d, "*"+ext))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if s, err := describe(d, f); err == nil {
				out = append(out, s)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Time.Equal(out[j].Time) {
			return out[i].Time.After(out[j].Time)
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// Find picks one of db's snapshots by full name, "latest", a label, or a
// unique start of the name such as "2026-11-25_14".
func Find(root paths.Root, db, ref string) (Snapshot, error) {
	all, err := List(root, db)
	if err != nil {
		return Snapshot{}, err
	}
	if len(all) == 0 {
		return Snapshot{}, fmt.Errorf("%s has no snapshots yet", db)
	}
	ref = strings.TrimSuffix(strings.TrimSpace(ref), ext)
	if ref == "latest" {
		return all[0], nil
	}
	var hits []Snapshot
	for _, s := range all {
		if s.Name == ref {
			return s, nil
		}
		if s.Label == ref || strings.HasPrefix(s.Name, ref) {
			hits = append(hits, s)
		}
	}
	switch {
	case len(hits) == 1:
		return hits[0], nil
	case len(hits) > 1 && hits[0].Label == ref:
		return hits[0], nil // the newest with that label
	case len(hits) > 1:
		return Snapshot{}, fmt.Errorf("%q matches %d snapshots of %s; use the full name", ref, len(hits), db)
	}
	return Snapshot{}, fmt.Errorf("%s has no snapshot %q", db, ref)
}

// RestoreResult says what Restore did.
type RestoreResult struct {
	Restored Snapshot  `json:"restored"`
	Backup   *Snapshot `json:"backup"` // the data that was replaced; nil if the database did not exist
}

// Restore replaces db's contents with snapshot ref. The current data is
// saved first; if loading fails it is put back and the error says so.
func Restore(ctx context.Context, c *dbclient.Client, root paths.Root, db, ref string) (RestoreResult, error) {
	snap, err := Find(root, db, ref)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := checkGzip(snap.Path); err != nil {
		return RestoreResult{}, fmt.Errorf("snapshot %s is damaged (%v); nothing was changed", snap.Name, err)
	}
	res := RestoreResult{Restored: snap}
	exists, err := c.Exists(ctx, db)
	if err != nil {
		return res, err
	}
	if exists {
		b, err := Save(ctx, c, root, db, AutoLabel)
		if err != nil {
			return res, fmt.Errorf("could not save the current data first, so nothing was changed: %w", err)
		}
		res.Backup = &b
	}
	if err := replace(ctx, c, db, snap.Path); err != nil {
		if res.Backup == nil {
			c.Drop(context.Background(), db)
			return res, fmt.Errorf("restore %s failed: %w", snap.Name, err)
		}
		// Put the previous data back, even if ctx was cancelled.
		if rerr := replace(context.Background(), c, db, res.Backup.Path); rerr != nil {
			return res, fmt.Errorf("restore %s failed (%v), and putting the previous data back failed too (%v); it is saved in snapshot %s", snap.Name, err, rerr, res.Backup.Name)
		}
		return res, fmt.Errorf("restore %s failed (%v); the previous data was put back", snap.Name, err)
	}
	return res, nil
}

func replace(ctx context.Context, c *dbclient.Client, db, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(bufio.NewReaderSize(f, 1<<16))
	if err != nil {
		return err
	}
	if err := c.Drop(ctx, db); err != nil {
		return err
	}
	if err := c.Create(ctx, db); err != nil {
		return err
	}
	return c.Load(ctx, db, zr)
}

// checkGzip reads the whole file so a truncated snapshot is caught before
// the database is touched.
func checkGzip(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, zr)
	return err
}

// Delete removes one snapshot.
func Delete(root paths.Root, db, ref string) (Snapshot, error) {
	s, err := Find(root, db, ref)
	if err != nil {
		return s, err
	}
	if err := os.Remove(s.Path); err != nil {
		return s, err
	}
	os.Remove(filepath.Dir(s.Path)) // only succeeds when empty
	return s, nil
}
