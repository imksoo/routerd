---
title: Install
sidebar_position: 1
---

# Install

Install routerd from a release archive.
The router host does not need Go or a Makefile.

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/20260509.12/routerd-20260509.12-linux-amd64.tar.gz
tar -xzf routerd-20260509.12-linux-amd64.tar.gz
sudo ./install.sh
```

On Linux arm64 hosts, use `routerd-20260509.12-linux-arm64.tar.gz`.

For FreeBSD, download `routerd-20260509.12-freebsd-amd64.tar.gz` and run the
same `./install.sh`.
On FreeBSD arm64 hosts, use `routerd-20260509.12-freebsd-arm64.tar.gz`.

The installer:

- installs runtime packages on supported package managers
- copies binaries to `/usr/local/sbin`
- installs the systemd or rc.d service template
- writes `/usr/local/etc/routerd/router.yaml.sample`
- preserves an existing `/usr/local/etc/routerd/router.yaml`
- preserves state under `/var/lib/routerd` or `/var/db/routerd`
- runs `routerctl status` when the control socket exists

Common options:

```sh
./install.sh --list-deps
sudo ./install.sh --no-install-deps
sudo ./install.sh --deps-only
sudo ./install.sh --with-tailscale
sudo ./install.sh --dry-run
```

After installation, create a configuration and validate it:

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml

routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

Apply only after confirming that management access stays reachable:

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

See [Install and upgrade](../install-and-upgrade.md) for OS-specific package
lists, upgrade behavior, uninstall options, and developer release commands.
