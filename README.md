# devbox

Turns a fresh Ubuntu 24.04 server into an always-on box for AI coding agents, driven from T3 Code on your laptop and phone.

Works on any provider (netcup, Hetzner, …) or a machine at home. You do the setup once per server. After that the box comes back by itself after any reboot.

**What you end up with**

- T3 Code running as a background service that starts at boot, reachable from anywhere through T3 Connect
- Claude Code, GitHub CLI, Node 24, Docker and Tailscale installed
- A normal user called `dev` that runs everything (not root)
- SSH with keys only, root SSH login off, and a firewall that only lets in SSH and Tailscale
- Docker ports bound to `localhost`, so a database an agent starts isn't exposed to the internet

---

## One-time setup on your laptop

Do this once, ever.

1. **Install the T3 Code desktop app** and sign in to your T3 Connect account.
2. **Install the T3 Code app on your phone** (optional) and sign in to the same account.
3. **Install Tailscale** (optional, recommended): <https://tailscale.com/download>. Sign in and leave it on. It's free.
4. **Make sure you have an SSH key.** In Terminal, run:
   ```bash
   ls ~/.ssh/id_ed25519.pub
   ```
   If it says "No such file", create one (press Enter at every question):
   ```bash
   ssh-keygen -t ed25519
   ```
5. **Clone this repo:**
   ```bash
   gh repo clone Beechey/devbox ~/devbox
   ```

---

## Setting up a new server (about 10 minutes)

Replace `SERVER_IP` everywhere below with your server's IP address.

### Step 1: Create the server

- At your provider, install **Ubuntu 24.04**.
- If the provider lets you add an SSH key, paste in the output of `cat ~/.ssh/id_ed25519.pub`.
- If you only got a **root password** instead, copy your key over (it asks for that password once):
  ```bash
  ssh-copy-id root@SERVER_IP
  ```
- Check that you can log in **without a password**:
  ```bash
  ssh root@SERVER_IP exit
  ```
  If it asks for a password, stop and fix that first. The script turns password login off.

### Step 2: Run the script (from your laptop)

```bash
cd ~/devbox
scp bootstrap.sh root@SERVER_IP:
ssh -t root@SERVER_IP bash bootstrap.sh
```

- It takes 3–5 minutes.
- Near the end it asks you to **choose a password for `dev`**. You'll type this when you use `sudo`; it's not used to log in. Save it in your password manager.
- If you see `WARNING: dev has no SSH key`, step 1 wasn't finished. Fix it and run step 2 again. The script is safe to re-run.

After this, **log in as `dev`, not root.** Root SSH login is now off.

### Step 3: Log in to everything (one time per server)

```bash
ssh dev@SERVER_IP
```

Then run these one at a time. Each one shows a link or code: open it on your laptop and approve.

| Command | What it does |
|---|---|
| `sudo tailscale up --ssh` | Adds the server to your Tailscale network. Optional; skip it if you don't use Tailscale. |
| `claude auth login` | Signs Claude Code in with your Claude subscription. |
| `gh auth login` | Signs in to GitHub so agents can clone, push and open PRs. Choose GitHub.com → HTTPS → "Login with a web browser". |
| `t3 connect` | Links the server to your T3 Connect account and installs T3 as a background service that starts at boot. |

Optional, so agents can use a browser to check web pages:

```bash
sudo ~/.local/bin/t3 browser setup
```

### Step 4: Check it survives a reboot

```bash
t3 service status
```

You want to see it **running**, **enabled**, and with **linger** on. Then:

```bash
sudo reboot
```

Wait about a minute. Open T3 on your laptop: the server should show up under **Settings → Environments** without you doing anything. **Setup is done.**

---

## Day-to-day use

- In the T3 desktop app, your **Local** environment and the server are listed side by side. Choose the server when you start a thread to run the agent there.
- Agents keep running on the server when your laptop is closed or off. Check in from your phone.
- Code moves between laptop and server through **git** (push and pull). Files don't sync live. Clone a repo on the server once, either by asking an agent or with `gh repo clone owner/repo` over SSH.
- **To see a dev server an agent started** (for example on port 3000):
  - With Tailscale: on the server run `tailscale serve 3000`, then open the `https://….ts.net` link it prints.
  - Without Tailscale: on your laptop run `ssh -L 3000:localhost:3000 dev@SERVER_IP` and open <http://localhost:3000>.
- **Docker containers** only come back after a reboot if they're set to. Use `restart: unless-stopped` in compose files. Good to put in your projects' `CLAUDE.md`.

### After a reboot

Everything comes back on its own: T3, Tailscale, Docker, logins, and your threads. Two things don't:

- An agent that was mid-turn stops. Open the thread and send "continue".
- Dev servers (`npm run dev` and so on) need starting again.

---

## Moving to a new server

Nothing important lives only on the server: your code is in git and the setup is this script. To move:

1. Push any unfinished work from the old server (`git push` on each branch).
2. Do **Setting up a new server** above on the new one.
3. In T3 on your laptop, remove the old environment under **Settings → Environments**.
4. Cancel the old server.

A free T3 Connect account can link up to 3 machines, so remove old ones you no longer use.

---

## Updating

```bash
ssh dev@SERVER_IP
t3 update                          # T3 Code
claude update                      # Claude Code
sudo apt update && sudo apt upgrade -y   # everything else
```

Re-running `bootstrap.sh` is also safe. It skips anything already installed.

---

## When something's wrong

| Problem | Fix |
|---|---|
| Server doesn't show up in T3 | `ssh dev@SERVER_IP` then `t3 service status`. If it's not running: `t3 service restart`. Still broken: run `t3 connect` again. |
| Agent says Claude isn't logged in | `ssh dev@SERVER_IP` then `claude auth login`. |
| Can't SSH in at all | Use your provider's web console (netcup: Server Control Panel → your server → Console) and log in as `dev` with the password from step 2. |
| `t3: command not found` | Run `source ~/.bashrc`, or log out and back in. |
| Agent can't push to GitHub | `gh auth status`, then `gh auth login` if needed. |

---

## Good to know

- **Claude subscription:** signing in to the official Claude Code app with your own subscription on a server is allowed ([Anthropic's terms](https://code.claude.com/docs/en/legal-and-compliance)). Check your usage at claude.ai after the first few days to see how it's being counted.
- **Security:** the `dev` user can use Docker, which gives it full control of the server, and so do agents running as `dev`. That's normal for a personal dev box. Don't keep secrets you can't afford to lose on it.
- **Closing public SSH (optional):** once `ssh dev@<tailscale-name>` works, you can block SSH from the internet with `sudo ufw delete allow OpenSSH`. The provider's web console is then your backup way in.
