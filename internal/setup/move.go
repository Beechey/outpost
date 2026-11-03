package setup

import (
	"context"
	"errors"
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// Move sets up a new server and moves everything from an old one onto it: code,
// T3 threads and projects, and agent history and settings (see manifest.go).
// The new server takes over as the same T3 environment. Safe to re-run if it
// stops partway. yes skips the questions; access is how devices reach the new
// server, and swap and name what it gets, as the user typed them ("" asks, unless yes).
func Move(ctx context.Context, old, dst remote.Host, yes bool, access Access, swap, name string) error {
	if old.String() == dst.String() {
		return errors.New("the old and new server are the same")
	}
	if err := checkName(name); err != nil {
		return err
	}
	if yes && swap == "" {
		swap = "0"
	}
	var from dev
	to := dev{h: dst}
	var (
		sv           survey
		alreadyMoved bool
		admin        string
		inUse        bool
	)
	lipgloss.Println("\n" + ui.Banner)
	err := runPlan(ctx, []step{
		{"SSH key", ensureKey},
		{"Checking " + old.String(), func(ctx context.Context) (err error) {
			if from, err = open(ctx, old); err != nil {
				return err
			}
			out, err := from.output(ctx, surveyCmd())
			if err != nil {
				return fmt.Errorf("check %s: %w", old, err)
			}
			sv = parseSurvey(string(out))
			alreadyMoved = sv.movedTo == dst.String()
			switch {
			case sv.movedTo != "" && !alreadyMoved:
				return fmt.Errorf("%s was already moved to %s. Move from that server instead", old, sv.movedTo)
			case !sv.t3:
				return fmt.Errorf("%s doesn't have T3 Code set up, so there's nothing to move. Use: outpost setup %s", old, dst)
			}
			printSurvey(sv, alreadyMoved)
			return nil
		}},
		{"Connecting to " + dst.String(), func(ctx context.Context) (err error) {
			if admin, inUse, err = connect(ctx, dst); err != nil {
				return err
			}
			access = resolveAccess(ctx, to, admin == DevUser, access)
			if alreadyMoved || yes {
				return nil
			}
			return confirmMove(ctx, old, dst)
		}},
		{"Installing on " + dst.String() + " (3-5 minutes)", func(ctx context.Context) error {
			if err := nameServer(ctx, dst, admin, name, admin != DevUser && !yes); err != nil {
				return err
			}
			return install(ctx, dst, admin, swap)
		}},
		{"Moving your work", func(ctx context.Context) error {
			if alreadyMoved {
				ui.Check("Already moved", "finishing the remaining steps")
				return nil
			}
			return transfer(ctx, from, to, sv)
		}},
		{"Logins on " + dst.String(), func(ctx context.Context) error { return runLogins(ctx, to, access) }},
		// A first move reboots to prove the new server starts by itself; a re-run may find agents at work there.
		{"Final check", func(ctx context.Context) error { return finalCheck(ctx, to, !(alreadyMoved && inUse)) }},
	})
	if err != nil {
		return err
	}
	ui.Done(ui.Accent.Render("✓ Moved.") + " Everything from " + ui.Bold.Render(old.String()) + " now lives on " + ui.Bold.Render(dst.String()) + ".\n\n" +
		"Your threads continue in T3 Code where they left off; send any that were\n" +
		"mid-turn a \"continue\". Projects may need their dependencies reinstalled.\n\n" +
		ui.Muted.Render("When you're happy, delete "+old.String()+" at your provider and remove it from Tailscale.\n"+
			"Until then its files are untouched, and T3 there stays off.\n"+accessNote(dst, access)))
	return nil
}

func printSurvey(sv survey, alreadyMoved bool) {
	if alreadyMoved {
		lipgloss.Println("  " + ui.Muted.Render("Moved already; picking up where the last run stopped"))
		return
	}
	row := func(label, val string) {
		lipgloss.Println("  " + ui.Accent.Render("•") + " " + label + " " + ui.Muted.Render(val))
	}
	row("Code", plural(sv.projects, "folder")+" in ~/code, uncommitted work and worktrees included")
	row("T3 Code", "threads, projects and settings")
	row("Agents", plural(sv.sessions, "Claude or Codex session")+", plus your CLAUDE.md, skills and settings")
	if sv.size != "" {
		row("Total", sv.size+", not counting node_modules")
	}
}

func plural(n, noun string) string {
	if n == "1" {
		return n + " " + noun
	}
	return n + " " + noun + "s"
}

func confirmMove(ctx context.Context, old, dst remote.Host) error {
	return confirm(ctx, fmt.Sprintf("Move everything from %s to %s?", old, dst),
		fmt.Sprintf("T3 on %s stops now (agents mid-turn there will stop) and continues on %s.\n"+
			"%s's files stay untouched until you delete it. T3 data already on %s is replaced.", old, dst, old, dst),
		"Move")
}

// transfer stops T3 on the old host, copies everything to dst, and retires
// the old host's T3 so only one machine answers as this environment. If the
// copy fails, T3 starts again on the old host.
func transfer(ctx context.Context, from, to dev, sv survey) (err error) {
	old, dst := from.h, to.h
	p := ui.NewProgress()
	defer func() { p.Done(err) }()

	p.Task("Stopping T3 on " + old.String())
	if err := from.stopT3(ctx); err != nil {
		return fmt.Errorf("stop T3 on %s: %w", old, err)
	}
	// undo restarts T3 on the old host after a failure, even if the run was cancelled with Ctrl-C.
	undo := func(cause error) error {
		if err := from.startT3(context.WithoutCancel(ctx)); err != nil {
			return fmt.Errorf("%w. T3 on %s is still stopped; start it as %s there with: %s", cause, old, DevUser, cmdT3Start)
		}
		return fmt.Errorf("%w. T3 is running on %s again, so nothing is lost", cause, old)
	}

	p.Task("Clearing T3 data on " + dst.String())
	if err := to.stopT3(ctx); err != nil {
		return undo(fmt.Errorf("prepare %s: %w", dst, err))
	}
	if _, err := to.output(ctx, clearCmd()); err != nil {
		return undo(fmt.Errorf("prepare %s: %w", dst, err))
	}

	label := "Copying " + sv.size
	p.Task(label)
	err = old.CopyTo(ctx, dst, DevUser, packCmd(sv.paths), unpackCmd, func(n int64) {
		p.Update(label + " " + ui.Muted.Render(humanize.Bytes(uint64(n))+" sent"))
	})
	if err != nil {
		return undo(fmt.Errorf("copy failed: %w", err))
	}

	p.Task("Retiring T3 on " + old.String())
	if err := from.retireT3(ctx, dst); err != nil {
		return fmt.Errorf("retire T3 on %s: %w", old, err)
	}

	p.Task("Starting T3 on " + dst.String())
	// Only matters on a re-run, where the new server already had the service; otherwise the T3 Connect login installs it.
	_ = to.startT3(ctx)
	return nil
}
