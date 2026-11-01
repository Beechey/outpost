package setup

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// Access is how your devices reach T3 Code on a server.
type Access string

const (
	// AccessKeep keeps a server's current choice; a new server gets T3 Connect.
	AccessKeep Access = ""
	// AccessConnect uses T3 Connect: works from anywhere, uses one of your account's machine slots.
	AccessConnect Access = "connect"
	// AccessTailscale serves T3 only on your tailnet: free and unlimited, devices need Tailscale.
	AccessTailscale Access = "tailscale"
)

// A login signs the server in to one account. Check is a remote command that
// succeeds when it's already signed in; Run signs in, prompting the user.
type login struct {
	Name, Check, Run, Hint string
}

var accountLogins = []login{
	{
		Name:  "Tailscale",
		Check: "tailscale status >/dev/null 2>&1",
		Run:   "sudo tailscale up --ssh",
		Hint:  "Open the link and sign in with the same account as your laptop.",
	},
	{
		Name:  "Claude",
		Check: `claude auth status 2>/dev/null | grep -q '"loggedIn": true'`,
		Run:   "claude auth login",
		Hint:  "Open the link, approve, then paste the code back here.",
	},
	{
		Name: "GitHub",
		// setup-git lets plain git use these credentials, so private repos clone.
		Check: "gh auth status >/dev/null 2>&1 && gh auth setup-git",
		// The server has no browser; GH_BROWSER=true stops gh trying to open one.
		Run:  "GH_BROWSER=true gh auth login --hostname github.com --git-protocol https --web && gh auth setup-git",
		Hint: "Open github.com/login/device on your laptop and enter the code below.",
	},
}

// t3Status checks T3's settings on the server with a jq expression.
func t3Status(expr string) string { return "t3 status --json | jq -e '" + expr + "' >/dev/null" }

// tailscalePair lets the dev user publish T3 with Tailscale Serve, then prints a pairing QR code and link.
const tailscalePair = `sudo tailscale set --operator="$USER" && t3 pair --tailscale --label outpost`

var t3Logins = map[Access]login{
	AccessConnect: {
		Name:  "T3 Connect",
		Check: t3Status(".connect.enabled and .service.installed"),
		Run:   "t3 up --connect --no-lan --yes",
		Hint:  "Open the link, check the code matches, and approve.",
	},
	AccessTailscale: {
		Name:  "T3 over Tailscale",
		Check: t3Status(".service.installed and .lan.enabled and (.connect.enabled | not)"),
		Run:   "t3 up --lan --no-connect --yes && " + tailscalePair,
		Hint: "Scan the QR code, or open the link, in T3 Code on a device in your tailnet.\n" +
			"  If Tailscale asks you to enable HTTPS, do it once in its admin console and run this again.",
	},
}

// resolveAccess turns AccessKeep into the server's current choice: Tailscale
// if T3 there is already tailnet-only, otherwise T3 Connect.
func resolveAccess(ctx context.Context, d dev, admin string, access Access) Access {
	if access != AccessKeep {
		return access
	}
	if admin == DevUser && d.ok(ctx, t3Logins[AccessTailscale].Check) {
		return AccessTailscale
	}
	return AccessConnect
}

func runLogins(ctx context.Context, d dev, access Access) error {
	for _, l := range slices.Concat(accountLogins, []login{t3Logins[access]}) {
		if d.ok(ctx, l.Check) {
			ui.Check(l.Name, "already signed in")
			continue
		}
		lipgloss.Println("\n  " + ui.Accent.Render("◆") + " " + ui.Bold.Render(l.Name) + "  " + ui.Muted.Render(l.Hint))
		if err := d.interactive(ctx, l.Run); err != nil {
			return fmt.Errorf("%s sign-in didn't finish. Run the same command again to retry: %w", l.Name, err)
		}
		ui.Check(l.Name, "signed in")
	}
	return nil
}

// accessNote is the closing line about reaching the server from other devices.
func accessNote(h remote.Host, access Access) string {
	if access == AccessTailscale {
		return "Pair more devices (they need Tailscale) any time with: outpost pair " + h.String()
	}
	return "It's on T3 Connect, so your laptop, phone and browser reach it from anywhere."
}

// Pair prints a pairing QR code and link for a server that serves T3 over Tailscale.
func Pair(ctx context.Context, srv remote.Server) error {
	if err := srv.Validate(); err != nil {
		return err
	}
	return pair(ctx, srv)
}

func pair(ctx context.Context, h remote.Host) error {
	d := dev{h}
	if !h.CanLogin(ctx, DevUser) {
		return fmt.Errorf("can't log in to %s as %s. Set it up first: outpost setup %s --tailscale", h, DevUser, h)
	}
	if !d.ok(ctx, t3Logins[AccessTailscale].Check) {
		return errors.New("this server uses T3 Connect, so there's nothing to pair: sign in to T3 Connect on the device. " +
			"To switch it to Tailscale, run: outpost setup " + h.String() + " --tailscale")
	}
	lipgloss.Println("  " + ui.Muted.Render("Scan the QR code, or open the link, in T3 Code on a device in your tailnet. It works for 5 minutes."))
	return d.interactive(ctx, tailscalePair)
}
