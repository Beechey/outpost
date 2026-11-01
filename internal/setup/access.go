package setup

import (
	"context"
	"errors"
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// Access is how your devices reach T3 Code on a server.
type Access string

const (
	// AccessUnset means the user didn't choose: setup and move keep a server's
	// current access (T3 Connect for a new one), and SetAccess asks.
	AccessUnset Access = ""
	// AccessConnect uses T3 Connect: works from anywhere, uses one of your account's machine slots.
	AccessConnect Access = "connect"
	// AccessTailscale serves T3 only on your tailnet: free and unlimited, devices need Tailscale.
	AccessTailscale Access = "tailscale"
)

// ParseAccess reads an access as typed: "connect", "tailscale", or "" for unset.
func ParseAccess(s string) (Access, error) {
	switch a := Access(s); a {
	case AccessUnset, AccessConnect, AccessTailscale:
		return a, nil
	}
	return "", fmt.Errorf("%q isn't a way in; choose connect or tailscale", s)
}

func (a Access) String() string {
	switch a {
	case AccessConnect:
		return "T3 Connect"
	case AccessTailscale:
		return "Tailscale"
	}
	return "not chosen"
}

// t3Status checks T3's settings on the server with a jq expression.
func t3Status(expr string) string { return "t3 status --json | jq -e '" + expr + "' >/dev/null" }

// tailscalePair lets the dev user publish T3 with Tailscale Serve, then prints a pairing QR code and link.
const tailscalePair = `sudo tailscale set --operator="$USER" && t3 pair --tailscale --label outpost`

// t3Logins turn each access on, and their checks tell which one a server uses.
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

// access reports how devices reach T3 on the server now; ok is false when they can't yet.
func (d dev) access(ctx context.Context) (a Access, ok bool) {
	for _, a := range []Access{AccessTailscale, AccessConnect} {
		if d.signedIn(ctx, t3Logins[a]) {
			return a, true
		}
	}
	return AccessUnset, false
}

// resolveAccess settles the access a setup or move gives a server: the
// user's choice, else what a set-up server uses now, else T3 Connect.
func resolveAccess(ctx context.Context, d dev, setUp bool, chosen Access) Access {
	if chosen != AccessUnset {
		return chosen
	}
	if setUp {
		if now, ok := d.access(ctx); ok {
			return now
		}
	}
	return AccessConnect
}

// accessNote is the closing line about reaching the server from other devices.
func accessNote(h remote.Host, a Access) string {
	if a == AccessTailscale {
		return "Pair more devices (they need Tailscale) any time with: outpost pair " + h.String()
	}
	return "It's on T3 Connect, so your laptop, phone and browser reach it from anywhere."
}

// SetAccess switches how devices reach T3 on a server, asking which when a is AccessUnset.
func SetAccess(ctx context.Context, h remote.Host, a Access) error {
	d, err := openActive(ctx, h)
	if err != nil {
		return err
	}
	now, reachable := d.access(ctx)
	if reachable {
		lipgloss.Println("  " + ui.Muted.Render("Devices reach "+h.String()+" through "+now.String()))
	} else {
		lipgloss.Println("  " + ui.Muted.Render("Devices can't reach T3 on "+h.String()+" yet"))
	}
	if a == AccessUnset {
		if a, err = askAccess(ctx, h, now); err != nil {
			return err
		}
	}
	if a == now {
		ui.Check(a.String(), "unchanged")
		return nil
	}
	if err := runLogin(ctx, d, t3Logins[a]); err != nil {
		return err
	}
	lipgloss.Println("\n  " + ui.Muted.Render(accessNote(h, a)))
	if now == AccessConnect {
		lipgloss.Println("  " + ui.Muted.Render("T3 Connect is off here now. To be sure its machine slot is freed, deregister it in T3 Code: account menu → T3 Connect."))
	}
	return nil
}

func askAccess(ctx context.Context, h remote.Host, now Access) (Access, error) {
	if now == AccessUnset {
		now = AccessConnect
	}
	answer, err := ui.Ask.Choose(ctx, "How should your devices reach T3 on "+h.String()+"?", []ui.Option{
		{Label: "T3 Connect: from anywhere, with phone notifications; uses a machine slot", Value: string(AccessConnect)},
		{Label: "Tailscale: devices on your tailnet only; free and unlimited", Value: string(AccessTailscale)},
	}, string(now))
	if errors.Is(err, ui.ErrNoTerminal) {
		return "", fmt.Errorf("run in a terminal to choose, or name it: outpost access %s connect (or tailscale)", h)
	}
	if err != nil {
		return "", err
	}
	return ParseAccess(answer)
}

// Pair prints a pairing QR code and link for a server that serves T3 over Tailscale.
func Pair(ctx context.Context, h remote.Host) error {
	d, err := openActive(ctx, h)
	if err != nil {
		return err
	}
	if now, _ := d.access(ctx); now != AccessTailscale {
		return errors.New("this server doesn't serve T3 over Tailscale, so there's nothing to pair: sign in to T3 Connect on the device. " +
			"To switch it to Tailscale, run: outpost access " + h.String() + " tailscale")
	}
	lipgloss.Println("  " + ui.Muted.Render("Scan the QR code, or open the link, in T3 Code on a device in your tailnet. It works for 5 minutes."))
	return d.interactive(ctx, tailscalePair)
}
