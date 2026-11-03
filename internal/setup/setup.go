// Package setup prepares servers for T3 Code and moves work between them.
//
// The flows depend only on remote.Host, so tests run them against a fake.
package setup

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

//go:embed scripts/bootstrap.sh
var bootstrapScript []byte

// How long the final check waits. Tests shorten these.
var (
	rebootSettle    = 15 * time.Second // before trying to reconnect after a reboot
	rebootTimeout   = 5 * time.Minute
	servicesTimeout = 90 * time.Second // services can still be starting just after a boot
	pollInterval    = 3 * time.Second
)

// Setup prepares a server for T3 Code, reachable the given way, with swap the
// size the user typed and the name they gave ("" asks on a fresh server). It is
// safe to re-run on a finished server.
func Setup(ctx context.Context, h remote.Host, access Access, swap, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	d := dev{h: h} // the dev user exists once the install has run
	var admin string
	var inUse bool
	lipgloss.Println("\n" + ui.Banner)
	err := runPlan(ctx, []step{
		{"SSH key", ensureKey},
		{"Connecting to " + h.String(), func(ctx context.Context) (err error) {
			if admin, inUse, err = connect(ctx, h); err != nil {
				return err
			}
			if admin != DevUser {
				access = resolveAccess(ctx, d, false, access)
				return nil // a fresh server: nothing has run there yet
			}
			if d, err = openActive(ctx, h); err != nil {
				return err
			}
			access = resolveAccess(ctx, d, true, access)
			return nil
		}},
		{"Installing (3-5 minutes)", func(ctx context.Context) error {
			if err := nameServer(ctx, h, admin, name, admin != DevUser); err != nil {
				return err
			}
			return install(ctx, h, admin, swap)
		}},
		{"Logins", func(ctx context.Context) error { return runLogins(ctx, d, access) }},
		// A server already running T3 may have agents at work, so it gets a health check, not a reboot.
		{"Final check", func(ctx context.Context) error { return finalCheck(ctx, d, !inUse) }},
	})
	if err != nil {
		return err
	}
	ui.Done(ui.Accent.Render("✓ All set.") + " " + ui.Bold.Render(h.String()) + " is ready for your agents.\n\n" +
		"In T3 Code, pick this server under Settings → Environments,\n" +
		"add a project with ⌘K → Add Project, and start prompting.\n\n" +
		ui.Muted.Render(accessNote(h, access)))
	return nil
}

// A step is one numbered stage of a flow.
type step struct {
	title string
	run   func(context.Context) error
}

// runPlan runs steps in order under numbered headings, stopping at the first error.
func runPlan(ctx context.Context, plan []step) error {
	for i, s := range plan {
		ui.Header(i+1, len(plan), s.title)
		if err := s.run(ctx); err != nil {
			return err
		}
	}
	return nil
}

// confirm asks the user to go ahead, failing unless they choose yes.
func confirm(ctx context.Context, title, description, yes string) error {
	ok, err := ui.Ask.Confirm(ctx, title, description, yes)
	switch {
	case errors.Is(err, ui.ErrNoTerminal):
		return errors.New("run in a terminal to confirm, or pass --yes")
	case err != nil:
		return err
	case !ok:
		return ui.ErrCancelled
	}
	return nil
}

// connect logs in to the host, reporting who runs the install and whether the
// host already runs T3 (and so may have agents working).
func connect(ctx context.Context, h remote.Host) (admin string, inUse bool, err error) {
	if admin, err = adminUser(ctx, h); err != nil {
		return "", false, err
	}
	lipgloss.Println("  " + ui.Muted.Render("Logged in as "+admin))
	return admin, admin == DevUser && dev{h: h}.t3Installed(ctx), nil
}

