# routerd

`routerd` is a declarative router resource reconciler written in Go.

The first MVP targets Ubuntu Server and focuses on the smallest reliable core:
- YAML router config
- Kubernetes-like resource shapes
- interface alias resolution
- local trusted resource plugins
- dry-run reconcile
- machine-readable status JSON

The MVP does not implement firewall, NAT, DS-Lite, IPv6 PD, policy routing, remote plugin installation, or full rollback.

## Requirements

- Go 1.22 or newer
- `make`
- `iproute2`
- `jq`

On Ubuntu:

```sh
sudo apt-get update
sudo apt-get install -y golang-go make iproute2 jq
```

## Build

```sh
make build
```

or:

```sh
go build ./cmd/routerd
```

The build artifact is written to `bin/routerd`.

## Install

For local source installs:

```sh
sudo make install
```

The install target is intentionally simple so packaging can later wrap the same layout from ports, dpkg, or another package system. Override paths with `PREFIX`, `DESTDIR`, `SYSCONFDIR`, `PLUGINDIR`, `RUNDIR`, `STATEDIR`, or `SYSTEMDUNITDIR` as needed.

Example staged install:

```sh
make install DESTDIR=/tmp/routerd-root
```

Build a tarball containing the install tree:

```sh
make dist
```

Install to a remote test host without requiring Go or make on that host:

```sh
make remote-install REMOTE_HOST=user@router.example
```

Install a config file to a remote test host:

```sh
make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml
```

Install the systemd unit explicitly on Linux systems that use systemd:

```sh
sudo make install-systemd
```

## Test

```sh
make test
```

or:

```sh
go test ./...
```

## Example Commands

```sh
make validate-example
make dry-run-example
```

The current CLI is an initial scaffold. Resource loading, plugin execution, and reconcile behavior will be implemented incrementally.

## Default Paths

- Config: `/usr/local/etc/routerd/router.yaml`
- Plugin dir: `/usr/local/libexec/routerd/plugins`
- Binary: `/usr/local/sbin/routerd`

Linux runtime defaults:

- Runtime dir: `/run/routerd`
- State dir: `/var/lib/routerd`
- Status file: `/run/routerd/status.json`
- Lock file: `/run/routerd/routerd.lock`

FreeBSD runtime defaults:

- Runtime dir: `/var/run/routerd`
- State dir: `/var/db/routerd`
- Status file: `/var/run/routerd/status.json`
- Lock file: `/var/run/routerd/routerd.lock`
