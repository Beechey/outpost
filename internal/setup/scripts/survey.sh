#!/usr/bin/env bash
# Describes what `outpost move` would carry over from this server, as the dev user.
# Prints "key value" lines: t3, moved, projects, sessions, size, paths.
set -euo pipefail
cd ~

# Everything that makes up your work here. Credentials are excluded when copying.
paths=()
for p in code .t3/userdata .t3/worktrees .claude .claude.json .codex .gitconfig; do
  [ -e "$p" ] && paths+=("$p")
done

[ -d .t3/userdata ] && echo "t3 yes" || echo "t3 no"
[ -f .t3/moved-to ] && echo "moved $(cat .t3/moved-to)"
echo "projects $(find code -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l)"
echo "sessions $(find .claude/projects .codex/sessions -type f -name '*.jsonl' 2>/dev/null | wc -l)"
[ ${#paths[@]} -gt 0 ] && echo "size $(du -sch --exclude=node_modules "${paths[@]}" 2>/dev/null | tail -1 | cut -f1)"
echo "paths ${paths[*]}"
