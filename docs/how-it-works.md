# How outpost works

What outpost does to a server, why, and what each command changes. It follows the same order as the [guide](guide.md): set up, live with it, move on.

## The shape of a run

outpost runs on your laptop and drives the server over your normal `ssh`, so your keys, agent and `~/.ssh/config` all apply. Nothing is installed on your laptop besides the `outpost` binary, and nothing of outpost's runs on the server between commands: the server looks after itself.

```mermaid
flowchart LR
  L[Your laptop<br/>outpost] -- ssh --> S[Server<br/>bootstrap.sh as root]
  S --> D[dev user<br/>T3 Code, Claude Code, gh]
  D -- T3 Connect or tailnet --> P[T3 Code on your<br/>laptop, phone, browser]
```

## Setting up

`outpost setup` runs five steps:

1. **SSH key.** Uses your existing key or agent, or creates one.
2. **Connect.** Logs in as `dev` if the server is already set up, otherwise as `root`, copying your key over with your root password if needed.
3. **Install.** If the server has no swap, shows its memory and free disk and asks how much to add (`--swap` answers in advance), then makes the swap file. Then uploads `bootstrap.sh` to a private temporary file and runs it as root. It's safe to re-run: each part checks before it changes anything.
4. **Logins.** Each login is skipped if it's already done, so re-runs only ask for what's missing.
5. **Final check.** Reboots and waits for the server to come back, then confirms T3, Docker and Tailscale are running. A server that already runs T3 may have agents working, so it gets the health check without the reboot.

## What gets installed

| | |
|---|---|
| **T3 Code** | A systemd user service with lingering, so it starts at boot without anyone logging in |
| **Claude Code** | The native install in `~/.local/bin`, signed in with your Claude account |
| **GitHub CLI and git** | Signed in, with `gh auth setup-git` so plain git uses the same login for private repos |
| **Node.js 24** | From NodeSource |
| **Docker** | From get.docker.com, enabled at boot |
| **Tailscale** | Enabled at boot, with Tailscale SSH |
| **Basics** | build-essential, ripgrep, jq, tmux, unzip, ufw |
| **Security updates** | `unattended-upgrades`: daily, security fixes only, never rebooting by itself |
| **Swap** (if you choose it) | `/swapfile`, kept at boot in `/etc/fstab` |

Everything runs as a normal user called **`dev`**, and code lives in `~/code`. Downloads retry, so a brief network drop doesn't fail the install, and apt waits for the daily updates to finish rather than failing on their lock.

## Security

- **SSH:** key-only. Root login is switched off once `dev` has your key, so you can't lock yourself out.
- **Firewall (ufw):** only SSH and traffic on the Tailscale interface get in.
- **Docker:** published ports bind to `127.0.0.1` by default (`/etc/docker/daemon.json`), so `docker run -p 5432:5432 postgres` isn't exposed to the internet. Docker otherwise bypasses ufw.
- **Security updates:** Ubuntu's security fixes install daily. Kernel fixes take effect at the next reboot, which outpost leaves to you so agents aren't cut off; `outpost status` tells you when one is waiting.
- **`dev` has passwordless sudo and Docker access,** so agents running as `dev` can control the whole server. That's intended for a single-person dev box. Keep secrets on it to a minimum.
- **T3 over Tailscale** listens on the tailnet only; ufw blocks the port from the internet, and pairing tokens expire after 5 minutes.
- **Moves and backups never copy logins:** Claude and Codex credentials, and T3's per-process locks, are excluded. Tailscale and GitHub sign-ins live outside the copied folders.
- **Input checks:** server addresses are validated before they reach ssh or a remote shell, sizes are whole numbers, and moves and backups only ever pack their own fixed list of folders.
- **Optional:** once `ssh dev@<tailscale-name>` works, close public SSH with `sudo ufw delete allow OpenSSH`. Your provider's web console is then your backup way in.

## Living with it

Each command after setup logs in as `dev`, so it needs a server outpost set up. Those that would start T3 (`access`, `login`, `update`) refuse a [retired](#how-a-move-works) server, so two machines never answer as the same environment.

| Command | On the server |
|---|---|
| `status` | Reads memory, swap and disk from `/proc/meminfo` and `df`, `t3 status --json` for access, `systemctl is-active` for the services, each login's own status check, and `/var/run/reboot-required`. Changes nothing. |
| `access` | Runs `t3 up --connect --no-lan` or `t3 up --lan --no-connect` (then `t3 pair --tailscale`, whose `t3code://` link outpost opens here, or whose QR code it shows for another device), which turns the new way on and the old one off. No reinstall, no reboot. |
| `login` | Runs the same sign-in as setup for the named account, even if its status check passes, since a login can look valid after it has expired. With no account, signs in to each one whose check fails. |
| `swap` | As root: turns `/swapfile` off, removes it and its `/etc/fstab` line, then makes, turns on and records one of the new size. Swap from elsewhere, such as a provider's partition, is left alone. Free disk counts the space the old file gives back, and 10 GB always stays free. |
| `update` | `t3 update --yes`, which restarts T3 on the new version; `claude update`; then `apt-get update` and `upgrade`, keeping config files you changed. |
| `backup` | See [below](#how-a-backup-works). |

## How a backup works

`outpost backup SERVER [FILE]` packs the same folders as a move, with the same exclusions, and streams the archive over ssh into a file on your laptop. It writes to a hidden temporary file beside `FILE` and renames it only when the download finishes, so a failed backup leaves nothing behind.

T3 keeps threads in SQLite, so outpost stops T3's service for the download, as a move does, and starts it again afterwards, even if the download fails or you press Ctrl-C. A server whose T3 isn't running, such as a retired one, is backed up without being touched.

## How a move works

`outpost move OLD NEW` runs seven steps: SSH key, check the old server, connect to the new one (and confirm), install, move your work, logins, final check.

**What moves** is defined in one place, [`manifest.go`](../internal/setup/manifest.go): `~/code`, `~/.t3/userdata` and `~/.t3/worktrees`, `~/.claude` and `~/.claude.json`, `~/.codex`, and `~/.gitconfig`. Excluded: credentials, T3's lock files and log contents, and `node_modules`.

**Moving your work:**

1. **Stop T3 on the old server,** so nothing changes mid-copy.
2. **Clear T3's data on the new server,** including any leftover moved-to record.
3. **Stream a tar archive** from the old server to the new one, through your laptop, showing progress. Files that change during the copy (say, from a container) are copied as they were, not treated as an error.
4. **Retire T3 on the old server:** remove its service, keep its files, and write `~/.t3/moved-to` with the new server's address.
5. **Start T3 on the new server.**

If the copy fails, or you press Ctrl-C, T3 starts again on the old server; if even that fails, outpost tells you the command to run.

**Why threads continue:** T3's data folder holds the environment's identity, so the new server is the same environment to T3 Code. outpost always uses the same user and paths, so Claude's session history (stored by project path) lines up, and agents can resume.

**Guards:** a re-run of a finished move skips the copy and doesn't reboot. `setup`, `access`, `login` and `update` refuse a retired server, and moving from a retired server points you to where its work went.

## Reboots

Everything that matters is a systemd service enabled at boot: Docker, Tailscale, ssh, and T3 (a user service, kept running by lingering). Logins and swap are stored on disk. outpost's reboot check proves this on every new server. After a reboot:

- **Comes back:** T3 Code and your threads, Tailscale, Docker, logins, swap.
- **Doesn't:** an agent's turn in progress (say "continue"), dev servers, and containers without `restart: unless-stopped`.
