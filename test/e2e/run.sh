#!/usr/bin/env bash
# End-to-end test: runs outpost's scripts against a systemd container posing as a fresh VPS,
# reboots it, and checks everything comes back. Logins need a human, so they are skipped:
# T3 runs as a LAN service instead of T3 Connect, and a public repo is cloned with git.
# Usage: test/e2e/run.sh   (needs Docker and ~/.ssh/id_ed25519)
# shellcheck disable=SC2016 # single-quoted commands are meant to expand on the server
set -euo pipefail
cd "$(dirname "$0")"
NAME=outpost-e2e PORT=2222 SCRIPTS=../../internal/setup/scripts
SSH=(ssh -p "$PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)

cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup

docker build -q -t "$NAME" --build-arg PUBKEY="$(cat ~/.ssh/id_ed25519.pub)" . >/dev/null
docker run -d --name "$NAME" --privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
  -v /var/lib/docker -v /var/lib/containerd --restart unless-stopped -p "$PORT:22" "$NAME" >/dev/null
wait_ssh() { for _ in $(seq 60); do "${SSH[@]}" -o BatchMode=yes "$1@localhost" true 2>/dev/null && return; sleep 2; done; echo "ssh as $1 never came up"; exit 1; }
wait_ssh root

echo "--- bootstrap.sh"
"${SSH[@]}" root@localhost 'cat > /tmp/bootstrap.sh' < "$SCRIPTS/bootstrap.sh"
"${SSH[@]}" root@localhost 'bash /tmp/bootstrap.sh'

echo "--- re-run is safe (as dev, like outpost does)"
"${SSH[@]}" dev@localhost 'cat > /tmp/bootstrap-dev.sh' < "$SCRIPTS/bootstrap.sh"
"${SSH[@]}" dev@localhost 'sudo bash /tmp/bootstrap-dev.sh' >/dev/null

echo "--- root SSH is off"
if "${SSH[@]}" -o BatchMode=yes root@localhost true 2>/dev/null; then echo "root SSH is still on"; exit 1; fi

echo "--- T3 service (LAN mode, since T3 Connect needs a human)"
"${SSH[@]}" dev@localhost '$HOME/.local/bin/t3 up --lan --yes' >/dev/null

echo "--- repos.sh, twice (re-runs must be safe)"
for _ in 1 2; do
  "${SSH[@]}" dev@localhost 'cat > /tmp/repos.sh && bash /tmp/repos.sh octocat/Hello-World' < "$SCRIPTS/repos.sh"
done

echo "--- reboot"
"${SSH[@]}" dev@localhost 'sudo systemctl reboot' || true
sleep 10
wait_ssh dev

echo "--- after reboot"
"${SSH[@]}" dev@localhost 'systemctl is-active docker tailscaled && systemctl --user is-active t3code.service
  ~/.local/bin/claude --version && ~/.local/bin/t3 --version
  docker run --rm hello-world >/dev/null && echo "docker ok"'
echo "PASS"
