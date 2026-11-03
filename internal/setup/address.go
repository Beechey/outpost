package setup

import (
	"context"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// Address prints where a server is reached: T3 Code, by whichever way
// devices use, and each project's gateway name. It changes nothing.
func Address(ctx context.Context, h remote.Host) error {
	d, err := openActive(ctx, h)
	if err != nil {
		return err
	}
	access, ok := d.access(ctx)
	lipgloss.Println("\n  " + ui.Bold.Render("T3 Code"))
	switch {
	case !ok:
		ui.Problem("Access", "devices can't reach T3 yet", "outpost access "+h.String())
	case access == AccessTailscale:
		self, err := d.tailscaleSelf(ctx)
		if err != nil {
			return err
		}
		lipgloss.Println("    " + padRight("Tailscale", 20) + ui.Accent.Render("https://"+self.DNSName))
		lipgloss.Println("    " + padRight("", 20) + ui.Muted.Render("pair a device first: outpost pair "+h.String()))
	default:
		lipgloss.Println("    " + padRight("T3 Connect", 20) + "pick " + ui.Bold.Render(hostname(ctx, d)) +
			" in T3 Code, signed in to your T3 account")
	}

	c, rs := d.gateway(ctx)
	if !c.active() {
		lipgloss.Println("\n  " + ui.Muted.Render("Projects have no HTTPS names. Give them some: outpost gateway "+h.String()))
		return nil
	}
	lipgloss.Println("\n  " + ui.Bold.Render("Projects") + "  " + ui.Muted.Render(c.Mode.String()))
	if len(rs) == 0 {
		lipgloss.Println("    " + ui.Muted.Render("none yet. Add one: outpost gateway add "+h.String()+" PROJECT PORT"))
	}
	for _, r := range rs {
		lipgloss.Println("    " + padRight(r.Name, 20) + ui.Accent.Render(c.url(r)))
	}
	if c.Mode == GatewayDomain {
		lipgloss.Println("    " + padRight("trust it once", 20) + ui.Muted.Render("http://"+c.Domain))
	}
	return nil
}

func hostname(ctx context.Context, d dev) string {
	out, _ := d.output(ctx, "hostname -s")
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return d.h.String()
}
