package setup

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// report is what status finds on a server. A retired one is only movedTo.
type report struct {
	movedTo       string
	mem           memory
	access        Access
	reachable     bool     // devices can reach T3 through access
	t3URL         string   // T3's tailnet address, for Tailscale access
	down          []string // services that aren't running
	loggedOut     []login  // accounts it isn't signed in to
	rebootPending bool
	gateway       gatewayConfig
	routes        []route
	gatewayDown   []string // parts of an active gateway that aren't running
	name          string   // the server's hostname
}

// inspect reads how a server is set up and whether it's healthy. It changes nothing.
func inspect(ctx context.Context, d dev) (report, error) {
	r := report{movedTo: d.movedTo}
	if r.movedTo != "" {
		return r, nil
	}
	var err error
	if r.mem, err = readMemory(ctx, d.h, DevUser); err != nil {
		return report{}, err
	}
	r.name = hostname(ctx, d)
	r.access, r.reachable = d.access(ctx)
	if r.access == AccessTailscale {
		if self, err := d.tailscaleSelf(ctx); err == nil && self.DNSName != "" {
			r.t3URL = "https://" + self.DNSName
		}
	}
	r.down = d.servicesDown(ctx)
	for _, l := range accountLogins {
		if !d.signedIn(ctx, l) {
			r.loggedOut = append(r.loggedOut, l)
		}
	}
	r.rebootPending = d.rebootPending(ctx)
	r.gateway, r.routes = d.gateway(ctx)
	if r.gateway.active() {
		r.gatewayDown = d.gatewayDown(ctx, r.gateway, r.routes)
	}
	return r, nil
}

// Status shows how a server is set up and whether it's healthy, naming the
// command that changes or fixes each part. It changes nothing.
func Status(ctx context.Context, h remote.Host) error {
	p := ui.NewProgress()
	p.Task("Checking " + h.String())
	r, err := Inspect(ctx, h)
	p.Clear()
	if err != nil {
		return err
	}
	r.Print(h)
	return nil
}

// Report is a server's status, read once and shown by status and the menu.
type Report struct{ r report }

// Inspect reads a server's status. It changes nothing.
func Inspect(ctx context.Context, h remote.Host) (Report, error) {
	d, err := open(ctx, h)
	if err != nil {
		return Report{}, err
	}
	r, err := inspect(ctx, d)
	return Report{r}, err
}

// Retired reports whether the server's work was moved elsewhere.
func (r Report) Retired() bool { return r.r.movedTo != "" }

// Gateway reports whether the server's projects have gateway names, and how many projects do.
func (r Report) Gateway() (on bool, projectCount int) {
	return r.r.gateway.active(), len(projects(r.r.routes))
}

// Access is how devices reach T3 on the server.
func (r Report) Access() Access { return r.r.access }

func (r Report) Print(h remote.Host) { r.r.print(h) }

func (r report) print(h remote.Host) {
	if r.movedTo != "" {
		lipgloss.Println("\n" + ui.Card(h.String(), "retired", []ui.Row{
			{Label: "Moved to", Value: r.movedTo},
			{Label: "Files", Value: ui.Muted.Render("untouched; T3 stays off here")},
		}))
		return
	}

	access := ui.Warn("devices can't reach T3 yet")
	if r.reachable {
		access = r.access.String()
		if r.t3URL != "" {
			access = "T3 over Tailscale  " + ui.Accent.Render(r.t3URL)
		}
	}
	gateway := ui.Muted.Render("off")
	if r.gateway.active() {
		gateway = describeGateway(r.gateway, r.routes)
	}
	swap := "no swap"
	if r.mem.hasSwap {
		swap = fmt.Sprintf("%d GB swap", r.mem.swap)
	}
	running := []string{ui.Mark("T3 Code", r.isDown("T3")), ui.Mark("Docker", r.isDown("docker")), ui.Mark("Tailscale", r.isDown("tailscaled"))}
	if r.gateway.active() {
		running = append(running, ui.Mark("Gateway", len(r.gatewayDown) > 0))
	}
	var logins []string
	for _, l := range accountLogins {
		logins = append(logins, ui.Mark(l.Name, containsLogin(r.loggedOut, l)))
	}
	rows := []ui.Row{
		{Label: "Access", Value: access},
		{Label: "Gateway", Value: gateway},
		{Label: "Memory", Value: fmt.Sprintf("%d GB · %s · %d GB disk free", r.mem.ram, swap, r.mem.disk)},
		{Label: "Running", Value: strings.Join(running, "  ")},
		{Label: "Logins", Value: strings.Join(logins, "  ")},
	}
	title := h.String()
	if r.name != "" && r.name != title {
		title = r.name + ui.Muted.Render(" · "+title)
	}
	lipgloss.Println("\n" + ui.Card(title, r.headline(), rows))

	if len(r.routes) > 0 && r.gateway.active() {
		lipgloss.Println("\n  " + ui.Bold.Render("Projects"))
		for _, rt := range r.routes {
			lipgloss.Println("    " + padRight(rt.Name, 20) + ui.Accent.Render(r.gateway.url(rt)))
		}
		if r.gateway.Mode == GatewayDomain {
			lipgloss.Println("    " + padRight("trust it once", 20) + ui.Muted.Render("http://"+r.gateway.Domain))
		}
	}

	problems := r.problems(h)
	if len(problems) > 0 {
		fmt.Println()
		for _, p := range problems {
			ui.Problem(p[0], p[1], p[2])
		}
	}
}

// headline sums up the server's health in a few words.
func (r report) headline() string {
	if n := len(r.problems(nil)); n > 0 {
		return ui.Warn(plural(fmt.Sprint(n), "thing") + " to fix")
	}
	return ui.Accent.Render("all good")
}

func (r report) isDown(name string) bool {
	for _, d := range r.down {
		if strings.HasPrefix(d, name+" ") {
			return true
		}
	}
	return false
}

// problems are what's wrong, each as name, note and the command that fixes it.
func (r report) problems(h remote.Host) [][3]string {
	name := "SERVER"
	if h != nil {
		name = h.String()
	}
	var ps [][3]string
	if !r.reachable {
		ps = append(ps, [3]string{"Access", "devices can't reach T3 yet", "outpost access " + name})
	}
	if len(r.down) > 0 {
		ps = append(ps, [3]string{"Not running:", strings.Join(r.down, ", "), "outpost setup " + name})
	}
	if len(r.gatewayDown) > 0 {
		ps = append(ps, [3]string{"Gateway:", strings.Join(r.gatewayDown, ", "), "outpost gateway " + name + " " + string(r.gateway.Mode)})
	}
	for _, l := range r.loggedOut {
		ps = append(ps, [3]string{l.Name, "not signed in", "outpost login " + name + " " + loginKey(l)})
	}
	if r.rebootPending {
		hint := "a reboot, when agents are idle"
		if h != nil {
			hint = rebootHint(h)
		}
		ps = append(ps, [3]string{"Updates", "waiting for a reboot to finish", hint})
	}
	return ps
}

func containsLogin(ls []login, l login) bool {
	for _, x := range ls {
		if x.Name == l.Name {
			return true
		}
	}
	return false
}

// rebootHint says how to finish updates that wait for a reboot.
func rebootHint(h remote.Host) string {
	return "ssh dev@" + h.String() + " sudo reboot, when agents are idle"
}

// GatewayMode is which names the server's projects have.
func (r Report) GatewayMode() GatewayMode { return r.r.gateway.Mode }
