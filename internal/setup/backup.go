package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// Backup downloads a server's work into file, a .tar.gz on this machine: the
// same things a move carries (see manifest.go), logins excluded. T3 stops while
// it downloads, so its database is saved whole, then starts again. yes skips
// the confirmation. A retired server can be backed up too.
func Backup(ctx context.Context, h remote.Host, file string, yes bool) error {
	var d dev
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s already exists. Choose another file name", file)
	}
	var (
		sv      survey
		running bool
	)
	lipgloss.Println("\n" + ui.Banner)
	err := runPlan(ctx, []step{
		{"SSH key", ensureKey},
		{"Checking " + h.String(), func(ctx context.Context) (err error) {
			if d, err = open(ctx, h); err != nil {
				return err
			}
			out, err := d.output(ctx, surveyCmd())
			if err != nil {
				return fmt.Errorf("check %s: %w", h, err)
			}
			if sv = parseSurvey(string(out)); len(sv.paths) == 0 {
				return fmt.Errorf("%s has nothing to back up", h)
			}
			printSurvey(sv, false)
			running = d.t3Running(ctx)
			if !running || yes {
				return nil
			}
			return confirm(ctx, fmt.Sprintf("Pause T3 on %s while it backs up?", h),
				"Agents mid-turn there will stop. T3 starts again as soon as the download finishes.", "Back up")
		}},
		{"Downloading", func(ctx context.Context) error { return download(ctx, d, sv, file, running) }},
	})
	if err != nil {
		return err
	}
	ui.Done(ui.Accent.Render("✓ Backed up.") + " " + ui.Bold.Render(h.String()) + " is saved in " + ui.Bold.Render(file) + ".\n\n" +
		ui.Muted.Render("Logins aren't included. To restore it onto a server, see\n"+
			"https://github.com/Beechey/outpost/blob/main/docs/guide.md#back-up-a-server"))
	return nil
}

// download streams the archive into file, pausing T3 if it was running. A
// failed download leaves no file behind, and T3 starts again either way.
func download(ctx context.Context, d dev, sv survey, file string, running bool) (err error) {
	p := ui.NewProgress()
	defer func() { p.Done(err) }()

	if running {
		p.Task("Pausing T3")
		if err := d.stopT3(ctx); err != nil {
			return fmt.Errorf("pause T3: %w", err)
		}
		defer func() {
			if err == nil { // after a failure, the failed task keeps the last line
				p.Task("Starting T3 again")
			}
			// Even after Ctrl-C, so the server isn't left without T3.
			if startErr := d.startT3(context.WithoutCancel(ctx)); startErr != nil {
				err = errors.Join(err, fmt.Errorf("T3 on %s is still stopped; start it as %s there with: %s", d.h, DevUser, cmdT3Start))
			}
		}()
	}

	// Download beside the final name, so a failure never leaves a partial backup there.
	f, err := os.CreateTemp(filepath.Dir(file), ".outpost-backup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // fails harmlessly once renamed

	label := strings.TrimSpace("Downloading " + sv.size)
	p.Task(label)
	err = d.h.Download(ctx, DevUser, packCmd(sv.paths), f, func(n int64) {
		p.Update(label + " " + ui.Muted.Render(humanize.Bytes(uint64(n))+" received"))
	})
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	return os.Rename(f.Name(), file)
}
