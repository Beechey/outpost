#!/usr/bin/env bash
# Installs the gateway for a server's own domain: Caddy and CoreDNS as system
# services run as the dev user, and a certificate authority that can only sign
# names under the domain. Run as root by `outpost gateway`, after outpost has
# written ~/.outpost/gateway. Safe to re-run. outpost sets GATEWAY_DOMAIN above.
set -euo pipefail
task() { echo "::task $*"; }
fetch() { curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors "$@"; }
: "${GATEWAY_DOMAIN:?outpost sets the domain}"

DEV_USER=dev # must match DevUser in internal/setup/dev.go
HOME_DIR="$(getent passwd "$DEV_USER" | cut -d: -f6)"
DIR="$HOME_DIR/.outpost/gateway"
BIN=/usr/local/lib/outpost/bin
CADDY_VERSION=2.11.7
COREDNS_VERSION=1.15.0
case "$(uname -m)" in
  x86_64)
    ARCH=amd64
    CADDY_SHA512=a7a433a1b133efc3c8d10eb0b99d52a24b5ef5c322dc77f5282182b1c0402139ab83f3a99f0c52409df77d20123fb0b523edad8a66d8f5e49136197bf61ef0e7
    COREDNS_SHA256=2495c5b8f3fab6d0a336c397f1227b75406d86e6ec7125744a29df9e936ab8cd ;;
  aarch64 | arm64)
    ARCH=arm64
    CADDY_SHA512=3db36ba90c7a6e8dda40ee3dd71fa08844c76b5fb08f61b31e5e78d2ed38e71c51dc7baed875e50d1ca1279196e84302967237386ae87c91ae9f2aaceada682e
    COREDNS_SHA256=73a5bf107c9791d57741ccd59ad138ab00bcd5bb3f62e2e28c999f5c047334a4 ;;
  *) echo "outpost's gateway supports x86_64 and arm64 servers, not $(uname -m)"; exit 1 ;;
esac
[ -f "$DIR/gateway.env" ] || { echo "outpost writes $DIR before running this"; exit 1; }

# install_release URL SUMTOOL SUM FILE: download a release, check it, and put FILE from it in $BIN.
install_release() {
  local tmp
  tmp="$(mktemp -d)"
  fetch -o "$tmp/release.tgz" "$1"
  echo "$3  $tmp/release.tgz" | "$2" -c --quiet
  tar -xzf "$tmp/release.tgz" -C "$tmp" "$4"
  install -D -m 755 "$tmp/$4" "$BIN/$4"
  rm -rf "$tmp"
}

task "Caddy $CADDY_VERSION"
if ! "$BIN/caddy" version 2>/dev/null | grep -q "^v$CADDY_VERSION "; then
  install_release "https://github.com/caddyserver/caddy/releases/download/v$CADDY_VERSION/caddy_${CADDY_VERSION}_linux_$ARCH.tar.gz" \
    sha512sum "$CADDY_SHA512" caddy
fi

task "CoreDNS $COREDNS_VERSION"
if ! "$BIN/coredns" -version 2>/dev/null | grep -q "CoreDNS-$COREDNS_VERSION"; then
  install_release "https://github.com/coredns/coredns/releases/download/v$COREDNS_VERSION/coredns_${COREDNS_VERSION}_linux_$ARCH.tgz" \
    sha256sum "$COREDNS_SHA256" coredns
fi

task "Certificate authority for $GATEWAY_DOMAIN"
# One root per domain, kept when the domain changes, so switching back needs no new trust.
PKI="$DIR/pki/$GATEWAY_DOMAIN"
if [ ! -s "$PKI/root.crt" ]; then
  sudo -u "$DEV_USER" mkdir -p "$PKI"
  chmod 700 "$DIR/pki" "$PKI"
  sudo -u "$DEV_USER" bash -c 'umask 077 && openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -sha256 -days 3650 \
    -keyout "$1/root.key" -out "$1/root.crt" -subj "/CN=outpost $2" \
    -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -addext "nameConstraints=critical,permitted;DNS:$2"' _ "$PKI" "$GATEWAY_DOMAIN" >/dev/null 2>&1
  chmod 644 "$PKI/root.crt"
fi
# Caddy keeps an intermediate signed by the root; a different root needs a fresh one.
if [ "$(cat "$DIR/data/root-domain" 2>/dev/null)" != "$GATEWAY_DOMAIN" ]; then
  rm -rf "$DIR/data/pki" "$DIR/data/certificates"
  sudo -u "$DEV_USER" mkdir -p "$DIR/data"
  echo "$GATEWAY_DOMAIN" | sudo -u "$DEV_USER" tee "$DIR/data/root-domain" >/dev/null
fi

task "Gateway services"
unit() { # unit NAME DESCRIPTION EXEC [EXTRA...]
  local name="$1" desc="$2" exec="$3"
  shift 3
  {
    printf '[Unit]\nDescription=%s\nAfter=network-online.target tailscaled.service\nWants=network-online.target\n\n' "$desc"
    printf '[Service]\nUser=%s\nGroup=%s\nWorkingDirectory=%s\nEnvironmentFile=%s/gateway.env\nExecStart=%s\n' \
      "$DEV_USER" "$DEV_USER" "$DIR" "$DIR" "$exec"
    printf '%s\n' "$@"
    # The tailnet address may not be up yet at boot; keep trying until it is.
    printf 'Restart=always\nRestartSec=3\n\n[Install]\nWantedBy=multi-user.target\n'
  } > "/etc/systemd/system/$name.service"
}
unit outpost-caddy "outpost gateway: HTTPS for your projects (Caddy)" \
  "$BIN/caddy run --config $DIR/Caddyfile --adapter caddyfile" \
  "ExecReload=$BIN/caddy reload --config $DIR/Caddyfile --adapter caddyfile --address localhost:44319"
unit outpost-dns "outpost gateway: names for your projects (CoreDNS)" \
  "$BIN/coredns -conf $DIR/Corefile" \
  "AmbientCapabilities=CAP_NET_BIND_SERVICE" "CapabilityBoundingSet=CAP_NET_BIND_SERVICE"
systemctl daemon-reload
systemctl enable outpost-caddy outpost-dns >/dev/null 2>&1
systemctl restart outpost-caddy outpost-dns
