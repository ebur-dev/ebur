package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ebur/ebur/core/config"
	"github.com/ebur/ebur/core/host"
	"github.com/ebur/ebur/core/manifest"
	"github.com/ebur/ebur/core/paths"
	"github.com/ebur/ebur/core/provision"
	"github.com/ebur/ebur/core/runtimes"
	"github.com/ebur/ebur/core/sites"
	manifestdata "github.com/ebur/ebur/manifest"
)

func loadManifest() (*manifest.Manifest, error) { return manifest.Parse(manifestdata.JSON) }

// arch maps GOARCH to manifest arch. Official PHP and MariaDB builds are
// x64 only, so ARM64 falls back to x64 under emulation (SRS §2.3).
func resolve(m *manifest.Manifest, name, want string) (manifest.Release, error) {
	if goruntime.GOARCH == "arm64" {
		if rel, err := m.Resolve(name, want, "arm64"); err == nil {
			return rel, nil
		}
	}
	return m.Resolve(name, want, "x64")
}

func installRuntime(root paths.Root, name, want string) error {
	m, err := loadManifest()
	if err != nil {
		return err
	}
	rel, err := resolve(m, name, want)
	if err != nil {
		return &hintErr{err.Error(), fmt.Sprintf("Run `ebur %s list` to see available versions.", cmdFor(name))}
	}
	if v, _ := runtimes.Installed(root, name); contains(v, rel.Version) {
		fmt.Printf("%s %s is already installed.\n", name, rel.Version)
	} else {
		ctx, cancel := signalContext()
		defer cancel()
		fmt.Printf("Downloading %s %s ...\n", name, rel.Version)
		t0 := time.Now()
		last := -1
		in := &runtimes.Installer{Root: root}
		_, err := in.Install(ctx, name, rel, func(done, total int64) {
			if total <= 0 {
				return
			}
			if pct := int(done * 100 / total); pct != last && (pct%5 == 0 || pct == 100) {
				last = pct
				fmt.Printf("\r  %3d%%  %.1f / %.1f MB", pct, float64(done)/(1<<20), float64(total)/(1<<20))
			}
		})
		fmt.Println()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return errors.New("download cancelled; nothing was installed")
			}
			return err
		}
		fmt.Printf("Installed %s %s (checksum verified) in %v\n", name, rel.Version, time.Since(t0).Round(100*time.Millisecond))
	}
	ctx, cancel := signalContext()
	defer cancel()
	t0 := time.Now()
	rep, err := provision.Prepare(ctx, root, name, rel.Version, func(s string) { fmt.Println(s) })
	if err != nil {
		return &hintErr{err.Error(), "Run `ebur doctor` for details. Reinstall with `ebur " + cmdFor(name) + " remove` if the files are damaged."}
	}
	if len(rep.Steps) > 0 {
		fmt.Printf("Ready in %v: %s\n", time.Since(t0).Round(100*time.Millisecond), strings.Join(rep.Steps, "; "))
	}
	for _, w := range rep.Warnings {
		fmt.Println("warning:", w)
	}
	cfg, err := config.Load(root.Config())
	if err != nil {
		return err
	}
	if cfg.Active[name] == "" {
		cfg.Active[name] = rel.Version
		if err := config.Save(root.Config(), cfg); err != nil {
			return err
		}
		fmt.Printf("%s %s is now the active version.\n", name, rel.Version)
	}
	return nil
}

func cmdFor(name string) string {
	if name == "php" {
		return "php"
	}
	return "install " + name
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func installCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "install <runtime> [version]",
		Short:   "Install a runtime (php, nginx, mariadb, adminer) from the manifest",
		Example: "  ebur install nginx\n  ebur install php 8.3\n  ebur install mariadb\n  ebur install adminer",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			want := ""
			if len(args) == 2 {
				want = args[1]
			}
			return installRuntime(root, args[0], want)
		},
	}
}

// matchInstalled resolves "8.3" to the newest installed 8.3.x.
func matchInstalled(root paths.Root, name, want string) (string, error) {
	installed, err := runtimes.Installed(root, name)
	if err != nil {
		return "", err
	}
	for _, v := range installed { // newest first
		if v == want || strings.HasPrefix(v, want+".") {
			return v, nil
		}
	}
	return "", &hintErr{
		fmt.Sprintf("%s %s is not installed (installed: %s)", name, want, joinOr(installed, "none")),
		fmt.Sprintf("Run `ebur %s install %s`.", cmdFor(name), want),
	}
}

func phpCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "php [version]",
		Short: "Show the active PHP version, or switch to another installed one",
		Example: "  ebur php            show active and installed versions\n" +
			"  ebur php 8.3        switch to the newest installed 8.3.x\n" +
			"  ebur php install 8.4",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			if len(args) == 0 {
				installed, _ := runtimes.Installed(root, "php")
				fmt.Printf("Active PHP: %s\nInstalled:  %s\n", orNone(cfg.Active["php"]), joinOr(installed, "none (run `ebur php install 8.4`)"))
				return nil
			}
			v, err := matchInstalled(root, "php", args[0])
			if err != nil {
				return err
			}
			if cfg.Active["php"] == v {
				fmt.Printf("PHP %s is already active.\n", v)
				return nil
			}
			cfg.Active["php"] = v
			if err := config.Save(root.Config(), cfg); err != nil {
				return err
			}
			fmt.Printf("Active PHP set to %s.\n", v)
			var st host.Status
			if err := call(root, "status", nil, &st); err != nil || st.Summary == "none" {
				return nil // nothing running; takes effect on next start
			}
			t0 := time.Now()
			if err := call(root, "restart", host.Selection{}, &st); err != nil {
				return err
			}
			fmt.Printf("Services restarted on PHP %s in %v.\n", st.Info.PHP, time.Since(t0).Round(10*time.Millisecond))
			return nil
		},
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "install <version>",
			Short: "Download and install a PHP version, e.g. 8.4 or 8.3.37",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				root, err := findRoot()
				if err != nil {
					return err
				}
				return installRuntime(root, "php", args[0])
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List installed and available PHP versions",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				root, err := findRoot()
				if err != nil {
					return err
				}
				return listRuntime(root, "php")
			},
		},
		&cobra.Command{
			Use:   "remove <version>",
			Short: "Remove an installed PHP version",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				root, err := findRoot()
				if err != nil {
					return err
				}
				v, err := matchInstalled(root, "php", args[0])
				if err != nil {
					return err
				}
				inUse := func(_, version string) bool {
					var st host.Status
					return call(root, "status", nil, &st) == nil && st.Summary != "none" && st.Info.PHP == version
				}
				if err := runtimes.Remove(root, "php", v, inUse); err != nil {
					if errors.Is(err, runtimes.ErrInUse) {
						return &hintErr{err.Error(), "Run `ebur stop` or switch with `ebur php <other version>` first."}
					}
					return err
				}
				cfg, _ := config.Load(root.Config())
				if cfg.Active["php"] == v {
					delete(cfg.Active, "php")
					config.Save(root.Config(), cfg)
					fmt.Println("That was the active version; the newest installed one will be used next.")
				}
				fmt.Printf("Removed PHP %s.\n", v)
				return nil
			},
		},
	)
	return c
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func listRuntime(root paths.Root, name string) error {
	m, err := loadManifest()
	if err != nil {
		return err
	}
	installed, _ := runtimes.Installed(root, name)
	cfg, _ := config.Load(root.Config())
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VERSION\tSTATUS")
	seen := map[string]bool{}
	for _, r := range m.Releases(name, "x64") {
		seen[r.Version] = true
		status := "available"
		if contains(installed, r.Version) {
			status = "installed"
		}
		if cfg.Active[name] == r.Version {
			status += ", active"
		}
		fmt.Fprintf(w, "%s\t%s\n", r.Version, status)
	}
	for _, v := range installed {
		if !seen[v] {
			fmt.Fprintf(w, "%s\tinstalled (no longer in manifest)\n", v)
		}
	}
	return w.Flush()
}

func sitesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sites",
		Short: "List sites: every folder in www\\ is served at <folder>.test",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			found, skipped, err := sites.Scan(root.WWW(), cfg.TLD)
			if err != nil {
				return err
			}
			if len(found) == 0 && len(skipped) == 0 {
				fmt.Printf("No sites yet. Create a folder in %s and it is served at <folder>.%s\n", root.WWW(), cfg.TLD)
				return nil
			}
			var st host.Status
			running := call(root, "status", nil, &st) == nil && st.Summary != "none"
			php := orNone(cfg.Active["php"])
			if running {
				php = st.Info.PHP
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "URL\tPHP\tSTATUS\tDOCUMENT ROOT")
			for _, s := range found {
				status := "stopped"
				if running {
					status = "running"
				}
				fmt.Fprintf(w, "http://%s\t%s\t%s\t%s\n", s.Host, php, status, s.DocRoot)
			}
			w.Flush()
			for _, s := range skipped {
				fmt.Printf("skipped www\\%s: %s\n", s.Folder, s.Reason)
			}
			return nil
		},
	}
}
