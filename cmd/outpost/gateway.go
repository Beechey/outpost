package main

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Beechey/outpost/internal/servers"
	"github.com/Beechey/outpost/internal/setup"
)

// gatewayFlags are a gateway's answers given on the command line.
type gatewayFlags struct {
	mode, domain string
	yes, trust   bool
}

// add adds --gateway (when withMode) and --domain.
func (g *gatewayFlags) add(cmd *cobra.Command, withMode bool) {
	if withMode {
		cmd.Flags().StringVar(&g.mode, "gateway", "", "give projects HTTPS names without asking: tailscale, domain or off")
	}
	cmd.Flags().StringVar(&g.domain, "domain", "", "for --gateway domain (or gateway domain): the domain names go under, like devbox.test")
	cmd.Flags().BoolVar(&g.trust, "trust", false, "with your own domain: trust its certificate on this computer without asking")
}

func (g *gatewayFlags) value() (setup.GatewayOptions, error) {
	mode, err := setup.ParseGatewayMode(g.mode)
	opts := setup.GatewayOptions{Mode: mode, Domain: g.domain, Yes: g.yes}
	if g.trust {
		opts.Trust = "yes"
	}
	return opts, err
}

func gatewayCmd() *cobra.Command {
	var gw gatewayFlags
	cmd := &cobra.Command{
		Use:   "gateway [server] [tailscale|domain|off]",
		Short: "Give projects private HTTPS addresses on your tailnet",
		Long: "Browsers only allow some features (the clipboard, passkeys, crypto.randomUUID) over\n" +
			"HTTPS, so a web app on the server breaks when opened from another device over http.\n" +
			"The gateway gives each project an HTTPS address every device on your tailnet can open:\n\n" +
			"  tailscale  https://myapp-devbox.tail1234.ts.net. Real certificates from Tailscale,\n" +
			"             so devices need nothing. The default.\n" +
			"  domain     https://myapp.devbox.test, under a domain you choose. outpost runs Caddy\n" +
			"             and CoreDNS, and each device trusts its certificate once.\n" +
			"  off        no addresses.\n\n" +
			"outpost does the Tailscale admin steps with an API key, or shows you each one.\n" +
			"Leave out the choice to see the current one and pick. Choosing what the server has\n" +
			"already checks it and repairs anything that's wrong.",
		Example: "  outpost gateway devbox tailscale\n" +
			"  outpost gateway devbox domain --domain devbox.test\n" +
			"  outpost gateway add devbox myapp 3000 --path /ws=3001 --name files.myapp=9000\n" +
			"  outpost gateway remove devbox myapp\n" +
			"  outpost gateway trust devbox                   # trust its certificate on this computer",
		Args:      cobra.MaximumNArgs(2),
		ValidArgs: []string{"tailscale", "domain", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && slices.Contains([]string{"tailscale", "domain", "off"}, args[0]) {
				args = []string{"", args[0]} // just the choice: pick the server
			}
			var srvArgs []string
			if len(args) > 0 && args[0] != "" {
				srvArgs = args[:1]
			}
			srv, err := server(cmd.Context(), srvArgs, 0, "Which server?")
			if err != nil {
				return err
			}
			if len(args) > 1 {
				gw.mode = args[1]
			}
			opts, err := gw.value()
			if err != nil {
				return err
			}
			return setup.Gateway(cmd.Context(), srv, opts)
		},
	}
	gw.add(cmd, false)
	cmd.Flags().BoolVarP(&gw.yes, "yes", "y", false, "don't ask: add what the tailnet policy needs, and change names without confirming")
	cmd.AddCommand(gatewayAddCmd(), gatewayRemoveCmd(), gatewayTrustCmd())
	return cmd
}

func gatewayAddCmd() *cobra.Command {
	var paths, names []string
	var yes bool
	cmd := &cobra.Command{
		Use:   "add [server] PROJECT PORT",
		Short: "Give a project an HTTPS address",
		Long: "Sends the project's address to PORT on the server's 127.0.0.1. --path sends a path on\n" +
			"the same address to another port (say, a socket server that needs the app's cookies),\n" +
			"and --name gives the project an extra address. Adding a project again replaces its\n" +
			"addresses. Leave out the project to be asked.",
		Example: "  outpost gateway add devbox myapp 3000\n" +
			"  outpost gateway add devbox myapp 3000 --path /ws=3001 --name files.myapp=9000\n" +
			"  outpost gateway add myapp 3000             # pick the server from a list",
		Args: cobra.MaximumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			rest := args
			if len(args) == 3 || len(args) == 1 {
				rest = args[1:]
			} else {
				args = nil // PROJECT PORT, or nothing: pick the server
			}
			srv, err := server(cmd.Context(), args, 0, "Which server is the project on?")
			if err != nil {
				return err
			}
			project, port := "", ""
			if len(rest) == 2 {
				project, port = rest[0], rest[1]
			}
			return setup.GatewayAdd(cmd.Context(), srv, project, port, paths, names, yes)
		},
	}
	cmd.Flags().StringArrayVar(&paths, "path", nil, "send a path to another port, as /PATH=PORT (repeat for more)")
	cmd.Flags().StringArrayVar(&names, "name", nil, "an extra address for the project, as NAME=PORT (repeat for more)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask: add what the tailnet policy needs")
	return cmd
}

func gatewayRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove [server] [PROJECT]",
		Short:   "Take a project's HTTPS addresses away",
		Example: "  outpost gateway remove devbox myapp\n  outpost gateway remove devbox                 # pick the project",
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			project := ""
			switch {
			case len(args) == 2:
				project = args[1]
			case len(args) == 1 && !looksLikeServer(args[0]):
				project, args = args[0], nil
			}
			srv, err := server(cmd.Context(), args, 0, "Which server is the project on?")
			if err != nil {
				return err
			}
			return setup.GatewayRemove(cmd.Context(), srv, project, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask")
	return cmd
}

func gatewayTrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust [server]",
		Short: "Trust a gateway's certificate on this computer",
		Long: "For a gateway with your own domain: adds its certificate to this computer, asking\n" +
			"for your password, so browsers here open its addresses securely. On other devices,\n" +
			"open http://DOMAIN and follow the steps there. Tailscale names need none of this.",
		Example: "  outpost gateway trust devbox",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server's gateway?")
			if err != nil {
				return err
			}
			return setup.GatewayTrust(cmd.Context(), srv)
		},
	}
}

func addressCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "address [server]",
		Short: "Show where to reach a server: T3 Code and each project",
		Long: "Prints T3 Code's address, for the way your devices reach it, and each project's\n" +
			"HTTPS address on the gateway. It changes nothing.",
		Example: "  outpost address devbox",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := server(cmd.Context(), args, 0, "Which server?")
			if err != nil {
				return err
			}
			return setup.Address(cmd.Context(), srv)
		},
	}
}

// looksLikeServer reports whether a lone argument names a server rather than
// a project: one outpost remembers, or an address with a dot or a port.
func looksLikeServer(s string) bool {
	srv, err := servers.Parse(s)
	return err == nil && (slices.Contains(servers.List(), srv) || strings.ContainsAny(s, ".:"))
}
