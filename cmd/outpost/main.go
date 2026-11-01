// Command outpost prepares servers for T3 Code and moves your work between them.
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
		Short: "Always-on servers for your AI coding agents, ready for T3 Code",
		Long: "outpost turns a fresh Ubuntu server into a home for your agents, and moves\n" +
			"everything to a new server when you switch providers.",
		SilenceUsage: true,
	}
	root.AddCommand(setupCmd(), moveCmd())
	return root
}

func setupCmd() *cobra.Command {
	var opts setup.Options
	cmd := &cobra.Command{
		Use:   "setup <server>",
		Short: "Prepare a fresh Ubuntu 24.04 server, or repair an existing one",
		Long: "Installs and secures the server, signs it in to Tailscale, Claude, GitHub and\n" +
			"T3 Connect, then reboots it to prove everything comes back by itself.\n\n" +
			"Safe to re-run: finished steps are skipped, and a server already in use gets a\n" +
			"health check instead of a reboot.",
		Example: "  outpost setup 203.0.113.7\n" +
			"  outpost setup -p 2222 my-home-server.local",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Host = args[0]
			return setup.Run(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVarP(&opts.Port, "port", "p", 0, "port for SSH (default: ~/.ssh/config, else 22)")
	return cmd
}

func moveCmd() *cobra.Command {
	var opts setup.MoveOptions
	cmd := &cobra.Command{
		Use:   "move <old-server> <new-server>",
		Short: "Move everything to a new server: code, T3 threads, agent history",
		Long: "Sets up the new server, then moves your work onto it: everything in ~/code\n" +
			"(uncommitted changes and worktrees included), T3 Code's threads, projects and\n" +
			"settings, and your Claude and Codex history, skills and settings.\n\n" +
			"The new server takes over as the same T3 environment, so threads continue where\n" +
			"they left off. Logins aren't copied: you approve them once on the new server.\n" +
			"The old server's files stay untouched until you delete it.",
		Example: "  outpost move 203.0.113.7 198.51.100.4",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Old, opts.New = args[0], args[1]
			return setup.Move(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.OldPort, "old-port", 0, "port for SSH on the old server")
	cmd.Flags().IntVar(&opts.NewPort, "new-port", 0, "port for SSH on the new server")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "don't ask for confirmation")
	return cmd
}
