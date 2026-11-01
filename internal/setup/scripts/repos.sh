#!/usr/bin/env bash
# Clones repos into ~/code and adds them as T3 projects, as the dev user. Run for you by `outpost setup`.
# Args: repos as owner/name. Prints a "::task" line per repo for outpost's progress display. Safe to re-run.
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"

repos=("$@")
[ ${#repos[@]} -gt 0 ] || exit 0

# T3 lets only one process own its data, so pause the service while adding projects.
systemctl --user stop t3code.service
trap 'systemctl --user start t3code.service' EXIT

mkdir -p ~/code
for repo in "${repos[@]}"; do
  dir="$HOME/code/${repo##*/}"
  echo "::task $repo"
  # Plain git works for private repos too, via the credential helper `gh auth setup-git` installed.
  [ -d "$dir/.git" ] || git clone "https://github.com/$repo.git" "$dir"
  if ! out=$(t3 project add "$dir" 2>&1); then
    grep -q ProjectAlreadyExists <<<"$out" || { echo "$out" >&2; exit 1; }
    out="Already in T3: $dir"
  fi
  echo "$out"
done
