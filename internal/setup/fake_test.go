package setup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// fakeHost is a remote.Host that records every command and answers from rules,
// so the setup and move flows can be tested without servers.
type fakeHost struct {
	name    string
	logins  map[string]bool // users that can log in
	rules   []rule          // answers for Output, first match wins
	copyErr error           // what CopyTo and Download return
	cmds    []string        // everything run here, in order
}

// A rule answers one command, named by the constant or function that builds
// it (cmdT3Running, surveyCmd(), a login's Check), so tests never repeat shell.
type rule struct {
	match string
	out   string
	err   error
}

var _ remote.Host = (*fakeHost)(nil)

func newFake(name string, users ...string) *fakeHost {
	f := &fakeHost{name: name, logins: map[string]bool{}}
	for _, u := range users {
		f.logins[u] = true
	}
	return f
}

// healthy answers the checks a working, signed-in outpost server passes.
func (f *fakeHost) healthy() *fakeHost {
	f.rules = append(f.rules, rule{match: cmdServices, out: "docker active\ntailscaled active\nT3 active\n"})
	return f
}

func (f *fakeHost) on(match, out string, err error) *fakeHost {
	f.rules = append([]rule{{match, out, err}}, f.rules...) // newest rule wins
	return f
}

func (f *fakeHost) String() string { return f.name }

func (f *fakeHost) CanLogin(_ context.Context, user string) bool { return f.logins[user] }

func (f *fakeHost) CopyID(_ context.Context, user string) error {
	f.cmds = append(f.cmds, "ssh-copy-id "+user)
	f.logins[user] = true
	return nil
}

func (f *fakeHost) Output(_ context.Context, user, cmd string) ([]byte, error) {
	f.cmds = append(f.cmds, user+": "+cmd)
	for _, r := range f.rules {
		if strings.TrimPrefix(cmd, userPath) == r.match {
			return []byte(r.out), r.err
		}
	}
	return nil, nil // anything else succeeds, quietly
}

func (f *fakeHost) Interactive(_ context.Context, user, cmd string) error {
	f.cmds = append(f.cmds, user+" (interactive): "+cmd)
	return nil
}

func (f *fakeHost) RunStreaming(_ context.Context, user string, sc remote.Script, _ func(string)) error {
	f.cmds = append(f.cmds, user+": install script")
	f.logins[DevUser] = true // the install creates the dev user
	return nil
}

func (f *fakeHost) CopyTo(_ context.Context, dst remote.Host, _, _, _ string, _ func(int64)) error {
	f.cmds = append(f.cmds, "copy to "+dst.String())
	return f.copyErr
}

func (f *fakeHost) Download(_ context.Context, _, _ string, w io.Writer, _ func(int64)) error {
	f.cmds = append(f.cmds, "download")
	if _, err := io.WriteString(w, "archive"); err != nil {
		return err
	}
	return f.copyErr
}

// ran reports whether any recorded command contains s.
func (f *fakeHost) ran(s string) bool {
	for _, c := range f.cmds {
		if strings.Contains(c, s) {
			return true
		}
	}
	return false
}

// loginNamed is the account login called name.
func loginNamed(name string) login {
	for _, l := range accountLogins {
		if l.Name == name {
			return l
		}
	}
	panic("no login " + name)
}

// scripted answers outpost's questions in order, as if typed. With no answers
// left it acts as if there's no terminal; "^C" presses Ctrl-C.
type scripted struct{ answers, asked []string }

var _ ui.Asker = (*scripted)(nil)

func (s *scripted) next(title string) (string, error) {
	s.asked = append(s.asked, title)
	if len(s.answers) == 0 {
		return "", ui.ErrNoTerminal
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	if a == "^C" {
		return "", ui.ErrCancelled
	}
	return a, nil
}

func (s *scripted) Confirm(_ context.Context, title, _, _ string) (bool, error) {
	a, err := s.next(title)
	return a == "yes", err
}

func (s *scripted) Choose(_ context.Context, title string, _ []ui.Option, _ string) (string, error) {
	return s.next(title)
}

func (s *scripted) Input(_ context.Context, title, _, _ string, validate func(string) error) (string, error) {
	a, err := s.next(title)
	if err == nil {
		err = validate(a) // a terminal would ask again; a test wants to know
	}
	return a, err
}

// answer makes the user give these answers, in order, for the rest of the test.
func answer(t *testing.T, answers ...string) *scripted {
	t.Helper()
	s := &scripted{answers: answers}
	ui.Ask = s
	return s
}

// testEnv gives a flow test an SSH key to find, near-instant waits, and a
// user who answers nothing until the test says so.
func testEnv(t *testing.T) context.Context {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519.pub"), []byte("ssh-ed25519 AAAA test"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	saved := ui.Ask
	ui.Ask = &scripted{}
	t.Cleanup(func() { ui.Ask = saved })
	for _, d := range []*time.Duration{&rebootSettle, &pollInterval} {
		old := *d
		*d = time.Millisecond
		t.Cleanup(func() { *d = old })
	}
	return t.Context()
}
