package setup

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/dustin/go-humanize"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

//go:embed scripts/survey.sh
var surveyScript []byte

// movable lists what a move carries over, relative to the dev user's home.
// It must match the paths in survey.sh.
var movable = map[string]bool{
	"code": true, ".t3/userdata": true, ".t3/worktrees": true,
	".claude": true, ".claude.json": true, ".codex": true, ".gitconfig": true,
}

// Never copied: logins (you approve them again on the new server), T3's
// per-process lock files and logs, and installed dependencies (reinstalled on demand).
var moveExcludes = []string{
	".claude/.credentials.json", ".codex/auth.json",
	".t3/userdata/server-owner.sqlite*", ".t3/userdata/server-runtime.json",
	".t3/userdata/logs/*", // the folder must exist: T3's service writes its output there
	"node_modules",
}

// MoveOptions configures a move from one server to another.
type MoveOptions struct {
	Old, New         string
	OldPort, NewPort int
	Yes              bool // skip the confirmation
}

type survey struct {
	t3                 bool
	movedTo            string
	projects, sessions string
	size               string
	paths              []string
}

// Move sets up a new server and moves everything from an old one onto it: code,
// T3 threads and projects, and agent history and settings. The new server
// takes over as the same T3 environment. Safe to re-run if it stops partway.
func Move(ctx context.Context, o MoveOptions) error {
	if err := validateServer(o.Old, o.OldPort); err != nil {
		return err
	}
	if err := validateServer(o.New, o.NewPort); err != nil {
		return err
	}
	if o.Old == o.New && o.OldPort == o.NewPort {
		return errors.New("the old and new server are the same")
	}
	old := remote.Server{Host: o.Old, Port: o.OldPort}
	srv := remote.Server{Host: o.New, Port: o.NewPort}
	lipgloss.Println("\n" + banner)
	st := &steps{total: 7}

	st.next("SSH key")
	if err := ensureKey(ctx); err != nil {
		return fmt.Errorf("create SSH key: %w", err)
	}

	st.next("Checking " + o.Old)
	sv, err := surveyServer(ctx, old)
	if err != nil {
		return err
	}
	alreadyMoved := sv.movedTo == o.New
	if sv.movedTo != "" && !alreadyMoved {
		return fmt.Errorf("%s was already moved to %s. Move from that server instead", o.Old, sv.movedTo)
	}
	if !sv.t3 {
		return fmt.Errorf("%s doesn't have T3 Code set up, so there's nothing to move. Use: outpost setup %s", o.Old, o.New)
	}
	printSurvey(sv, alreadyMoved)

	st.next("Connecting to " + o.New)
	admin, _, err := connect(ctx, srv)
	if err != nil {
		return err
	}
	if !alreadyMoved && !o.Yes {
		if err := confirmMove(ctx, o); err != nil {
			return err
		}
	}

	st.next("Installing on " + o.New + " (3-5 minutes)")
	if err := install(ctx, srv, admin); err != nil {
		return err
	}

	st.next("Moving your work")
	if alreadyMoved {
		lipgloss.Println("  " + ui.Accent.Render("✓") + " Already moved " + ui.Muted.Render("finishing the remaining steps"))
	} else if err := transfer(ctx, old, srv, sv); err != nil {
		return err
	}

	st.next("Logins on " + o.New)
	if err := runLogins(ctx, srv); err != nil {
		return err
	}
	if err := finalCheck(ctx, srv, st, false); err != nil {
		return err
	}

	lipgloss.Println("\n" + doneBox.Render(
		ui.Accent.Render("✓ Moved.")+" Everything from "+ui.Bold.Render(o.Old)+" now lives on "+ui.Bold.Render(o.New)+".\n\n"+
			"Your threads continue in T3 Code where they left off; send any that were\n"+
			"mid-turn a \"continue\". Projects may need their dependencies reinstalled.\n\n"+
			ui.Muted.Render("When you're happy, delete "+o.Old+" at your provider and remove it from Tailscale.\n"+
				"Until then its files are untouched, and T3 there stays off.")))
	return nil
}

