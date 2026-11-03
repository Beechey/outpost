#!/usr/bin/env bash
# End-to-end test against two systemd containers posing as fresh Ubuntu servers.
#
#   1. Sets up an "old" server with bootstrap.sh, then gives it work: a T3 project,
#      uncommitted changes, an unpushed branch, Claude settings and a session.
#   2. Runs `outpost move old new` and checks everything arrived, nothing secret did,
#      T3 runs on the new server after a reboot, and the old one is retired.
#   3. Backs up the new server and checks the file holds the work and T3 runs again.
#   4. Runs the move again to check a re-run is safe.
#   5. Installs the gateway for a domain of its own (Caddy, CoreDNS and its
#      certificate authority) and checks a project answers over HTTPS, its name
#      resolves, and the certificate can only vouch for that domain.
#
# Logins need a human, so on the new server tiny stand-ins answer "signed in".
# Usage: test/e2e/run.sh   (needs Docker, Go and ~/.ssh/id_ed25519)
# shellcheck disable=SC2016 # single-quoted commands are meant to expand on the server
set -euo pipefail
cd "$(dirname "$0")"
ROOT=../.. OLD=2297 NEW=2298 BIN="$(mktemp -d)/outpost"
ssh_to() { ssh -p "$1" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$2@localhost" "${@:3}"; }
check() { if eval "$2"; then echo "  ok: $1"; else echo "  FAIL: $1"; return 1; fi; }

cleanup() {
  docker rm -f -v outpost-e2e-old outpost-e2e-new >/dev/null 2>&1 || true
  # Each run's containers have new host keys; outpost's own ssh would refuse the old ones.
  for port in $OLD $NEW; do ssh-keygen -R "[localhost]:$port" >/dev/null 2>&1 || true; done
  # outpost remembers the servers it sets up; keep these test ones out of your list.
  for list in "$HOME/Library/Application Support/outpost/servers" "${XDG_CONFIG_HOME:-$HOME/.config}/outpost/servers"; do
    if [ -f "$list" ]; then sed -i.bak -e "/^localhost:$OLD$/d" -e "/^localhost:$NEW$/d" "$list" && rm -f "$list.bak"; fi
  done
}
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
check "security updates on" 'ssh_to $OLD dev "apt-config dump APT::Periodic::Unattended-Upgrade" | grep -q "\"1\""'

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

echo "--- outpost setup on a server already in use"
"$BIN" setup localhost:$NEW </dev/null > /tmp/outpost-e2e-setup.log 2>&1 || { cat /tmp/outpost-e2e-setup.log; exit 1; }
check "health check, not a reboot" 'grep -q "T3 Code, Docker and Tailscale running" /tmp/outpost-e2e-setup.log && ! grep -q Rebooting /tmp/outpost-e2e-setup.log'
check "all set" 'grep -q "All set" /tmp/outpost-e2e-setup.log'

echo "--- outpost move"
"$BIN" move localhost:$OLD localhost:$NEW --yes </dev/null

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
"$BIN" setup localhost:$OLD </dev/null > /tmp/outpost-e2e-revive.log 2>&1 || true
check "setup refuses to revive it" 'grep -q retired /tmp/outpost-e2e-revive.log && ! grep -q Installing /tmp/outpost-e2e-revive.log' ||
  { cat /tmp/outpost-e2e-revive.log; exit 1; }

echo "--- outpost status"
"$BIN" status localhost:$NEW </dev/null > /tmp/outpost-e2e-status.log 2>&1 || { cat /tmp/outpost-e2e-status.log; exit 1; }
check "status shows it healthy" 'grep -q "✓ T3 Code" /tmp/outpost-e2e-status.log && grep -q "T3 Connect" /tmp/outpost-e2e-status.log'

echo "--- outpost backup"
BACKUP="$(mktemp -d)/new.tar.gz"
"$BIN" backup localhost:$NEW "$BACKUP" --yes </dev/null > /tmp/outpost-e2e-backup.log 2>&1 || { cat /tmp/outpost-e2e-backup.log; exit 1; }
# grep without -q reads all of tar's output; -q stops early, and tar fails writing the rest.
check "backup has uncommitted work" 'tar -xzOf "$BACKUP" code/Hello-World/notes.txt | grep wip >/dev/null'
check "backup has T3 threads" 'tar -tzf "$BACKUP" | grep "^\.t3/userdata/statev2\.sqlite$" >/dev/null'
check "T3 running again after backup" '[ "$(on_new "systemctl --user is-active t3code.service")" = active ]'

echo "--- re-running the move is safe"
"$BIN" move localhost:$OLD localhost:$NEW --yes </dev/null > /tmp/outpost-e2e-rerun.log 2>&1
check "skips the copy" 'grep -q "Already moved" /tmp/outpost-e2e-rerun.log'
check "doesn't reboot a server in use" '! grep -q "Rebooting" /tmp/outpost-e2e-rerun.log'
check "still the same environment" '[ "$(on_new "cat ~/.t3/userdata/environment-id")" = "$OLD_ENV" ]'

echo "--- gateway with its own domain, on the server"
# Tailscale can't run here, so the tailnet's side is left out: the gateway's
# address and DNS are 127.0.0.1, and the files outpost writes are written by hand,
# from the same templates.
GW=.outpost/gateway
ssh_to $NEW dev "mkdir -p ~/$GW/sites && cat > ~/$GW/Caddyfile" < "$ROOT/internal/setup/gateway/Caddyfile"
ssh_to $NEW dev "cat > ~/$GW/Corefile" < "$ROOT/internal/setup/gateway/Corefile"
ssh_to $NEW dev "printf '%s\n' MODE=domain GATEWAY_DOMAIN=e2e.test SUFFIX=new SERVICE=svc:gateway-new GATEWAY_ADDRESS=127.0.0.1 TAILSCALE_IP=127.0.0.1 > ~/$GW/gateway.env
  printf '%s\n' 'app.{\$GATEWAY_DOMAIN} {' '	reverse_proxy 127.0.0.1:3001' '}' > ~/$GW/sites/app.caddy
  mkdir -p /tmp/app && echo hello-from-app > /tmp/app/index.html
  nohup python3 -m http.server 3001 --bind 127.0.0.1 --directory /tmp/app >/dev/null 2>&1 &"
{ echo "GATEWAY_DOMAIN=e2e.test"; cat "$ROOT/internal/setup/gateway/install.sh"; } |
  ssh_to $NEW dev 'cat > /tmp/gw.sh && sudo bash /tmp/gw.sh' > /tmp/outpost-e2e-gateway.log 2>&1 || { tail -30 /tmp/outpost-e2e-gateway.log; exit 1; }
gw_up() { for _ in $(seq 20); do on_new "curl -s -o /dev/null --cacert ~/$GW/pki/e2e.test/root.crt --resolve app.e2e.test:44343:127.0.0.1 https://app.e2e.test:44343/" && return; sleep 1; done; return 1; }
check "Caddy and CoreDNS running" '[ "$(on_new "systemctl is-active outpost-caddy outpost-dns" | sort -u)" = active ]'
check "project answers over HTTPS, trusted by the root" 'gw_up && on_new "curl -s --cacert ~/$GW/pki/e2e.test/root.crt --resolve app.e2e.test:44343:127.0.0.1 https://app.e2e.test:44343/" | grep -q hello-from-app'
check "its name resolves to the gateway" 'on_new "dig +short @127.0.0.1 app.e2e.test A" | grep -qx 127.0.0.1'
check "other record types get an empty answer, not an error" 'on_new "dig @127.0.0.1 app.e2e.test HTTPS" | grep -q "status: NOERROR"'
check "the root can only sign e2e.test" 'on_new "openssl x509 -in ~/$GW/pki/e2e.test/root.crt -noout -ext nameConstraints" | grep -q "DNS:e2e.test"'
check "the root key is private" '[ "$(on_new "stat -c %a ~/$GW/pki/e2e.test/root.key")" = 600 ]'
check "Caddy listens on loopback only" '! on_new "ss -Hltn" | grep -E ":(44343|44380|44319) " | grep -vq "127.0.0.1:"'
check "re-running the install is safe" 'ssh_to $NEW dev "sudo bash /tmp/gw.sh" >/dev/null 2>&1 && gw_up'

echo "PASS"
