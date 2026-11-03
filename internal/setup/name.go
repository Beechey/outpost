package setup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// A server's name is its hostname. T3 Connect and Tailscale show it, and
// T3 Connect keeps the name it saw when it first signed in, so setup names
// the server before the logins.

var nameRule = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func checkName(s string) error {
	if s != "" && !nameRule.MatchString(s) {
		return fmt.Errorf("%q can't be a server name: use lowercase letters, numbers and hyphens, up to 63 characters", s)
	}
	return nil
}

// setNameCmd sets the hostname, keeps sudo resolving it, stops cloud-init
// putting the provider's name back on the next boot, and renames the server in
// Tailscale if it's signed in, which otherwise keeps its first name.
func setNameCmd(name string) string {
	return "sudo bash -c " + quote(`set -e
hostnamectl set-hostname `+name+`
sed -i '/^127\.0\.1\.1[[:space:]]/d' /etc/hosts
echo '127.0.1.1 `+name+`' >> /etc/hosts
if [ -d /etc/cloud/cloud.cfg.d ]; then echo 'preserve_hostname: true' > /etc/cloud/cloud.cfg.d/99-outpost-hostname.cfg; fi
if command -v tailscale >/dev/null && tailscale status >/dev/null 2>&1; then tailscale set --hostname=`+name+`; fi`)
}

// nameServer renames the server to answer, as admin. With no answer, a fresh
// server asks and a set-up one keeps its name.
func nameServer(ctx context.Context, h remote.Host, admin, answer string, fresh bool) error {
	if err := checkName(answer); err != nil {
		return err
	}
	out, _ := h.Output(ctx, admin, "hostname")
	now := strings.TrimSpace(string(out))
	if answer == "" && fresh {
		var err error
		answer, err = ui.Ask.Input(ctx, "What should this server be called?",
			"T3 Code and Tailscale show this name, and T3 Connect can't change it later.\n"+
				"Leave it empty to keep "+now+".",
			"e.g. outpost", checkName)
		if errors.Is(err, ui.ErrNoTerminal) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	if answer == "" {
		return nil
	}
	if _, err := h.Output(ctx, admin, setNameCmd(answer)); err != nil {
		return fmt.Errorf("rename the server: %w", err)
	}
	ui.Check("Name", answer)
	return nil
}

// Rename renames a set-up server, in Tailscale too.
func Rename(ctx context.Context, h remote.Host, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if _, err := openActive(ctx, h); err != nil {
		return err
	}
	return nameServer(ctx, h, DevUser, name, false)
}
