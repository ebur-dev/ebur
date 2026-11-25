// Command ebur is the Ebur CLI (SRS §4.2). Commands that change running
// services go through the Ebur host over IPC, starting it in the
// background when needed, so the CLI and tray app always agree.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/ipc"
	"github.com/ebur-dev/ebur/core/paths"
)

var version = "dev"

func main() {
	host.Version = version
	root := &cobra.Command{
		Use:           "ebur",
		Short:         "Ebur: local PHP development environment",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		statusCmd(), startCmd(), stopCmd(), restartCmd(), quitCmd(),
		phpCmd(), installCmd(), sitesCmd(), dbCmd(), mailCmd(), sslCmd(), doctorCmd(),
		terminalCmd(), execCmd(), hostsCmd(), setupCmd(), daemonCmd(),
	)
	if err := root.Execute(); err != nil {
		printError(err)
		os.Exit(1)
	}
}

// printError shows the error and, when known, a next step (NFR-14).
func printError(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	var ie *ipc.Error
	var he interface{ Hint() string }
	switch {
	case errors.As(err, &ie) && ie.Hint != "":
		fmt.Fprintln(os.Stderr, "next step:", ie.Hint)
	case errors.As(err, &he) && he.Hint() != "":
		fmt.Fprintln(os.Stderr, "next step:", he.Hint())
	}
}

func findRoot() (paths.Root, error) {
	r, err := paths.Find()
	if err != nil {
		return r, err
	}
	return r, r.Ensure()
}

// signalContext is cancelled on Ctrl+C.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

type hintErr struct{ msg, hint string }

func (e *hintErr) Error() string { return e.msg }
func (e *hintErr) Hint() string  { return e.hint }

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func joinOr(list []string, empty string) string {
	if len(list) == 0 {
		return empty
	}
	return strings.Join(list, ", ")
}
