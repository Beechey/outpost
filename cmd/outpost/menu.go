package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/servers"
	"github.com/Beechey/outpost/internal/setup"
	"github.com/Beechey/outpost/internal/ui"
)

// interactive reports whether outpost can show the menu: a person at a terminal.
func interactive() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

// menu is plain `outpost`: pick a server, see its status card, and change
// any part of it. Each choice runs the same command you could type, so the
// menu and the commands never disagree.
func menu(ctx context.Context) error {
	lipgloss.Println("\n" + ui.Banner)
	srv, r, err := pickOrSetUp(ctx)
	if err != nil {
		return quiet(err)
	}
	for {
		r.Print(srv)
		if r.Retired() {
			lipgloss.Println("\n  " + ui.Muted.Render("Its work lives on another server now. Pick that one:"))
			if srv, r, err = pickOrSetUp(ctx); err != nil {
				return quiet(err)
			}
			continue
		}
		fmt.Println()
		lipgloss.Println("  " + ui.Muted.Render("↑↓ to choose, Enter to go, Esc or ← to go back"))
		choice, err := ui.Ask.Choose(ctx, "What would you like to do?", actions(r), "address")
		if errors.Is(err, ui.ErrCancelled) {
			choice = "switch" // back, from the menu, is the server list
		} else if err != nil {
			return err
		}
		if choice == "quit" {
			return nil
		}
		if choice == "switch" {
			if srv, r, err = pickOrSetUp(ctx); err != nil {
				return quiet(err)
			}
			continue
		}
		next, err := runAction(ctx, srv, choice)
		switch {
		case errors.Is(err, ui.ErrCancelled):
			// Back: straight to the menu.
		case err != nil:
			lipgloss.Println("\n  " + ui.Warn("✗ ") + err.Error())
			ui.Pause(ctx, "Back to the menu")
		default:
			ui.Pause(ctx, "Back to the menu")
		}
		if next != nil {
			srv = *next
		}
		if r, err = inspect(ctx, srv); err != nil {
			return err
		}
	}
}

// actions are the menu's choices for a server, labelled with what each changes.
func actions(r setup.Report) []ui.Option {
	gwOn, projects := r.Gateway()
	opts := []ui.Option{
		{Label: "Show addresses: T3 Code and each project", Value: "address"},
		{Label: "Access: how devices reach T3 (now " + r.Access().String() + ")", Value: "access"},
	}
	if r.Access() == setup.AccessTailscale {
		opts = append(opts, ui.Option{Label: "Pair a device with T3 over Tailscale", Value: "pair"})
	}
	gw := "off"
	if gwOn {
		gw = fmt.Sprintf("on, %d project%s", projects, map[bool]string{true: "", false: "s"}[projects == 1])
	}
	opts = append(opts,
		ui.Option{Label: "Gateway: HTTPS addresses for projects (now " + gw + ")", Value: "gateway"},
		ui.Option{Label: "Add a project's address", Value: "add"},
	)
	if projects > 0 {
		opts = append(opts, ui.Option{Label: "Remove a project's address", Value: "remove"})
	}
	if r.GatewayMode() == setup.GatewayDomain {
		opts = append(opts, ui.Option{Label: "Trust the gateway's certificate on this computer", Value: "trust"})
	}
	return append(opts,
		ui.Option{Label: "Sign in again, where a login has expired", Value: "login"},
		ui.Option{Label: "Update T3 Code, Claude Code and packages", Value: "update"},
		ui.Option{Label: "Swap: backup memory on disk", Value: "swap"},
		ui.Option{Label: "Rename the server", Value: "rename"},
		ui.Option{Label: "Back up to this computer", Value: "backup"},
		ui.Option{Label: "Move everything to a new server", Value: "move"},
		ui.Option{Label: "Switch to another server", Value: "switch"},
		ui.Option{Label: "Quit", Value: "quit"},
	)
}

// runAction runs one menu choice, asking whatever the command would. It
// returns the server to show next when that changes, as after a move.
func runAction(ctx context.Context, srv remote.Server, choice string) (*remote.Server, error) {
	switch choice {
	case "address":
		return nil, setup.Address(ctx, srv)
	case "access":
		return nil, setup.SetAccess(ctx, srv, setup.AccessUnset)
	case "pair":
		return nil, setup.Pair(ctx, srv)
	case "gateway":
		return nil, setup.Gateway(ctx, srv, setup.GatewayOptions{})
	case "add":
		return nil, setup.GatewayAdd(ctx, srv, "", "", nil, nil, false)
	case "remove":
		return nil, setup.GatewayRemove(ctx, srv, "", false)
	case "trust":
		return nil, setup.GatewayTrust(ctx, srv)
	case "login":
		return nil, setup.Login(ctx, srv, "")
	case "update":
		return nil, setup.Update(ctx, srv, false)
	case "swap":
		return nil, setup.Swap(ctx, srv, "")
	case "rename":
		return nil, setup.Rename(ctx, srv, "")
	case "backup":
		file := "outpost-" + strings.ReplaceAll(srv.String(), ":", "-") + "-" + time.Now().Format("2006-01-02-1504") + ".tar.gz"
		return nil, setup.Backup(ctx, srv, file, false)
	case "move":
		dst, err := servers.Pick(ctx, "Which server are you moving to?", srv)
		if err != nil {
			return nil, err
		}
		if err := setup.Move(ctx, srv, dst, false, setup.AccessUnset, "", ""); err != nil {
			return nil, err
		}
		_ = servers.Forget(srv)
		_ = servers.Remember(dst)
		return &dst, nil
	}
	return nil, fmt.Errorf("unknown choice %q", choice)
}

// pickOrSetUp picks a server and reads its status, offering to set it up if
// outpost hasn't yet.
func pickOrSetUp(ctx context.Context) (remote.Server, setup.Report, error) {
	srv, err := servers.Pick(ctx, "Which server?")
	if err != nil {
		return srv, setup.Report{}, err
	}
	if r, err := inspect(ctx, srv); err == nil {
		_ = servers.Remember(srv)
		return srv, r, nil
	}
	ok, err := ui.Ask.Confirm(ctx, srv.String()+" isn't set up with outpost yet. Set it up now?",
		"outpost installs and secures it, signs it in, and reboots it to prove it comes back.\nAbout ten minutes.", "Set it up")
	if err != nil {
		return srv, setup.Report{}, err
	}
	if !ok {
		return srv, setup.Report{}, ui.ErrCancelled
	}
	if err := setup.Setup(ctx, srv, setup.AccessUnset, "", "", setup.GatewayOptions{}); err != nil {
		return srv, setup.Report{}, err
	}
	_ = servers.Remember(srv)
	r, err := inspect(ctx, srv)
	return srv, r, err
}

// inspect reads a server's status with a spinner, since it takes a few seconds.
func inspect(ctx context.Context, srv remote.Server) (r setup.Report, err error) {
	p := ui.NewProgress()
	p.Task("Checking " + srv.String())
	r, err = setup.Inspect(ctx, srv)
	p.Clear()
	return r, err
}

// quiet turns Ctrl-C at the menu into a clean exit.
func quiet(err error) error {
	if errors.Is(err, ui.ErrCancelled) {
		return nil
	}
	return err
}
