---
title: Install
sidebar_position: 1
---

# Install

![Diagram showing routerd installation from release archive, dependency and service template installation, preserved config and state, and validate-plan-dry-run after install](/img/diagrams/tutorial-install.png)

Install routerd from a release archive.
The router host does not need Go or a Makefile.

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

On Linux arm64 hosts, use `routerd-linux-arm64.tar.gz`.

For FreeBSD, download `routerd-freebsd-amd64.tar.gz` and run the
same `./install.sh`.
On FreeBSD arm64 hosts, use `routerd-freebsd-arm64.tar.gz`.
Use the versioned archives on a release page when you need an exact release.

Linux archives contain statically linked routerd binaries (`CGO_ENABLED=0`).
They are not tied to the glibc version on the router host.

The installer:

- installs runtime packages on supported package managers
- copies binaries to `/usr/local/sbin`
- installs the systemd or rc.d service template
- writes `/usr/local/etc/routerd/router.yaml.sample`
- preserves an existing `/usr/local/etc/routerd/router.yaml`
- preserves state under `/var/lib/routerd` or `/var/db/routerd`
- runs `routerctl status` when the read-only status socket exists

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

routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
```

Apply only after confirming that management access stays reachable:

```sh
sudo routerctl apply -f /usr/local/etc/routerd/router.yaml --replace
```

See [Install and upgrade](../install-and-upgrade.md) for OS-specific package
lists, upgrade behavior, uninstall options, and developer release commands.

To try routerd without installing to disk, boot `routerd-live.iso`.
The ISO starts the same `install.sh configure` wizard after root login.
It also supports Proxmox VE serial consoles through `qm terminal`.
When the wizard asks about USB persistence, choose a USB partition to turn the
live ISO into a diskless persistent router. Without USB persistence, the ISO
runs as an ephemeral demo and loses config at reboot.
