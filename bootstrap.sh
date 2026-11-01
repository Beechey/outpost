#!/usr/bin/env bash
# Turns a fresh Ubuntu 24.04 machine into a T3 Code agent box. Safe to re-run.
# Usage, from your laptop (see README.md):
#   scp bootstrap.sh root@SERVER_IP:  &&  ssh -t root@SERVER_IP bash bootstrap.sh
# Logins (Tailscale, Claude, GitHub, T3 Connect) need a browser, so they are printed at the end.
set -euo pipefail

DEV_USER="${DEV_USER:-dev}"
[ "$(id -u)" -eq 0 ] || { echo "Run as root (sudo bash bootstrap.sh)"; exit 1; }
export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y git gh curl ca-certificates build-essential unzip jq ripgrep tmux ufw

# Node for your projects (T3 itself ships its own runtime)
if ! command -v node >/dev/null; then
  curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
  apt-get install -y nodejs
fi

# Docker publishes ports past ufw, so bind published ports to localhost by default.
# Otherwise an agent's `docker run -p 5432:5432 postgres` is open to the internet.
mkdir -p /etc/docker
[ -f /etc/docker/daemon.json ] || echo '{ "ip": "127.0.0.1" }' > /etc/docker/daemon.json
command -v docker >/dev/null || curl -fsSL https://get.docker.com | sh
systemctl enable docker && systemctl restart docker

command -v tailscale >/dev/null || curl -fsSL https://tailscale.com/install.sh | sh
systemctl enable --now tailscaled

# Non-root user that runs T3 and the agents (T3's browser sandbox refuses root).
if ! id "$DEV_USER" >/dev/null 2>&1; then
  useradd -m -s /bin/bash -G sudo "$DEV_USER"
fi
usermod -aG docker "$DEV_USER"
loginctl enable-linger "$DEV_USER" # keeps T3's user service running after logout
HOME_DIR="$(getent passwd "$DEV_USER" | cut -d: -f6)"

# Reuse root's SSH keys (the ones you gave the provider at install time).
install -d -m 700 -o "$DEV_USER" -g "$DEV_USER" "$HOME_DIR/.ssh"
if [ -f /root/.ssh/authorized_keys ] && [ ! -s "$HOME_DIR/.ssh/authorized_keys" ]; then
  install -m 600 -o "$DEV_USER" -g "$DEV_USER" /root/.ssh/authorized_keys "$HOME_DIR/.ssh/authorized_keys"
fi

# Key-only SSH, but only once the dev user has a key, so you can't lock yourself out.
if [ -s "$HOME_DIR/.ssh/authorized_keys" ]; then
  printf 'PasswordAuthentication no\nPermitRootLogin no\n' > /etc/ssh/sshd_config.d/10-devbox.conf
  systemctl reload ssh
else
  echo "WARNING: $DEV_USER has no SSH key; password login left on. Add a key and re-run."
fi

ufw allow OpenSSH
ufw allow in on tailscale0
ufw --force enable

# Per-user tools, installed into ~/.local/bin
sudo -iu "$DEV_USER" bash <<'EOF'
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
command -v t3 >/dev/null || curl -fsSL https://t3.codes/install.sh | sh
command -v claude >/dev/null || curl -fsSL https://claude.ai/install.sh | bash
grep -q '.local/bin' ~/.bashrc || echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
EOF

# Root SSH is now off, so give the dev user a sudo password while we still have a root shell.
if [ -t 0 ] && passwd -S "$DEV_USER" | awk '{exit !($2=="L" || $2=="NP")}'; then
  echo "Choose a password for $DEV_USER (used for sudo, not for SSH):"
  passwd "$DEV_USER"
fi

cat <<EOF

Done. Now log in as $DEV_USER and finish the logins (README.md, step 3):
  ssh $DEV_USER@<this-box>
  sudo tailscale up --ssh
  claude auth login
  gh auth login
  t3 connect

Then test a reboot:  sudo reboot  (wait a minute, and the box should show up in T3 again)
Check boot startup any time with:  t3 service status   (want: running, enabled, linger yes)
For agent-started containers, use restart: unless-stopped in docker compose files.
EOF
