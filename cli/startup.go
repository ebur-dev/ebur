package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/startup"
)

// startupCmd shows or changes what happens when Ebur opens and when the
// user signs in to Windows (SVC-06); the same switches as Settings >
// Startup in the window.
func startupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "startup",
		Short: "Show or change whether services start when Ebur opens, and whether Ebur opens at Windows sign-in",
		Example: "  ebur startup                  show both settings\n" +
			"  ebur startup services off     opening Ebur no longer starts services\n" +
			"  ebur startup signin on        open Ebur (in the tray) when you sign in to Windows",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			fmt.Printf("Start services when Ebur opens: %s\n", onOff(cfg.StartsServices()))
			if len(cfg.AutoStart) > 0 {
				fmt.Printf("  (only these, from auto_start in ebur.json: %s)\n", strings.Join(cfg.AutoStart, ", "))
			}
			fmt.Printf("Open Ebur when you sign in to Windows: %s\n", onOff(startup.Enabled()))
			return nil
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:       "services on|off",
		Short:     "Start services when the Ebur app opens (on by default)",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			on, err := parseOnOff(args[0])
			if err != nil {
				return err
			}
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			cfg.StartServices = &on
			if err := config.Save(root.Config(), cfg); err != nil {
				return err
			}
			if on {
				fmt.Println("Opening the Ebur app now starts its services.")
			} else {
				fmt.Println("Opening the Ebur app no longer starts services; use Start all or `ebur start`.")
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:       "signin on|off",
		Short:     "Open Ebur in the tray when you sign in to Windows",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			on, err := parseOnOff(args[0])
			if err != nil {
				return err
			}
			if !on {
				if err := startup.Disable(); err != nil {
					return err
				}
				fmt.Println("Ebur no longer opens when you sign in to Windows.")
				return nil
			}
			app, err := startup.AppNextTo()
			if err != nil {
				return err
			}
			if err := startup.Enable(app); err != nil {
				return err
			}
			fmt.Printf("Ebur opens in the tray when you sign in to Windows (%s).\n", app)
			return nil
		},
	})
	return cmd
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "on", "yes", "true":
		return true, nil
	case "off", "no", "false":
		return false, nil
	}
	return false, &hintErr{fmt.Sprintf("%q is not on or off", s), "Use on or off, e.g. `ebur startup signin on`."}
}
