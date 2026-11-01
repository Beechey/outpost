<p align="center">
  <img src="docs/logo.svg" width="120" alt="outpost logo">
</p>

<h1 align="center">outpost</h1>

<p align="center">
  <strong>Always-on servers for your AI coding agents, ready for <a href="https://t3.codes">T3 Code</a>.</strong><br>
  Turn a fresh server into a home for your agents with one command, and move everything to a new one just as easily.
</p>

<p align="center">
  <a href="https://github.com/Beechey/outpost/actions/workflows/ci.yml"><img src="https://github.com/Beechey/outpost/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/server-Ubuntu%2024.04-E95420?logo=ubuntu&logoColor=white" alt="Ubuntu 24.04">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT license"></a>
</p>

---

```console
$ outpost setup 203.0.113.7

3/5  Installing (3-5 minutes)
  ✓ System packages 41s
  ✓ Docker 38s
  ✓ Tailscale 9s
  ⠼ T3 Code and Claude Code

4/5  Logins
  ✓ Tailscale already signed in
  ◆ Claude  Open the link, approve, then paste the code back here.
```

```console
$ outpost move 203.0.113.7 198.51.100.4

5/7  Moving your work
  ✓ Stopping T3 on 203.0.113.7 1s
  ✓ Clearing T3 data on 198.51.100.4 0s
  ⠼ Copying 2.4G 1.1 GB sent
```

T3 Code runs your agents on any machine, so they keep working while your laptop is closed. outpost handles the parts T3 doesn't:

- **`outpost setup`** turns a bare Ubuntu server into one that's secure, signed in, and runs T3 around the clock, surviving reboots.
- **`outpost move`** carries everything to a new server when you switch providers: your code, unfinished work, T3 threads and agent history. Your threads continue where they left off.

## Contents

