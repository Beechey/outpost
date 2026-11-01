// Command outpost prepares servers for T3 Code and moves your work between them.
package main

import (
	"context"
	"os"
	"syscall"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/servers"
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
			"everything to a new server when you switch providers.\n\n" +
			"Servers are written as an IP address or hostname, with :port if SSH isn't on 22.\n" +
			"Leave one out to pick from the servers outpost has set up before.",
		SilenceUsage: true,
	}
	root.AddCommand(setupCmd(), moveCmd(), pairCmd())
	return root
}

func setupCmd() *cobra.Command {
	var access accessFlags
	cmd := &cobra.Command{
		Use:   "setup [server]",
		Short: "Prepare a fresh Ubuntu 24.04 server, or repair an existing one",
		Long: "Installs and secures the server, signs it in to Tailscale, Claude and GitHub,\n" +
			"makes T3 reachable (T3 Connect, or your tailnet only with --tailscale), then\n" +
			"reboots it to prove everything comes back by itself.\n\n" +
			"Safe to re-run: finished steps are skipped, and a server already in use gets a\n" +
			"health check instead of a reboot.",
		Example: "  outpost setup 203.0.113.7\n" +
			"  outpost setup 203.0.113.7 --tailscale    # tailnet only, no T3 Connect slot\n" +
			"  outpost setup my-home-server.local:2222\n" +
			"  outpost setup                            # pick a server you've set up before",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server should outpost set up?")
			if err != nil {
				return err
			}
			if err := setup.Setup(cmd.Context(), srv, access.value()); err != nil {
				return err
			}
			_ = servers.Remember(srv) // only for the list next time
			return nil
		},
	}
	access.add(cmd)
	return cmd
}

func moveCmd() *cobra.Command {
	var yes bool
	var access accessFlags
	cmd := &cobra.Command{
		Use:   "move [old-server] [new-server]",
		Short: "Move everything to a new server: code, T3 threads, agent history",
		Long: "Sets up the new server, then moves your work onto it: everything in ~/code\n" +
			"(uncommitted changes and worktrees included), T3 Code's threads, projects and\n" +
			"settings, and your Claude and Codex history, skills and settings.\n\n" +
			"The new server takes over as the same T3 environment, so threads continue where\n" +
			"they left off. Logins aren't copied: you approve them once on the new server.\n" +
			"The old server's files stay untouched until you delete it.",
		Example: "  outpost move 203.0.113.7 198.51.100.4\n" +
			"  outpost move                             # pick both servers from a list",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			old, err := server(cmd.Context(), args, 0, "Which server are you moving from?")
			if err != nil {
				return err
			}
			dst, err := server(cmd.Context(), args, 1, "Which server are you moving to?", old)
			if err != nil {
				return err
			}
			if err := setup.Move(cmd.Context(), old, dst, yes, access.value()); err != nil {
				return err
			}
			_ = servers.Forget(old) // only for the list next time
			_ = servers.Remember(dst)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	access.add(cmd)
	return cmd
}

func pairCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pair [server]",
		Short: "Pair another device with a server that uses Tailscale",
		Long: "Prints a QR code and link that pair T3 Code on another device, such as your\n" +
			"phone, with a server set up with --tailscale. The device needs Tailscale too.\n" +
			"The link works for 5 minutes.",
		Example: "  outpost pair 203.0.113.7",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server should the device pair with?")
			if err != nil {
				return err
			}
			return setup.Pair(cmd.Context(), srv)
		},
	}
}

// accessFlags chooses how devices reach T3 on the server.
type accessFlags struct{ tailscale, connect bool }

func (a *accessFlags) add(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&a.tailscale, "tailscale", false, "serve T3 on your tailnet only: free, unlimited machines, devices need Tailscale")
	cmd.Flags().BoolVar(&a.connect, "connect", false, "use T3 Connect: reach it from anywhere (the default for new servers)")
	cmd.MarkFlagsMutuallyExclusive("tailscale", "connect")
}

// value is the chosen access; with neither flag, a server keeps what it has.
func (a *accessFlags) value() setup.Access {
	switch {
	case a.tailscale:
		return setup.AccessTailscale
	case a.connect:
		return setup.AccessConnect
	}
	return setup.AccessKeep
}

// server reads the server from args[i], or asks the user to pick one.
func server(ctx context.Context, args []string, i int, prompt string, exclude ...remote.Server) (remote.Server, error) {
	if i < len(args) {
		return servers.Parse(args[i])
	}
	return servers.Pick(ctx, prompt, exclude...)
}
