package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/hostsfile"
	"github.com/ebur-dev/ebur/core/hostsync"
	"github.com/ebur-dev/ebur/core/paths"
)

// syncHosts maps every site to 127.0.0.1, prompting once only if the
// hosts file needs to change (SITE-02).
func syncHosts(root paths.Root, want []string) error {
	if ok, _ := hostsync.InSync(want); ok {
		fmt.Println("Hosts file is up to date.")
		return nil
	}
	fmt.Println("Updating the hosts file: Windows will ask for administrator permission for ebur-helper.")
	hostsync.Forget(root) // a manual sync always tries, and resets the automatic back-off
	changed, err := hostsync.Sync(want)
	if err != nil {
		return explainSyncError(err)
	}
	if changed {
		if len(want) == 0 {
			fmt.Println("Removed Ebur's entries from the hosts file.")
		} else {
			fmt.Printf("Hosts file updated: %s -> 127.0.0.1\n", strings.Join(want, ", "))
		}
	}
	return nil
}

func hostsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "hosts",
		Short: "Show whether every site is mapped to 127.0.0.1 in the hosts file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			want, err := hostsync.Wanted(root)
			if err != nil {
				return err
			}
			ok, err := hostsync.InSync(want)
			if err != nil {
				return err
			}
			fmt.Printf("Hosts file: %s\n", hostsfile.Path())
			fmt.Printf("Sites:      %s\n", joinOr(want, "none"))
			if ok {
				fmt.Println("Status:     up to date")
			} else {
				fmt.Println("Status:     needs updating; run `ebur hosts sync`")
			}
			return nil
		},
	}
	c.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Map every site to 127.0.0.1 (one administrator prompt, only if needed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			want, err := hostsync.Wanted(root)
			if err != nil {
				return err
			}
			return syncHosts(root, want)
		},
	}, &cobra.Command{
		Use:   "clear",
		Short: "Remove all of Ebur's entries from the hosts file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			return syncHosts(root, nil)
		},
	})
	return c
}

// autoSyncHosts runs after `ebur start` unless manual_hosts is set. A
// failure is shown but does not fail the start: sites still answer at
// 127.0.0.1.
func autoSyncHosts(root paths.Root) {
	cfg, err := config.Load(root.Config())
	if err != nil || cfg.ManualHosts {
		return
	}
	want, err := hostsync.Wanted(root)
	if err != nil {
		return
	}
	if ok, _ := hostsync.InSync(want); ok {
		return
	}
	if hostsync.Skipped(root, want) == nil {
		fmt.Println("Updating the hosts file: Windows will ask for administrator permission for ebur-helper.")
	}
	changed, err := hostsync.AutoSync(root, want)
	var skipped *hostsync.ErrSkipped
	switch {
	case errors.As(err, &skipped):
		// Do not prompt on every start for the same names.
		fmt.Printf("Note: %s. Run `ebur hosts sync` to try again.\n", skipped.Error())
	case err != nil:
		fmt.Println()
		printError(explainSyncError(err))
	case changed:
		fmt.Printf("Hosts file updated: %s -> 127.0.0.1\n", strings.Join(want, ", "))
	}
}

func explainSyncError(err error) error {
	switch {
	case errors.Is(err, hostsync.ErrDeclined):
		return &hintErr{"the hosts file was not updated (the permission prompt was declined)",
			"Run `ebur hosts sync` and click Yes, or set \"manual_hosts\": true in ebur.json to manage it yourself."}
	case errors.Is(err, hostsync.ErrLocked):
		return &hintErr{err.Error(),
			"Allow hosts-file changes in your security software (Webroot: \"Prevent any program from modifying the HOSTS file\"), then run `ebur hosts sync`."}
	}
	return err
}
