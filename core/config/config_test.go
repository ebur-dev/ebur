package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "ebur.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.WebServer != "nginx" || s.PHPPoolSize != 4 || s.TLD != "test" || s.Active == nil {
		t.Fatalf("unexpected defaults: %+v", s)
	}
}

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ebur.json")
	in := Defaults()
	in.Active["php"] = "8.4.28"
	in.AutoStart = []string{"nginx", "mariadb"}
	if err := Save(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Active["php"] != "8.4.28" || len(out.AutoStart) != 2 {
		t.Fatalf("round trip lost data: %+v", out)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestPartialFileGetsDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ebur.json")
	os.WriteFile(p, []byte(`{"active":{"php":"8.3.37"}}`), 0o644)
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.WebServer != "nginx" || s.PHPPoolSize != 4 || s.Active["php"] != "8.3.37" {
		t.Fatalf("%+v", s)
	}
}

func TestInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ebur.json")
	os.WriteFile(p, []byte(`{not json`), 0o644)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("want JSON error, got %v", err)
	}
	os.WriteFile(p, []byte(`{"web_server":"iis"}`), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("unknown web server accepted")
	}
	bad := Defaults()
	bad.PHPPoolSize = 100
	if err := Save(p, bad); err == nil {
		t.Fatal("oversized pool accepted")
	}
	bad = Defaults()
	bad.DBPort = 80
	if err := Save(p, bad); err == nil {
		t.Fatal("privileged db_port accepted")
	}
	bad = Defaults()
	bad.Theme = "purple"
	if err := Save(p, bad); err == nil {
		t.Fatal("unknown theme accepted")
	}
	if d := Defaults(); d.DBPort != 3306 {
		t.Fatal(d.DBPort)
	}
	// SITE-04: only the endings the hosts helper accepts.
	for _, tld := range []string{"dev", "com", "local"} {
		bad = Defaults()
		bad.TLD = tld
		if err := Save(p, bad); err == nil {
			t.Errorf("tld %q accepted", tld)
		}
	}
	ok := Defaults()
	ok.TLD = "localhost"
	if err := Save(p, ok); err != nil {
		t.Errorf("localhost rejected: %v", err)
	}
	if !SelfResolving("localhost") || SelfResolving("test") {
		t.Error("SelfResolving")
	}
	// MySQL alone may use 3306 like MariaDB's default (SVC-01).
	ok = Defaults()
	ok.MySQLPort = ok.DBPort
	if err := Save(p, ok); err != nil {
		t.Errorf("mysql_port equal to db_port rejected: %v", err)
	}
}
