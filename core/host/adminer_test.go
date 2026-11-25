package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/paths"
)

func TestAdminerWrapperTurnsOffUpdateCheck(t *testing.T) {
	root, _ := paths.New(filepath.Join(t.TempDir(), "it's here")) // a quote in the path must not break PHP
	dir, err := writeAdminer(root, "6.3.1")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root.Etc(), "adminer") {
		t.Fatalf("served from %s", dir)
	}
	b, err := os.ReadFile(filepath.Join(dir, "index.php"))
	if err != nil {
		t.Fatal(err)
	}
	php := string(b)
	target := filepath.Join(root.RuntimeDir("adminer", "6.3.1"), "index.php")
	quoted := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(target)
	for _, want := range []string{
		"function adminer_object()",
		`extends \Adminer\Adminer`,
		"function verifyVersion()",
		"return false;",
		"include '" + quoted + "';",
	} {
		if !strings.Contains(php, want) {
			t.Errorf("wrapper lacks %q:\n%s", want, php)
		}
	}
}
