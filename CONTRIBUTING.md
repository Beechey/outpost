# Contributing to outpost

## Layout

```
cmd/outpost/          CLI entry point (cobra + fang): every command and its flags
  menu.go             Plain `outpost`: pick a server, its status card, and a menu of the commands
internal/setup/       The flows behind each command
  setup.go            Setup, and the steps it shares with move: connect, install, final check
  move.go             Move: survey, confirm, transfer, retire
  backup.go           Backup: survey, pause T3, download to a local file
  swap.go             Swap: the question, what fits, and making or resizing /swapfile
  manifest.go         What a move carries, and every shell command built from that
  dev.go              The managed server: the dev user on a server outpost set up. Every
                      T3 and systemd command, by name; open and openActive (refuses retired)
  access.go           How devices reach T3: parse, detect, switch, and pair
  logins.go           Sign-ins: the accounts, and login
  status.go           Status: inspect builds a report, printed as a card (also the menu's)
  address.go          Address: where T3 and each project are reached
  gateway.go          The gateway's model: settings, routes, names, and what they render to
  gateway_server.go   The gateway on the server: every command, files, install, the trust page
  gateway_flow.go     Gateway flows: the question tree, on/off/switch, add, remove, trust, move
  gateway_admin.go    The Tailscale admin steps' seam: API, manual (shows steps), or none
  gateway/            Templates embedded in the binary: Caddyfile, Corefile, install.sh, page.html
  update.go           Update: T3 Code, Claude Code and system packages
  scripts/
    bootstrap.sh      Runs as root: packages, user, SSH, firewall, Docker, Tailscale, T3, Claude
internal/remote/      Host, the seam the flows depend on, and Server, its ssh adapter
internal/tailnet/     The Tailscale API: policy patching (HuJSON, comments kept), tags,
                      Services, approval, split DNS. Callers describe a Want
internal/servers/     Remembered servers and the picker
internal/ui/          Banner, step headers, live progress, and Ask: every question to the user
test/e2e/             End-to-end test with systemd containers posing as servers
docs/                 User guide, how it works, logo and demo
```

## Design notes

- **Three seams.** The flows depend on `remote.Host` for the server, `ui.Ask` for the user, and `tailnetAdmin` for the Tailscale admin steps (the API, the user following steps, or nothing on a keyless repair; tests use a recording fake). `remote.Server` and `ui.Terminal` are the real adapters; tests use a recording fake host and scripted answers. New behavior in a flow should come with a test through both.
- **A server outpost set up goes through `dev`.** Commands get one from `open` (reading) or `openActive` (changing, refuses a retired server), so the retired guard can't be forgotten. Its commands are named constants (`cmdT3Running`, `cmdServices`, ...), and the fake host answers only those exact commands, so tests never repeat shell.
- **Questions go through `ui.Ask`.** It checks for a terminal and handles Ctrl-C once; callers decide what no terminal means (a default, or an error naming the flag that answers instead).
- **Shell lives in few places.** `bootstrap.sh` for installing; `dev.go` for T3 and systemd; and beside the one module that needs it (`manifest.go`, `setSwapCmd`, `updates`). Callers express intent (`stopT3`, `retireT3`), not shell.
- **Every choice setup makes has a command to change it later** (`swap`, `access`, `login`, `gateway`), `status` shows it, and the menu offers it. A new question in setup should come with both. Commands that would start T3 refuse a retired server.
- **Anything that reaches a remote shell is validated or ours:** server addresses are validated once, where they're read (`servers.Parse`), moved paths come from the manifest, sizes are whole numbers, and values are quoted with `quote`.
- **Re-runs must be safe.** Every step checks before it acts, and a server already in use is never rebooted.

## Tests

```bash
go test ./...                                          # unit tests, in under a second
shellcheck internal/setup/scripts/*.sh internal/setup/gateway/install.sh test/e2e/run.sh
test/e2e/run.sh                                        # about 10 minutes; needs Docker, Go and ~/.ssh/id_ed25519
```

- **Unit tests** run the flows against the fake host and scripted answers: fresh and re-run setups, swap and access changes, answered and cancelled questions, logins, updates, backups, status reports, retired servers, failed copies and their undo. The manifest's and swap file's commands run against temporary folders with real `tar` and `sed` (on Linux; skipped on macOS, whose tools differ).
- **The end-to-end test** sets up an "old" server, gives it work (a T3 project, uncommitted changes, an unpushed branch, Claude settings and history), runs `outpost setup` and `outpost move` through the real binary, and checks that everything arrived, credentials didn't, T3 runs after a reboot, the old server is retired, and re-runs are safe. Logins need a human, so small stand-ins answer for them.

CI runs gofmt, vet, the unit tests, shellcheck and the end-to-end test on every push.

## Try the real flow locally

Logins included, against a container that acts like a fresh server:

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
