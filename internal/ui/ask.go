package ui

import (
	"context"
	"errors"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
)

// Asker asks the user questions. Terminal asks in the terminal; tests answer
// from a script instead.
type Asker interface {
	// Confirm asks a yes-or-no question; yes labels the button that goes ahead.
	Confirm(ctx context.Context, title, description, yes string) (bool, error)
	// Choose asks for one of options, starting on current, and returns its Value.
	Choose(ctx context.Context, title string, options []Option, current string) (string, error)
	// Input asks for text that validate accepts.
	Input(ctx context.Context, title, description, placeholder string, validate func(string) error) (string, error)
	// Secret asks for text that isn't shown as it's typed, such as an API key.
	Secret(ctx context.Context, title, description string) (string, error)
}

// An Option is one answer to Choose.
type Option struct{ Label, Value string }

var (
	// ErrNoTerminal means there's no terminal to ask in, so the caller falls
	// back: to a default, or to an error naming the flag that answers instead.
	ErrNoTerminal = errors.New("no terminal to ask in")
	// ErrCancelled means the user pressed Ctrl-C at a question.
	ErrCancelled = errors.New("cancelled. Nothing was changed")
)

// Ask is how outpost asks its questions.
var Ask Asker = Terminal{}

// Terminal asks with interactive forms on the terminal.
type Terminal struct{}

func (Terminal) Confirm(ctx context.Context, title, description, yes string) (bool, error) {
	ok := false
	err := run(ctx, huh.NewConfirm().Title(title).Description(description).Affirmative(yes).Negative("Cancel").Value(&ok))
	return ok, err
}

func (Terminal) Choose(ctx context.Context, title string, options []Option, current string) (string, error) {
	opts := make([]huh.Option[string], 0, len(options))
	for _, o := range options {
		opts = append(opts, huh.NewOption(o.Label, o.Value))
	}
	// In a list, ← goes back too: it has no other use there.
	err := run(ctx, huh.NewSelect[string]().Title(title).Options(opts...).Value(&current), "left")
	return current, err
}

func (Terminal) Input(ctx context.Context, title, description, placeholder string, validate func(string) error) (string, error) {
	answer := ""
	err := run(ctx, huh.NewInput().Title(title).Description(description).Placeholder(placeholder).Validate(validate).Value(&answer))
	return answer, err
}

func (Terminal) Secret(ctx context.Context, title, description string) (string, error) {
	answer := ""
	notEmpty := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("paste it here, or press Ctrl-C")
		}
		return nil
	}
	err := run(ctx, huh.NewInput().Title(title).Description(description).EchoMode(huh.EchoModePassword).Validate(notEmpty).Value(&answer))
	return strings.TrimSpace(answer), err
}

// Pause waits for Enter (or Esc), so what's on screen stays in view until the user moves on.
func Pause(ctx context.Context, label string) {
	_ = run(ctx, huh.NewNote().Next(true).NextLabel(label), "left")
}

// run asks one question. Esc goes back, as do any extra keys; Ctrl-C too.
// All of them come back as ErrCancelled.
func run(ctx context.Context, field huh.Field, back ...string) error {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return ErrNoTerminal
	}
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys(append([]string{"ctrl+c", "esc"}, back...)...))
	err := huh.NewForm(huh.NewGroup(field)).WithKeyMap(keys).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}
	return err
}
