// Package setup turns a fresh Ubuntu server into an always-on T3 Code agent box.
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

const totalSteps = 6

// DevUser runs T3 Code and the agents. It must match DEV_USER in bootstrap.sh.
const DevUser = "dev"

var (
	//go:embed scripts/bootstrap.sh
	bootstrapScript []byte
	//go:embed scripts/repos.sh
	reposScript []byte

	// No leading "-", so a host can't be read as an ssh option.
	hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]*$`)
	repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// Options configures a setup run.
type Options struct {
	Host  string
	Port  int
	Repos []string // owner/name; empty means pick from a list
}

// Validate checks everything that ends up in a remote shell command.
func (o Options) Validate() error {
	if !hostRe.MatchString(o.Host) {
		return fmt.Errorf("%q doesn't look like a server address", o.Host)
	}
	if o.Port < 1 || o.Port > 65535 {
		return fmt.Errorf("port %d is out of range", o.Port)
	}
	return validateRepos(o.Repos)
}

func validateRepos(repos []string) error {
	for _, r := range repos {
		if !repoRe.MatchString(r) {
			return fmt.Errorf("%q isn't a repo name like owner/name", r)
		}
	}
	return nil
}

// Run performs the full setup. It is safe to re-run on a finished server.
func Run(ctx context.Context, o Options) error {
	if err := o.Validate(); err != nil {
		return err
	}
	srv := remote.Server{Host: o.Host, Port: o.Port}
	lipgloss.Println("\n" + banner)

	ui.Header(1, totalSteps, "SSH key")
	if err := ensureKey(ctx); err != nil {
		return fmt.Errorf("create SSH key: %w", err)
	}

	ui.Header(2, totalSteps, "Connecting to "+o.Host)
	admin, err := adminUser(ctx, srv)
	if err != nil {
		return err
	}
	lipgloss.Println("  " + ui.Muted.Render("Logged in as "+admin))

	ui.Header(3, totalSteps, "Installing (3-5 minutes)")
	install := remote.Script{Body: bootstrapScript, Runner: "sudo bash"}
	if err := runTasks(ctx, srv, admin, install); err != nil {
		return fmt.Errorf("the install failed (see above). Run the same command again to retry: %w", err)
	}

	ui.Header(4, totalSteps, "Logins")
	if err := runLogins(ctx, srv); err != nil {
		return err
	}

	ui.Header(5, totalSteps, "Repos")
	repos := o.Repos
	if len(repos) == 0 {
		if repos, err = pickRepos(ctx, srv); err != nil {
			return err
		}
	}
	if len(repos) == 0 {
		lipgloss.Println("  " + ui.Muted.Render("Skipped. Add repos any time: outpost setup "+o.Host+" owner/repo"))
	} else if err := runTasks(ctx, srv, DevUser, remote.Script{Body: reposScript, Runner: "bash", Args: repos}); err != nil {
		return fmt.Errorf("repo setup failed (see above): %w", err)
	}

	ui.Header(6, totalSteps, "Reboot check")
	if err := rebootCheck(ctx, srv); err != nil {
		return err
	}

	lipgloss.Println("\n" + doneBox.Render(
		ui.Accent.Render("✓ All set.")+" "+ui.Bold.Render(o.Host)+" is ready for your agents.\n\n"+
			"Open T3 Code, pick this server under Settings → Environments,\n"+
			"choose a project, and start prompting."))
	return nil
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

func rebootCheck(ctx context.Context, srv remote.Server) (err error) {
	p := ui.NewProgress()
	defer func() { p.Done(err) }()

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

	p.Task("T3 Code, Docker and Tailscale started by themselves")
	out, err := srv.Output(ctx, DevUser,
		"systemctl is-active docker tailscaled && systemctl --user is-active t3code.service")
	if err != nil {
		return fmt.Errorf("something didn't start after the reboot: %s\nRun the same command again to repair it", strings.TrimSpace(string(out)))
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
