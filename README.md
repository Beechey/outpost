<p align="center">
  <img src="docs/logo.svg" width="110" alt="outpost logo">
</p>

<h1 align="center">outpost</h1>

<h3 align="center">Always-on servers for your AI coding agents.</h3>

<p align="center">
  One command turns a fresh server into a secure, signed-in home for your agents, ready for <a href="https://t3.codes">T3 Code</a>.<br>
  Another moves everything to a new one, threads and all.
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
  <a href="docs/guide.md"><b>Guide</b></a> ·
  <a href="docs/how-it-works.md"><b>How it works</b></a> ·
  <a href="CONTRIBUTING.md"><b>Contributing</b></a>
</p>

<p align="center">
  <img src="docs/demo.svg" width="720" alt="outpost setup installing, signing in and reboot-checking a server">
</p>

## Why outpost

Your agents should keep working when your laptop is closed. T3 Code runs them on any machine; outpost makes that machine.

<table>
<tr>
<td width="33%" valign="top">

### One command

`outpost setup` installs, secures and signs in a bare Ubuntu server in about ten minutes. You just approve a few links.

</td>
<td width="33%" valign="top">

### Survives reboots

T3, Docker and Tailscale start at boot. outpost reboots every new server to prove it, then hands it over.

</td>
<td width="33%" valign="top">

### Moves with you

`outpost move` carries your code, unpushed work, T3 threads and agent history to a new server. Threads continue where they left off.

</td>
</tr>
<tr>
<td valign="top">

### Locked down

Key-only SSH, no root login, a firewall, and Docker ports that stay private.

</td>
<td valign="top">

### Your way in

T3 Connect from anywhere, or `--tailscale` for a free, private, unlimited tailnet setup.

</td>
<td valign="top">

### Safe to re-run

Finished steps are skipped, and a server already in use gets a health check, never a surprise reboot.

</td>
</tr>
</table>

## Quick start

**1. Install outpost** (needs [Go 1.26+](https://go.dev/dl/))

```bash
gh repo clone Beechey/outpost && cd outpost && go install ./cmd/outpost
```

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

<details>
<summary><b>Once the repo is public</b></summary>

```bash
go install github.com/Beechey/outpost/cmd/outpost@latest
```
</details>

**2. Get a server** with **Ubuntu 24.04**, from any provider or at home, and note its IP.

**3. Set it up**

```bash
outpost setup 203.0.113.7
```

Approve the logins as they come up. When you see **All set**, open T3 Code, pick the server, add a project with **⌘K → Add Project**, and start prompting.

> [!TIP]
> No root SSH key on the server? outpost asks for the root password once. Using a computer at home? See the [guide](docs/guide.md#1-get-a-server).

## Commands

| Command | What it does |
|---|---|
| `outpost setup SERVER` | Prepare a fresh server, or repair one. Leave out `SERVER` to pick from a list. |
| `outpost move OLD NEW` | Move everything to a new server. |
| `outpost pair SERVER` | Pair another device with a Tailscale server. |
| `--tailscale` / `--connect` | Choose how devices reach the server (on `setup` and `move`). |

Servers are an IP or hostname, with `:port` if SSH isn't on 22.

## T3 Connect or Tailscale

|  | **T3 Connect** (default) | **Tailscale** (`--tailscale`) |
|---|---|---|
| Reach it from | Anywhere | Your tailnet |
| Devices need | Nothing extra | The Tailscale app, connected |
| Phone notifications | Yes: agent done, needs approval, Live Activities | No, T3 sends these only through T3 Connect |
| Cost | Free for 3 machines, more with [T3 Chat](https://t3.chat) Pro (10) or Premier (100) | Free, unlimited |
| Add a device | Sign in to T3 Connect | `outpost pair SERVER` and scan |

## Moving servers

```bash
outpost move 203.0.113.7 198.51.100.4
```

| Moves | Doesn't move |
|---|---|
| Everything in `~/code`, including uncommitted work, unpushed branches and agent worktrees | Logins: approve them again, about a minute |
| T3 Code threads, projects and settings | `node_modules`: reinstalled on demand |
| Claude and Codex history, `CLAUDE.md`, skills and settings | Docker containers and their data |

The new server becomes the same environment in T3 Code. The old one is switched off with its files untouched, and if the copy fails, it starts again so nothing is lost. [More on moving →](docs/guide.md#move-to-a-new-server)

## What you need

- **Accounts:** [Claude](https://claude.ai) (Pro or Max), [GitHub](https://github.com), [Tailscale](https://tailscale.com) (free), [T3 Code](https://t3.codes) (free).
- **Your laptop:** macOS or Linux, with the T3 Code app and Tailscale installed and signed in. Use the same Tailscale account everywhere.
- **A server:** Ubuntu 24.04, x86 or ARM. Agents love memory: **8 GB** runs a few, **16–32 GB** runs many.

## Learn more

- **[Guide](docs/guide.md):** every login step, home servers, Tailscale pairing, day-to-day use, troubleshooting and FAQ.
- **[How it works](docs/how-it-works.md):** what gets installed, security, how moves and reboots work.
- **[Contributing](CONTRIBUTING.md):** code layout, tests, and trying the full flow locally.

<p align="center"><sub>MIT licensed · Built for <a href="https://t3.codes">T3 Code</a> · Not affiliated with T3 Tools or Anthropic</sub></p>
