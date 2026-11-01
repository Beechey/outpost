<p align="center">
  <img src="docs/logo.svg" width="120" alt="outpost logo">
</p>

<h1 align="center">outpost</h1>

<p align="center">
  <strong>Turn any Ubuntu server into an always-on home for your AI coding agents.</strong><br>
  One command. A few clicks to approve logins. Then open <a href="https://t3.codes">T3 Code</a> and start prompting.
</p>

<p align="center">
  <a href="https://github.com/Beechey/outpost/actions/workflows/ci.yml"><img src="https://github.com/Beechey/outpost/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/server-Ubuntu%2024.04-E95420?logo=ubuntu&logoColor=white" alt="Ubuntu 24.04">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT license"></a>
</p>

---

```console
$ outpost setup 203.0.113.7 you/app you/api
[1/6] Checking your SSH key
[2/6] Connecting to 203.0.113.7
[3/6] Installing everything (3-5 minutes)
[4/6] Logins: approve each link on your laptop
[5/6] Cloning your repos into T3
[6/6] Rebooting to check everything comes back by itself

All set. Open T3 Code, pick this server, and start prompting.
```

Your agents keep working while your laptop is closed. You check in from your laptop or phone through T3 Code. The server comes back by itself after any reboot.

## Contents

- [What you get](#what-you-get)
- [Quick start](#quick-start)
- [Using it day to day](#using-it-day-to-day)
- [Adding repos, repairing, moving servers](#adding-repos-repairing-moving-servers)
- [What gets installed](#what-gets-installed)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [FAQ](#faq)
- [Development](#development)

## What you get

- **One command** from your laptop sets up the whole server: tools, security, logins, and your repos.
- **Always on:** T3 Code runs as a service that starts at boot. outpost reboots the server at the end to prove it.
- **Reachable from anywhere** through T3 Connect, with no ports to open or networking to configure.
- **Ready to work:** your repos are cloned and added to T3, and Claude Code and GitHub are signed in.
- **Locked down:** key-only SSH, no root login, a firewall, and Docker ports that stay private.
- **Works anywhere:** any provider (netcup, Hetzner, DigitalOcean, …) or a machine at home.
- **Safe to re-run:** use the same command to add repos or repair a server.

## Quick start

### 1. On your laptop (once)

You need:

- **macOS or Linux** (`ssh` is built in)
- **[Go 1.26+](https://go.dev/dl/)** (`brew install go` on a Mac)
- **[T3 Code](https://t3.codes)**: the desktop app, signed in to T3 Connect
- **[Tailscale](https://tailscale.com/download)** (optional): lets you reach the server's dev servers and SSH privately

Install outpost:

```bash
go install github.com/Beechey/outpost/cmd/outpost@latest
```

> **Repo still private?** `go install` can't fetch it yet. Use this instead:
> ```bash
> gh repo clone Beechey/outpost && cd outpost && go install ./cmd/outpost
> ```

If your terminal says `outpost: command not found`, add Go's bin folder to your PATH:

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

### 2. Get a server

Create a server with **Ubuntu 24.04**. Note its **IP address**. If the provider offers to add an SSH key, add the contents of `~/.ssh/id_ed25519.pub`. If it only gives you a root password, that's fine: outpost asks for it once.

Sizing: agents use little CPU but plenty of memory. **8 GB RAM** runs a few agents at once; **16–32 GB** runs many.

### 3. Run outpost

```bash
outpost setup YOUR_SERVER_IP
```

or name the repos to set up straight away:

```bash
outpost setup YOUR_SERVER_IP you/app you/api
```

It then walks you through these, in order:

| Step | What you do |
|---|---|
| Root password | Only if your SSH key isn't on the server yet. Typed once. |
| **Tailscale** | Open the link and sign in with the **same account as your laptop**. |
| **Claude** | Open the link, approve, and paste the code back into the terminal. |
| **GitHub** | Copy the code shown, press Enter, open the link, and paste the code. |
| **T3 Connect** | Open the link, check the code matches, and approve. |
| Repos | If you didn't list any, pick from your GitHub repos, or press Enter to skip. |

Then it reboots the server and checks everything comes back. When you see **All set**, open T3 Code: the server appears next to your laptop under **Settings → Environments**.

## Using it day to day

- **Start a thread on the server:** in T3 Code, pick the server's environment and one of its projects, then prompt. Close your laptop whenever you like; check in from your phone.
- **Laptop and server both work.** Your laptop keeps its local environment, so you choose per thread where the agent runs.
- **Code moves through git.** Agents on the server push branches or open PRs; you pull them anywhere. Files are not synced live.
- **Seeing a dev server an agent started**, for example on port 3000:
  - With Tailscale: on the server, run `tailscale serve 3000` and open the `https://….ts.net` link it prints.
  - Without: `ssh -L 3000:localhost:3000 dev@YOUR_SERVER_IP`, then open <http://localhost:3000>.
- **SSH in** any time with `ssh dev@YOUR_SERVER_IP`.

### After a reboot

T3, Tailscale, Docker, your logins and your threads all come back by themselves. Two things don't:

- **An agent in the middle of a turn** stops. Open the thread and say "continue".
- **Dev servers and containers** need restarting, unless the compose file has `restart: unless-stopped`. That's worth adding to your projects' `CLAUDE.md`.

## Adding repos, repairing, moving servers

| To… | Do this |
|---|---|
| Add repos | `outpost setup YOUR_SERVER_IP you/new-repo` (steps already done are skipped) |
| Repair a broken server | `outpost setup YOUR_SERVER_IP` |
| Update tools | `ssh dev@YOUR_SERVER_IP`, then `t3 update`, `claude update`, `sudo apt update && sudo apt upgrade -y` |
| Move to a new server | Push unfinished work, run `outpost setup NEW_IP`, then remove the old environment in T3 under **Settings → Environments** |
| Use a non-standard SSH port | `outpost setup -p 2222 YOUR_SERVER_IP` |

A free T3 Connect account links up to 3 machines, so remove servers you no longer use.

## What gets installed

| | |
|---|---|
| **T3 Code** | Background service that starts at boot (systemd user service with lingering) |
| **Claude Code** | Native install, signed in with your Claude account |
| **GitHub CLI + git** | Signed in; git uses it for credentials, so private repos work |
| **Node.js 24** | For your projects |
| **Docker** | For databases and services your projects need |
| **Tailscale** | Private network between your devices, with Tailscale SSH |
| **Basics** | build-essential, ripgrep, jq, tmux, unzip |

Everything runs as a normal user called **`dev`**. Repos live in `~/code`.

## Security

- **SSH:** key-only, and root login is turned off once `dev` has your key.
- **Firewall (ufw):** allows only SSH and Tailscale traffic in.
- **Docker:** published ports bind to `127.0.0.1` by default, so `docker run -p 5432:5432 postgres` isn't exposed to the internet. Docker otherwise bypasses the firewall.
- **`dev` has passwordless sudo and Docker access**, so agents running as `dev` can control the whole server. That's intentional on a single-person dev box, but keep secrets on it to a minimum.
- **Optional:** once `ssh dev@<tailscale-name>` works, close public SSH with `sudo ufw delete allow OpenSSH`. Your provider's web console is then your backup way in.

## Troubleshooting

| Problem | Fix |
|---|---|
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | You reinstalled the server. Run `ssh-keygen -R YOUR_SERVER_IP`, then try again. |
| `can't log in as root` | Check the IP and root password. Some providers email the password, or let you set one in their panel. |
| A login step failed | Run the same `outpost setup` command again. Finished steps are skipped. |
| Server doesn't appear in T3 | `ssh dev@YOUR_SERVER_IP`, then `t3 service status`. If it isn't running: `t3 service restart`. |
| Agent says Claude isn't logged in | `ssh dev@YOUR_SERVER_IP`, then `claude auth login`. |
| Can't SSH in at all | Use your provider's web console and log in as `root`, then check `ufw status` and `systemctl status ssh`. |

## FAQ

**Can I use my Claude subscription on a server?**
Yes. Anthropic's terms allow signing in to the official Claude Code app with your own subscription, including on hosted machines ([details](https://code.claude.com/docs/en/legal-and-compliance)). T3 Code uses that same login. Check your usage page after the first few days to see how it's counted.

**Why not Docker for the whole setup?**
Agents need the whole machine: Docker for databases, browsers, package installs and git. A plain server set up by a script is simpler and faster to work with. Nothing important lives only on the server: your code is in git and the setup is this tool.

**Does it work on a Raspberry Pi or home server?**
Any machine running Ubuntu 24.04 that you can SSH into, x86 or ARM. T3 Connect works through your home router without port forwarding.

**Can I use Codex or other agents?**
T3 Code supports them. Install and sign in to the agent's CLI on the server as `dev`, and it shows up in T3.

## Development

```
cmd/outpost/          CLI entry point (cobra + fang)
internal/setup/       The setup steps; validates everything that reaches a remote shell
  scripts/            Shell scripts embedded in the binary and run on the server
    bootstrap.sh      As root: packages, user, SSH, firewall, Docker, Tailscale, T3, Claude
    login.sh          As dev: Tailscale, Claude, GitHub, T3 Connect logins
    repos.sh          As dev: clone repos and add them as T3 projects
internal/remote/      Runs commands over the system ssh binary
test/e2e/             End-to-end test against a systemd container posing as a fresh VPS
```

```bash
go test ./...            # unit tests, including a syntax check of the embedded scripts
shellcheck internal/setup/scripts/*.sh test/e2e/run.sh
test/e2e/run.sh          # full install + reboot in Docker (about 5 minutes; needs Docker and ~/.ssh/id_ed25519)
```

The end-to-end test runs every script for real, except the logins (they need a human). It then reboots the container and checks T3, Docker and Tailscale all come back.

## License

[MIT](LICENSE)
