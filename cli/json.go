package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/ipc"
)

// jsonOut is set by the global --json flag: supported commands print one
// JSON document on stdout for scripts and editor plugins.
var jsonOut bool

// jsonStdout is the real stdout. In --json mode os.Stdout is pointed at
// stderr, so progress text (e.g. a download) can never corrupt the JSON.
var jsonStdout = os.Stdout

const jsonAnnotation = "json"

// supportsJSON marks a command as able to print --json output.
func supportsJSON(c *cobra.Command) *cobra.Command {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[jsonAnnotation] = "yes"
	return c
}

// enableJSON adds --json to root and checks it is only used where supported.
func enableJSON(root *cobra.Command) {
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON (status, sites, doctor, db, db list, db snapshots, share, php list, node list)")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if !jsonOut {
			return nil
		}
		jsonStdout, os.Stdout = os.Stdout, os.Stderr
		if cmd.Annotations[jsonAnnotation] != "yes" {
			return &hintErr{fmt.Sprintf("--json is not available for `%s`", cmd.CommandPath()),
				"It works with: " + strings.Join(jsonCommands(root), ", ") + "."}
		}
		return nil
	}
}

func jsonCommands(root *cobra.Command) []string {
	var out []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Annotations[jsonAnnotation] == "yes" {
			out = append(out, strings.TrimPrefix(c.CommandPath(), "ebur "))
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
	sort.Strings(out)
	return out
}

// emit prints v as indented JSON on the real stdout.
func emit(v any) error {
	enc := json.NewEncoder(jsonStdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// emitError prints an error as JSON: {"error": ..., "next_step": ...}.
func emitError(err error) {
	out := map[string]string{"error": err.Error()}
	var ie *ipc.Error
	var he interface{ Hint() string }
	switch {
	case errors.As(err, &ie) && ie.Hint != "":
		out["next_step"] = ie.Hint
	case errors.As(err, &he) && he.Hint() != "":
		out["next_step"] = he.Hint()
	}
	emit(out)
}
