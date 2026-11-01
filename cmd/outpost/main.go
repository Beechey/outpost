// Command outpost turns any Ubuntu server into an always-on home for AI coding agents.
package main

import (
	"context"
	"os"
	"syscall"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/Beechey/outpost/internal/setup"
)

func main() {
	if err := fang.Execute(context.Background(), rootCmd(), fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM)); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "outpost",
		Short: "Turn any server into an always-on home for your AI coding agents",
		Long: "outpost sets up an Ubuntu server for T3 Code: tools, security, logins and your repos.\n" +
			"When it finishes, open T3 Code, pick the server, and start prompting.",
		SilenceUsage: true,
	}
	root.AddCommand(setupCmd())
	return root
}

func setupCmd() *cobra.Command {
	opts := setup.Options{}
	cmd := &cobra.Command{
		Use:   "setup <server-ip> [owner/repo...]",
		Short: "Set up a fresh Ubuntu 24.04 server, or repair and update an existing one",
		Long: "Installs everything, signs the server in to Tailscale, Claude, GitHub and T3 Connect,\n" +
			"clones your repos into T3, then reboots to prove it all comes back by itself.\n\n" +
			"With no repos given, it lists yours and asks. Safe to re-run, for example to add repos.",
		Example: "  outpost setup 203.0.113.7\n" +
			"  outpost setup 203.0.113.7 you/app you/api\n" +
			"  outpost setup -p 2222 my-home-server.local",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Host, opts.Repos = args[0], args[1:]
			return setup.Run(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVarP(&opts.Port, "port", "p", 22, "port the server listens on for SSH")
	return cmd
}
