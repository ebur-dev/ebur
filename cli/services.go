package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/ipc"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/supervisor"
)

func call(root paths.Root, method string, params, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return ipc.Call(ctx, root, method, params, out)
}

// ensureHost starts the host in the background if it is not running.
func ensureHost(root paths.Root) error {
	if call(root, "ping", nil, nil) == nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(root.Logs(), "ebur-host.log")
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	cmd := exec.Command(exe, "daemon")
	cmd.Env = append(os.Environ(), paths.EnvRoot+"="+root.Dir())
	cmd.Stdout, cmd.Stderr = lf, lf
	if err := startDetached(cmd); err != nil {
		return fmt.Errorf("start ebur host: %w", err)
	}
	cmd.Process.Release()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if call(root, "ping", nil, nil) == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return &hintErr{"ebur host did not start within 10 seconds", "See " + logPath}
}

func selection(args []string) host.Selection { return host.Selection{Services: args} }

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show services and sites",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			var st host.Status
			if err := call(root, "status", nil, &st); errors.Is(err, ipc.ErrNotRunning) {
				fmt.Println("Ebur is not running. Start it with: ebur start")
				return nil
			} else if err != nil {
				return err
			}
			printStatus(st)
			return nil
		},
	}
}

func serviceCmd(use, short, method string, needsHost bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " [service...]",
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			if needsHost {
				if err := ensureHost(root); err != nil {
					return err
				}
			}
			t0 := time.Now()
			var st host.Status
			err = call(root, method, selection(args), &st)
			if errors.Is(err, ipc.ErrNotRunning) {
				fmt.Println("Ebur is not running; nothing to stop.")
				return nil
			}
			if len(st.Services) > 0 {
				printStatus(st)
			}
			if err == nil {
				fmt.Printf("\n%s done in %v\n", method, time.Since(t0).Round(10*time.Millisecond))
				if method == "start" || method == "restart" {
					autoSyncHosts(root) // SITE-02: map new sites, one prompt if needed
				}
			}
			return err
		},
	}
}

func startCmd() *cobra.Command {
	return serviceCmd("start", "Start all services, or the ones named (nginx, php)", "start", true)
}
func stopCmd() *cobra.Command {
	return serviceCmd("stop", "Stop all services, or the ones named", "stop", false)
}
func restartCmd() *cobra.Command {
	return serviceCmd("restart", "Restart services, reloading settings when all restart", "restart", true)
}

func quitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quit",
		Short: "Stop all services and the Ebur host",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			if err := call(root, "shutdown", nil, nil); errors.Is(err, ipc.ErrNotRunning) {
				fmt.Println("Ebur is not running.")
				return nil
			} else if err != nil {
				return err
			}
			fmt.Println("Ebur stopped.")
			return nil
		},
	}
}

func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "daemon",
		Short:  "Run the Ebur host in the foreground (used by the CLI and tray app)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			log.SetFlags(log.LstdFlags)
			ctx, cancel := signalContext()
			defer cancel()
			return host.New(root).Run(ctx)
		},
	}
}

// printStatus groups php-cgi workers into one line per pool.
func printStatus(st host.Status) {
	type group struct {
		name    string
		running int
		total   int
		pids    []string
		errs    []string
	}
	groups := map[string]*group{}
	for _, s := range st.Services {
		name := s.Name
		if i := strings.Index(name, "#"); i > 0 {
			name = name[:i]
		}
		g := groups[name]
		if g == nil {
			g = &group{name: name}
			groups[name] = g
		}
		g.total++
		if s.State == supervisor.Running {
			g.running++
			g.pids = append(g.pids, fmt.Sprint(s.PID))
		}
		if s.Error != "" {
			g.errs = append(g.errs, s.Error)
		}
	}
	var names []string
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SERVICE\tSTATE\tPIDS")
	for _, n := range names {
		g := groups[n]
		state := "stopped"
		switch {
		case g.running == g.total:
			state = "running"
		case g.running > 0:
			state = fmt.Sprintf("partial (%d/%d)", g.running, g.total)
		case len(g.errs) > 0:
			state = "failed"
		}
		if g.total > 1 && g.running == g.total {
			state = fmt.Sprintf("running (%s)", plural(g.total, "worker"))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", g.name, state, joinOr(g.pids, "-"))
	}
	w.Flush()
	if st.Summary != "none" {
		fmt.Println()
		scheme := "http"
		if st.Info.HTTPS {
			scheme = "https"
		}
		for _, s := range st.Info.Sites {
			fmt.Printf("  %s://%s  ->  %s\n", scheme, s.Host, s.DocRoot)
		}
		if st.Info.Adminer != "" {
			fmt.Printf("  %s  ->  Adminer (credentials: ebur db)\n", strings.Replace(st.Info.Adminer, "http:", scheme+":", 1))
		}
	}
	for _, s := range st.Info.Skipped {
		fmt.Printf("  skipped www\\%s: %s\n", s.Folder, s.Reason)
	}
}
