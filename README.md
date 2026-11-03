<p align="center">
  <img src="docs/logo.svg" width="110" alt="outpost logo">
</p>

<h1 align="center">outpost</h1>

<h3 align="center">Turn any server into an always-on home for your AI coding agents.</h3>

<p align="center">
  One command turns a fresh server into a secure, signed-in home for your agents, ready for <a href="https://t3.codes">T3 Code</a>.<br>
  Every project you build there opens over HTTPS on your laptop and phone, and everything moves to a new server, threads and all.
</p>

<p align="center">
  <a href="https://github.com/Beechey/outpost/actions/workflows/ci.yml"><img src="https://github.com/Beechey/outpost/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/server-Ubuntu%2024.04-E95420?logo=ubuntu&logoColor=white" alt="Ubuntu 24.04">
  <img src="https://img.shields.io/badge/works%20with-T3%20Code-4ADE80" alt="Works with T3 Code">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT license"></a>
</p>

<p align="center">
  <a href="#quick-start"><b>Quick start</b></a> ·
  <a href="#your-projects-on-every-device"><b>Projects on every device</b></a> ·
  <a href="#commands"><b>Commands</b></a> ·
  <a href="docs/guide.md"><b>Guide</b></a> ·
  <a href="docs/how-it-works.md"><b>How it works</b></a>
</p>

<p align="center">
  <img src="docs/demo.svg" width="100%" alt="Split terminal: outpost setup prepares a new server, gateway included; then plain outpost opens the menu and gives a project an HTTPS address">
</p>

## Why outpost

Your agents should keep working when your laptop is closed, and what they build should open on any device you own. T3 Code runs the agents on any machine; outpost makes that machine, and looks after it.

<table>
<tr>
<td width="33%" valign="top">

### One command

`outpost setup` installs, secures and signs in a bare Ubuntu server in about ten minutes. You answer a few questions and approve a few links.

</td>
<td width="33%" valign="top">

### Projects on every device

Each project gets a private HTTPS address your laptop and phone can open, so the clipboard, passkeys and the rest just work. No ports, no tunnels.

</td>
<td width="33%" valign="top">

### Moves with you

`outpost move` carries code, unpushed work, T3 threads, agent history and project addresses to a new server. Devices stay trusted.

</td>
</tr>
<tr>
<td valign="top">

### One menu

Run `outpost` for a server's status at a glance and a menu of everything you can change. Every choice is also a command you can type.

</td>
<td valign="top">

### Survives reboots

T3, Docker, Tailscale and the gateway start at boot. outpost reboots every new server to prove it, then hands it over.

</td>
<td valign="top">

### Locked down

Key-only SSH, a firewall, Docker ports that stay private, daily security fixes, and nothing of the gateway's facing the internet.

</td>
</tr>
</table>

## Quick start

