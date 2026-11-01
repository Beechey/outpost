#!/usr/bin/env bash
# Signs the server in to your accounts, as the dev user. Run for you by `outpost setup`.
# Each login prints a link or code: open it on your laptop and approve.
# Finished steps are skipped, so it is safe to re-run.
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

