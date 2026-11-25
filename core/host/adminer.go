package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/internal/fsutil"
)

// adminerWrapper serves Adminer through its documented customisation hook
// (a global adminer_object()) with the update check turned off. Without
// it, every page asks the browser to load
// https://www.adminer.org/version/?current=<version>, which would tell a
// third party the user's IP address each time they open db.<tld>; Ebur
// promises no such requests (PRIVACY.md, NFR-17). Ebur updates Adminer
// through its signed runtime list instead.
const adminerWrapper = `<?php
// Written by Ebur; regenerated on every start, so edits are overwritten.
// Serves Adminer %s with its update check off: opening this page never
// contacts adminer.org. Ebur updates Adminer itself.
function adminer_object() {
    class EburAdminer extends \Adminer\Adminer {
        function verifyVersion() {
            return false;
        }
    }
    return new EburAdminer;
}
include '%s';
`

// writeAdminer writes etc\adminer\index.php for the given Adminer version
// and returns the folder nginx should serve as db.<tld>.
func writeAdminer(root paths.Root, version string) (string, error) {
	dir := filepath.Join(root.Etc(), "adminer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(root.RuntimeDir("adminer", version), "index.php")
	quoted := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(target) // PHP single-quoted string
	php := fmt.Sprintf(adminerWrapper, version, quoted)
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, "index.php"), []byte(php), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}
