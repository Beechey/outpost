package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Beechey/outpost/internal/remote"
)

// fakeHost is a remote.Host that records every command and answers from rules,
// so the setup and move flows can be tested without servers.
type fakeHost struct {
	name    string
	logins  map[string]bool // users that can log in
	rules   []rule          // answers for Output, first match wins
	copyErr error           // what CopyTo returns
	cmds    []string        // everything run here, in order
}

// A rule answers Output commands containing match.
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
	f.rules = append(f.rules, rule{match: "is-active", out: "docker active\ntailscaled active\nT3 active\n"})
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
		if strings.Contains(cmd, r.match) {
			return []byte(r.out), r.err
		}
	}
	return nil, nil
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

// ran reports whether any recorded command contains s.
func (f *fakeHost) ran(s string) bool {
	for _, c := range f.cmds {
		if strings.Contains(c, s) {
			return true
		}
	}
	return false
}

// testEnv gives a flow test an SSH key to find and near-instant waits.
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
	for _, d := range []*time.Duration{&rebootSettle, &pollInterval} {
		old := *d
		*d = time.Millisecond
		t.Cleanup(func() { *d = old })
	}
	return t.Context()
}
