#!/usr/bin/env bash
# End-to-end test against two systemd containers posing as fresh Ubuntu servers.
#
#   1. Sets up an "old" server with bootstrap.sh, then gives it work: a T3 project,
#      uncommitted changes, an unpushed branch, Claude settings and a session.
#   2. Runs `outpost move old new` and checks everything arrived, nothing secret did,
#      T3 runs on the new server after a reboot, and the old one is retired.
#   3. Runs the move again to check a re-run is safe.
#
# Logins need a human, so on the new server tiny stand-ins answer "signed in".
# Usage: test/e2e/run.sh   (needs Docker, Go and ~/.ssh/id_ed25519)
# shellcheck disable=SC2016 # single-quoted commands are meant to expand on the server
set -euo pipefail
cd "$(dirname "$0")"
ROOT=../.. OLD=2297 NEW=2298 BIN="$(mktemp -d)/outpost"
ssh_to() { ssh -p "$1" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$2@localhost" "${@:3}"; }
check() { if eval "$2"; then echo "  ok: $1"; else echo "  FAIL: $1"; return 1; fi; }

cleanup() { docker rm -f -v outpost-e2e-old outpost-e2e-new >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup

echo "--- build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/outpost)
docker build -q -t outpost-e2e --build-arg PUBKEY="$(cat ~/.ssh/id_ed25519.pub)" . >/dev/null
for pair in old:$OLD new:$NEW; do
  docker run -d --name "outpost-e2e-${pair%%:*}" --privileged --cgroupns=host \
    -v /sys/fs/cgroup:/sys/fs/cgroup:rw -v /var/lib/docker -v /var/lib/containerd \
    --restart unless-stopped -p "${pair##*:}:22" outpost-e2e >/dev/null
done
wait_ssh() { for _ in $(seq 60); do ssh_to "$1" "$2" -o BatchMode=yes true 2>/dev/null && return; sleep 2; done; echo "no SSH on $1"; exit 1; }
wait_ssh $OLD root
wait_ssh $NEW root

echo "--- bootstrap both servers"
for port in $OLD $NEW; do
  ssh_to "$port" root 'cat > /tmp/bootstrap.sh' < "$ROOT/internal/setup/scripts/bootstrap.sh"
  ssh_to "$port" root 'bash /tmp/bootstrap.sh' > "/tmp/outpost-e2e-bootstrap-$port.log" 2>&1 ||
    { tail -30 "/tmp/outpost-e2e-bootstrap-$port.log"; exit 1; }
done
check "root SSH is off" '! ssh_to $OLD root -o BatchMode=yes true 2>/dev/null'
check "re-running bootstrap is safe" \
  'ssh_to $OLD dev "cat > /tmp/b.sh && sudo bash /tmp/b.sh" < "$ROOT/internal/setup/scripts/bootstrap.sh" >/dev/null 2>&1'

echo "--- give the old server some work"
ssh_to $OLD dev 'set -e; export PATH=$HOME/.local/bin:$PATH
  t3 up --lan --yes >/dev/null
  git clone -q https://github.com/octocat/Hello-World ~/code/Hello-World
  systemctl --user stop t3code.service; t3 project add ~/code/Hello-World >/dev/null; systemctl --user start t3code.service
  cd ~/code/Hello-World && echo wip > notes.txt && git branch feature-x
  mkdir -p ~/.claude/skills/demo ~/.claude/projects/-home-dev-code-Hello-World
  echo "# Rules" > ~/.claude/CLAUDE.md
  echo "demo" > ~/.claude/skills/demo/SKILL.md
  echo "{}" > ~/.claude/projects/-home-dev-code-Hello-World/session-1.jsonl
  echo SECRET > ~/.claude/.credentials.json'
# shellcheck disable=SC2034 # read inside check strings
OLD_ENV=$(ssh_to $OLD dev 'cat ~/.t3/userdata/environment-id')

echo "--- stand-in logins on the new server"
ssh_to $NEW dev 'set -e; cd ~/.local/bin
  ./t3 up --lan --yes >/dev/null && mv t3 t3-real
  printf "%s\n" "#!/bin/bash" "[ \"\$1 \$2\" = \"status --json\" ] && exec echo \"{\\\"connect\\\":{\\\"enabled\\\":true},\\\"service\\\":{\\\"installed\\\":true}}\"" "exec ~/.local/bin/t3-real \"\$@\"" > t3
  printf "%s\n" "#!/bin/bash" "[ \"\$1 \$2\" = \"auth status\" ] && echo \"{\\\"loggedIn\\\": true}\"; exit 0" > claude
  printf "%s\n" "#!/bin/bash" "exit 0" > tailscale
  cp tailscale gh
  chmod +x t3 claude tailscale gh'

echo "--- outpost move"
"$BIN" move localhost localhost --old-port $OLD --new-port $NEW --yes </dev/null

echo "--- the new server has everything"
on_new() { ssh_to $NEW dev "$1"; }
check "same T3 environment" '[ "$(on_new "cat ~/.t3/userdata/environment-id")" = "$OLD_ENV" ]'
check "T3 project" 'on_new "sqlite3 -readonly ~/.t3/userdata/statev2.sqlite \"select title from projection_projects\"" | grep -q Hello-World'
check "T3 running after reboot" '[ "$(on_new "systemctl --user is-active t3code.service")" = active ]'
check "uncommitted work" 'on_new "cat ~/code/Hello-World/notes.txt" | grep -q wip'
check "unpushed branch" 'on_new "git -C ~/code/Hello-World branch --list feature-x" | grep -q feature-x'
check "Claude settings and skills" 'on_new "cat ~/.claude/CLAUDE.md ~/.claude/skills/demo/SKILL.md" >/dev/null'
check "agent session history" 'on_new "test -f ~/.claude/projects/-home-dev-code-Hello-World/session-1.jsonl"'
check "credentials NOT copied" 'on_new "test ! -e ~/.claude/.credentials.json"'
check "Docker works" 'on_new "docker run --rm hello-world" >/dev/null 2>&1'

echo "--- the old server is retired, with its files intact"
check "T3 service removed" '[ "$(ssh_to $OLD dev "systemctl --user is-enabled t3code.service 2>/dev/null")" != enabled ]'
check "records where it moved" '[ "$(ssh_to $OLD dev "cat ~/.t3/moved-to")" = localhost:$NEW ]'
check "files untouched" 'ssh_to $OLD dev "cat ~/code/Hello-World/notes.txt" | grep -q wip'
"$BIN" setup -p $OLD localhost </dev/null > /tmp/outpost-e2e-revive.log 2>&1 || true
check "setup refuses to revive it" 'grep -q retired /tmp/outpost-e2e-revive.log && ! grep -q Installing /tmp/outpost-e2e-revive.log' ||
  { cat /tmp/outpost-e2e-revive.log; exit 1; }

echo "--- re-running the move is safe"
"$BIN" move localhost localhost --old-port $OLD --new-port $NEW --yes </dev/null > /tmp/outpost-e2e-rerun.log 2>&1
check "skips the copy" 'grep -q "Already moved" /tmp/outpost-e2e-rerun.log'
check "doesn't reboot a server in use" 'grep -q "Health check" /tmp/outpost-e2e-rerun.log && ! grep -q "Rebooting" /tmp/outpost-e2e-rerun.log'
check "still the same environment" '[ "$(on_new "cat ~/.t3/userdata/environment-id")" = "$OLD_ENV" ]'

echo "PASS"
