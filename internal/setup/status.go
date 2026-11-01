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
	down          []string // services that aren't running
	loggedOut     []login  // accounts it isn't signed in to
	rebootPending bool
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
	r.access, r.reachable = d.access(ctx)
	r.down = d.servicesDown(ctx)
	for _, l := range accountLogins {
		if !d.signedIn(ctx, l) {
			r.loggedOut = append(r.loggedOut, l)
		}
	}
	r.rebootPending = d.rebootPending(ctx)
	return r, nil
}

// Status shows how a server is set up and whether it's healthy, naming the
// command that changes or fixes each part. It changes nothing.
func Status(ctx context.Context, h remote.Host) error {
	d, err := open(ctx, h)
	if err != nil {
		return err
	}
	r, err := inspect(ctx, d)
	if err != nil {
		return err
	}
	r.print(h)
	return nil
}

func (r report) print(h remote.Host) {
	lipgloss.Println("\n" + ui.Bold.Render(h.String()))
	if r.movedTo != "" {
		lipgloss.Println("  " + ui.Muted.Render("Retired: its work was moved to "+r.movedTo+". Its files are untouched."))
		return
	}
	row := func(label, val, change string) {
		lipgloss.Println("  " + ui.Accent.Render("•") + " " + label + " " + val + "  " + ui.Muted.Render(change))
	}
	swap := "no swap"
	if r.mem.hasSwap {
		swap = fmt.Sprintf("%d GB of swap", r.mem.swap)
	}
	row("Memory", fmt.Sprintf("%d GB, %s, %d GB of disk free", r.mem.ram, swap, r.mem.disk), "outpost swap "+h.String())
	if r.reachable {
		row("Access", r.access.String(), "outpost access "+h.String())
	} else {
		ui.Problem("Access", "devices can't reach T3 yet", "outpost access "+h.String())
	}
	if len(r.down) > 0 {
		ui.Problem("Not running:", strings.Join(r.down, ", "), "outpost setup "+h.String())
	} else {
		ui.Check("T3 Code, Docker and Tailscale", "running")
	}
	for _, l := range accountLogins {
		if containsLogin(r.loggedOut, l) {
			ui.Problem(l.Name, "not signed in", "outpost login "+h.String()+" "+loginKey(l))
		} else {
			ui.Check(l.Name, "signed in")
		}
	}
	if r.rebootPending {
		ui.Problem("Updates", "waiting for a reboot to finish", rebootHint(h))
	}
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
