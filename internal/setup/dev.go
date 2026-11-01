package setup

import (
	"context"
	"strings"

	"github.com/Beechey/outpost/internal/remote"
)

// DevUser runs T3 Code and the agents. It must match DEV_USER in bootstrap.sh.
const DevUser = "dev"

// dev is the dev user on a host: it runs shell commands with the user's tools
// on PATH and names the T3 Code operations the flows need.
type dev struct{ h remote.Host }

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
func (d dev) t3Installed(ctx context.Context) bool {
	return d.ok(ctx, "t3 status --json | jq -e .service.installed")
}

// stopT3 stops T3's service, if there is one.
func (d dev) stopT3(ctx context.Context) error {
	_, err := d.output(ctx, service("stop")+" 2>/dev/null; true")
	return err
}

// startT3 starts T3's service, reporting whether it could.
func (d dev) startT3(ctx context.Context) error {
	_, err := d.output(ctx, service("start"))
	return err
}

// retireT3 removes T3's service, keeping its data, and records where the work went.
func (d dev) retireT3(ctx context.Context, movedTo remote.Host) error {
	_, err := d.output(ctx, "t3 service uninstall >/dev/null 2>&1; echo "+quote(movedTo.String())+" > "+movedToFile)
	return err
}

// movedTo reports where this host's work was moved, if outpost move retired it.
func (d dev) movedTo(ctx context.Context) string {
	out, _ := d.output(ctx, "cat "+movedToFile+" 2>/dev/null")
	return strings.TrimSpace(string(out))
}

// servicesDown lists the services that aren't running, as "<name> is <state>".
func (d dev) servicesDown(ctx context.Context) []string {
	// Prints "<name> <state>" per line; is-active exits non-zero for stopped units, so ignore the exit code.
	out, _ := d.output(ctx, `for u in docker tailscaled; do echo "$u $(systemctl is-active $u)"; done; `+
		`echo "T3 $(`+service("is-active")+`)"`)
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

// reboot restarts the host; the connection drops as it goes down.
func (d dev) reboot(ctx context.Context) {
	_, _ = d.output(ctx, "sudo systemctl reboot >/dev/null 2>&1")
}

// service is a systemctl command for T3's service.
func service(action string) string { return "systemctl --user " + action + " t3code.service" }

// quote makes s a single shell word.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
