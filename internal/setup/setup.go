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
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
)

const totalSteps = 6

// DevUser runs T3 Code and the agents. It must match DEV_USER in bootstrap.sh.
const DevUser = "dev"

var (
	//go:embed scripts/bootstrap.sh
	bootstrapScript []byte
	//go:embed scripts/login.sh
	loginScript []byte
	//go:embed scripts/repos.sh
	reposScript []byte

	// No leading "-", so a host can't be read as an ssh option.
	hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]*$`)
	repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

	stepStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7AA2F7"))
	okStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#9ECE6A"))
)

// Options configures a setup run.
type Options struct {
	Host  string
	Port  int
	Repos []string // owner/name; empty means ask on the server
}

// Validate checks everything that ends up in a remote shell command.
func (o Options) Validate() error {
	if !hostRe.MatchString(o.Host) {
		return fmt.Errorf("%q doesn't look like a server address", o.Host)
	}
	if o.Port < 1 || o.Port > 65535 {
		return fmt.Errorf("port %d is out of range", o.Port)
	}
	for _, r := range o.Repos {
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

	step(1, "Checking your SSH key")
	if err := ensureKey(ctx); err != nil {
		return fmt.Errorf("create SSH key: %w", err)
	}

	step(2, "Connecting to "+o.Host)
	admin, err := adminUser(ctx, srv)
	if err != nil {
		return err
	}

	step(3, "Installing everything (3-5 minutes)")
	if err := srv.RunScript(ctx, admin, bootstrapScript, "sudo bash"); err != nil {
		return fmt.Errorf("server install failed, see the output above: %w", err)
	}

	step(4, "Logins: approve each link on your laptop")
	if err := srv.RunScript(ctx, DevUser, loginScript, "bash"); err != nil {
		return fmt.Errorf("a login failed, see the output above, then run the same command again: %w", err)
	}

	step(5, "Cloning your repos into T3")
	if err := srv.RunScript(ctx, DevUser, reposScript, "bash", o.Repos...); err != nil {
		return fmt.Errorf("repo setup failed, see the output above: %w", err)
	}

	step(6, "Rebooting to check everything comes back by itself")
	if err := rebootCheck(ctx, srv); err != nil {
		return err
	}
	lipgloss.Println(okStyle.Render("\nAll set.") + " Open T3 Code, pick this server, and start prompting.")
	return nil
}

func step(n int, msg string) {
	lipgloss.Println(stepStyle.Render(fmt.Sprintf("\n[%d/%d] %s", n, totalSteps, msg)))
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
	fmt.Println("Your SSH key isn't on the server yet. Enter the root password from your provider:")
	if err := srv.CopyID(ctx, "root"); err != nil || !srv.CanLogin(ctx, "root") {
		return "", fmt.Errorf("can't log in as root: check the address and password. "+
			"If you reinstalled the server, first run: ssh-keygen -R %s", srv.Host)
	}
	return "root", nil
}

func rebootCheck(ctx context.Context, srv remote.Server) error {
	_ = srv.Run(ctx, DevUser, "sudo systemctl reboot") // the connection drops as the server goes down
	if err := sleep(ctx, 15*time.Second); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Minute)
	for !srv.CanLogin(ctx, DevUser) {
		if time.Now().After(deadline) {
			return errors.New("the server didn't come back within 5 minutes; check it in your provider's console")
		}
		if err := sleep(ctx, 5*time.Second); err != nil {
			return err
		}
	}
	if err := srv.Run(ctx, DevUser,
		"systemctl is-active docker tailscaled && systemctl --user is-active t3code.service"); err != nil {
		return errors.New("something didn't start after the reboot (see above); run the same command again to repair it")
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
		fmt.Println("Using", key+".pub")
		return nil
	}
	fmt.Println("No SSH key yet, creating one. Press Enter at each question.")
	gen := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-f", key)
	gen.Stdin, gen.Stdout, gen.Stderr = os.Stdin, os.Stdout, os.Stderr
	return gen.Run()
}
