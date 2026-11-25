package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/doctor"
)

func doctorCmd() *cobra.Command {
	return supportsJSON(&cobra.Command{
		Use:   "doctor",
		Short: "Check ports, runtimes, configs and the hosts file, and suggest fixes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			running := call(root, "ping", nil, nil) == nil
			checks := doctor.Run(root, doctor.Options{HostRunning: running})
			if jsonOut {
				out := struct {
					Checks   []doctor.Check `json:"checks"`
					Problems int            `json:"problems"`
					Warnings int            `json:"warnings"`
				}{Checks: checks}
				for _, c := range checks {
					switch c.Level {
					case doctor.Fail:
						out.Problems++
					case doctor.Warn:
						out.Warnings++
					}
				}
				if err := emit(out); err != nil {
					return err
				}
				if out.Problems > 0 {
					os.Exit(1) // same exit code as the text report
				}
				return nil
			}
			marks := map[doctor.Level]string{doctor.OK: "ok  ", doctor.Info: "info", doctor.Warn: "WARN", doctor.Fail: "FAIL"}
			width := 0
			for _, c := range checks {
				if len(c.Name) > width {
					width = len(c.Name)
				}
			}
			fails, warns := 0, 0
			for _, c := range checks {
				fmt.Printf("[%s] %-*s  %s\n", marks[c.Level], width, c.Name, c.Detail)
				if c.Hint != "" && c.Level != doctor.OK {
					fmt.Printf("       %s  -> %s\n", strings.Repeat(" ", width), c.Hint)
				}
				switch c.Level {
				case doctor.Fail:
					fails++
				case doctor.Warn:
					warns++
				}
			}
			fmt.Printf("\n%s, %s.\n", plural(fails, "problem"), plural(warns, "warning"))
			if fails > 0 {
				os.Exit(1)
			}
			return nil
		},
	})
}
