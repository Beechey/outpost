# outpost guide

Everything about using outpost, in the order you'll need it: set a server up, live with it, then move on. For the short version, see the [README](../README.md). For what happens under the hood, see [How it works](how-it-works.md).

- [Set up a server](#set-up-a-server)
- [Live with it](#live-with-it)
  - [Check on it](#check-on-it)
  - [Choose how your devices connect](#choose-how-your-devices-connect)
  - [Sign in again](#sign-in-again)
  - [Change swap](#change-swap)
  - [Update it](#update-it)
  - [Back it up](#back-it-up)
  - [Working with it day to day](#working-with-it-day-to-day)
- [Move to a new server](#move-to-a-new-server)
- [Troubleshooting](#troubleshooting)
- [FAQ](#faq)

Every command takes a server as an IP or hostname, with `:port` if SSH isn't on 22. Leave it out and outpost lets you pick from the servers you've set up. `outpost list` shows them, and `outpost forget SERVER` takes one off the list, say after you delete it at your provider; nothing on the server changes.

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

**Swap.** First it shows the server's memory and free disk and asks how many GB to use as swap: disk the server uses as backup memory, so when agents fill the memory it slows down instead of stopping programs. About as much as the server's memory is plenty; enter `0` for none. outpost always leaves 10 GB of disk free. You can [change it later](#change-swap).

**Install.** Then it installs everything, which takes 3–5 minutes.

**Logins.** Each shows a link or code: open it on your laptop and approve.

| Login | What you do |
|---|---|
| Root password | Only if your SSH key isn't on the server yet. Typed once. |
| **Tailscale** | Open the link and sign in with the **same account as your laptop**. |
| **Claude** | Open the link, approve, and paste the code back into the terminal. |
| **GitHub** | Answer **Yes** to "Authenticate Git", then open <https://github.com/login/device> and enter the code shown. |
| **T3 Connect** | Open the link, check the code matches, and approve. With Tailscale access, you scan a pairing QR code instead. |

**Reboot check.** Finally it reboots the server to prove everything comes back by itself. When you see **All set**:

1. Open T3 Code. The server appears under **Settings → Environments**.
2. Add your repos with **⌘K → Add Project** (Ctrl+K on Linux), choosing the server as the environment. T3 clones from GitHub, GitLab, Bitbucket and more.
3. Start a thread on the server and prompt.

If anything fails, run the same command again. Finished steps are skipped, and a server already in use gets a health check instead of a reboot, so it's also how you repair one.

<details>
<summary><b>Answer the questions in advance</b></summary>

| Flag | Does |
|---|---|
| `--swap 4` | Adds 4 GB of swap without asking. `--swap 0` adds none. |
| `--tailscale` | Serves T3 on your tailnet only, instead of T3 Connect. |
| `--connect` | Uses T3 Connect, the default for a new server. |
</details>

## Live with it

Every choice setup makes has its own command, so you never need to set a server up again to change one.

### Check on it

```bash
outpost status SERVER_IP
```

```
203.0.113.7
  • Memory 8 GB, 4 GB of swap, 52 GB of disk free  outpost swap 203.0.113.7
  • Access T3 Connect  outpost access 203.0.113.7
  ✓ T3 Code, Docker and Tailscale running
  ✓ Tailscale signed in
  ✗ Claude not signed in. Fix: outpost login 203.0.113.7 claude
  ✓ GitHub signed in
```

It shows memory, swap and disk, how devices reach T3, whether everything is running, and each sign-in, with the command that changes or fixes each line. It only looks; nothing on the server changes. It also tells you when installed updates are waiting for a reboot.

### Choose how your devices connect

| | **T3 Connect** (default) | **Tailscale** |
|---|---|---|
| Reach the server from | Anywhere: laptop, phone, browser | Devices on your tailnet |
| Devices need | Nothing extra | The Tailscale app, signed in and connected |
| Phone notifications and Live Activities | Yes | No: T3 delivers these only through T3 Connect |
| Traffic goes | Through T3 Connect's relay | Directly between your devices, encrypted by Tailscale |
| Cost | Free for 3 machines; [T3 Chat](https://t3.chat) Pro raises it to 10, Premier to 100 | Free, unlimited |
| Adding a device | Sign in to T3 Connect on it | `outpost pair SERVER`, then scan the QR code |

```bash
outpost access SERVER_IP tailscale   # serve T3 on your tailnet only
outpost access SERVER_IP connect     # back to T3 Connect
outpost access SERVER_IP             # see the current choice, then pick
outpost pair SERVER_IP               # pair another device, such as your phone
```

Switching signs in to the new way and turns the old one off, without reinstalling or rebooting. To be sure a server that leaves T3 Connect frees its machine slot, deregister it in T3 Code, under the account menu → **T3 Connect**. Pairing links work for 5 minutes; a paired device stays paired.

**Which to pick:** T3 Connect for the servers you check from your phone, since only T3 Connect gives push notifications. Tailscale for extra servers beyond your T3 Connect limit, or if you'd rather traffic didn't go through a relay.

The first time, Tailscale may ask you to **enable HTTPS** for your tailnet (T3 is served over HTTPS). Do it once in the [Tailscale admin console](https://login.tailscale.com/admin/dns), then run the same command again.

### Sign in again

Logins can expire, or you may want to switch account:

```bash
outpost login SERVER_IP claude       # or tailscale, github, t3
outpost login SERVER_IP              # every account it isn't signed in to
```

Naming an account signs in to it again even if it looks signed in. `t3` is T3 Connect, or the Tailscale pairing link, depending on how devices reach the server.

### Change swap

```bash
outpost swap SERVER_IP               # see memory, swap and disk, then choose
outpost swap SERVER_IP 8             # set it to 8 GB
outpost swap SERVER_IP 0             # remove it
```

No reboot needed, and outpost still leaves 10 GB of disk free. Shrinking or removing swap moves what's in it back into memory, so it can fail while agents are using most of the memory; try again when they're quieter. outpost only changes its own swap file, never a swap partition from your provider.

### Update it

Security fixes install by themselves every day, without rebooting. For everything else:

```bash
outpost update SERVER_IP
```

It updates T3 Code, Claude Code and the system packages. T3 restarts on its new version, so agents mid-turn stop, and outpost asks first (`--yes` skips the question). If Docker is updated it restarts too. When an update needs a reboot to take effect, outpost says so; reboot when agents are idle with `ssh dev@SERVER_IP sudo reboot`.

### Back it up

```bash
outpost backup SERVER_IP
outpost backup SERVER_IP ~/backups/agents.tar.gz
```

This downloads the same things a move carries into a `.tar.gz` on your laptop, by default `outpost-SERVER_IP-DATE.tar.gz` in the current folder. Logins aren't included. T3 pauses while it downloads, so threads are saved whole, then starts again; outpost asks first, and `--yes` skips the question. If the download fails, no file is left behind and T3 starts again anyway.

The file holds your code, so keep it somewhere safe.

<details>
<summary><b>Restore a backup onto a server</b></summary>

The restored server becomes the same environment in T3 Code as the one you backed up, so don't run both at once. Delete the old server first, or remove its service with `ssh dev@OLD_SERVER_IP .local/bin/t3 service uninstall`.

```bash
outpost setup NEW_SERVER_IP
ssh dev@NEW_SERVER_IP 'systemctl --user stop t3code.service && rm -rf ~/.t3/userdata ~/.t3/worktrees'
ssh dev@NEW_SERVER_IP 'tar -xz -C ~' < outpost-OLD_SERVER_IP-DATE.tar.gz
ssh dev@NEW_SERVER_IP 'systemctl --user start t3code.service'
outpost status NEW_SERVER_IP
```

If T3 Code doesn't show the server afterwards, sign it in again with `outpost login NEW_SERVER_IP t3`.
</details>

### Working with it day to day

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

After a reboot, T3, Tailscale, Docker, your logins and your threads all come back by themselves. An agent that was mid-turn stops (say "continue"), and dev servers need starting again. Containers come back if their compose file has `restart: unless-stopped`, which is worth putting in your projects' `CLAUDE.md`.

## Move to a new server

Switching providers, upgrading, or moving from a VPS to a box at home:

```bash
outpost move OLD_SERVER_IP NEW_SERVER_IP
```

Or run plain `outpost move` and pick both from a list. It checks the old server, asks you to confirm, sets up the new one (asking about swap, as setup does), moves everything across, and signs the new one in. The new server takes over as **the same environment in T3 Code**, so your threads continue where they left off. `--tailscale`, `--connect` and `--swap` work as they do for setup, and `--yes` asks nothing at all.

| Moves | Doesn't move |
|---|---|
| Everything in `~/code`: uncommitted changes, unpushed branches, agent worktrees | Logins. You approve them once on the new server, about a minute |
| T3 Code threads, projects and settings | `node_modules`. Agents reinstall dependencies when they need them |
| Claude Code and Codex history, so agents can resume their sessions | Docker containers and their data. Recreate them from your compose files |
| Your `CLAUDE.md`, skills, agents, commands and settings | Anything outside the `dev` user's home |
| Your git identity (`~/.gitconfig`) | |

**Afterwards:** send "continue" to any thread that was mid-turn. When you're happy, delete the old server at your provider and remove it from the [Tailscale admin console](https://login.tailscale.com/admin/machines). Until then, its files stay untouched and T3 there stays off. outpost refuses to bring T3 back on it, so two machines never answer as the same environment.

`outpost move` is safe to re-run. If it stops partway, run it again and it picks up from where it got to. If the copy itself fails, T3 starts again on the old server, so nothing is lost.

## Troubleshooting

Start with `outpost status SERVER_IP`: it names the fix for most problems.

| Problem | Fix |
|---|---|
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | The server was reinstalled. Run `ssh-keygen -R SERVER_IP`, then try again. |
| `can't log in as root` | Check the IP and root password. On a home machine, see [A computer at home](#1-get-a-server). |
| A login didn't finish | Run the same command again, or `outpost login SERVER_IP ACCOUNT`. |
| Server doesn't appear in T3 | `outpost status SERVER_IP`. If T3 isn't running, `outpost setup SERVER_IP` repairs it; if it is, `outpost login SERVER_IP t3`. |
| Tailscale asks to enable HTTPS | Enable it once in the [Tailscale admin console](https://login.tailscale.com/admin/dns), then run the command again. |
| Pairing link expired | Run `outpost pair SERVER_IP` for a new one. |
| Agent says Claude isn't logged in | `outpost login SERVER_IP claude`. |
| `swapoff` failed while changing swap | Memory is too full to empty the old swap into. Try again when agents are quieter. |
| Some org repos are missing in T3 | The org restricts third-party apps. An org owner approves **GitHub CLI** under the org's **Settings → Third-party access**. |
| `was already moved to …` | That server's work already moved. Move from the newer server instead. |
| `it's retired` | The server's work was moved elsewhere. Use the new server instead, or delete `~/.t3/moved-to` on it to reuse it. |
| Can't SSH in at all | Use your provider's web console, log in as root, and check `ufw status` and `systemctl status ssh`. |

## FAQ

**What does outpost do that T3 Code doesn't?**
T3 Code runs agents on any machine you install it on, clones repos, and connects your devices. outpost covers what comes before and around that: preparing and securing a bare server, signing it in, proving it survives reboots, looking after it, backing it up, and moving everything to another server. T3 Code has no export of its own.

**Do I need outpost running all the time?**
No. outpost runs from your laptop only when you use a command. The server looks after itself: T3, Docker and Tailscale start at boot, and Ubuntu installs security fixes daily.

**Can I use my Claude subscription on a server?**
Yes. Anthropic's terms allow signing in to the official Claude Code app with your own subscription, including on hosted machines ([details](https://code.claude.com/docs/en/legal-and-compliance)). T3 Code uses that same login.

**More than 3 machines on T3 Connect?**
Link a paid [T3 Chat](https://t3.chat) account in T3 Code (account menu → **T3 Chat** → **Link T3 Chat**): Pro allows 10 machines, Premier 100. Or switch some servers to Tailscale with `outpost access SERVER tailscale`, which is free and unlimited.

**Why not run everything in Docker?**
Agents need the whole machine: Docker for databases, browsers, package installs, git. A plain server is simpler and faster to work with, and outpost makes it reproducible.

**Does it work on ARM, like a Raspberry Pi or an ARM VPS?**
Yes. Any Ubuntu 24.04 machine, x86 or ARM. When moving between x86 and ARM, agents reinstall dependencies, since `node_modules` isn't copied.

**Can I use Codex or other agents?**
Yes. Install and sign in to the agent's CLI on the server as `dev`, and it shows up in T3. `outpost move` and `outpost backup` carry Codex history too.
