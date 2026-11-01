package setup

import (
	"slices"
	"strings"
)

// The move manifest: everything about what `outpost move` carries lives here,
// and every shell command that depends on it is built from it. Paths are
// relative to the dev user's home.

// movePaths is what a move carries: code, T3's data, and agent history and settings.
var movePaths = []string{"code", ".t3/userdata", ".t3/worktrees", ".claude", ".claude.json", ".codex", ".gitconfig"}

// moveExcludes is never copied: logins (approved again on the new server),
// T3's per-process lock files and logs, and dependencies (reinstalled on demand).
var moveExcludes = []string{
	".claude/.credentials.json", ".codex/auth.json",
	".t3/userdata/server-owner.sqlite*", ".t3/userdata/server-runtime.json",
	".t3/userdata/logs/*", // the folder must exist: T3's service writes its output there
	"node_modules",
}

// movedToPath records, on a retired server, where its work went.
const movedToPath = ".t3/moved-to"

const movedToFile = "~/" + movedToPath

// clearedOnDestination is removed before the copy: T3 data the copy replaces,
// and a moved-to record left over if the destination was itself moved from.
var clearedOnDestination = []string{".t3/userdata", ".t3/worktrees", movedToPath}

// survey describes what a move would carry from a server.
type survey struct {
	t3                 bool
	movedTo            string
	projects, sessions string
	size               string
	paths              []string // the movePaths that exist there
}

// surveyCmd prints "key value" lines that parseSurvey reads.
func surveyCmd() string {
	return `cd ~ || exit 1
set --
for p in ` + strings.Join(movePaths, " ") + `; do if [ -e "$p" ]; then echo "path $p"; set -- "$@" "$p"; fi; done
if [ -d .t3/userdata ]; then echo "t3 yes"; fi
if [ -f ` + movedToPath + ` ]; then echo "moved $(cat ` + movedToPath + `)"; fi
echo "projects $(find code -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l)"
echo "sessions $(find .claude/projects .codex/sessions -type f -name '*.jsonl' 2>/dev/null | wc -l)"
if [ $# -gt 0 ]; then echo "size $(du -sch --exclude=node_modules "$@" 2>/dev/null | tail -1 | cut -f1)"; fi
true`
}

func parseSurvey(out string) survey {
	var sv survey
	for line := range strings.Lines(out) {
		key, val, _ := strings.Cut(strings.TrimSpace(line), " ")
		val = strings.TrimSpace(val)
		switch key {
		case "path":
			if slices.Contains(movePaths, val) { // only our own fixed paths ever reach the tar command
				sv.paths = append(sv.paths, val)
			}
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
		}
	}
	return sv
}

// packCmd writes a gzipped tar of paths (from a survey) to stdout.
func packCmd(paths []string) string {
	args := make([]string, 0, len(moveExcludes))
	for _, e := range moveExcludes {
		args = append(args, "--exclude="+quote(e))
	}
	// tar exits 1 when a file changes while it's read (an agent's container still
	// writing, say). That file is copied as it was, which is fine; exit 2 is a real failure.
	return "cd ~ || exit 2; tar -cz --warning=no-file-changed " + strings.Join(args, " ") + " " +
		strings.Join(paths, " ") + "; [ $? -le 1 ]"
}

// unpackCmd reads packCmd's output into the home directory.
const unpackCmd = "tar -xz -C ~"

// clearCmd removes what the copy replaces on the destination.
func clearCmd() string {
	paths := make([]string, 0, len(clearedOnDestination))
	for _, p := range clearedOnDestination {
		paths = append(paths, "~/"+p)
	}
	return "rm -rf " + strings.Join(paths, " ")
}
