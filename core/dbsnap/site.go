package dbsnap

import (
	"bufio"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	wpNameRe = regexp.MustCompile(`define\(\s*['"]DB_NAME['"]\s*,\s*['"]([^'"]+)['"]`)
	wpHostRe = regexp.MustCompile(`define\(\s*['"]DB_HOST['"]\s*,\s*['"]([^'"]+)['"]`)
)

// SiteDB is the database a site's own settings point at.
type SiteDB struct {
	Name   string // database name
	Port   int    // 0 when the settings name none (the default server is meant)
	Source string // where it came from, e.g. ".env DB_DATABASE"
}

// SiteDatabase reads which database a site uses from its own settings:
// Laravel's .env DB_DATABASE and DB_PORT, Symfony's DATABASE_URL (.env.local,
// then .env), WordPress's wp-config.php DB_NAME and DB_HOST; otherwise the
// name `ebur new` gives it (hyphens become underscores). The port tells
// MariaDB and MySQL apart when both run.
func SiteDatabase(dir, site string) SiteDB {
	for _, f := range []string{".env.local", ".env"} {
		env := readEnv(filepath.Join(dir, f))
		if u := env["DATABASE_URL"]; u != "" {
			if p, err := url.Parse(u); err == nil && (p.Scheme == "mysql" || p.Scheme == "mariadb") {
				if db := strings.Trim(p.Path, "/"); db != "" {
					port, _ := strconv.Atoi(p.Port())
					return SiteDB{db, port, f + " DATABASE_URL"}
				}
			}
		}
		if db := env["DB_DATABASE"]; db != "" {
			conn := strings.ToLower(env["DB_CONNECTION"])
			if conn == "" || conn == "mysql" || conn == "mariadb" {
				port, _ := strconv.Atoi(env["DB_PORT"])
				return SiteDB{db, port, f + " DB_DATABASE"}
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "wp-config.php")); err == nil {
		if m := wpNameRe.FindSubmatch(b); m != nil {
			port := 0
			if h := wpHostRe.FindSubmatch(b); h != nil {
				if _, p, ok := strings.Cut(string(h[1]), ":"); ok {
					port, _ = strconv.Atoi(p)
				}
			}
			return SiteDB{string(m[1]), port, "wp-config.php"}
		}
	}
	return SiteDB{strings.ReplaceAll(site, "-", "_"), 0, "site name"}
}

// readEnv reads KEY=value lines, ignoring comments and quotes.
func readEnv(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}
