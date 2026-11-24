package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur/ebur/core/certs"
	"github.com/ebur/ebur/core/config"
	"github.com/ebur/ebur/core/host"
	"github.com/ebur/ebur/core/paths"
)

// restartIfRunning applies a change to running services.
func restartIfRunning(root paths.Root, why string) error {
	var st host.Status
	if call(root, "status", nil, &st) != nil || st.Summary == "none" {
		return nil
	}
	fmt.Printf("Restarting services %s...\n", why)
	if err := call(root, "restart", host.Selection{}, &st); err != nil {
		return err
	}
	printStatus(st)
	return nil
}

func sslCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ssl",
		Short: "Manage local HTTPS: the Ebur certificate authority and site certificates",
	}
	c.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Create the local CA and trust it, so https://<site>.test works without warnings",
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
			// Public roots for PHP's bundle (SSL-04); small, verified download.
			if err := installRuntime(root, "cacert", ""); err != nil {
				fmt.Println("warning: could not get public root certificates for PHP:", err)
			}
			dir := certs.Dir(root.SSL())
			ca, err := dir.LoadCA()
			if errors.Is(err, certs.ErrNoCA) {
				if ca, err = dir.CreateCA(cfg.TLD); err != nil {
					return err
				}
				fmt.Printf("Created the Ebur local CA. It can only sign names under .%s and localhost.\n", cfg.TLD)
			} else if err != nil {
				return &hintErr{err.Error(), "Run `ebur ssl uninstall` to start over."}
			}
			if ok, _ := ca.IsTrusted(); !ok {
				fmt.Println("Windows will now ask you to confirm installing the certificate")
				fmt.Printf("  %q\n  SHA-256 %s\n", ca.Cert.Subject.CommonName, ca.Fingerprint())
				fmt.Println("Click Yes to trust it for your Windows account only.")
				if err := ca.Trust(); err != nil {
					if errors.Is(err, certs.ErrDeclined) {
						return &hintErr{"the CA was not trusted, so browsers will warn on https:// sites", "Run `ebur ssl install` again and click Yes."}
					}
					return err
				}
			}
			fmt.Println("The Ebur CA is trusted. Sites are served over https:// as well as http://.")
			return restartIfRunning(root, "to enable HTTPS")
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the local CA and whether Windows trusts it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			ca, err := certs.Dir(root.SSL()).LoadCA()
			if errors.Is(err, certs.ErrNoCA) {
				fmt.Println("Local HTTPS is not set up. Run `ebur ssl install`.")
				return nil
			} else if err != nil {
				return err
			}
			trusted, terr := ca.IsTrusted()
			state := "trusted by Windows (current user)"
			if terr != nil {
				state = "unknown: " + terr.Error()
			} else if !trusted {
				state = "NOT trusted: run `ebur ssl install`"
			}
			fmt.Printf("CA:         %s\nSHA-256:    %s\nValid to:   %s\nMay sign:   %s\nStatus:     %s\n",
				ca.Cert.Subject.CommonName, ca.Fingerprint(), ca.Cert.NotAfter.Format("2006-01-02"),
				strings.Join(ca.Cert.PermittedDNSDomains, ", "), state)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove the local CA from Windows and delete all Ebur certificates (SSL-03)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			dir := certs.Dir(root.SSL())
			ca, err := dir.LoadCA()
			if errors.Is(err, certs.ErrNoCA) {
				fmt.Println("There is no Ebur CA to remove.")
				return nil
			}
			if err == nil {
				if ok, _ := ca.IsTrusted(); ok {
					fmt.Println("Windows will ask you to confirm deleting the certificate; click Yes.")
					if err := ca.Untrust(); err != nil {
						if errors.Is(err, certs.ErrDeclined) {
							return &hintErr{"the CA is still trusted; nothing was deleted", "Run `ebur ssl uninstall` again and click Yes."}
						}
						return err
					}
				}
			}
			if err := dir.Remove(); err != nil {
				return err
			}
			fmt.Println("Removed the Ebur CA from Windows and deleted its keys and all site certificates.")
			return restartIfRunning(root, "without HTTPS")
		},
	})
	return c
}
