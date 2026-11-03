// Package ui holds outpost's terminal output: step headers and live progress.
package ui

import (
	"fmt"
	"os"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

var (
	Accent  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80"))
	Bold    = lipgloss.NewStyle().Bold(true)
	Muted   = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B90A0"))
	failed  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
	frames  = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	isTTY   = term.IsTerminal(os.Stdout.Fd())
	tickDur = 80 * time.Millisecond
)

// Header prints a numbered step heading.
func Header(n, total int, msg string) {
	lipgloss.Println("\n" + Accent.Render(fmt.Sprintf("%d/%d", n, total)) + "  " + Bold.Render(msg))
}

var doneBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Accent.GetForeground()).Padding(0, 2)

// Done prints the closing summary of a flow in a box.
func Done(text string) {
	lipgloss.Println("\n" + doneBox.Render(text))
}

// Check prints a finished item: "✓ name note".
func Check(name, note string) {
	lipgloss.Println("  " + Accent.Render("✓") + " " + name + " " + Muted.Render(note))
}

// Problem prints something wrong and how to fix it: "✗ name note. Fix: fix".
func Problem(name, note, fix string) {
	lipgloss.Println("  " + failed.Render("✗") + " " + name + " " + Muted.Render(note+". Fix: ") + fix)
}

// Progress shows one line per task: a spinner while it runs, then ✓ and its duration.
type Progress struct {
	mu      sync.Mutex
	task    string
	started time.Time
	stop    chan struct{}
	stopped chan struct{}
}

// NewProgress starts a progress display. Call Done when finished.
func NewProgress() *Progress {
	p := &Progress{stop: make(chan struct{}), stopped: make(chan struct{})}
	go p.spin()
	return p
}

// Task marks the current task as finished and starts the next one.
func (p *Progress) Task(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finish(Accent.Render("✓"))
	p.task, p.started = name, time.Now()
	if !isTTY {
		fmt.Println("  • " + name)
	}
}

// Update renames the running task, for example to show how far along it is.
func (p *Progress) Update(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.task = name
}

// Done marks the current task finished, or failed if err is non-nil, and stops the spinner.
func (p *Progress) Done(err error) {
	close(p.stop)
	<-p.stopped
	p.mu.Lock()
	defer p.mu.Unlock()
	mark := Accent.Render("✓")
	if err != nil {
		mark = failed.Render("✗")
	}
	p.finish(mark)
}

// Clear stops the spinner and erases its line, for a wait that needs no record.
func (p *Progress) Clear() {
	p.mu.Lock()
	p.task = ""
	p.mu.Unlock()
	close(p.stop)
	<-p.stopped
	if isTTY {
		fmt.Print("\r\033[K")
	}
}

func (p *Progress) finish(mark string) {
	if p.task == "" {
		return
	}
	if isTTY {
		elapsed := time.Since(p.started).Round(time.Second)
		lipgloss.Printf("\r\033[K  %s %s %s\n", mark, p.task, Muted.Render(elapsed.String()))
	}
	p.task = ""
}

func (p *Progress) spin() {
	defer close(p.stopped)
	if !isTTY {
		<-p.stop
		return
	}
	t := time.NewTicker(tickDur)
	defer t.Stop()
	for i := 0; ; i++ {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.mu.Lock()
			if p.task != "" {
				lipgloss.Printf("\r\033[K  %s %s", Accent.Render(frames[i%len(frames)]), p.task)
			}
			p.mu.Unlock()
		}
	}
}

// Warn colours text as a problem.
func Warn(s string) string { return failed.Render(s) }

var (
	cardBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3A4152")).Padding(1, 2)
	cardTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC"))
	cardLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B90A0")).Width(10)
)

// A Row is one line of a Card: a label and its value.
type Row struct{ Label, Value string }

// Card draws a titled box of labelled rows, with an optional subtitle.
func Card(title, subtitle string, rows []Row) string {
	head := Accent.Render("▲") + " " + cardTitle.Render(title)
	if subtitle != "" {
		head += "  " + Muted.Render(subtitle)
	}
	lines := []string{head, ""}
	for _, r := range rows {
		lines = append(lines, cardLabel.Render(r.Label)+r.Value)
	}
	return cardBox.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// Mark is ✓ name, or ✗ name when bad.
func Mark(name string, bad bool) string {
	if bad {
		return failed.Render("✗ " + name)
	}
	return Accent.Render("✓") + " " + name
}
