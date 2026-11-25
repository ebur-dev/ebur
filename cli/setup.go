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
		noMail, withNode bool
		withRedis        bool
		tld              string
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
			ch := setup.Choices{PHP: php, DB: db, DBPort: dbPort, Adminer: !noAdminer, Mail: !noMail, Node: withNode, Redis: withRedis, HTTPS: !noHTTPS, TLD: strings.TrimPrefix(tld, ".")}
			if ch.TLD == "" {
				ch.TLD = opts.TLD // .localhost when the hosts file looks locked
			}
			if ch.TLD != "test" && ch.TLD != "localhost" {
				return &hintErr{fmt.Sprintf("--tld %q is not supported", tld), "Use --tld test or --tld localhost."}
			}
			if ch.PHP == "" {
				ch.PHP = opts.PHPDefault
			}
			switch {
			case ch.DB == "":
				ch.DB = opts.DBDefault
			case ch.DB == "none":
				ch.DB = ""
			case ch.DB == "mysql": // newest supported MySQL (SVC-01)
				ch.DB = setup.MySQLChoice + "8.4"
			case strings.HasPrefix(ch.DB, "mysql:"):
				ch.DB = setup.MySQLChoice + strings.TrimPrefix(ch.DB, "mysql:")
			}
			if ch.DBPort == 0 {
				ch.DBPort = opts.DBPort
			}
			fmt.Printf("Ebur setup in %s\n", opts.Root)
			fmt.Printf("  Web server: %s\n  PHP:        %s\n", opts.WebServer, ch.PHP)
			if ch.DB != "" {
				server := "MariaDB " + ch.DB
				if v, ok := strings.CutPrefix(ch.DB, setup.MySQLChoice); ok {
					server = "MySQL " + v
				}
				fmt.Printf("  Database:   %s on port %d", server, ch.DBPort)
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
			fmt.Printf("  Mail:       %v\n", map[bool]string{true: "captured by Mailpit at mail." + opts.TLD, false: "no capture"}[ch.Mail])
			fmt.Printf("  Node.js:    %v\n", map[bool]string{true: "newest LTS, for npm and Vite", false: "no (add later with `ebur node install 24`, or --node)"}[ch.Node])
			fmt.Printf("  Redis:      %v\n", map[bool]string{true: "yes, on 127.0.0.1:6379, with the phpredis extension", false: "no (add later with `ebur install redis`, or --redis)"}[ch.Redis])
			fmt.Printf("  HTTPS:      %v\n", map[bool]string{true: "yes (Windows will ask to trust Ebur's certificate)", false: "no"}[ch.HTTPS])
			if ch.TLD == "localhost" {
				note := ""
				if opts.HostsBlocked {
					note = " because the hosts file looks locked by security software"
				}
				fmt.Printf("  Sites:      <name>.localhost%s (no hosts file needed; change later with `ebur tld`)\n", note)
			} else {
				fmt.Println("  Sites:      <name>.test (one administrator prompt to update the hosts file)")
			}
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
	c.Flags().StringVar(&db, "db", "", `database: MariaDB version (e.g. 12.3), "mysql" or "mysql:8.4" for MySQL, or "none" (default: newest MariaDB LTS)`)
	c.Flags().IntVar(&dbPort, "db-port", 0, "database port (default: 3306, or the next free one)")
	c.Flags().BoolVar(&noAdminer, "no-adminer", false, "do not install Adminer")
	c.Flags().BoolVar(&noMail, "no-mail", false, "do not install Mailpit (mail capture)")
	c.Flags().BoolVar(&withNode, "node", false, "also install Node.js (newest LTS) for front-end tools")
	c.Flags().BoolVar(&withRedis, "redis", false, "also install Redis and the phpredis extension")
	c.Flags().StringVar(&tld, "tld", "", "site address ending: test, or localhost where the hosts file is locked (default: suggested)")
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
