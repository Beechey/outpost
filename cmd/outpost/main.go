// Command outpost prepares servers for T3 Code and moves your work between them.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/servers"
	"github.com/Beechey/outpost/internal/setup"
	"github.com/Beechey/outpost/internal/ui"
)

func main() {
	if err := fang.Execute(context.Background(), rootCmd(), fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM)); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "outpost",
		Short: "Turn any server into an always-on home for your AI coding agents, ready for T3 Code",
		Long: "outpost turns a fresh Ubuntu server into a home for your agents, and moves\n" +
			"everything to a new server when you switch providers.\n\n" +
			"Servers are written as an IP address or hostname, with :port if SSH isn't on 22.\n" +
			"Leave one out to pick from the servers outpost has set up before.",
		SilenceUsage: true,
	}
	root.AddCommand(setupCmd(), statusCmd(), accessCmd(), pairCmd(), loginCmd(), swapCmd(), renameCmd(), updateCmd(), backupCmd(), moveCmd(), listCmd(), forgetCmd(), upgradeCmd())
	return root
}

func setupCmd() *cobra.Command {
	var access accessFlags
	var swap, name string
	cmd := &cobra.Command{
		Use:   "setup [server]",
		Short: "Prepare a fresh Ubuntu 24.04 server, or repair an existing one",
		Long: "Installs and secures the server, signs it in to Tailscale, Claude and GitHub,\n" +
			"makes T3 reachable (T3 Connect, or your tailnet only with --tailscale), then\n" +
			"reboots it to prove everything comes back by itself.\n\n" +
			"Safe to re-run: finished steps are skipped, and a server already in use gets a\n" +
			"health check instead of a reboot.\n\n" +
			"A server without swap gets the choice of adding some; --swap answers for you.\n" +
			"A fresh server is asked for a name, shown in T3 Code; --name answers for you.",
		Example: "  outpost setup 203.0.113.7\n" +
			"  outpost setup 203.0.113.7 --tailscale    # tailnet only, no T3 Connect slot\n" +
			"  outpost setup 203.0.113.7 --swap 4       # add 4 GB of swap without asking\n" +
			"  outpost setup 203.0.113.7 --name tars    # name it tars without asking\n" +
			"  outpost setup my-home-server.local:2222\n" +
			"  outpost setup                            # pick a server you've set up before",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server should outpost set up?")
			if err != nil {
				return err
			}
			if err := setup.Setup(cmd.Context(), srv, access.value(), swap, name); err != nil {
				return err
			}
			_ = servers.Remember(srv) // only for the list next time
			return nil
		},
	}
	access.add(cmd)
	addSwapFlag(cmd, &swap)
	addNameFlag(cmd, &name)
	return cmd
}

func moveCmd() *cobra.Command {
	var yes bool
	var access accessFlags
	var swap, name string
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
			if err := setup.Move(cmd.Context(), old, dst, yes, access.value(), swap, name); err != nil {
				return err
			}
			_ = servers.Forget(old) // only for the list next time
			_ = servers.Remember(dst)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask anything: confirm the move, add swap only with --swap, and rename only with --name")
	access.add(cmd)
	addSwapFlag(cmd, &swap)
	addNameFlag(cmd, &name)
	return cmd
}

func backupCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "backup [server] [file]",
		Short: "Download a server's work to a file on this machine",
		Long: "Saves what a move carries into a .tar.gz: everything in ~/code (uncommitted\n" +
			"changes and worktrees included), T3 Code's threads, projects and settings, and\n" +
			"your Claude and Codex history, skills and settings. Logins aren't included.\n\n" +
			"T3 pauses while the backup downloads, so its threads are saved whole, then\n" +
			"starts again. The file defaults to outpost-SERVER-DATE.tar.gz here.",
		Example: "  outpost backup 203.0.113.7\n" +
			"  outpost backup 203.0.113.7 ~/backups/agents.tar.gz\n" +
			"  outpost backup                           # pick a server from a list",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server should outpost back up?")
			if err != nil {
				return err
			}
			file := "outpost-" + strings.ReplaceAll(srv.String(), ":", "-") + "-" + time.Now().Format("2006-01-02-1504") + ".tar.gz"
			if len(args) > 1 {
				file = args[1]
			}
			return setup.Backup(cmd.Context(), srv, file, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask before pausing T3")
	return cmd
}

func swapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "swap [server] [GB]",
		Short: "Change how much swap a server has",
		Long: "Shows the server's memory, swap and free disk, then sets its swap to the size\n" +
			"you choose, without a reboot. 0 removes it. outpost always leaves 10 GB of disk free.\n\n" +
			"Shrinking or removing swap moves what's in it back into memory, so it can fail\n" +
			"while agents are using most of the memory.",
		Example: "  outpost swap 203.0.113.7                 # see what it has, then choose\n" +
			"  outpost swap 203.0.113.7 8               # set it to 8 GB\n" +
			"  outpost swap 203.0.113.7 0               # remove it",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server's swap should outpost change?")
			if err != nil {
				return err
			}
			size := ""
			if len(args) > 1 {
				size = args[1]
			}
			return setup.Swap(cmd.Context(), srv, size)
		},
	}
}

func renameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename SERVER NAME",
		Short: "Rename a server, in Tailscale too",
		Long: "Sets the server's hostname, keeps it across reboots, and renames it in Tailscale.\n" +
			"Use lowercase letters, numbers and hyphens.",
		Example: "  outpost rename 203.0.113.7 tars",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := servers.Parse(args[0])
			if err != nil {
				return err
			}
			return setup.Rename(cmd.Context(), srv, args[1])
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [server]",
		Short: "See how a server is set up and whether anything needs fixing",
		Long: "Shows a server's memory, swap and disk, how devices reach T3, whether T3,\n" +
			"Docker and Tailscale are running, and its sign-ins, with the command that\n" +
			"changes or fixes each. It changes nothing.",
		Example: "  outpost status 203.0.113.7",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server?")
			if err != nil {
				return err
			}
			return setup.Status(cmd.Context(), srv)
		},
	}
}

func accessCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "access [server] [connect|tailscale]",
		Short: "Switch how your devices reach T3: T3 Connect or Tailscale",
		Long: "T3 Connect works from anywhere and sends phone notifications, using one of your\n" +
			"account's machine slots. Tailscale serves T3 on your tailnet only: free and\n" +
			"unlimited, but devices need Tailscale. Leave out the choice to pick from a list.",
		Example: "  outpost access 203.0.113.7 tailscale\n" +
			"  outpost access 203.0.113.7 connect\n" +
			"  outpost access 203.0.113.7               # see the current choice, then pick",
		Args:      cobra.MaximumNArgs(2),
		ValidArgs: []string{"connect", "tailscale"},
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server?")
			if err != nil {
				return err
			}
			access := setup.AccessUnset
			if len(args) > 1 {
				if access, err = setup.ParseAccess(args[1]); err != nil {
					return err
				}
			}
			return setup.SetAccess(cmd.Context(), srv, access)
		},
	}
}

func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login [server] [tailscale|claude|github|t3]",
		Short: "Sign a server in again, when a login expires",
		Long: "Name an account to sign in to it again, even if it looks signed in. Leave it out\n" +
			"to sign in to every account the server isn't signed in to.",
		Example: "  outpost login 203.0.113.7 claude\n" +
			"  outpost login 203.0.113.7                # whatever is missing",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server?")
			if err != nil {
				return err
			}
			account := ""
			if len(args) > 1 {
				account = args[1]
			}
			return setup.Login(cmd.Context(), srv, account)
		},
	}
}

func updateCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "update [server]",
		Short: "Update T3 Code, Claude Code and system packages on a server",
		Long: "Security fixes install by themselves every day; this updates everything else.\n" +
			"T3 restarts on its new version, so agents mid-turn stop, and outpost asks first.",
		Example: "  outpost update 203.0.113.7",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server should outpost update?")
			if err != nil {
				return err
			}
			return setup.Update(cmd.Context(), srv, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask before restarting T3")
	return cmd
}

func pairCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pair [server]",
		Short: "Pair a device with a server that uses Tailscale",
		Long: "Pairs T3 Code with a server set up with --tailscale. It asks which device:\n" +
			"this computer, where it opens T3 Code to add the server, or another device,\n" +
			"such as your phone, where you scan a QR code. The device needs Tailscale too.\n" +
			"Each pairing code works once, for 5 minutes.",
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

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the servers outpost remembers",
		Long: "Lists the servers you've set up with outpost, most recently used first. These are\n" +
			"the ones offered when you leave out a server.",
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			list := servers.List()
			if len(list) == 0 {
				fmt.Println("No servers yet. Set one up with: outpost setup SERVER")
			}
			for _, srv := range list {
				fmt.Println(srv)
			}
		},
	}
}

func forgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget [server]",
		Short: "Remove a server from outpost's list; the server itself is untouched",
		Long: "Takes a server off the list outpost offers when you leave one out, say after\n" +
			"deleting it at your provider. Nothing on the server changes, and setting it up\n" +
			"again adds it back.",
		Example: "  outpost forget 203.0.113.7\n" +
			"  outpost forget                           # pick one from the list",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			list := servers.List()
			if len(args) == 0 && len(list) == 0 {
				return errors.New("your list is empty, so there's nothing to forget")
			}
			srv, err := server(cmd.Context(), args, 0, "Which server should outpost forget?")
			if err != nil {
				return err
			}
			if !slices.Contains(list, srv) {
				return fmt.Errorf("not in your list: %s. See the list with: outpost list", srv)
			}
			if err := servers.Forget(srv); err != nil {
				return err
			}
			fmt.Printf("Forgot %s. The server itself is untouched.\n", srv)
			return nil
		},
	}
}

// module is where outpost installs from.
const module = "github.com/Beechey/outpost"

func upgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade outpost itself to the newest version",
		Long: "Installs the newest outpost with Go, replacing this one. To update the software\n" +
			"on a server instead, use: outpost update SERVER",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := exec.LookPath("go"); err != nil {
				return errors.New("outpost upgrades itself with Go, which isn't on your PATH. Install it from https://go.dev/dl/, then run this again")
			}
			current := "a development build"
			if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "(devel)" {
				current = info.Main.Version
			}
			latest, err := installLatest(cmd.Context(), current)
			if err != nil {
				return fmt.Errorf("upgrade failed: %w", err)
			}
			if latest == current {
				ui.Check("outpost is up to date", latest)
				return nil
			}
			ui.Check("Upgraded outpost", "from "+current+" to "+latest)
			bin, err := goBin(cmd)
			if err != nil {
				return err
			}
			// go install writes to Go's bin folder; an outpost run from elsewhere stays as it was.
			installed := filepath.Join(bin, "outpost")
			if self, err := os.Executable(); err == nil && filepath.Clean(self) != filepath.Clean(installed) {
				fmt.Printf("  The new outpost is %s, but you ran %s. Put %s first on your PATH.\n", installed, self, bin)
			}
			return nil
		},
	}
}

// installLatest installs the newest outpost unless current already is it,
// showing progress, and returns the newest version.
func installLatest(ctx context.Context, current string) (latest string, err error) {
	p := ui.NewProgress()
	defer func() { p.Done(err) }()
	p.Task("Finding the newest outpost")
	out, err := goRun(ctx, "list", "-m", "-f", "{{.Version}}", module+"@latest")
	if err != nil {
		return "", err
	}
	if latest = strings.TrimSpace(string(out)); latest == current {
		return latest, nil
	}
	p.Task("Downloading and installing " + latest)
	_, err = goRun(ctx, "install", module+"/cmd/outpost@"+latest)
	return latest, err
}

// goRun runs a go command quietly, returning its output, or its error output in the error.
func goRun(ctx context.Context, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, "go", args...)
	c.Dir = os.TempDir() // outside any Go project, whose go.mod would change the answer
	// Go's module cache can serve an older @latest for a while after a push; with git, ask GitHub directly.
	if _, err := exec.LookPath("git"); err == nil {
		c.Env = append(os.Environ(), "GOPROXY=direct")
	}
	var stderr strings.Builder
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// goBin is the folder go install writes to.
func goBin(cmd *cobra.Command) (string, error) {
	out, err := exec.CommandContext(cmd.Context(), "go", "env", "GOBIN", "GOPATH").Output()
	if err != nil {
		return "", fmt.Errorf("find Go's bin folder: %w", err)
	}
	return binDir(string(out)), nil
}

// binDir reads `go env GOBIN GOPATH`: one line each, GOBIN empty when unset.
func binDir(goEnv string) string {
	lines := strings.Split(goEnv, "\n")
	if gobin := strings.TrimSpace(lines[0]); gobin != "" || len(lines) < 2 {
		return gobin
	}
	return filepath.Join(strings.TrimSpace(lines[1]), "bin")
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
	return setup.AccessUnset
}

// addSwapFlag adds --swap: how many GB of swap to add without asking.
func addNameFlag(cmd *cobra.Command, name *string) {
	cmd.Flags().StringVar(name, "name", "", "rename the server to this without asking (shown in T3 Code)")
}

func addSwapFlag(cmd *cobra.Command, swap *string) {
	cmd.Flags().StringVar(swap, "swap", "", "add this many GB of swap without asking (0 for none)")
}

// server reads the server from args[i], or asks the user to pick one.
func server(ctx context.Context, args []string, i int, prompt string, exclude ...remote.Server) (remote.Server, error) {
	if i < len(args) {
		return servers.Parse(args[i])
	}
	return servers.Pick(ctx, prompt, exclude...)
}
