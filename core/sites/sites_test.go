package sites

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	www := t.TempDir()
	for _, d := range []string{"Shop/public", "legacy/web", "blog", "bad_name", ".git", "both/public", "both/web"} {
		os.MkdirAll(filepath.Join(www, d), 0o755)
	}
	os.WriteFile(filepath.Join(www, "readme.txt"), nil, 0o644)

	got, skipped, err := Scan(www, "test")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"blog":   filepath.Join(www, "blog"),
		"both":   filepath.Join(www, "both", "public"),
		"legacy": filepath.Join(www, "legacy", "web"),
		"shop":   filepath.Join(www, "Shop", "public"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, s := range got {
		if want[s.Name] != s.DocRoot || s.Host != s.Name+".test" {
			t.Errorf("%+v", s)
		}
	}
	if got[0].Name != "blog" {
		t.Error("not sorted")
	}
	if len(skipped) != 1 || skipped[0].Folder != "bad_name" {
		t.Errorf("skipped = %+v", skipped)
	}
}

func TestScanMissingWWW(t *testing.T) {
	s, sk, err := Scan(filepath.Join(t.TempDir(), "nope"), "test")
	if err != nil || s != nil || sk != nil {
		t.Fatal(s, sk, err)
	}
}
