package setup

import (
	"context"
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// aptGet runs apt-get as root without questions, waiting for the daily
// security updates rather than failing on apt's lock, and keeping config files you changed.
const aptGet = "sudo DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=600 -o Dpkg::Options::=--force-confold "

// updates are what Update brings up to date, in order.
var updates = []struct{ name, cmd string }{
	{"T3 Code", "t3 update --yes"}, // --yes restarts its service on the new version
	{"Claude Code", "claude update"},
	{"System packages", aptGet + "update && " + aptGet + "upgrade -y"},
}

// Update brings a server's T3 Code, Claude Code and system packages up to
// date. T3 restarts on its new version, so agents mid-turn stop; yes skips the
// confirmation.
func Update(ctx context.Context, h remote.Host, yes bool) error {
	d, err := openActive(ctx, h)
	if err != nil {
		return err
	}
	if !yes {
		if err := confirm(ctx, "Update "+h.String()+" now?",
			"T3 restarts on its new version, so agents mid-turn stop; send them \"continue\".\n"+
				"If Docker is updated it restarts too, stopping containers without restart: unless-stopped.", "Update"); err != nil {
			return err
		}
	}
	if err := runUpdates(ctx, d); err != nil {
		return err
	}
	ui.Check("Up to date", "")
	if d.rebootPending(ctx) {
		lipgloss.Println("  " + ui.Muted.Render("Some updates take effect at the next reboot: "+rebootHint(h)))
	}
	return nil
}

func runUpdates(ctx context.Context, d dev) (err error) {
	p := ui.NewProgress()
	defer func() { p.Done(err) }()
	for _, u := range updates {
		p.Task(u.name)
		if _, err := d.output(ctx, u.cmd); err != nil {
			return fmt.Errorf("update %s: %w", u.name, err)
		}
	}
	return nil
}