// adminUser picks who runs the install: root on a fresh server, or dev on a
// re-run, because the install turns root SSH off.
func adminUser(ctx context.Context, h remote.Host) (string, error) {
	if h.CanLogin(ctx, DevUser) {
		return DevUser, nil
	}
	if h.CanLogin(ctx, "root") {
		return "root", nil
	}
	fmt.Println("  Your SSH key isn't on the server yet. Enter the root password from your provider:")
	if err := h.CopyID(ctx, "root"); err != nil {
		return "", fmt.Errorf("can't log in as root (%w). Check the address and password. "+
			"If you reinstalled the server, first run: ssh-keygen -R %s", err, h)
	}
	if !h.CanLogin(ctx, "root") {
		return "", errors.New("your key was copied, but logging in as root with it still fails. " +
			"The server may not allow root login with a key (PermitRootLogin)")
	}
	return "root", nil
}

// install runs bootstrap.sh, first adding the swap the user chose, so a small
// server has room for the install.
func install(ctx context.Context, h remote.Host, admin, swap string) error {
	if err := installSwap(ctx, h, admin, swap); err != nil {
		return err
	}
	if err := runTasks(ctx, h, admin, remote.Script{Body: bootstrapScript, AsRoot: true}); err != nil {
		return fmt.Errorf("the install failed (see above). Run the same command again to retry: %w", err)
	}
	return nil
}

// runTasks runs a script that announces its stages as "::task <name>" lines,
// showing them as live progress. Other output is hidden, but the last lines are
// shown if the script fails.
func runTasks(ctx context.Context, h remote.Host, user string, sc remote.Script) error {
	const keep = 25
	var tail []string
	p := ui.NewProgress()
	err := h.RunStreaming(ctx, user, sc, func(line string) {
		if name, ok := strings.CutPrefix(line, "::task "); ok {
			p.Task(name)
			return
		}
		if tail = append(tail, line); len(tail) > keep {
			tail = tail[1:]
		}
	})
	p.Done(err)
	if err != nil && len(tail) > 0 {
		lipgloss.Println("\n  " + ui.Muted.Render("Last output from the server:"))
		for _, l := range tail {
			fmt.Println("  │ " + l)
		}
	}
	return err
}

// finalCheck confirms T3, Docker and Tailscale are running, after first
// rebooting if reboot is set, to prove they start by themselves.
func finalCheck(ctx context.Context, d dev, reboot bool) (err error) {
	p := ui.NewProgress()
	defer func() { p.Done(err) }()

	if reboot {
		p.Task("Rebooting")
		d.reboot(ctx)
		if err := sleep(ctx, rebootSettle); err != nil {
			return err
		}
		p.Task("Waiting for the server to come back")
		if err := poll(ctx, rebootTimeout, func() bool { return d.h.CanLogin(ctx, DevUser) }); err != nil {
			return errors.New("the server didn't come back within 5 minutes; check it in your provider's console")
		}
	}

	p.Task("T3 Code, Docker and Tailscale running")
	var down []string
	if err := poll(ctx, servicesTimeout, func() bool { down = d.servicesDown(ctx); return len(down) == 0 }); err != nil {
		return fmt.Errorf("not everything is running (%s). Run the same command again to repair it", strings.Join(down, ", "))
	}
	return nil
}

// poll calls done every pollInterval until it returns true, or fails after timeout.
func poll(ctx context.Context, timeout time.Duration, done func() bool) error {
	deadline := time.Now().Add(timeout)
	for !done() {
		if time.Now().After(deadline) {
			return errors.New("timed out")
		}
		if err := sleep(ctx, pollInterval); err != nil {
			return err
		}
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// ensureKey makes sure there's an SSH key to log in with: an existing key file,
// a key in the SSH agent, or a new one.
func ensureKey(ctx context.Context) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if keys, _ := filepath.Glob(filepath.Join(home, ".ssh", "id_*.pub")); len(keys) > 0 {
		lipgloss.Println("  " + ui.Muted.Render("Using "+keys[0]))
		return nil
	}
	if exec.CommandContext(ctx, "ssh-add", "-L").Run() == nil {
		lipgloss.Println("  " + ui.Muted.Render("Using the keys in your SSH agent"))
		return nil
	}
	fmt.Println("  No SSH key yet, creating one. Press Enter at each question.")
	gen := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-f", filepath.Join(home, ".ssh", "id_ed25519"))
	gen.Stdin, gen.Stdout, gen.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := gen.Run(); err != nil {
		return fmt.Errorf("create SSH key: %w", err)
	}
	return nil
}
