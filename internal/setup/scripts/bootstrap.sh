#!/usr/bin/env bash
# Turns a fresh Ubuntu 24.04 machine into a T3 Code agent box. Safe to re-run.
# Run for you by `outpost setup`. Logins and repos happen afterwards in finish.sh.
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
# Passwordless sudo: dev already controls the box via the docker group, so a password adds friction, not safety.
echo "$DEV_USER ALL=(ALL) NOPASSWD:ALL" > /tmp/outpost-sudoers
visudo -cf /tmp/outpost-sudoers && install -m 440 /tmp/outpost-sudoers /etc/sudoers.d/outpost
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
# So T3's background service can find claude, t3 and friends after a reboot.
mkdir -p ~/.config/environment.d
echo 'PATH=${HOME}/.local/bin:${PATH}' > ~/.config/environment.d/10-outpost.conf
EOF

echo
echo "outpost: server install done."
