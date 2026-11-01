package setup

import (
	"context"
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/ui"
)

// A login signs the server in to one account. Check is a remote command that
// succeeds when it's already signed in; Run signs in, prompting the user.
type login struct {
	Name, Check, Run, Hint string
}

var logins = []login{
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
	{
		Name:  "T3 Connect",
		Check: "t3 status --json | jq -e '.connect.enabled and .service.installed' >/dev/null",
		Run:   "t3 up --connect --yes",
		Hint:  "Open the link, check the code matches, and approve.",
	},
}

func runLogins(ctx context.Context, d dev) error {
	for _, l := range logins {
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
