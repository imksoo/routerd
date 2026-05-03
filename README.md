# routerd

[Project site and documentation: routerd.net](https://routerd.net/)

routerd is a small software router for Linux. You describe how the router
should behave in a YAML file, and routerd brings up the matching interfaces,
addresses, DHCP and DNS service, NAT, policy routing, firewall, and route
health checks. Editing the YAML and running one apply is enough to change
all of those at once. The goal is to keep router configuration as something
reviewable in Git instead of a pile of one-off shell commands.

The current implementation targets Ubuntu Server first and installs under
`/usr/local`, with a layout that stays friendly to source installs and future
packaging. NixOS and FreeBSD are second-tier targets: the build, install
layout, and service-manager integration scaffolds are in place, but several
host-integration renderers (pf for FreeBSD, NixOS-native interface
configuration) are still being ported. See [docs/platforms.md](docs/platforms.md)
for the current matrix. routerd is still pre-release: read the plan and
dry-run output before applying it to a remote router.

## What routerd makes the router do

- Declare interfaces and ownership scope so routerd does not fight with
  cloud-init or netplan over the management link.
- Acquire WAN-side addressing and routing through DHCPv4, DHCPv6, IPv6 prefix
  delegation, PPPoE, or DS-Lite.
- Hand out LAN-side IPv4 and IPv6 addresses, including addresses derived from
  a delegated IPv6 prefix.
- Serve LAN clients through a managed dnsmasq: DHCPv4, DHCPv6, RA, DNS cache,
  and conditional forwarding.
- Source-NAT IPv4 traffic, hash-balance flows across multiple egress paths, and
  pin established flows to the same path through conntrack marks.
- Run health checks against multiple uplinks and switch the IPv4 default route
  to the healthy candidate with the lowest priority.
- Compute the effective path MTU toward each upstream, advertise it over IPv6
  RA, and clamp forwarded TCP MSS for IPv4 and IPv6.
- Apply a small default-deny home-router firewall preset and publish only the
  services declared in the config.
- Manage sysctl values, hostname, systemd-timesyncd, and the routerd event
  sink from the same YAML.
- Plan and dry-run applies, expose status as JSON, and accept control
  requests over a Unix domain socket.
- Extend resource-specific behavior through trusted local plugins.

Remote plugin installation and full rollback of OS-level changes are
intentionally out of scope for now. The firewall is a focused home-router
preset rather than a general rule language. These boundaries are working
defaults, not permanent constraints — when a small design change reduces
future migration cost or makes the router safer, the project updates the
direction deliberately.

Japanese documentation is available in [README.ja.md](README.ja.md).

## Requirements

- Go 1.24 or newer
- make
- iproute2
- jq
- dnsmasq
- nftables
- conntrack
- dig, ping, tcpdump, and tracepath for IPv4/IPv6 diagnostics
- pppd when using PPPoE
- mstpd when using Linux `Bridge` resources with RSTP
- sqlite3 is optional for human inspection of the local state database

On Ubuntu:

```sh
sudo apt-get update
sudo apt-get install -y golang-go make iproute2 jq dnsmasq nftables conntrack ppp mstpd dnsutils iputils-ping iputils-tracepath tcpdump
```

conntrack is also used for diagnostics around multi-tunnel DS-Lite policy
routing. PPPoE resources rely on pppd and the rp-pppoe plugin shipped by the
distribution's PPP packages. `dig`, `ping`, `tcpdump`, and `tracepath` are
treated as standard router diagnostics because IPv4/IPv6 reachability and
provider-specific DNS behavior need to be checked from the router itself.
routerd embeds SQLite support in the static binary, so the `sqlite3` command
is not required at runtime; install it only when you want to inspect
`/var/lib/routerd/routerd.db` by hand.

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

Regenerate the YAML authoring schema from Go API structs. The same target also
generates control API JSON Schema and OpenAPI definitions:

```sh
make generate-schema
make check-schema
```

## Install

For local source installs:

```sh
sudo make install
```

The install target stays intentionally simple so packaging can later wrap the
same layout from ports, dpkg, or another package system. Override paths with
`PREFIX`, `DESTDIR`, `SYSCONFDIR`, `PLUGINDIR`, `RUNDIR`, `STATEDIR`, or
`SYSTEMDUNITDIR` as needed.

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

When installing to a FreeBSD test host from a Linux workstation, build the
FreeBSD binaries explicitly:

```sh
make remote-install ROUTERD_OS=freebsd REMOTE_HOST=user@router.example
```

Check minimum remote host dependencies:

```sh
make check-remote-deps REMOTE_HOST=user@router.example CONFIG=examples/router-lab.yaml
```

When `CONFIG` is set, optional checks follow the selected resources. For
example, `pppd` is required only when `PPPoEInterface` is configured, and
`wide-dhcpv6-client` is required only when the Linux fallback
`client: dhcp6c` is selected. Linux `Bridge` resources require `mstpd` when
RSTP is enabled.

On Ubuntu, the current source install expects host tools such as `systemd`,
`iproute2`, `dnsmasq`, `nftables`, `conntrack`, and `jq`.
Install `wide-dhcpv6-client` only when `DHCPv6PrefixDelegation` explicitly uses
the Linux fallback `client: dhcp6c`; install `pppd` only when using
`PPPoEInterface`; install `mstpd` when using Linux bridge resources with RSTP.
`sqlite3` is optional for manual state inspection. On FreeBSD, the limited groundwork
expects base networking tools plus the `dnsmasq` and `dhcp6` packages so
`dnsmasq` and `dhcp6c` are available, plus `jq` for local status inspection
scripts.

Install only the config file to a remote test host:

```sh
make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml
```

Install the appropriate service-manager integration. The Makefile picks
`install-systemd` on Linux and `install-rc-freebsd` on FreeBSD automatically:

```sh
sudo make install-service
```

The OS-specific targets are also available directly:

```sh
sudo make install-systemd      # Linux (Ubuntu, NixOS, ...)
sudo make install-rc-freebsd   # FreeBSD rc.d
```

For NixOS, prefer the repository-root flake and the module under
`contrib/nix/` instead of `make install`. See
[contrib/nix/README.md](contrib/nix/README.md).

## Test

```sh
make test
```

or:

```sh
go test ./...
```

## Example commands

```sh
make validate-example
make dry-run-example
make website-build
```

Useful direct commands:

```sh
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
routerd adopt --config examples/router-lab.yaml --candidates
routerd apply --config examples/router-lab.yaml --once --dry-run
routerd serve --config examples/router-lab.yaml --socket /run/routerd/routerd.sock
routerctl status
routerctl get ipv6pd
routerctl describe ipv6pd/wan-pd
routerctl show ipv6pd
routerctl show interface/wan -o yaml
routerctl show ipv4sourcenat/lan-to-wan --diff
routerctl plan
```

`routerctl get <kind>` reads only `router.yaml` and prints desired resources.
Use `routerctl get --list-kinds` to list configured kinds, and `-o json` or
`-o yaml` for structured output. `routerctl describe <kind>/<name>` is the
human-readable investigation view: it combines observed status, recent events,
owned host artifacts, and the last apply generation. `routerctl show`
remains the all-in-one view for scripts and debugging, with `--diff`,
`--ledger`, `--adopt`, `--events`, `--spec`, and `--status`. Common aliases
include `if`, `pd`, `ipv6pd`, `nat`, `dslite`, `pppoe`, `fw`, `zone`,
`hostname`, and `route`. NAPT/conntrack details are shown under
`IPv4SourceNAT` observed state, so there is no separate `show napt` command.

`routerd apply --once` applies managed netplan, systemd-networkd drop-ins,
dnsmasq, nftables, sysctl values, DS-Lite tunnels, and policy routing. By
default apply is additive: it updates resources in the current YAML and leaves
previously managed but now-unmentioned resources in place. Remove resources
with explicit `routerd delete` or `routerctl delete` commands. Avoid running it
against a remote router until the adoption plan is understood, especially where
cloud-init or existing netplan owns the management interface. routerd reports
such cases as adoption candidates rather than silently taking them over.

For DHCPv6-PD lab work, `routerd apply --once --override-client <client>` and
`--override-profile <profile>` can override every `DHCPv6PrefixDelegation` for
that run without changing the YAML file. Known problematic OS/client/profile
combinations are reported as warnings rather than hard validation failures.

When a host already carries matching configuration, run
`routerd adopt --candidates` to inspect the existing artifacts, then
`routerd adopt --apply` to record them in the local ownership ledger without
changing host state.

## Documentation

- [Resource API v1alpha1](docs/api-v1alpha1.md)
- [Resource ownership and apply model](docs/resource-ownership.md)
- [Control API v1alpha1](docs/control-api-v1alpha1.md)
- [Plugin protocol](docs/plugin-protocol.md)
- [Supported platforms](docs/platforms.md)
- [Getting started](docs/tutorials/getting-started.md)
- [Getting started on Nix and NixOS](docs/tutorials/nixos-getting-started.md)
- [Design notes and roadmap](docs/design-notes.md)
- [NTT NGN/HGW DHCPv6-PD knowledge base](docs/knowledge-base/ntt-ngn-pd-acquisition.md)
- [DHCPv6-PD client matrix](docs/knowledge-base/dhcpv6-pd-clients.md)
- [Changelog](docs/releases/changelog.md)
- [API v1alpha1 — Japanese](docs/api-v1alpha1.ja.md)
- [Resource ownership — Japanese](docs/resource-ownership.ja.md)
- [Control API v1alpha1 — Japanese](docs/control-api-v1alpha1.ja.md)
- [Plugin protocol — Japanese](docs/plugin-protocol.ja.md)

The public website lives in `website/` and is built with Docusaurus. It
publishes English and Japanese docs to Cloudflare Pages. Use `website` as the
Pages root directory, `npm ci && npm run build` as the build command, and
`build` as the output directory. Add `routerd.net` as the Cloudflare Pages
custom domain.

## Default paths

- Config: /usr/local/etc/routerd/router.yaml
- Plugin dir: /usr/local/libexec/routerd/plugins
- Binary: /usr/local/sbin/routerd
- Client binary: /usr/local/sbin/routerctl

Linux runtime defaults:

- Runtime dir: /run/routerd
- State dir: /var/lib/routerd
- Status file: /run/routerd/status.json
- Control socket: /run/routerd/routerd.sock
- Lock file: /run/routerd/routerd.lock

FreeBSD runtime defaults:

- Runtime dir: /var/run/routerd
- State dir: /var/db/routerd
- rc.d script: /usr/local/etc/rc.d/routerd
