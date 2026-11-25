package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/core/share"
)

// shareCmd makes a site public for a while through a Cloudflare quick
// tunnel (TOOL-04).
func shareCmd() *cobra.Command {
	var (
		dur      time.Duration
		password bool
	)
	c := &cobra.Command{
		Use:   "share [site]",
		Short: "Give a site a temporary public https:// address (Cloudflare quick tunnel)",
		Long: "Gives a site a random public address on trycloudflare.com so a client or a phone can open it.\n" +
			"Only that site is reachable: not Adminer, Mailpit, the database or other sites.\n" +
			"A share ends after --for (default 1h), with `ebur share stop`, or when the web server stops.\n" +
			"Traffic passes through Cloudflare. No account is needed.",
		Example: "  ebur share shop                 public for 1 hour\n" +
			"  ebur share shop --for 3h --password\n" +
			"  ebur share                      list active shares\n" +
			"  ebur share stop shop            end it now (or: ebur share stop all)",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return listShares(root)
			}
			if dur < time.Minute || dur > share.MaxDuration {
				return &hintErr{fmt.Sprintf("--for must be between 1m and %v", share.MaxDuration), "For example: --for 30m or --for 3h."}
			}
			if inst, _ := runtimes.Installed(root, "cloudflared"); len(inst) == 0 {
				fmt.Println("Sharing uses Cloudflare's cloudflared (about 53 MB, downloaded once).")
				if err := installRuntime(root, "cloudflared", ""); err != nil {
					return err
				}
				fmt.Println()
			}
			if err := ensureHost(root); err != nil {
				return err
			}
			var st host.Status
			if call(root, "status", nil, &st) == nil && st.Summary == "none" {
				fmt.Println("Starting services...")
				if err := call(root, "start", selection(nil), &st); err != nil {
					return err
				}
			}
			fmt.Printf("Opening a tunnel for %s (usually a few seconds)...\n", args[0])
			var sh share.Share
			req := host.ShareRequest{Site: args[0], Minutes: int(dur.Round(time.Minute) / time.Minute), Password: password}
			if err := call(root, "share", req, &sh); err != nil {
				return err
			}
			fmt.Printf("\n  %s\n\n", sh.URL)
			fmt.Printf("%s is public at that address until %s (%s).\n", sh.Site, sh.Expires.Format("15:04"), untilText(sh.Expires))
			if sh.Password != "" {
				fmt.Printf("Visitors sign in as %s with password %s\n", sh.User, sh.Password)
			}
			fmt.Println("Anyone with the link can open it; traffic passes through Cloudflare. Debug pages are off for these visitors.")
			fmt.Printf("End it now with `ebur share stop %s`.\n", sh.Site)
			return nil
		},
	}
	c.Flags().DurationVar(&dur, "for", share.DefaultDuration, "how long the share lasts, e.g. 30m, 3h (max 24h)")
	c.Flags().BoolVar(&password, "password", false, "ask visitors for a generated password")
	c.AddCommand(&cobra.Command{
		Use:   "stop <site|all>",
		Short: "End a share now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			var left []share.Share
			if err := call(root, "unshare", host.ShareRequest{Site: args[0]}, &left); err != nil {
				return err
			}
			if args[0] == "all" {
				fmt.Println("Ended every share.")
			} else {
				fmt.Printf("Ended the share of %s; its public address no longer works.\n", strings.ToLower(args[0]))
			}
			return nil
		},
	})
	return c
}

func listShares(root paths.Root) error {
	var shares []share.Share
	if err := call(root, "shares", nil, &shares); err != nil || len(shares) == 0 {
		fmt.Println("Nothing is shared. Share a site with `ebur share <site>`.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SITE\tPUBLIC ADDRESS\tENDS\tPASSWORD")
	for _, s := range shares {
		pw := "-"
		if s.Password != "" {
			pw = s.User + " / " + s.Password
		}
		fmt.Fprintf(w, "%s\t%s\t%s (%s)\t%s\n", s.Site, orNone(s.URL), s.Expires.Format("15:04"), untilText(s.Expires), pw)
	}
	return w.Flush()
}

// untilText says how long is left, e.g. "in 59 min".
func untilText(t time.Time) string {
	d := time.Until(t).Round(time.Minute)
	switch {
	case d <= 0:
		return "ending now"
	case d < time.Hour:
		return fmt.Sprintf("in %d min", int(d.Minutes()))
	case d%time.Hour == 0:
		return fmt.Sprintf("in %d h", int(d.Hours()))
	}
	return fmt.Sprintf("in %d h %d min", int(d.Hours()), int(d.Minutes())%60)
}
