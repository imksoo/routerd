---
title: Install and upgrade
---

# Install and upgrade

Use the release archive when you install routerd on a router host.
The archive contains the binaries, service template, sample configuration, and
the installer scripts.
You do not need a Go toolchain or the Makefile on the router host.

## Quick install

Download the archive for your OS and architecture from the
[GitHub Releases page](https://github.com/imksoo/routerd/releases).

Linux amd64:

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/20260509.7/routerd-20260509.7-linux-amd64.tar.gz
tar -xzf routerd-20260509.7-linux-amd64.tar.gz
sudo ./install.sh
```

FreeBSD amd64:

```sh
fetch https://github.com/imksoo/routerd/releases/download/20260509.7/routerd-20260509.7-freebsd-amd64.tar.gz
tar -xzf routerd-20260509.7-freebsd-amd64.tar.gz
sudo ./install.sh
```

`install.sh` detects whether this is a fresh install or an upgrade.
It installs the binaries under `/usr/local/sbin`, installs the service template,
and writes `/usr/local/etc/routerd/router.yaml.sample`.
It never overwrites an existing `/usr/local/etc/routerd/router.yaml`.

## Runtime dependencies

By default, `install.sh` installs known OS packages before copying routerd.
Use `--list-deps` to inspect the package list:

```sh
./install.sh --list-deps
```

Use `--no-install-deps` when dependencies are already managed by another tool:

```sh
sudo ./install.sh --no-install-deps
```

Use `--deps-only` when you only want to install dependencies:

```sh
sudo ./install.sh --deps-only
```

Tailscale is optional.
Add it to the package list with `--with-tailscale`:

```sh
sudo ./install.sh --with-tailscale
```

### Debian and Ubuntu

The installer uses `apt-get` and installs:

```text
dnsmasq nftables wireguard-tools chrony bind9-dnsutils tcpdump cron jq ppp pppoeconf conntrack iproute2 iputils-ping iputils-tracepath net-tools kmod
```

### Fedora-like systems

The installer uses `dnf` and installs:

```text
dnsmasq nftables wireguard-tools chrony bind-utils tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute iputils traceroute kmod
```

### Arch-like systems

The installer uses `pacman` and installs:

```text
dnsmasq nftables wireguard-tools chrony bind tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute2 iputils traceroute kmod
```

### FreeBSD

The installer uses `pkg` and installs:

```text
dnsmasq wireguard-tools mpd5 bind-tools tcpdump jq
```

FreeBSD `pf`, `ifconfig`, `sysctl`, `service`, `sysrc`, `cron`, `netstat`,
`sockstat`, `ping`, and `traceroute` are base-system tools.
The installer checks for the commands but does not install them as packages.

### NixOS

NixOS should keep package state in the NixOS configuration.
When `install.sh` detects NixOS-style tooling, it prints a warning instead of
calling `nix-env`.
Declare packages through the NixOS configuration or routerd `Package` resources.

## Upgrade

Extract the new archive and run the same installer:

```sh
tar -xzf routerd-20260509.7-linux-amd64.tar.gz
sudo ./install.sh
```

When `/usr/local/sbin/routerd` already exists, the installer switches to upgrade
mode.
It prints the old and new `routerd --version` output.
It replaces binaries and service templates, keeps configuration and state, and
restarts the routerd service if it was already active.

Every replaced file is copied to `*.backup.YYYYMMDDHHMMSS` before replacement.
If the install fails partway through, the script restores files from the
temporary rollback backup.

Useful options:

```sh
sudo ./install.sh --no-restart
sudo ./install.sh --dry-run
sudo ./install.sh --verbose
sudo ./install.sh --no-config-update
```

## Layout

The release installer uses these paths:

| Item | Linux | FreeBSD |
| --- | --- | --- |
| Configuration | `/usr/local/etc/routerd/router.yaml` | `/usr/local/etc/routerd/router.yaml` |
| Sample configuration | `/usr/local/etc/routerd/router.yaml.sample` | `/usr/local/etc/routerd/router.yaml.sample` |
| Binaries | `/usr/local/sbin` | `/usr/local/sbin` |
| Service template | `/etc/systemd/system/routerd.service` | `/usr/local/etc/rc.d/routerd` |
| Runtime sockets | `/run/routerd` | `/var/run/routerd` |
| Persistent state | `/var/lib/routerd` | `/var/db/routerd` |

The installer never removes these state locations:

- `/usr/local/etc/routerd/router.yaml`
- `/var/lib/routerd`
- `/var/db/routerd`
- `/run/routerd`
- `/var/run/routerd`
- `/var/log/otelcol`

## First configuration

Copy a sample configuration into place and edit it for your interfaces:

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml
```

Then validate and review the plan:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

Apply only after the management path is safe:

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

Start the service when the one-shot apply is healthy:

```sh
sudo systemctl enable --now routerd.service
```

On FreeBSD:

```sh
sudo sysrc routerd_enable=YES
sudo service routerd start
```

## Uninstall

The release archive also contains `uninstall.sh`.
The default uninstall removes binaries, service templates, and runtime files.
It keeps configuration and state.

```sh
sudo ./uninstall.sh --yes
```

Purge options are explicit:

```sh
sudo ./uninstall.sh --yes --purge-config
sudo ./uninstall.sh --yes --purge-state
sudo ./uninstall.sh --yes --all
```

Use `--dry-run` to preview removal.

## Developer workflow

The Makefile is for development only.
Use it to test, build, generate schemas, validate examples, build the website,
and create release archives:

```sh
make test
make check-schema
make validate-example
make website-build
make dist ROUTERD_OS=linux GOARCH=amd64 VERSION=20260509.7
```

Do not use the Makefile as the user-facing install path.
The release archive and `install.sh` are the supported deployment path.
