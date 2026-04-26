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
- PPPoE interface setup through pppd/rp-pppoe
- runtime sysctl management
- internal event logging to syslog/journald or trusted local log plugins
- nftables-based minimal firewall policy, IPv4 source NAT, and policy routing
- path MTU propagation through IPv6 RA and TCP MSS clamping
- DS-Lite ipip6 tunnel setup, including multiple tunnel policy routing
- local trusted resource plugins
- local HTTP+JSON daemon control API over a Unix domain socket
- `routerctl` client CLI
- dry-run reconcile
- machine-readable status JSON

Remote plugin installation and full rollback are still intentionally limited.
Firewall policy starts with a small default-deny home-router model rather than a
general rule language. The same reconcile logic is used for one-shot CLI
execution and daemon-triggered reconcile.

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
- `pppd` when using PPPoE

On Ubuntu:

```sh
sudo apt-get update
sudo apt-get install -y golang-go make iproute2 jq dnsmasq nftables conntrack ppp
```

`conntrack` is used for diagnostics and policy-routing verification around
multi-tunnel DS-Lite.
PPPoE resources require pppd and the rp-pppoe plugin provided by the distro's
PPP packages.

## Build

```sh
make build
```

or:

```sh
go build ./cmd/routerd
go build ./cmd/routerctl
```

Build artifacts are written to `bin/routerd` and `bin/routerctl`.

Check local build dependencies:

```sh
make check-build-deps
```

Regenerate the YAML authoring schema from Go API structs:
This also generates control API JSON Schema and OpenAPI definitions.

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
make website-build
```

Useful direct commands:

```sh
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
routerd reconcile --config examples/router-lab.yaml --once --dry-run
routerd serve --config examples/router-lab.yaml --socket /run/routerd/routerd.sock
routerctl status
routerctl show napt --limit 20
routerctl plan
```

`reconcile --once` can apply managed netplan, networkd drop-ins, dnsmasq,
nftables, sysctls, DS-Lite tunnels, and policy routing. Avoid using it on a
remote router until the adoption plan is understood, especially where
cloud-init or existing netplan configuration owns the management interface.

## Documentation

- [API v1alpha1](docs/api-v1alpha1.md)
- [Control API v1alpha1](docs/control-api-v1alpha1.md)
- [Plugin protocol](docs/plugin-protocol.md)
- [Getting started](docs/tutorials/getting-started.md)
- [Changelog](docs/releases/changelog.md)
- [API v1alpha1 Japanese](docs/api-v1alpha1.ja.md)
- [Control API v1alpha1 Japanese](docs/control-api-v1alpha1.ja.md)
- [Plugin protocol Japanese](docs/plugin-protocol.ja.md)

The public website lives in `website/` and is built with Docusaurus. It
publishes English and Japanese docs to Cloudflare Pages. Use `website` as the
Pages root directory, `npm ci && npm run build` as the build command, and
`build` as the output directory. Add `routerd.net` as a Cloudflare Pages custom
domain.

## Default Paths

- Config: `/usr/local/etc/routerd/router.yaml`
- Plugin dir: `/usr/local/libexec/routerd/plugins`
- Binary: `/usr/local/sbin/routerd`
- Client binary: `/usr/local/sbin/routerctl`

Linux runtime defaults:

- Runtime dir: `/run/routerd`
- State dir: `/var/lib/routerd`
- Status file: `/run/routerd/status.json`
- Control socket: `/run/routerd/routerd.sock`
- Lock file: `/run/routerd/routerd.lock`

FreeBSD runtime defaults:

- Runtime dir: `/var/run/routerd`
- State dir: `/var/db/routerd`
- Status file: `/var/run/routerd/status.json`
- Control socket: `/var/run/routerd/routerd.sock`
- Lock file: `/var/run/routerd/routerd.lock`
