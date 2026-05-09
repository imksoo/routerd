---
title: Release process
---

# Release process

routerd uses date-based release versions.
The executable version, release tag, and release archive name use `yyyymmdd.N`
format, such as `20260509.0`.

## Automated release

Push a release tag to start the GitHub Actions workflow:

```sh
git tag 20260509.0
git push origin 20260509.0
```

The `Release` workflow builds these targets:

- `linux-amd64`
- `linux-arm64`
- `freebsd-amd64`
- `freebsd-arm64`

Each target archive is published with two names:

- `routerd-<tag>-<os>-<arch>.tar.gz` for an exact release
- `routerd-<os>-<arch>.tar.gz` for a fixed latest-download URL

Both names also have `.sha256` files.
The archive contains:

- `bin/`: `routerd`, `routerctl`, and the managed daemon binaries
- `install.sh`: POSIX sh installer
- `uninstall.sh`: POSIX sh uninstaller
- `etc/routerd/router.yaml.sample`: sanitized sample configuration
- `systemd/` or `rc.d/`: service templates for the target OS
- `share/doc/`: README, VERSION, and LICENSE notice

The workflow uploads the versioned archive, the fixed-name archive, and their
`.sha256` files to the GitHub Release page.
Documentation should use the fixed latest-download URL for quick starts:

```text
https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
```

Use versioned URLs only when a runbook must pin a specific release.

Normal branch pushes and pull requests use the separate `CI` workflow.
That workflow runs development checks only and does not publish release assets.
See [Development checks](/docs/operations/development) for the pre-commit hook and CI scope.

## Responsibility split

Installation logic lives in `install.sh`.
The Makefile is only for development tasks such as building, testing, schema checks, example validation, website builds, and release archive generation.
The release archive does not include the Makefile.
This keeps end-user installation and upgrade behavior in one script.

Development tests use Makefile targets:

```sh
make test
make check-schema
make validate-example
make dist ROUTERD_OS=linux GOARCH=amd64 VERSION=20260509.0
```

Deployment smoke checks use `install.sh`.
After installation, `install.sh` calls `routerctl status` when the routerd control socket exists.
The GitHub release workflow also extracts each archive and runs `install.sh` with a temporary non-system prefix.
That smoke test verifies that the archive can install and uninstall without using a Makefile.
The CI smoke test passes `--no-install-deps` because dependency installation belongs to the target router host.

Install a release archive on the router host:

```sh
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

`install.sh` copies binaries to `/usr/local/sbin`, installs service templates, and writes `router.yaml.sample`.
At the start of the run, it detects the OS package manager and installs known runtime packages unless `--no-install-deps` is passed.
It does not overwrite an existing `/usr/local/etc/routerd/router.yaml`.
When an existing `/usr/local/sbin/routerd` is found, the installer switches to upgrade mode automatically.
It prints the old and new `routerd --version` output, replaces binaries and service templates, preserves configuration and state, and restarts `routerd.service` or the FreeBSD `routerd` rc.d service if it was already running.
Replaced files are copied to `*.backup.YYYYMMDDHHMMSS` before replacement.
Pass `--no-restart` to replace files without restarting the service.
Pass `--dry-run` to print planned file and service-manager changes.
Pass `--verbose` for shell tracing.
Pass `--no-config-update` to leave `router.yaml.sample` unchanged.
Pass `--no-install-deps` to skip OS package installation.
Pass `--list-deps` to print the package and command list without changing the host.
Pass `--deps-only` to install packages and then exit before copying routerd files.
Pass `--with-tailscale` to include the optional Tailscale package and command check.
Pass `--enable-service` or `--start-service` when you want a fresh install to call the host service manager.
After installation, the script runs `routerctl status` when the routerd control socket exists.

The installer never modifies these runtime or state locations:

- `/usr/local/etc/routerd/router.yaml`
- `/var/lib/routerd`
- `/var/db/routerd`
- `/run/routerd`
- `/var/run/routerd`
- `/var/log/otelcol`

## Runtime dependencies

`install.sh` keeps dependency installation in the same end-user path as binary installation.
This avoids a separate Makefile install path.

On Debian and Ubuntu, the installer uses `apt-get` and installs:

```text
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind9-dnsutils tcpdump cron jq ppp pppoe conntrack iproute2 iputils-ping iputils-tracepath net-tools kmod radvd strongswan-swanctl iptables
```

On Fedora-like systems, the installer uses `dnf` and installs:

```text
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind-utils tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute iputils traceroute kmod radvd strongswan iptables
```

On Arch-like systems, the installer uses `pacman` and installs:

```text
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute2 iputils traceroute kmod radvd strongswan iptables
```

On FreeBSD, the installer uses `pkg` and installs:

```text
ca_root_nss curl dnsmasq wireguard-tools mpd5 bind-tools tcpdump jq chrony strongswan
```

FreeBSD `pf`, `ifconfig`, `route`, `service`, `sysrc`, and `cron` are base-system tools and are checked as commands rather than installed as packages.

On NixOS, the installer prints a warning instead of calling `nix-env`.
Declare packages in the NixOS configuration or in routerd `Package` resources.

After dependency installation, the script checks that the expected commands exist.
Missing commands are warnings, not fatal errors, because package names vary between distributions.
Use this command to inspect the dependency set:

```sh
./install.sh --list-deps
```

## Uninstall

Use `uninstall.sh` when you want to remove installed files separately from state:

```sh
sudo ./uninstall.sh --yes
```

The default uninstall stops and disables the service, removes routerd binaries, removes the service template, and removes runtime files.
It keeps `/usr/local/etc/routerd`, `/var/lib/routerd`, `/var/db/routerd`, and `/var/log/otelcol`.

Purge options are explicit:

```sh
sudo ./uninstall.sh --yes --purge-config
sudo ./uninstall.sh --yes --purge-state
sudo ./uninstall.sh --yes --all
```

Use `--dry-run` to preview removal without changing the host.

## Manual dispatch

If a tag already exists, the workflow can also be started from GitHub Actions with the `workflow_dispatch` input:

```text
tag = 20260509.0
```

The workflow checks out that tag before building.

## Fallback

If GitHub Actions is unavailable, build the same archives locally:

```sh
make dist ROUTERD_OS=linux GOARCH=amd64 VERSION=20260509.0
make dist ROUTERD_OS=freebsd GOARCH=amd64 VERSION=20260509.0
```

Then create a release with the GitHub CLI:

```sh
gh release create 20260509.0 \
  dist/linux-amd64/routerd-20260509.0-linux-amd64.tar.gz \
  dist/linux-amd64/routerd-20260509.0-linux-amd64.tar.gz.sha256 \
  dist/linux-amd64/routerd-linux-amd64.tar.gz \
  dist/linux-amd64/routerd-linux-amd64.tar.gz.sha256 \
  dist/freebsd-amd64/routerd-20260509.0-freebsd-amd64.tar.gz \
  dist/freebsd-amd64/routerd-20260509.0-freebsd-amd64.tar.gz.sha256 \
  dist/freebsd-amd64/routerd-freebsd-amd64.tar.gz \
  dist/freebsd-amd64/routerd-freebsd-amd64.tar.gz.sha256 \
  --title "routerd 20260509.0" \
  --generate-notes \
  --verify-tag
```
