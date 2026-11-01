#!/usr/bin/env bash
# Runs as the dev user, for you, at the end of `outpost setup`: signs the server in to your
# accounts, then clones repos and adds them to T3. Each login prints a link or code: open it on
# your laptop and approve. Finished steps are skipped, so it is safe to re-run.
# Args: repos to clone (owner/name). With none, it lists your repos and asks.
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }

step "Tailscale"
if tailscale status >/dev/null 2>&1; then
  echo "Already connected."
else
  echo "Open the link below and sign in with the SAME account as your laptop."
  sudo tailscale up --ssh
fi

step "Claude"
if claude auth status 2>/dev/null | grep -q '"loggedIn": true'; then
  echo "Already logged in."
else
  echo "Open the link, approve, then paste the code back here."
  claude auth login
fi

step "GitHub"
if gh auth status >/dev/null 2>&1; then
  echo "Already logged in."
else
  echo "Copy the one-time code, press Enter, then open the link and paste the code."
  gh auth login --hostname github.com --git-protocol https --web
fi
gh auth setup-git

step "T3 Connect"
echo "Open the link, check the code matches, and approve."
t3 up --connect --yes

step "Repos"
repos=("$@")
if [ ${#repos[@]} -eq 0 ] && [ -t 0 ]; then
  echo "Your most recent repos:"
  gh repo list --limit 20 --json nameWithOwner --jq '.[].nameWithOwner' | sed 's/^/  /'
  read -rp "Repos to set up (space-separated, e.g. Beechey/app), or Enter to skip: " -a repos
fi
mkdir -p ~/code
for repo in "${repos[@]}"; do
  dir="$HOME/code/${repo##*/}"
  [ -d "$dir/.git" ] || gh repo clone "$repo" "$dir"
  t3 project add "$dir"
done

step "Done"
echo "Open T3 Code on your laptop or phone, pick this server, and start prompting."
