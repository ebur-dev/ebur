package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/shellenv"
)

// terminalCmd is the CLI twin of the window's "Open terminal" (CLI-01).
func terminalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "terminal",
		Short: "Open a console with Ebur's PHP, Composer, Node, database tools and Nginx on PATH",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			dir, _ := os.Getwd()
			return shellenv.Open(root, dir)
		},
	}
}

// execCmd runs one command with Ebur's environment, for scripts and AI
// coding tools: `ebur exec composer install`, `ebur exec php artisan migrate`.
func execCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "exec <command> [args...]",
		Short:              "Run a command with Ebur's runtimes on PATH, e.g. ebur exec composer install",
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true, // pass every flag through to the command
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "-h" || args[0] == "--help" {
				return cmd.Help()
			}
			root, err := findRoot()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			env := shellenv.For(root, cwd) // inside a site: that site's PHP (PHP-04)
			environ := env.Environ(os.Environ())
			// Look the command up on the Ebur PATH, not the parent's.
			path, err := lookPath(args[0], environ)
			if err != nil {
				return &hintErr{fmt.Sprintf("%s not found on Ebur's PATH (%s)", args[0], env.Summary()),
					"Install it, e.g. `ebur install composer`, or check the spelling."}
			}
			c := exec.Command(path, args[1:]...)
			c.Env = environ
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := c.Run(); err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					os.Exit(ee.ExitCode()) // keep the command's exit code for scripts
				}
				return err
			}
			return nil
		},
	}
}

// lookPath resolves name using the PATH and PATHEXT in environ, so the
// command found is Ebur's, not one earlier on the user's own PATH.
func lookPath(name string, environ []string) (string, error) {
	if strings.ContainsAny(name, `/\`) {
		return exec.LookPath(name)
	}
	var pathVal, extVal string
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "PATH":
			pathVal = v
		case "PATHEXT":
			extVal = v
		}
	}
	exts := []string{""}
	if runtime.GOOS == "windows" {
		if extVal == "" {
			extVal = ".COM;.EXE;.BAT;.CMD"
		}
		exts = strings.Split(strings.ToLower(extVal), ";")
		if filepath.Ext(name) != "" {
			exts = append([]string{""}, exts...)
		}
	}
	for _, dir := range filepath.SplitList(pathVal) {
		for _, e := range exts {
			p := filepath.Join(dir, name+e)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, nil
			}
		}
	}
	return "", exec.ErrNotFound
}