func surveyServer(ctx context.Context, old remote.Server) (survey, error) {
	var sv survey
	if !old.CanLogin(ctx, DevUser) {
		return sv, fmt.Errorf("can't log in to %s as %s. Is it a server set up with outpost?", old.Host, DevUser)
	}
	err := old.RunStreaming(ctx, DevUser, remote.Script{Body: surveyScript, Runner: "bash"}, func(line string) {
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "t3":
			sv.t3 = val == "yes"
		case "moved":
			sv.movedTo = val
		case "projects":
			sv.projects = val
		case "sessions":
			sv.sessions = val
		case "size":
			sv.size = val
		case "paths":
			for _, p := range strings.Fields(val) {
				if movable[p] { // only ever our own fixed paths reach the tar command
					sv.paths = append(sv.paths, p)
				}
			}
		}
	})
	if err != nil {
		return sv, fmt.Errorf("check %s: %w", old.Host, err)
	}
	return sv, nil
}

func printSurvey(sv survey, alreadyMoved bool) {
	if alreadyMoved {
		lipgloss.Println("  " + ui.Muted.Render("Moved already; picking up where the last run stopped"))
		return
	}
	row := func(label, val string) {
		lipgloss.Println("  " + ui.Accent.Render("•") + " " + label + " " + ui.Muted.Render(val))
	}
	row("Code", sv.projects+" folders in ~/code, uncommitted work and worktrees included")
	row("T3 Code", "threads, projects and settings")
	row("Agents", sv.sessions+" Claude and Codex sessions, plus your CLAUDE.md, skills and settings")
	row("Total", sv.size+", not counting node_modules")
}

func confirmMove(ctx context.Context, o MoveOptions) error {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return errors.New("run in a terminal to confirm, or pass --yes")
	}
	ok := false
	err := huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title("Move everything from " + o.Old + " to " + o.New + "?").
		Description("T3 on " + o.Old + " stops now (agents mid-turn there will stop) and continues on " + o.New + ".\n" +
			o.Old + "'s files stay untouched until you delete it. T3 data already on " + o.New + " is replaced.").
		Affirmative("Move").
		Negative("Cancel").
		Value(&ok),
	)).RunWithContext(ctx)
	if err != nil && !errors.Is(err, huh.ErrUserAborted) {
		return err
	}
	if !ok {
		return errors.New("cancelled. Nothing was changed")
	}
	return nil
}

// transfer stops T3 on the old server, copies everything across, and retires
// the old server's T3 so only one machine answers as this environment. If the
// copy fails, T3 starts again on the old server.
func transfer(ctx context.Context, old, srv remote.Server, sv survey) (err error) {
	p := ui.NewProgress()
	defer func() { p.Done(err) }()

	p.Task("Stopping T3 on " + old.Host)
	if _, err := old.Output(ctx, DevUser, "systemctl --user stop t3code.service 2>/dev/null; true"); err != nil {
		return fmt.Errorf("stop T3 on %s: %w", old.Host, err)
	}
	restartOld := func() { _, _ = old.Output(ctx, DevUser, "systemctl --user start t3code.service") }

	p.Task("Clearing T3 data on " + srv.Host)
	if _, err := srv.Output(ctx, DevUser,
		"systemctl --user stop t3code.service 2>/dev/null; rm -rf ~/.t3/userdata ~/.t3/worktrees"); err != nil {
		restartOld()
		return fmt.Errorf("prepare %s: %w", srv.Host, err)
	}

	label := "Copying " + sv.size
	p.Task(label)
	excludes := make([]string, 0, len(moveExcludes))
	for _, e := range moveExcludes {
		excludes = append(excludes, "--exclude='"+e+"'")
	}
	src := "cd ~ && tar -cz " + strings.Join(excludes, " ") + " " + strings.Join(sv.paths, " ")
	err = remote.Copy(ctx, old, srv, DevUser, src, "tar -xz -C ~", func(n int64) {
		p.Update(label + " " + ui.Muted.Render(humanize.Bytes(uint64(n))+" sent"))
	})
	if err != nil {
		restartOld()
		return fmt.Errorf("copy failed, so T3 was restarted on %s and nothing is lost: %w", old.Host, err)
	}

	p.Task("Retiring T3 on " + old.Host)
	// Keep the data, but don't let T3 start there again, and remember where it went.
	if _, err := old.Output(ctx, DevUser,
		`export PATH="$HOME/.local/bin:$PATH"; t3 service uninstall >/dev/null 2>&1; echo `+srv.Host+` > ~/.t3/moved-to`); err != nil {
		return fmt.Errorf("retire T3 on %s: %w", old.Host, err)
	}

	p.Task("Starting T3 on " + srv.Host)
	// Only matters on a re-run, where the new server already had the service; otherwise the T3 Connect login installs it.
	_, _ = srv.Output(ctx, DevUser, "systemctl --user start t3code.service 2>/dev/null; true")
	return nil
}
