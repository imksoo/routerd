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
Pass `--enable-service` or `--start-service` when you want the installer to call the host service manager.

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
