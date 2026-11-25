package dbsnap

import (
	"bufio"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var wpNameRe = regexp.MustCompile(`define\(\s*['"]DB_NAME['"]\s*,\s*['"]([^'"]+)['"]`)

// SiteDatabase guesses which database a site uses, from its own settings:
// Laravel's .env DB_DATABASE, Symfony's DATABASE_URL (.env.local, then
// .env), WordPress's wp-config.php DB_NAME; otherwise the name `ebur new`
// gives it (hyphens become underscores). source says where it came from.
func SiteDatabase(dir, site string) (name, source string) {
	for _, f := range []string{".env.local", ".env"} {
		env := readEnv(filepath.Join(dir, f))
		if u := env["DATABASE_URL"]; u != "" {
			if p, err := url.Parse(u); err == nil && (p.Scheme == "mysql" || p.Scheme == "mariadb") {
				if db := strings.Trim(p.Path, "/"); db != "" {
					return db, f + " DATABASE_URL"
				}
			}
		}
		if db := env["DB_DATABASE"]; db != "" {
			conn := strings.ToLower(env["DB_CONNECTION"])
			if conn == "" || conn == "mysql" || conn == "mariadb" {
				return db, f + " DB_DATABASE"
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "wp-config.php")); err == nil {
		if m := wpNameRe.FindSubmatch(b); m != nil {
			return string(m[1]), "wp-config.php"
		}
	}
	return strings.ReplaceAll(site, "-", "_"), "site name"
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
