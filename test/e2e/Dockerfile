# A stand-in for a freshly installed Ubuntu 24.04 VPS: systemd as PID 1, sshd, root key login.
FROM ubuntu:24.04
RUN apt-get update \
 && apt-get install -y --no-install-recommends systemd systemd-sysv dbus openssh-server sudo curl ca-certificates sqlite3 dnsutils python3 \
 && rm -rf /var/lib/apt/lists/* \
 && systemctl enable ssh
ARG PUBKEY
RUN install -d -m 700 /root/.ssh && echo "$PUBKEY" > /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
