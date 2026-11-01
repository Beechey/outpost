package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseSurvey(t *testing.T) {
	sv := parseSurvey("path code\npath /etc\npath .claude\nt3 yes\nmoved new:22\nprojects 3\nsessions 7\nsize 1.2G\n")
	if !slices.Equal(sv.paths, []string{"code", ".claude"}) {
		t.Errorf("paths = %v; only manifest paths may reach tar", sv.paths)
	}
	if !sv.t3 || sv.movedTo != "new:22" || sv.projects != "3" || sv.sessions != "7" || sv.size != "1.2G" {
		t.Errorf("parsed %+v", sv)
	}
}

// home builds a fake dev-user home with files under the given relative paths.
func home(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func shell(t *testing.T, home, cmd string) string {
	t.Helper()
	c := exec.CommandContext(t.Context(), "bash", "-c", cmd)
	c.Env = append(os.Environ(), "HOME="+home)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", cmd, err, out)
	}
	return string(out)
}

func TestSurveyCmdFindsWhatExists(t *testing.T) {
	h := home(t, "code/app/main.go", ".t3/userdata/statev2.sqlite", ".t3/moved-to", ".claude/projects/x/s.jsonl", "unrelated/file")
	sv := parseSurvey(shell(t, h, surveyCmd()))
	if !slices.Equal(sv.paths, []string{"code", ".t3/userdata", ".claude"}) {
		t.Errorf("paths = %v", sv.paths)
	}
	if !sv.t3 || sv.movedTo != ".t3/moved-to" || sv.projects != "1" || sv.sessions != "1" {
		t.Errorf("survey = %+v", sv)
	}
}

func TestPackAndUnpackCarryWorkButNotSecrets(t *testing.T) {
	if out, _ := exec.Command("tar", "--version").Output(); !strings.Contains(string(out), "GNU tar") {
		t.Skip("needs GNU tar, as on the servers")
	}
	src := home(t,
		"code/app/notes.txt", "code/app/node_modules/dep/index.js",
		".t3/userdata/statev2.sqlite", ".t3/userdata/server-owner.sqlite", ".t3/userdata/logs/boot.log",
		".claude/CLAUDE.md", ".claude/.credentials.json", ".codex/auth.json", ".codex/sessions/a.jsonl")
	dst := t.TempDir()
	sv := parseSurvey(shell(t, src, surveyCmd()))
	shell(t, src, "("+packCmd(sv.paths)+") > "+filepath.Join(dst, "move.tgz")) // over ssh, all of stdout is the archive
	shell(t, dst, unpackCmd+" < ~/move.tgz")

	for _, want := range []string{"code/app/notes.txt", ".t3/userdata/statev2.sqlite", ".claude/CLAUDE.md", ".codex/sessions/a.jsonl", ".t3/userdata/logs"} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("%s should be moved: %v", want, err)
		}
	}
	for _, never := range []string{".claude/.credentials.json", ".codex/auth.json", ".t3/userdata/server-owner.sqlite", ".t3/userdata/logs/boot.log", "code/app/node_modules"} {
		if _, err := os.Stat(filepath.Join(dst, never)); err == nil {
			t.Errorf("%s must not be moved", never)
		}
	}
}

func TestClearCmdCoversMovedToRecord(t *testing.T) {
	h := home(t, ".t3/userdata/x", ".t3/worktrees/y", ".t3/moved-to", ".t3/runtime/keep")
	shell(t, h, clearCmd())
	for _, gone := range []string{".t3/userdata", ".t3/worktrees", ".t3/moved-to"} {
		if _, err := os.Stat(filepath.Join(h, gone)); err == nil {
			t.Errorf("%s should be cleared", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(h, ".t3/runtime/keep")); err != nil {
		t.Error("T3's installed runtime must be kept")
	}
}
