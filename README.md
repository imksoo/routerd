# routerd

`routerd` is a declarative router resource reconciler written in Go.

The current implementation targets Ubuntu Server first and keeps the install
layout friendly to source installs and future packaging:

- YAML router config
- Kubernetes-like resource shapes
- interface alias resolution
- IPv4/IPv6 address planning
- DHCPv4, DHCPv6, RA, and DNS forwarding through managed dnsmasq
- IPv6 prefix delegation through systemd-networkd drop-ins
- runtime sysctl management
- nftables-based IPv4 source NAT and policy routing
- DS-Lite ipip6 tunnel setup, including multiple tunnel policy routing
- local trusted resource plugins
- dry-run reconcile
- machine-readable status JSON

Firewall policy, remote plugin installation, full rollback, and a long-running
daemon loop are still intentionally limited. The same reconcile logic is used
for one-shot CLI execution and future daemon mode.

The MVP scope is a working boundary, not a permanent constraint. When a small design improvement reduces future migration cost or improves router safety, the project should update the MVP direction deliberately instead of preserving a weak early assumption.

Japanese documentation is available in [README.ja.md](README.ja.md).

## Requirements

- Go 1.24 or newer
- `make`
- `iproute2`
- `jq`
- `dnsmasq`
- `nftables`
- `conntrack`

On Ubuntu:

```sh
sudo apt-get update
sudo apt-get install -y golang-go make iproute2 jq dnsmasq nftables conntrack
```

`conntrack` is used for diagnostics and policy-routing verification around
multi-tunnel DS-Lite.

## Build

```sh
make build
```

or:

```sh
go build ./cmd/routerd
```

The build artifact is written to `bin/routerd`.

Check local build dependencies:

```sh
make check-build-deps
```

Regenerate the YAML authoring schema from Go API structs:

```sh
make generate-schema
make check-schema
```

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

Check minimum remote host dependencies:

```sh
make check-remote-deps REMOTE_HOST=user@router.example
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

Useful direct commands:

```sh
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
routerd reconcile --config examples/router-lab.yaml --once --dry-run
```

`reconcile --once` can apply managed netplan, networkd drop-ins, dnsmasq,
nftables, sysctls, DS-Lite tunnels, and policy routing. Avoid using it on a
remote router until the adoption plan is understood, especially where
cloud-init or existing netplan configuration owns the management interface.

## Documentation

- [API v1alpha1](docs/api-v1alpha1.md)
- [Plugin protocol](docs/plugin-protocol.md)
- [API v1alpha1 Japanese](docs/api-v1alpha1.ja.md)
- [Plugin protocol Japanese](docs/plugin-protocol.ja.md)

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
