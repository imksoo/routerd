---
title: Release process
---

# Release process

routerd uses date-based release versions.
The executable version is `yyyymmdd`, and GitHub release tags add a per-day build suffix such as `20260509.0`.

## Automated release

Push a release tag to start the GitHub Actions workflow:

```sh
git tag 20260509.0
git push origin 20260509.0
```

The `Release` workflow builds these targets:

- `linux-amd64`
- `freebsd-amd64`

Each target archive is named `routerd-<tag>-<os>-<arch>.tar.gz`.
The archive contains:

- `bin/`: `routerd`, `routerctl`, and the managed daemon binaries
- `install.sh`: POSIX sh installer
- `uninstall.sh`: POSIX sh uninstaller
- `etc/routerd/router.yaml.sample`: sanitized sample configuration
- `systemd/` or `rc.d/`: service templates for the target OS
- `share/doc/`: README, VERSION, and LICENSE notice

The workflow uploads each `.tar.gz` archive and its `.sha256` file to the GitHub Release page.

Install a release archive on the router host:

```sh
tar -xzf routerd-20260509.0-linux-amd64.tar.gz
sudo ./install.sh
```

`install.sh` copies binaries to `/usr/local/sbin`, installs service templates, and writes `router.yaml.sample`.
It does not overwrite an existing `/usr/local/etc/routerd/router.yaml`.
When an existing `/usr/local/sbin/routerd` is found, the installer switches to upgrade mode automatically.
It prints the old and new `routerd --version` output, replaces binaries and service templates, preserves configuration and state, and restarts `routerd.service` or the FreeBSD `routerd` rc.d service if it was already running.
Replaced files are copied to `*.backup.YYYYMMDDHHMMSS` before replacement.
Pass `--no-restart` to replace files without restarting the service.
Pass `--dry-run` to print planned file and service-manager changes.
Pass `--verbose` for shell tracing.
Pass `--no-config-update` to leave `router.yaml.sample` unchanged.
Pass `--enable-service` or `--start-service` when you want a fresh install to call the host service manager.
After installation, the script runs `routerctl status` when the routerd control socket exists.

The installer never modifies these runtime or state locations:

- `/usr/local/etc/routerd/router.yaml`
- `/var/lib/routerd`
- `/var/db/routerd`
- `/run/routerd`
- `/var/run/routerd`
- `/var/log/otelcol`

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
  dist/freebsd-amd64/routerd-20260509.0-freebsd-amd64.tar.gz \
  dist/freebsd-amd64/routerd-20260509.0-freebsd-amd64.tar.gz.sha256 \
  --title "routerd 20260509.0" \
  --generate-notes \
  --verify-tag
```
