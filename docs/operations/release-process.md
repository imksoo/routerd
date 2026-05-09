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

Each target archive contains:

- `routerd`
- `routerctl`
- `routerd-dhcpv4-client`
- `routerd-dhcpv6-client`
- `routerd-dhcp-event-relay`
- `routerd-healthcheck`
- `routerd-dns-resolver`
- `routerd-firewall-logger`
- `routerd-pppoe-client`

The workflow uploads each `.tar.gz` archive and its `.sha256` file to the GitHub Release page.

## Manual dispatch

If a tag already exists, the workflow can also be started from GitHub Actions with the `workflow_dispatch` input:

```text
tag = 20260509.0
```

The workflow checks out that tag before building.

## Fallback

If GitHub Actions is unavailable, build the same archives locally:

```sh
make build-daemons ROUTERD_OS=linux GOARCH=amd64 VERSION=20260509.0
make build-daemons ROUTERD_OS=freebsd GOARCH=amd64 VERSION=20260509.0
```

Then create a release with the GitHub CLI:

```sh
gh release create 20260509.0 \
  routerd-20260509.0-linux-amd64.tar.gz \
  routerd-20260509.0-linux-amd64.tar.gz.sha256 \
  routerd-20260509.0-freebsd-amd64.tar.gz \
  routerd-20260509.0-freebsd-amd64.tar.gz.sha256 \
  --title "routerd 20260509.0" \
  --generate-notes \
  --verify-tag
```

