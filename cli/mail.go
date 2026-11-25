package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/certs"
	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/provision"
	"github.com/ebur-dev/ebur/core/runtimes"
)

// mailCmd shows where captured mail goes and how to point an app at it
// (TOOL-03).
func mailCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mail",
		Short: "Show mail capture settings: every email your sites send lands in Mailpit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			if inst, _ := runtimes.Installed(root, "mailpit"); len(inst) == 0 {
				return &hintErr{"mail capture is not installed", "Run `ebur install mailpit`, then `ebur restart`."}
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			scheme := "http"
			if certs.Dir(root.SSL()).Exists() {
				scheme = "https"
			}
			fmt.Printf("Inbox:   %s://mail.%s\n", scheme, cfg.TLD)
			fmt.Printf("SMTP:    127.0.0.1:%d (no login needed; any login is accepted)\n\n", provision.MailSMTPPort)
			fmt.Println("PHP's mail() (WordPress, plain PHP) is already sent to Mailpit.")
			fmt.Println("Mail is never delivered to real addresses.")
			fmt.Println("\nFor Laravel, in .env:")
			fmt.Printf("  MAIL_MAILER=smtp\n  MAIL_HOST=127.0.0.1\n  MAIL_PORT=%d\n  MAIL_USERNAME=null\n  MAIL_PASSWORD=null\n  MAIL_ENCRYPTION=null\n", provision.MailSMTPPort)
			fmt.Println("\nFor Symfony, in .env:")
			fmt.Printf("  MAILER_DSN=smtp://127.0.0.1:%d\n", provision.MailSMTPPort)
			return nil
		},
	}
}
