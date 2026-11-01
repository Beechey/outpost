package setup

import (
	"context"
	"fmt"
	"strings"

	"github.com/Beechey/outpost/internal/remote"
)

// DevUser runs T3 Code and the agents. It must match DEV_USER in bootstrap.sh.
const DevUser = "dev"

// dev is the dev user on a server outpost has set up: everything outpost asks
// of or does to such a server goes through it, and every T3 and systemd
// command lives here. Commands get one with open or openActive.
type dev struct {
	h       remote.Host
	movedTo string // where outpost move took this server's work, if it's retired
}

// open returns the dev user on a server outpost has set up. A retired server
// opens too, for the commands that only read from it.
func open(ctx context.Context, h remote.Host) (dev, error) {
	if !h.CanLogin(ctx, DevUser) {
		return dev{}, fmt.Errorf("can't log in to %s as %s. Set it up first: outpost setup %s", h, DevUser, h)
	}
	d := dev{h: h}
	out, _ := d.output(ctx, cmdMovedTo)
	d.movedTo = strings.TrimSpace(string(out))
	return d, nil
}

// openActive is open for the commands that change a server or start T3 there.
// It refuses a retired server: T3 there again would be a second machine
// answering as the same environment.
func openActive(ctx context.Context, h remote.Host) (dev, error) {
	d, err := open(ctx, h)
	if err != nil {
		return dev{}, err
	}
	return d, d.notRetired()
}

func (d dev) notRetired() error {
	if d.movedTo != "" {
		return fmt.Errorf("this server's work was moved to %s with outpost move, so it's retired. "+
			"Use %s instead, or to reuse this server, delete %s on it first", d.movedTo, d.movedTo, movedToFile)
	}
	return nil
}

// The commands outpost runs as the dev user, by name, so tests can answer them.
const (
	t3Unit           = "t3code.service"
	cmdT3Installed   = "t3 status --json | jq -e .service.installed"
	cmdT3Running     = "systemctl --user is-active --quiet " + t3Unit
	cmdT3Start       = "systemctl --user start " + t3Unit
	cmdT3Stop        = "systemctl --user stop " + t3Unit + " 2>/dev/null; true"
	cmdMovedTo       = "cat " + movedToFile + " 2>/dev/null"
	cmdRebootPending = "test -f /var/run/reboot-required"
	cmdReboot        = "sudo systemctl reboot >/dev/null 2>&1"
	// cmdServices prints "<name> <state>" per line; is-active exits non-zero
	// for stopped units, so its exit code means nothing.
	cmdServices = `for u in docker tailscaled; do echo "$u $(systemctl is-active $u)"; done; ` +
		`echo "T3 $(systemctl --user is-active ` + t3Unit + `)"`
)

// cmdRetire removes T3's service, keeping its data, and records where the work went.
func cmdRetire(to remote.Host) string {
	return "t3 service uninstall >/dev/null 2>&1; echo " + quote(to.String()) + " > " + movedToFile
}

// userPath puts the tools installed in ~/.local/bin (t3, claude) on PATH.
const userPath = `export PATH="$HOME/.local/bin:$PATH"; `

func (d dev) output(ctx context.Context, cmd string) ([]byte, error) {
	return d.h.Output(ctx, DevUser, userPath+cmd)
}

// ok reports whether a shell check succeeds.
func (d dev) ok(ctx context.Context, check string) bool {
	_, err := d.output(ctx, check)
	return err == nil
}

func (d dev) interactive(ctx context.Context, cmd string) error {
	return d.h.Interactive(ctx, DevUser, userPath+cmd)
}

// t3Installed reports whether T3 runs as a service here, so agents may be at work.
func (d dev) t3Installed(ctx context.Context) bool { return d.ok(ctx, cmdT3Installed) }

// t3Running reports whether T3's service is running now.
func (d dev) t3Running(ctx context.Context) bool { return d.ok(ctx, cmdT3Running) }

// stopT3 stops T3's service, if there is one.
func (d dev) stopT3(ctx context.Context) error {
	_, err := d.output(ctx, cmdT3Stop)
	return err
}

// startT3 starts T3's service, reporting whether it could.
func (d dev) startT3(ctx context.Context) error {
	_, err := d.output(ctx, cmdT3Start)
	return err
}

// retireT3 removes T3's service, keeping its data, and records where the work went.
func (d dev) retireT3(ctx context.Context, to remote.Host) error {
	_, err := d.output(ctx, cmdRetire(to))
	return err
}

// signedIn reports whether the server is signed in to l's account.
func (d dev) signedIn(ctx context.Context, l login) bool { return d.ok(ctx, l.Check) }

// rebootPending reports whether installed updates wait for a reboot to take effect.
func (d dev) rebootPending(ctx context.Context) bool { return d.ok(ctx, cmdRebootPending) }

// servicesDown lists the services that aren't running, as "<name> is <state>".
func (d dev) servicesDown(ctx context.Context) []string {
	out, _ := d.output(ctx, cmdServices)
	if len(out) == 0 {
		return []string{"the server didn't answer"}
	}
	var down []string
	for line := range strings.Lines(string(out)) {
		if name, state, _ := strings.Cut(strings.TrimSpace(line), " "); state != "active" {
			down = append(down, name+" is "+state)
		}
	}
	return down
}

// reboot restarts the server; the connection drops as it goes down.
func (d dev) reboot(ctx context.Context) {
	_, _ = d.output(ctx, cmdReboot)
}

// quote makes s a single shell word.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
