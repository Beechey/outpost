# Contributing to outpost

## Layout

```
cmd/outpost/          CLI entry point (cobra + fang): setup, move, pair
internal/setup/       The setup and move flows, written as step lists
  setup.go            Setup, and the steps it shares with move: connect, install, final check
  move.go             Move: survey, confirm, transfer, retire
  manifest.go         What a move carries, and every shell command built from that
  dev.go              The dev user on a server: PATH, quoting, T3 service operations
  logins.go           Sign-ins and how devices reach T3 (T3 Connect or Tailscale), and pair
  scripts/
    bootstrap.sh      Runs as root: packages, user, SSH, firewall, Docker, Tailscale, T3, Claude
internal/remote/      Host, the seam the flows depend on, and Server, its ssh adapter
internal/servers/     Remembered servers and the picker
internal/ui/          Banner, step headers and live progress
test/e2e/             End-to-end test with systemd containers posing as servers
docs/                 User guide, how it works, logo and demo
```

## Design notes

- **One seam.** The flows depend only on `remote.Host`. `remote.Server` is the ssh adapter; tests use a recording fake. New behavior in a flow should come with a fake-host test.
- **Shell lives in two places.** `bootstrap.sh` for installing, and the `dev` and manifest modules for everything else. Callers express intent (`stopT3`, `retireT3`), not shell.
- **Anything that reaches a remote shell is validated or ours:** server addresses go through `Server.Validate`, moved paths come from the manifest, and values are quoted with `quote`.
- **Re-runs must be safe.** Every step checks before it acts, and a server already in use is never rebooted.

## Tests

```bash
go test ./...                                          # unit tests, in under a second
shellcheck internal/setup/scripts/*.sh test/e2e/run.sh
test/e2e/run.sh                                        # about 10 minutes; needs Docker, Go and ~/.ssh/id_ed25519
```

- **Unit tests** run the flows against the fake host: fresh and re-run setups, Tailscale and T3 Connect access, retired servers, failed copies and their undo. The manifest's commands run against temporary folders with real `tar` (on Linux; skipped on macOS, whose `tar` differs).
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
