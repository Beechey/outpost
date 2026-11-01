// Package setup prepares servers for T3 Code and moves them between machines.
package setup

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// DevUser runs T3 Code and the agents. It must match DEV_USER in bootstrap.sh.
const DevUser = "dev"

var (
	//go:embed scripts/bootstrap.sh
	bootstrapScript []byte

	// No leading "-", so a host can't be read as an ssh option.
	hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]*$`)
)

func validateServer(host string, port int) error {
	if !hostRe.MatchString(host) {
		return fmt.Errorf("%q doesn't look like a server address", host)
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("port %d is out of range", port)
	}
	return nil
}

// Options configures a setup run.
type Options struct {
	Host string
	Port int // 0 means ssh decides: ~/.ssh/config, else 22
}

// Run prepares a server for T3 Code. It is safe to re-run on a finished server.
func Run(ctx context.Context, o Options) error {
	if err := validateServer(o.Host, o.Port); err != nil {
		return err
	}
	srv := remote.Server{Host: o.Host, Port: o.Port}
	lipgloss.Println("\n" + banner)
	st := &steps{total: 5}

	st.next("SSH key")
	if err := ensureKey(ctx); err != nil {
		return fmt.Errorf("create SSH key: %w", err)
	}
	st.next("Connecting to " + o.Host)
	admin, inUse, err := connect(ctx, srv)
	if err != nil {
		return err
	}
	st.next("Installing (3-5 minutes)")
	if err := install(ctx, srv, admin); err != nil {
		return err
	}
	st.next("Logins")
	if err := runLogins(ctx, srv); err != nil {
		return err
	}
	if err := finalCheck(ctx, srv, st, inUse); err != nil {
		return err
	}

	lipgloss.Println("\n" + doneBox.Render(
		ui.Accent.Render("✓ All set.")+" "+ui.Bold.Render(o.Host)+" is ready for your agents.\n\n"+
			"In T3 Code, pick this server under Settings → Environments,\n"+
			"add a project with ⌘K → Add Project, and start prompting."))
	return nil
}

// steps numbers the headings of a run.
type steps struct{ n, total int }

func (s *steps) next(msg string) {
	s.n++
	ui.Header(s.n, s.total, msg)
}

// connect logs in to the server, reporting whether it already runs T3 (and so
// may have agents working).
func connect(ctx context.Context, srv remote.Server) (admin string, inUse bool, err error) {
	if admin, err = adminUser(ctx, srv); err != nil {
		return "", false, err
	}
	lipgloss.Println("  " + ui.Muted.Render("Logged in as "+admin))
	inUse = admin == DevUser && srvOK(ctx, srv, "t3 status --json | jq -e .service.installed")
	return admin, inUse, nil
}

func install(ctx context.Context, srv remote.Server, admin string) error {
	if err := runTasks(ctx, srv, admin, remote.Script{Body: bootstrapScript, Runner: "sudo bash"}); err != nil {
		return fmt.Errorf("the install failed (see above). Run the same command again to retry: %w", err)
	}
	return nil
}

// finalCheck reboots a new server to prove everything starts by itself. A
// server already in use only gets a health check, so working agents aren't cut off.
func finalCheck(ctx context.Context, srv remote.Server, st *steps, inUse bool) error {
	if inUse {
		st.next("Health check")
	} else {
		st.next("Reboot check")
	}
	return checkServices(ctx, srv, !inUse)
}

var doneBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#4ADE80")).Padding(0, 2)

// runTasks runs a script that announces its stages as "::task <name>" lines,
// showing them as live progress. Other output is hidden, but the last lines are
// shown if the script fails.
func runTasks(ctx context.Context, srv remote.Server, user string, sc remote.Script) error {
	const keep = 25
	var tail []string
	p := ui.NewProgress()
	err := srv.RunStreaming(ctx, user, sc, func(line string) {
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

// adminUser picks who runs the install: root on a fresh server, or dev on a
// re-run, because the install turns root SSH off.
func adminUser(ctx context.Context, srv remote.Server) (string, error) {
	if srv.CanLogin(ctx, DevUser) {
		return DevUser, nil
	}
	if srv.CanLogin(ctx, "root") {
		return "root", nil
	}
	fmt.Println("  Your SSH key isn't on the server yet. Enter the root password from your provider:")
	if err := srv.CopyID(ctx, "root"); err != nil || !srv.CanLogin(ctx, "root") {
		return "", fmt.Errorf("can't log in as root: check the address and password. "+
			"If you reinstalled the server, first run: ssh-keygen -R %s", srv.Host)
	}
	return "root", nil
}

// srvOK reports whether a check run as the dev user succeeds.
func srvOK(ctx context.Context, srv remote.Server, check string) bool {
	_, err := srv.Output(ctx, DevUser, userPath+check)
	return err == nil
}

// checkServices confirms T3, Docker and Tailscale are running, after first
// rebooting the server if reboot is set, to prove they start by themselves.
func checkServices(ctx context.Context, srv remote.Server, reboot bool) (err error) {
	p := ui.NewProgress()
	defer func() { p.Done(err) }()
	if reboot {
		if err := rebootAndWait(ctx, srv, p); err != nil {
			return err
		}
	}

	p.Task("T3 Code, Docker and Tailscale running")
	// Services can still be starting just after a boot, so give them a minute and a half.
	deadline := time.Now().Add(90 * time.Second)
	for {
		down := servicesDown(ctx, srv)
		if len(down) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("not everything is running (%s). Run the same command again to repair it", strings.Join(down, ", "))
		}
		if err := sleep(ctx, 3*time.Second); err != nil {
			return err
		}
	}
}

// servicesDown lists the services that aren't running, as "<name> is <state>".
func servicesDown(ctx context.Context, srv remote.Server) []string {
	// Prints "<name> <state>" per line; is-active exits non-zero for stopped units, so ignore the exit code.
	out, _ := srv.Output(ctx, DevUser, `for u in docker tailscaled; do echo "$u $(systemctl is-active $u)"; done; `+
		`echo "T3 $(systemctl --user is-active t3code.service)"`)
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

func rebootAndWait(ctx context.Context, srv remote.Server, p *ui.Progress) error {
	p.Task("Rebooting")
	_ = srv.Run(ctx, DevUser, "sudo systemctl reboot >/dev/null 2>&1") // the connection drops as the server goes down
	if err := sleep(ctx, 15*time.Second); err != nil {
		return err
	}

	p.Task("Waiting for the server to come back")
	deadline := time.Now().Add(5 * time.Minute)
	for !srv.CanLogin(ctx, DevUser) {
		if time.Now().After(deadline) {
			return errors.New("the server didn't come back within 5 minutes; check it in your provider's console")
		}
		if err := sleep(ctx, 5*time.Second); err != nil {
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

func ensureKey(ctx context.Context) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	key := filepath.Join(home, ".ssh", "id_ed25519")
	if _, err := os.Stat(key + ".pub"); err == nil {
		lipgloss.Println("  " + ui.Muted.Render("Using "+key+".pub"))
		return nil
	}
	fmt.Println("  No SSH key yet, creating one. Press Enter at each question.")
	gen := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-f", key)
	gen.Stdin, gen.Stdout, gen.Stderr = os.Stdin, os.Stdout, os.Stderr
	return gen.Run()
}
