package setup

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
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

// logins are the sign-ins a server needs when devices reach it the given way.
func logins(a Access) []login { return slices.Concat(accountLogins, []login{t3Logins[a]}) }

// loginKey is how a login is named on the command line: tailscale, claude, github or t3.
func loginKey(l login) string { return strings.ToLower(strings.Fields(l.Name)[0]) }

func runLogins(ctx context.Context, d dev, a Access) error {
	for _, l := range logins(a) {
		if d.signedIn(ctx, l) {
			ui.Check(l.Name, "already signed in")
			continue
		}
		if err := runLogin(ctx, d, l); err != nil {
			return err
		}
	}
	return nil
}

// runLogin signs in to one account, prompting the user.
func runLogin(ctx context.Context, d dev, l login) error {
	lipgloss.Println("\n  " + ui.Accent.Render("◆") + " " + ui.Bold.Render(l.Name) + "  " + ui.Muted.Render(l.Hint))
	if err := d.interactive(ctx, l.Run); err != nil {
		return fmt.Errorf("%s sign-in didn't finish. Run the same command again to retry: %w", l.Name, err)
	}
	ui.Check(l.Name, "signed in")
	return nil
}

// Login signs a server in again: to the named account, even if it looks signed
// in (a login can expire), or with account "" to every one it isn't signed in to.
func Login(ctx context.Context, h remote.Host, account string) error {
	d, err := openActive(ctx, h)
	if err != nil {
		return err
	}
	a := resolveAccess(ctx, d, true, AccessUnset)
	if account == "" {
		return runLogins(ctx, d, a)
	}
	for _, l := range logins(a) {
		if loginKey(l) == strings.ToLower(account) {
			return runLogin(ctx, d, l)
		}
	}
	return fmt.Errorf("%q isn't an account outpost signs in to. Choose tailscale, claude, github or t3", account)
}