- [What you need](#what-you-need)
- [Install](#install)
- [Set up a server](#set-up-a-server)
- [Move to a new server](#move-to-a-new-server)
- [Day to day](#day-to-day)
- [What gets installed](#what-gets-installed)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [FAQ](#faq)
- [Development](#development)

## What you need

**Accounts.** outpost signs the server in to each of these:

| Account | Cost | Used for |
|---|---|---|
| [Claude](https://claude.ai) | Pro or Max subscription | Your agents |
| [GitHub](https://github.com) | Free | Pushing and pulling code |
| [Tailscale](https://tailscale.com) | Free | A private link between your devices and the server |
| [T3 Connect](https://t3.codes) | Free, up to 3 machines | Reaching the server from T3 Code, anywhere |

**On your laptop:** macOS or Linux, with the [T3 Code](https://t3.codes) app and [Tailscale](https://tailscale.com/download) installed and signed in. Use the same Tailscale account everywhere.

**A server:** any machine running **Ubuntu 24.04** that you can SSH into: a VPS from any provider, or a computer at home. Agents use little CPU but plenty of memory: **8 GB RAM** runs a few at once, **16–32 GB** runs many.

## Install

outpost is a single binary, built with Go.

**1. Install Go 1.26 or newer**

<details>
<summary><b>macOS</b></summary>

```bash
brew install go
```
</details>

<details>
<summary><b>Ubuntu / Debian</b></summary>

```bash
sudo snap install go --classic
```

`apt`'s Go is usually too old.
</details>

<details>
<summary><b>Other</b></summary>

Download it from <https://go.dev/dl/>.
</details>

**2. Install outpost**

The repo is private for now, so install from a clone (you need access to it, and the [GitHub CLI](https://cli.github.com) signed in):

```bash
gh repo clone Beechey/outpost && cd outpost && go install ./cmd/outpost
```

To update later, run `git pull && go install ./cmd/outpost` in that folder.

<details>
<summary>Once the repo is public</summary>

```bash
go install github.com/Beechey/outpost/cmd/outpost@latest
```
</details>

**3. Make sure your shell can find it**

Run `outpost --version`. If it says `command not found`, add Go's bin folder to your PATH:

<details>
<summary><b>zsh</b> (macOS default)</summary>

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```
</details>

<details>
<summary><b>bash</b></summary>

On Linux:

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
```

On macOS, use `~/.bash_profile` instead of `~/.bashrc`.
</details>

<details>
<summary><b>fish</b></summary>

```fish
fish_add_path ~/go/bin
```
</details>

## Set up a server

### 1. Get a server

Install **Ubuntu 24.04** on it and note its **IP address**. outpost logs in as **root** the first time, so you need one of:

- **An SSH key** you added when creating the server. If you have one, it's in `~/.ssh/id_ed25519.pub`.
- **A root password** from the provider. outpost asks for it once, then uses a key from then on. If you don't have a key yet, outpost creates one.

<details>
<summary><b>A computer at home</b> (or any machine where root login is off)</summary>

Ubuntu Server switches off root login and gives you a normal user instead. Copy your key over to root once, from your laptop:

```bash
ssh-copy-id YOUR_USER@SERVER_IP
ssh YOUR_USER@SERVER_IP 'sudo mkdir -p /root/.ssh && sudo cp ~/.ssh/authorized_keys /root/.ssh/'
```

T3 Connect works through your home router, so you don't need to open any ports.
</details>

### 2. Run outpost

```bash
outpost setup SERVER_IP
```

If SSH on the server isn't on port 22, add the port: `outpost setup SERVER_IP:2222`. Next time, run plain `outpost setup` to pick from the servers you've set up before.

It installs everything (3–5 minutes), then walks you through four logins. Each one shows a link or code: open it on your laptop and approve.

| Login | What you do |
|---|---|
| Root password | Only if your SSH key isn't on the server yet. Typed once. |
| **Tailscale** | Open the link and sign in with the **same account as your laptop**. |
| **Claude** | Open the link, approve, and paste the code back into the terminal. |
| **GitHub** | Answer **Yes** to "Authenticate Git", then open <https://github.com/login/device> and enter the code shown. |
| **T3 Connect** | Open the link, check the code matches, and approve. |

Then it reboots the server to prove everything comes back by itself. When you see **All set**:

1. Open T3 Code. The server appears under **Settings → Environments**.
2. Add your repos with **⌘K → Add Project** (Ctrl+K on Linux), choosing the server as the environment. T3 clones from GitHub, GitLab, Bitbucket and more.
3. Start a thread on the server and prompt.

If anything fails, run the same command again. Finished steps are skipped.

## Move to a new server

Switching providers, upgrading, or moving from a VPS to a box at home:

```bash
outpost move OLD_SERVER_IP NEW_SERVER_IP
```

Or run plain `outpost move` and pick both servers from a list.

It checks the old server, sets up the new one, moves everything across, and signs the new one in. The new server takes over as **the same environment in T3 Code**, so your threads continue where they left off.

| Moves | Doesn't move |
|---|---|
| Everything in `~/code`: uncommitted changes, unpushed branches, agent worktrees | Logins. You approve them once on the new server, about a minute |
| T3 Code threads, projects and settings | `node_modules`. Agents reinstall dependencies when they need them |
| Claude Code and Codex history, so agents can resume their sessions | Docker containers and their data. Recreate them from your compose files |
| Your `CLAUDE.md`, skills, agents, commands and settings | Anything outside the `dev` user's home |
| Your git identity (`~/.gitconfig`) | |

**How it works:** T3 on the old server stops before the copy, so nothing changes mid-move. The copy streams from server to server through your laptop. On success, T3 on the old server is switched off for good, so only one machine answers as this environment. If the copy fails, T3 starts again on the old server and nothing is lost.

**Afterwards:** send "continue" to any thread that was mid-turn. When you're happy, delete the old server at your provider and remove it from the [Tailscale admin console](https://login.tailscale.com/admin/machines). Until then, its files stay untouched.

`outpost move` is safe to re-run. If it stops partway, run it again and it picks up from where it got to.

## Day to day

- **Laptop and server side by side.** T3 Code keeps your laptop's local environment, so you choose for each thread where the agent runs. Code moves between them through git.
- **Check in from anywhere** with the T3 Code [iOS](https://apps.apple.com/us/app/t3-code-remote-claude-more/id6787819824) or [Android](https://play.google.com/store/apps/details?id=com.t3tools.t3code) app.
- **See a dev server an agent started**, for example on port 3000:

  <details>
  <summary>With Tailscale</summary>

  On the server, run `tailscale serve 3000`, then open the `https://….ts.net` link it prints.
  </details>

  <details>
  <summary>Without Tailscale</summary>

  ```bash
  ssh -L 3000:localhost:3000 dev@SERVER_IP
  ```

  Then open <http://localhost:3000> on your laptop.
  </details>

- **SSH in** with `ssh dev@SERVER_IP`.
- **Repair** a server with `outpost setup` (pick it from the list). On a server already in use, it checks everything is running instead of rebooting, so working agents aren't cut off.
- **Update the tools:**

  ```bash
  ssh dev@SERVER_IP
  t3 update && claude update && sudo apt update && sudo apt upgrade -y
  ```

After a reboot, T3, Tailscale, Docker, your logins and your threads all come back by themselves. An agent that was mid-turn stops (say "continue"), and dev servers need starting again. Containers come back if their compose file has `restart: unless-stopped`, which is worth putting in your projects' `CLAUDE.md`.

## What gets installed

| | |
|---|---|
| **T3 Code** | Runs as a service that starts at boot, linked to T3 Connect |
| **Claude Code** | Signed in with your Claude account |
| **GitHub CLI and git** | Signed in, and git uses the same login, so private repos work |
| **Node.js 24** | For your projects |
| **Docker** | For the databases and services your projects need |
| **Tailscale** | Private network between your devices, with Tailscale SSH |
| **Basics** | build-essential, ripgrep, jq, tmux, unzip |

Everything runs as a normal user called **`dev`**, and your code lives in `~/code`.

## Security

- **SSH:** key-only, and root login is switched off once `dev` has your key.
- **Firewall (ufw):** only SSH and Tailscale traffic gets in.
- **Docker:** published ports bind to `127.0.0.1` by default, so `docker run -p 5432:5432 postgres` isn't exposed to the internet. Docker otherwise bypasses the firewall.
- **`dev` has passwordless sudo and Docker access,** so agents running as `dev` can control the whole server. That's intended for a single-person dev box. Keep secrets on it to a minimum.
- **Moves never copy logins.** Claude, Codex, GitHub and Tailscale credentials stay on the old server.
- **Optional:** once `ssh dev@<tailscale-name>` works, close public SSH with `sudo ufw delete allow OpenSSH`. Your provider's web console is then your backup way in.

## Troubleshooting

| Problem | Fix |
|---|---|
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | The server was reinstalled. Run `ssh-keygen -R SERVER_IP`, then try again. |
| `can't log in as root` | Check the IP and root password. On a home machine, see [A computer at home](#1-get-a-server). |
| A login didn't finish | Run the same command again. Finished steps are skipped. |
| Server doesn't appear in T3 | `ssh dev@SERVER_IP`, then `t3 connect status` and `t3 service status`. If the service isn't running: `t3 service restart`. |
| Agent says Claude isn't logged in | `ssh dev@SERVER_IP`, then `claude auth login`. |
| Some org repos are missing in T3 | The org restricts third-party apps. An org owner approves **GitHub CLI** under the org's **Settings → Third-party access**. |
| `was already moved to …` | That server's work already moved. Move from the newer server instead. |
| Can't SSH in at all | Use your provider's web console, log in as root, and check `ufw status` and `systemctl status ssh`. |

## FAQ

**What does outpost do that T3 Code doesn't?**
T3 Code runs agents on any machine you install it on, clones repos, and connects your devices. outpost covers what comes before and around that: preparing and securing a bare server, signing it in, proving it survives reboots, and moving everything to another server.

**Can I use my Claude subscription on a server?**
Yes. Anthropic's terms allow signing in to the official Claude Code app with your own subscription, including on hosted machines ([details](https://code.claude.com/docs/en/legal-and-compliance)). T3 Code uses that same login.

**Why not run everything in Docker?**
Agents need the whole machine: Docker for databases, browsers, package installs, git. A plain server is simpler and faster to work with, and outpost makes it reproducible.

**Does it work on ARM, like a Raspberry Pi or an ARM VPS?**
Yes. Any Ubuntu 24.04 machine, x86 or ARM. When moving between x86 and ARM, agents reinstall dependencies, since `node_modules` isn't copied.

**Can I use Codex or other agents?**
Yes. T3 Code supports them. Install and sign in to the agent's CLI on the server as `dev`, and it shows up in T3. `outpost move` carries Codex history over too.

## Development

```
cmd/outpost/          CLI entry point (cobra + fang)
internal/setup/       The setup and move flows, written as step lists
  setup.go            Setup, and the steps it shares with move: connect, install, final check
  move.go             Move: survey, confirm, transfer, retire
  manifest.go         What a move carries, and every shell command built from that
  dev.go              The dev user on a server: PATH, quoting, T3 service operations
  logins.go           Tailscale, Claude, GitHub and T3 Connect sign-ins (skipped when done)
  scripts/
    bootstrap.sh      Runs as root: packages, user, SSH, firewall, Docker, Tailscale, T3, Claude
internal/remote/      Host, the seam the flows depend on, and Server, its ssh adapter
internal/servers/     Remembered servers and the picker
internal/ui/          Banner, step headers and live progress
test/e2e/             End-to-end test with systemd containers posing as servers
```

```bash
go test ./...                                          # unit tests, in under a second
shellcheck internal/setup/scripts/*.sh test/e2e/run.sh
test/e2e/run.sh                                        # about 10 minutes; needs Docker and ~/.ssh/id_ed25519
```

The flows depend only on `remote.Host`, so unit tests run them against a recording fake: fresh and re-run setups, retired servers, failed copies and their undo. The move manifest's commands run against temporary folders with real `tar` (on Linux; skipped on macOS).

The end-to-end test sets up an "old" server, gives it work (a T3 project, uncommitted changes, an unpushed branch, Claude settings and history), moves it to a "new" server, and checks that everything arrived, credentials didn't, T3 runs after a reboot, and a re-run is safe. Logins need a human, so small stand-ins answer for them. CI runs it on every push.

<details>
<summary><b>Try the real flow locally, logins included</b></summary>

```bash
cd test/e2e
docker build -t outpost-try --build-arg PUBKEY="$(cat ~/.ssh/id_ed25519.pub)" .
docker run -d --name outpost-try --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw -v /var/lib/docker -v /var/lib/containerd \
  --restart unless-stopped -p 2222:22 outpost-try
outpost setup localhost:2222
```

To clean up, sign the container out first so it leaves your Tailscale network and T3 Connect:

```bash
ssh -p 2222 dev@localhost 'export PATH=$HOME/.local/bin:$PATH; t3 connect logout; claude auth logout; gh auth logout; sudo tailscale logout'
docker rm -f -v outpost-try
ssh-keygen -R "[localhost]:2222"
```

If it still shows in the [Tailscale admin console](https://login.tailscale.com/admin/machines), remove it there.
</details>

## License

[MIT](LICENSE)