**1. Install outpost** (needs [Go 1.26+](https://go.dev/dl/))

```bash
go install github.com/Beechey/outpost/cmd/outpost@latest
```

Later, `outpost upgrade` brings it up to date.

<details>
<summary><b>Install Go</b></summary>

| macOS | Ubuntu / Debian | Other |
|---|---|---|
| `brew install go` | `sudo snap install go --classic` | <https://go.dev/dl/> |
</details>

<details>
<summary><b><code>outpost: command not found</code>?</b> Add Go's bin folder to your PATH</summary>

**zsh** (macOS default)
```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

**bash** (on macOS, use `~/.bash_profile`)
```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
```

**fish**
```fish
fish_add_path ~/go/bin
```
</details>

**2. Get a server** with **Ubuntu 24.04**, from any provider or at home, and note its IP.

**3. Set it up**

```bash
outpost setup 203.0.113.7
```

It asks what to call the server, how much swap to add, and whether projects should get HTTPS addresses (`--tailscale` keeps T3 itself on your tailnet instead of T3 Connect). Then it installs everything, walks you through the logins and reboots the server to prove it comes back. When you see **All set**, open T3 Code, pick the server, add a project with **⌘K → Add Project**, and start prompting.

> [!TIP]
> No root SSH key on the server? outpost asks for the root password once. Using a computer at home? See the [guide](docs/guide.md#1-get-a-server).

## Living with it

Run `outpost` on its own. Pick a server and you get its status, then a menu of everything you can change: how devices connect, project addresses, sign-ins, swap, updates, backups and moves.

```
╭───────────────────────────────────────────────────────────────╮
│                                                               │
│  ▲ devbox · 203.0.113.7  1 thing to fix                       │
│                                                               │
│  Access    T3 over Tailscale  https://devbox.tail1234.ts.net  │
│  Gateway   Tailscale names, 2 projects                        │
│  Memory    16 GB · 4 GB swap · 120 GB disk free               │
│  Running   ✓ T3 Code  ✓ Docker  ✓ Tailscale  ✓ Gateway        │
│  Logins    ✓ Tailscale  ✗ Claude  ✓ GitHub                    │
│                                                               │
╰───────────────────────────────────────────────────────────────╯

  Projects
    myapp               https://myapp-devbox.tail1234.ts.net
    files.myapp         https://files-myapp-devbox.tail1234.ts.net

  ✗ Claude not signed in. Fix: outpost login devbox claude
```

Every choice in the menu is also a command that goes straight there, and each one asks for whatever you leave out:

```bash
outpost status 203.0.113.7               # the card above; changes nothing
outpost access 203.0.113.7 tailscale     # tailnet only, or back with: connect
outpost login 203.0.113.7 claude         # a login expired
outpost update 203.0.113.7               # T3 Code, Claude Code and system packages
outpost backup 203.0.113.7               # a copy of your work on your laptop
```

## Your projects on every device

Browsers only allow some features over HTTPS: the clipboard, passkeys, `crypto.randomUUID`, service workers. So a web app an agent builds on the server breaks when you open it from your laptop or phone over plain http. outpost's gateway gives each project a private HTTPS address on your tailnet:

```bash
outpost gateway add 203.0.113.7 myapp 3000
# ✓ myapp https://myapp-devbox.tail1234.ts.net
```

A path can go to another port on the same address, and a project can have extra addresses, say for its file storage:

```bash
outpost gateway add 203.0.113.7 myapp 3000 --path /ws=3001 --name files.myapp=9000
```

You choose the kind of address during setup, and can change it any time with `outpost gateway`:

| | **Tailscale names** (default) | **Your own domain** |
|---|---|---|
| Looks like | `https://myapp-devbox.tail1234.ts.net` | `https://myapp.devbox.test` |
| Certificates | Real ones, from Tailscale | outpost's own, which can only vouch for your domain |
| On each device | Nothing | Trust it once: open `http://devbox.test`, or scan the QR code outpost shows |
| On this computer | Nothing | outpost trusts it for you if you like, Zen and Firefox included |

outpost does the Tailscale admin steps itself with an API key, and opens the page where you make one: tagging the server, the policy additions (shown before they're saved), the Services, split DNS, and turning on MagicDNS and HTTPS certificates if they're off. Without a key, it shows each step and checks it. [More on the gateway →](docs/guide.md#give-projects-https-addresses)

## Moving on

Switching providers, upgrading, or bringing a server home:

```bash
outpost move 203.0.113.7 198.51.100.4
```

| Moves | Doesn't move |
|---|---|
| Everything in `~/code`, including uncommitted work, unpushed branches and agent worktrees | Logins: approve them again, about a minute |
| T3 Code threads, projects and settings | `node_modules`: reinstalled on demand |
| Claude and Codex history, `CLAUDE.md`, skills and settings | Docker containers and their data |
| Project addresses, and the certificate your devices trust | |

The new server becomes the same environment in T3 Code, so threads continue where they left off, and it takes over the same project addresses. The old one is switched off with its files untouched, and if the copy fails, it starts again so nothing is lost. [More on moving →](docs/guide.md#move-to-a-new-server)

## Commands

| Command | What it does |
|---|---|
| `outpost` | The menu: pick a server, see its status, change anything. |
| `outpost setup SERVER` | Prepare a fresh server, or repair one. Safe to re-run. |
| `outpost status SERVER` | How a server is set up, and what needs fixing. Changes nothing. |
| `outpost address SERVER` | Where to reach T3 Code and each project. |
| `outpost access SERVER [connect\|tailscale]` | Switch how your devices reach T3. |
| `outpost pair SERVER` | Pair this computer, or a phone, with a server that uses Tailscale. |
| `outpost gateway SERVER [tailscale\|domain\|off]` | Choose, change or repair the HTTPS addresses projects get. |
| `outpost gateway add SERVER PROJECT PORT` | Give a project an address. `--path /PATH=PORT` and `--name NAME=PORT` add more. |
| `outpost gateway remove SERVER PROJECT` | Take a project's addresses away. |
| `outpost gateway trust SERVER` | Trust your own domain's certificate on this computer. |
| `outpost login SERVER [ACCOUNT]` | Sign in again: `tailscale`, `claude`, `github` or `t3`, or whatever is missing. |
| `outpost swap SERVER [GB]` | Change how much swap a server has. `0` removes it. |
| `outpost rename SERVER [NAME]` | Rename a server, in Tailscale too. |
| `outpost update SERVER` | Update T3 Code, Claude Code and system packages. |
| `outpost backup SERVER [FILE]` | Download a server's work to a file on your laptop. |
| `outpost move OLD NEW` | Move everything to a new server. |
| `outpost list` / `outpost forget SERVER` | See the servers outpost remembers, or take one off the list. |
| `outpost upgrade` | Upgrade outpost itself. |

Servers are an IP or hostname, with `:port` if SSH isn't on 22. Leave one out to pick from the servers you've set up. Flags answer setup's questions in advance: `--name`, `--swap GB`, `--tailscale` or `--connect`, and `--gateway tailscale|domain|off` with `--domain` and `--trust`.

## T3 Connect or Tailscale

How your devices reach T3 Code itself. Project addresses always go over your tailnet, so with T3 Connect, devices that open them need Tailscale too.

|  | **T3 Connect** (default) | **Tailscale** |
|---|---|---|
| Reach it from | Anywhere | Your tailnet |
| Devices need | Nothing extra | The Tailscale app, connected |
| Phone notifications | Yes: agent done, needs approval, Live Activities | No, T3 sends these only through T3 Connect |
| Cost | Free for 3 machines, more with [T3 Chat](https://t3.chat) Pro (10) or Premier (100) | Free, unlimited |
| Add a device | Sign in to T3 Connect | `outpost pair SERVER` and scan |
| Choose it | `outpost access SERVER connect` | `outpost access SERVER tailscale` |

## What you need

- **Accounts:** [Claude](https://claude.ai) (Pro or Max), [GitHub](https://github.com), [Tailscale](https://tailscale.com) (free), [T3 Code](https://t3.codes) (free).
- **Your laptop:** macOS or Linux, with the T3 Code app and Tailscale installed and signed in. Use the same Tailscale account everywhere.
- **A server:** Ubuntu 24.04, x86 or ARM. Agents love memory: **8 GB** runs a few, **16–32 GB** runs many.
- **For project addresses:** Tailscale on each device that opens them, and a [Tailscale API key](https://login.tailscale.com/admin/settings/keys) if you'd like outpost to do the admin steps.

## Learn more

- **[Guide](docs/guide.md):** setting up, living with a server, project addresses, moving, backups, troubleshooting and FAQ.
- **[How it works](docs/how-it-works.md):** what gets installed, security, and what each command does on the server.
- **[Contributing](CONTRIBUTING.md):** code layout, tests, and trying the full flow locally.

<p align="center"><sub>MIT licensed · Built for <a href="https://t3.codes">T3 Code</a> · Not affiliated with T3 Tools or Anthropic</sub></p>
