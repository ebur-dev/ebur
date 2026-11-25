package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/hostsync"
	"github.com/ebur-dev/ebur/core/setup"
)

// setupCmd is the CLI twin of the window's first-run wizard (CLI-01).
func setupCmd() *cobra.Command {
	var (
		php, db          string
		dbPort           int
		noAdminer        bool
		noHTTPS, noStart bool
	)
	c := &cobra.Command{
		Use:   "setup",
		Short: "First-run setup: install PHP, Nginx, a database and HTTPS, then start",
		Example: "  ebur setup                         recommended choices\n" +
			"  ebur setup --php 8.4 --db none     PHP 8.4, no database\n" +
			"  ebur setup --no-https --no-start",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			opts, err := setup.GetOptions(root)
			if err != nil {
				return err
			}
			ch := setup.Choices{PHP: php, DB: db, DBPort: dbPort, Adminer: !noAdminer, HTTPS: !noHTTPS}
			if ch.PHP == "" {
				ch.PHP = opts.PHPDefault
			}
			switch ch.DB {
			case "":
				ch.DB = opts.DBDefault
			case "none":
				ch.DB = ""
			}
			if ch.DBPort == 0 {
				ch.DBPort = opts.DBPort
			}
			fmt.Printf("Ebur setup in %s\n", opts.Root)
			fmt.Printf("  Web server: %s\n  PHP:        %s\n", opts.WebServer, ch.PHP)
			if ch.DB != "" {
				fmt.Printf("  Database:   MariaDB %s on port %d", ch.DB, ch.DBPort)
				if ch.Adminer {
					fmt.Print(", with Adminer")
				}
				fmt.Println()
				if opts.DBPortNote != "" && dbPort == 0 {
					fmt.Println("              " + opts.DBPortNote)
				}
			} else {
				fmt.Println("  Database:   none")
			}
			fmt.Printf("  HTTPS:      %v\n", map[bool]string{true: "yes (Windows will ask to trust Ebur's certificate)", false: "no"}[ch.HTTPS])
			for _, w := range opts.Warnings {
				fmt.Println("  warning:", w)
			}
			fmt.Println()

			var hooks setup.Hooks
			if !noStart {
				hooks.Start = func() error {
					if err := ensureHost(root); err != nil {
						return err
					}
					var st host.Status
					return call(root, "start", host.Selection{}, &st)
				}
				hooks.Hosts = func() error {
					want, err := hostsync.Wanted(root)
					if err != nil {
						return err
					}
					_, err = hostsync.AutoSync(root, want)
					return explainSyncError(err)
				}
			}
			ctx, cancel := signalContext()
			defer cancel()
			err = setup.Run(ctx, root, ch, hooks, newStepPrinter().print)
			var partial *setup.Partial
			switch {
			case errors.As(err, &partial):
				fmt.Println("\nSetup finished, with notes:")
				for _, n := range partial.Notes {
					fmt.Println("  -", n)
				}
			case err != nil:
				return &hintErr{"setup stopped: " + err.Error(), "Fix the cause and run `ebur setup` again; finished steps are kept."}
			default:
				fmt.Println("\nSetup finished.")
			}
			fmt.Printf("Create a folder in %s\\www and it is served at http://<folder>.%s\n", opts.Root, opts.TLD)
			return nil
		},
	}
	c.Flags().StringVar(&php, "php", "", "PHP version, e.g. 8.4 (default: recommended)")
	c.Flags().StringVar(&db, "db", "", `MariaDB version, e.g. 12.3, or "none" (default: newest LTS)`)
	c.Flags().IntVar(&dbPort, "db-port", 0, "database port (default: 3306, or the next free one)")
	c.Flags().BoolVar(&noAdminer, "no-adminer", false, "do not install Adminer")
	c.Flags().BoolVar(&noHTTPS, "no-https", false, "do not set up local HTTPS")
	c.Flags().BoolVar(&noStart, "no-start", false, "install only; do not start services or edit the hosts file")
	return c
}

// stepPrinter prints a line whenever a step changes state, and download
// progress in 25% steps, so the output stays readable.
type stepPrinter struct {
	mu    sync.Mutex
	state map[string]string
	pct   map[string]int
}

func newStepPrinter() *stepPrinter {
	return &stepPrinter{state: map[string]string{}, pct: map[string]int{}}
}

var marks = map[string]string{"running": "...", "done": " ok", "failed": "ERR", "skipped": " - "}

func (p *stepPrinter) print(steps []setup.Step) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range steps {
		key := s.State + "|" + s.Detail
		if s.Detail == "Downloading" && s.Total > 0 {
			pct := int(s.Done * 100 / s.Total)
			if q := pct / 25 * 25; q > p.pct[s.ID] {
				p.pct[s.ID] = q
				fmt.Printf("[...] %-32s downloading %d%%\n", s.Label, q)
			}
			continue
		}
		if s.State == "pending" || p.state[s.ID] == key {
			continue
		}
		p.state[s.ID] = key
		line := fmt.Sprintf("[%s] %-32s", marks[s.State], s.Label)
		if s.Detail != "" {
			line += " " + strings.ReplaceAll(s.Detail, "\n", " ")
		}
		fmt.Println(strings.TrimRight(line, " "))
	}
}
