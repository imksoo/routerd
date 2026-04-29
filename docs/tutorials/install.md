---
title: Install
sidebar_position: 1
---

# Install

This page covers building, installing, and enabling the routerd daemon on
a Linux host. After it you have a routerd binary, the standard install
layout, and a systemd unit ready to run. The next tutorial,
[First router](./first-router), walks through the smallest useful
configuration.

routerd is still v1alpha1 software. Do this on a lab VM or a host you can
reach through a console before pointing it at a remote router.

## 1. Build

From the source tree:

```bash
make build
```

This produces two binaries:

- `bin/routerd` — the apply engine and serve daemon.
- `bin/routerctl` — the read-only inspection CLI.

The build is `CGO_ENABLED=0` by default, so the binaries are statically
linked Go.

## 2. Install the source layout

routerd uses a `/usr/local` layout that is friendly to source installs and
to future packaging:

```bash
sudo make install
```

Default paths:

| Path | Purpose |
|---|---|
| `/usr/local/sbin/routerd` | apply / serve binary |
| `/usr/local/sbin/routerctl` | inspection CLI |
| `/usr/local/etc/routerd/router.yaml` | router config (you create this) |
| `/usr/local/libexec/routerd/plugins` | local plugin directory |
| `/run/routerd` | runtime socket and pid |
| `/var/lib/routerd` | state database |

## 3. Stage a config

Pick one of the examples and put it where the daemon expects it:

```bash
sudo install -m 0644 examples/basic-dhcp.yaml /usr/local/etc/routerd/router.yaml
```

[First router](./first-router) walks through what should go in this file
for the smallest working router.

## 4. Run a dry-run apply

Before enabling the daemon, do an apply with `--dry-run` so you can see
the plan:

```bash
sudo /usr/local/sbin/routerd apply \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

The output is structured JSON: which resources are healthy, which have
drifted, and what routerd would change. Read this carefully on a host
that already has its own networking — routerd may want to take ownership
of files that another tool (cloud-init, netplan) is also writing.

When the plan matches your intent, drop `--dry-run`:

```bash
sudo /usr/local/sbin/routerd apply \
  --config /usr/local/etc/routerd/router.yaml \
  --once
```

## 5. Enable the daemon

Once `apply --once` looks right, install the systemd unit and start the
daemon:

```bash
sudo make install-systemd
sudo systemctl daemon-reload
sudo systemctl enable --now routerd.service
```

The daemon keeps a control socket under `/run/routerd/` and re-applies
the YAML on a periodic schedule. `routerctl` uses the same socket to
read state.

```bash
routerctl get
routerctl describe interface/wan
```

## What changed on the host

`apply` may have written or modified:

- `/etc/systemd/network/*.network` drop-ins under
  `10-netplan-*.network.d/`.
- `/etc/dnsmasq.d/*.conf` for managed dnsmasq services.
- `/etc/nftables.d/*.conf` for NAT and firewall.
- The state database at `/var/lib/routerd/routerd.db`.

routerd remembers which of these it owns in its
[ownership ledger](../reference/resource-ownership). Files routerd did not
install are left alone.

## Next

- [First router](./first-router) — minimal WAN + LAN config.
- [Apply and render](../concepts/apply-and-render) — the verbs you just
  used, in detail.
- [State and ownership](../concepts/state-and-ownership) — what
  `/var/lib/routerd` contains.
