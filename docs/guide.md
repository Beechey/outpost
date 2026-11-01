# outpost guide

Everything about using outpost, task by task. For the short version, see the [README](../README.md). For what happens under the hood, see [How it works](how-it-works.md).

- [Set up a server](#set-up-a-server)
- [Choose how your devices connect](#choose-how-your-devices-connect)
- [Move to a new server](#move-to-a-new-server)
- [Day to day](#day-to-day)
- [Troubleshooting](#troubleshooting)
- [FAQ](#faq)

## Set up a server

### 1. Get a server

Install **Ubuntu 24.04** on it and note its **IP address**. outpost logs in as **root** the first time, so you need one of:

- **An SSH key** you added when creating the server. If you have one, it's in `~/.ssh/id_ed25519.pub` (or another `~/.ssh/id_*.pub`).
- **A root password** from the provider. outpost asks for it once, then uses your key from then on. If you don't have a key yet, outpost creates one.

Agents use little CPU but plenty of memory: **8 GB RAM** runs a few at once, **16–32 GB** runs many.

<details>
<summary><b>A computer at home</b> (or any machine where root login is off)</summary>

Ubuntu Server switches off root login and gives you a normal user instead. Copy your key over to root once, from your laptop:

```bash
ssh-copy-id YOUR_USER@SERVER_IP
ssh YOUR_USER@SERVER_IP 'sudo mkdir -p /root/.ssh && sudo cp ~/.ssh/authorized_keys /root/.ssh/'
```

T3 Connect and Tailscale both work through your home router, so you don't need to open any ports.
</details>

### 2. Run outpost

```bash
outpost setup SERVER_IP
```

- SSH on another port? Add it: `outpost setup SERVER_IP:2222`.
- Next time, run plain `outpost setup` and pick the server from a list.
- Want it on your tailnet only? Add `--tailscale` (see [below](#choose-how-your-devices-connect)).

It installs everything (3–5 minutes), then walks you through the logins. Each shows a link or code: open it on your laptop and approve.

| Login | What you do |
|---|---|
| Root password | Only if your SSH key isn't on the server yet. Typed once. |
| **Tailscale** | Open the link and sign in with the **same account as your laptop**. |
| **Claude** | Open the link, approve, and paste the code back into the terminal. |
| **GitHub** | Answer **Yes** to "Authenticate Git", then open <https://github.com/login/device> and enter the code shown. |
| **T3 Connect** | Open the link, check the code matches, and approve. With `--tailscale`, you scan a pairing QR code instead. |

Then it reboots the server to prove everything comes back by itself. When you see **All set**:

1. Open T3 Code. The server appears under **Settings → Environments**.
2. Add your repos with **⌘K → Add Project** (Ctrl+K on Linux), choosing the server as the environment. T3 clones from GitHub, GitLab, Bitbucket and more.
3. Start a thread on the server and prompt.

If anything fails, run the same command again. Finished steps are skipped.

## Choose how your devices connect

| | **T3 Connect** (default) | **Tailscale** (`--tailscale`) |
|---|---|---|
| Reach the server from | Anywhere: laptop, phone, browser | Devices on your tailnet |
| Devices need | Nothing extra | The Tailscale app, signed in and connected |
| Phone notifications and Live Activities | Yes | No: T3 delivers these only through T3 Connect |
| Traffic goes | Through T3 Connect's relay | Directly between your devices, encrypted by Tailscale |
| Cost | Free for 3 machines; [T3 Chat](https://t3.chat) Pro raises it to 10, Premier to 100 | Free, unlimited |
| Adding a device | Sign in to T3 Connect on it | `outpost pair SERVER`, then scan the QR code |

```bash
outpost setup SERVER_IP --tailscale     # serve T3 on your tailnet only
outpost pair SERVER_IP                   # pair another device, such as your phone
outpost setup SERVER_IP --connect        # switch back to T3 Connect
```

Re-runs keep whatever a server already uses, so a Tailscale server stays on Tailscale. Pairing links work for 5 minutes; a paired device stays paired.

**Which to pick:** T3 Connect for the servers you check from your phone, since only T3 Connect gives push notifications. Tailscale for extra servers beyond your T3 Connect limit, or if you'd rather traffic didn't go through a relay.

The first time, Tailscale may ask you to **enable HTTPS** for your tailnet (T3 is served over HTTPS). Do it once in the [Tailscale admin console](https://login.tailscale.com/admin/dns), then run the same command again.

## Move to a new server

Switching providers, upgrading, or moving from a VPS to a box at home:

```bash
outpost move OLD_SERVER_IP NEW_SERVER_IP
```

Or run plain `outpost move` and pick both from a list. Add `--tailscale` or `--connect` to choose how devices reach the new server.

It checks the old server, sets up the new one, moves everything across, and signs the new one in. The new server takes over as **the same environment in T3 Code**, so your threads continue where they left off.

| Moves | Doesn't move |
|---|---|
| Everything in `~/code`: uncommitted changes, unpushed branches, agent worktrees | Logins. You approve them once on the new server, about a minute |
| T3 Code threads, projects and settings | `node_modules`. Agents reinstall dependencies when they need them |
| Claude Code and Codex history, so agents can resume their sessions | Docker containers and their data. Recreate them from your compose files |
| Your `CLAUDE.md`, skills, agents, commands and settings | Anything outside the `dev` user's home |
| Your git identity (`~/.gitconfig`) | |

**Afterwards:** send "continue" to any thread that was mid-turn. When you're happy, delete the old server at your provider and remove it from the [Tailscale admin console](https://login.tailscale.com/admin/machines). Until then, its files stay untouched and T3 there stays off.

`outpost move` is safe to re-run. If it stops partway, run it again and it picks up from where it got to. If the copy itself fails, T3 starts again on the old server, so nothing is lost.

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

## Troubleshooting

| Problem | Fix |
|---|---|
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | The server was reinstalled. Run `ssh-keygen -R SERVER_IP`, then try again. |
| `can't log in as root` | Check the IP and root password. On a home machine, see [A computer at home](#1-get-a-server). |
| A login didn't finish | Run the same command again. Finished steps are skipped. |
| Server doesn't appear in T3 | `ssh dev@SERVER_IP`, then `t3 status` and `t3 service status`. If the service isn't running: `t3 service restart`. |
| Tailscale asks to enable HTTPS | Enable it once in the [Tailscale admin console](https://login.tailscale.com/admin/dns), then run the command again. |
| Pairing link expired | Run `outpost pair SERVER_IP` for a new one. |
| Agent says Claude isn't logged in | `ssh dev@SERVER_IP`, then `claude auth login`. |
| Some org repos are missing in T3 | The org restricts third-party apps. An org owner approves **GitHub CLI** under the org's **Settings → Third-party access**. |
| `was already moved to …` | That server's work already moved. Move from the newer server instead. |
| `it's retired` | The server's work was moved elsewhere. Set up the new server instead, or delete `~/.t3/moved-to` on it to reuse it. |
| Can't SSH in at all | Use your provider's web console, log in as root, and check `ufw status` and `systemctl status ssh`. |

## FAQ

**What does outpost do that T3 Code doesn't?**
T3 Code runs agents on any machine you install it on, clones repos, and connects your devices. outpost covers what comes before and around that: preparing and securing a bare server, signing it in, proving it survives reboots, and moving everything to another server.

**Can I use my Claude subscription on a server?**
Yes. Anthropic's terms allow signing in to the official Claude Code app with your own subscription, including on hosted machines ([details](https://code.claude.com/docs/en/legal-and-compliance)). T3 Code uses that same login.

**More than 3 machines on T3 Connect?**
Link a paid [T3 Chat](https://t3.chat) account in T3 Code (account menu → **T3 Chat** → **Link T3 Chat**): Pro allows 10 machines, Premier 100. Or use `--tailscale`, which is free and unlimited.

**Why not run everything in Docker?**
Agents need the whole machine: Docker for databases, browsers, package installs, git. A plain server is simpler and faster to work with, and outpost makes it reproducible.

**Does it work on ARM, like a Raspberry Pi or an ARM VPS?**
Yes. Any Ubuntu 24.04 machine, x86 or ARM. When moving between x86 and ARM, agents reinstall dependencies, since `node_modules` isn't copied.

**Can I use Codex or other agents?**
Yes. Install and sign in to the agent's CLI on the server as `dev`, and it shows up in T3. `outpost move` carries Codex history over too.
