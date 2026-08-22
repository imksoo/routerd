# routerd

[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

[Project site and documentation: routerd.net](https://routerd.net/) ·
[日本語の入口](https://routerd.net/ja/docs/) ·
[繁體中文入口](https://routerd.net/zh-Hant/docs/) ·
[简体中文入口](https://routerd.net/zh-Hans/docs/)

Prebuilt release archives for Linux amd64 and FreeBSD amd64 are published on
the [GitHub Releases page](https://github.com/imksoo/routerd/releases).
Installation and upgrade are documented in
[`docs/install-and-upgrade.md`](docs/install-and-upgrade.md).
Release automation for maintainers is documented in
[`docs/operations/release-process.md`](docs/operations/release-process.md).

routerd is a pre-release declarative router control plane for people who want a
general-purpose host to behave like an understandable router.

## Start safely

If you are new to routing, begin with an isolated Ubuntu Server VM or a spare
computer, not the only router for a home, school, or workplace. Keep a console
or a separate management NIC. `routerd apply --once` and `routerd serve` can
change the host network.

The short learning path is:

1. [Network basics](https://routerd.net/docs/tutorials/network-basics) — WAN,
   LAN, DHCP, DNS, NAT, and `/24` in plain language.
2. [Install and upgrade](https://routerd.net/docs/install-and-upgrade) — the
   authoritative installation path.
3. [Getting started safely](https://routerd.net/docs/tutorials/getting-started)
   — validate and dry-run before a live change.
4. [Bring up the first lab router](https://routerd.net/docs/tutorials/first-router)
   — a small DHCP and IPv4 NAT lab with an observable success check.

Ubuntu Server is the primary, most exercised target. FreeBSD and NixOS have
groundwork and selected integration paths; review
[the platform matrix](https://routerd.net/docs/platforms) before treating them
as equivalent first-lab targets.

Instead of spreading intent across netplan, systemd-networkd, dnsmasq,
nftables, sysctl files, custom scripts, and one-off daemon units, routerd keeps
the router shape in typed YAML resources. It then validates the configuration,
shows a plan, writes the required host artifacts, and lets
`routerd serve` own managed daemon lifecycle. Status stays visible through
`routerctl`, a local API, logs, and a read-only Web Console.

The project is built around a simple idea: a router should be configured like a
system, but observed like a service.

## Why routerd?

- **One intent file**: interfaces, WAN acquisition, LAN services, DNS, NAT,
  route policy, sysctl, packages, and service units live in the same resource
  model.
- **Small managed daemons**: DHCPv4, DHCPv6-PD, PPPoE, health checks, DNS, and
  event relays expose HTTP+JSON status over Unix sockets instead of hiding state
  in shell hooks.
- **Convergent routing**: health checks and `EgressRoutePolicy` let a router
  start with an available path, then move new traffic to a better path when it
  becomes healthy. routerd does not flush conntrack during that change.
- **Explicit DNS design**: dnsmasq is kept for DHCP and RA. DNS answering,
  conditional forwarding, DoH, DoT, DoQ, UDP fallback, cache, local zones, and
  DHCP-derived names live in `routerd-dns-resolver`.
- **Operational visibility**: bus events, resource status, DNS queries,
  connection observations, traffic flow logs, and firewall logs can be inspected
  locally without editing configuration from the browser.
- **Real host bootstrap**: package installation, sysctl defaults,
  systemd-networkd adoption, systemd units, log forwarding, and Web Console
  setup are declared as resources.

## Where routerd fits

routerd is designed to cover a rare span: a virtual router between SDN/VNET
segments and a diskless physical mini PC router can use the same resource
model. The host artifacts differ. The intent file stays recognizable.

routerd is not trying to replace every router project or appliance UI. It is
strongest when the same network intent must move between a Proxmox lab, an
Ubuntu home gateway, and a diskless mini PC booted from the live ISO. FreeBSD
and NixOS work is intentionally second-tier while native renderer parity is
still under development.

The project focuses on a few independent strengths:

- **Cross-OS declarative resources** with Ubuntu Server as the primary host;
  FreeBSD and NixOS integration is documented as groundwork where the native
  renderer is not yet complete.
- **Live ISO plus USB persistence** for diskless mini PC routers.
- **Observable routing decisions** through events, generation diffs,
  health checks, Web Console, and OpenTelemetry.
- **Multi-stage WAN fallback** across DS-Lite, PPPoE, DHCP WAN, and local
  route policy without flushing conntrack.
- **Client-aware LAN policy** through DHCP reservations, neighbor inventory,
  and MAC-based guest isolation on supported platforms.
- **CloudEdge SAM** for selected `/32` IPv4 mobility over BGP, with IPIP/GRE
  transport profiles and optional WireGuard encryption as endpoint-only
  underlay. Selected addresses move across on-prem/AWS/Azure/OCI with the BGP
  best path as the source of truth: equal-priority members are no-preempt to
  minimize churn, unequal-priority members auto-restore to the higher priority
  with no dataplane dip, and an active node's death fails over new flows to a
  standby after convergence. Generic RR-published `SAMPeerGroup` transport
  sync is fail-static on leaves: a stale last-known-good sync record keeps
  generated transport and BGP artifacts in place while status reports the
  source as stale with an operator warning. Enrollment is separate: an
  admitted leaf fetches a policy-scoped runtime `SAMRRSet`, never a statically
  authored RR topology. A policy can additionally offer an **optional direct
  leaf path**. routerd tries that path only for client-authored, opted-in
  leaves, gives it a higher BGP local preference while it is established, and
  keeps the RR peers as the safe fallback when the direct path is absent or
  unreachable. `joinTokenFrom` authenticates that client identity; a policy
  without it is a trusted control-plane deployment. Every configured RR must
  attest the same current client identity before routerd enables the
  higher-preference path. After a clean RR boot, only the explicit
  identity-aware "not admitted" response can trigger automatic re-admission;
  a revoked claim, different active identity, old RR, or uncertainty retains
  the ordinary RR fallback, never an unverified direct peer.
  Static identity/topology comes from `SAMNodeSet`, while
  each MobilityPool supplies the local `/32` and capture intent. Startup
  fencing is readiness-first but bounded, and
  generated RR-client import admission defaults to declared MobilityPool prefixes
  when explicit allowed prefixes are omitted.
  Abrupt failover does not promise that TCP sessions already in flight survive
  without an application retry. See
  [CloudEdge SAM とは](docs/concepts/cloudedge-sam.md) and
  [CloudEdge SAM internals](docs/reference/cloudedge-sam-internals.md).
- **Authoring tools** through generated JSON Schema, VS Code/YAML Language
  Server modelines, and the browser config wizard at `https://routerd.net/wizard`.

That makes routerd useful when a network grows sideways: from a Proxmox lab, to
a home DS-Lite router, to WireGuard/Tailscale overlays, to a diskless mini PC
that can be rebuilt from USB state.

## Current Scope

Implemented resource areas include:

- interface aliases, links, bridges, VRF, VXLAN, WireGuard, Tailscale exit
  node / subnet router setup, and cloud-oriented IPsec connection definitions
  with strongSwan `swanctl` rendering
- WAN acquisition through DHCPv6 prefix delegation, DHCPv6 information request,
  DHCPv4 leases, PPPoE sessions, and DS-Lite tunnels
- LAN service through managed dnsmasq: DHCPv4 scopes and reservations,
  DHCPv6 stateless/stateful/both modes, DHCP relay, RA, PIO, RDNSS, DNSSL, and
  MTU options
- DNS service through `DNSZone` and `DNSResolver`: local authoritative zones,
  DHCP-derived records, conditional forwarding, DoH, DoT, DoQ, UDP fallback,
  DNSSEC flags, multiple listen profiles, and cache
- IPv4 and IPv6 address derivation, static routes, default route policy,
  route-set exclusions, path MTU policy, TCP MSS clamping, NAT44, and DS-Lite
- Kubernetes edge building blocks: long-lived `routerd-bgp` GoBGP-backed BGP peers, static
  Pod/Service CIDR route helpers, keepalived-backed IPv4 and IPv6 VIPs, and
  multi-backend `IngressService` health/failover
- guest-device isolation with `ClientPolicy`, DHCPv4 reservations, and
  MAC-based nftables filtering on shared LAN segments
- `HealthCheck`, `EgressRoutePolicy`, `EventRule`, and `DerivedEvent`
  coordination
- `Package` overrides, `Sysctl`, `SysctlProfile`, derived host runtime
  dependencies, `NTPClient`, `LogSink`, `ObservabilityPipeline`,
  `RouterdCluster`, `LogRetention`, and `WebConsole`
- local NAPT/conntrack inspection through `routerctl`
- read-only Web Console for status, events, connections, DNS queries, traffic,
  firewall logs, and the active configuration
- OpenTelemetry SDK hooks and built-in event log forwarding to stdout, syslog,
  or Loki when exporters are configured
- CloudEdge Mobility (`MobilityPool` + `SAMTransportProfile`) for selective
  address mobility, provider action planning/execution gates, and BGP-mode
  `/32` delivery over generated tunnel/BGP peer resources
- owner-reference based lifecycle GC for routerd-managed artifacts and stale
  object status, with explicit teardown contracts for every config resource kind

Stateful firewall filtering is intentionally scoped. routerd renders NAT44,
zone policy, service holes, denial logging, and traffic inspection, but it is
not a general-purpose firewall rule language.

## Example Shape

The production-style examples show how the pieces fit together:

- `examples/home-router.yaml`: Ubuntu home-router style configuration with OS
  bootstrap auto-derivation, DHCPv6-PD, DS-Lite, DNS resolver, DHCP server,
  RA, BGP peering, and Web Console.
- `examples/router-lab.yaml`: smaller Linux lab configuration.
- `examples/freebsd-edge.yaml`: compact FreeBSD-native rc.d, pf, dnsmasq, and
  DS-Lite rendering example.
- `examples/tailscale-exit-subnet.yaml`: Tailscale exit-node and subnet-router
  advertisement through a managed systemd unit.
- `examples/guest-mode.yaml`: a MAC-based router policy example. It is not a
  substitute for VLAN, SSID, switch-port, or Wi-Fi client isolation.
- `examples/cloudedge-mobility-demo/`: on-prem/AWS/Azure/OCI CloudEdge SAM
  configs using `SAMTransportProfile`.
- `examples/README.md`: an index of focused templates, including minimal
  Tailscale, WireGuard hub-spoke, VRF lab, and multi-WAN home patterns.

You can also start from the browser wizard:

```text
https://routerd.net/wizard
```

The wizard generates Home Router, CloudEdge SAM, and Kubernetes BGP profiles
from the same builder used by CI fixtures. Output is checked against the
published config schema in the browser.

Static DHCPv4 reservations are declared as resources:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DHCPv4Reservation
metadata:
  name: printer
spec:
  server: lan-dhcpv4
  macAddress: 02:00:00:00:10:10
  hostname: printer
  ipAddress: 172.18.0.150
```

Private destinations can be excluded from NAT while internet traffic still
uses the selected egress path:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-wan
spec:
  type: masquerade
  egressInterface: wan
  sourceRanges:
    - 172.18.0.0/16
  excludeDestinationCIDRs:
    - 192.168.0.0/16
    - 172.16.0.0/12
    - 10.0.0.0/8
```

## Quick Start

Install from a release archive on the router host:

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

For FreeBSD, download `routerd-freebsd-amd64.tar.gz` from the stable release tag
and run the same `./install.sh`.
Use `routerd-linux-arm64.tar.gz` or `routerd-freebsd-arm64.tar.gz` on arm64 hosts.
Versioned archives such as `routerd-v20260707.1514-linux-amd64.tar.gz` are also
available on the same release page when you need an explicitly named artifact.

Linux release archives contain statically linked routerd binaries
(`CGO_ENABLED=0`). They do not depend on the target host's glibc version.

`install.sh` installs known OS packages, copies binaries to `/usr/local/sbin`,
installs the service template, writes `router.yaml.sample`, and preserves an
existing `/usr/local/etc/routerd/router.yaml`.
Use `./install.sh --list-deps` to inspect the package list.
Use `sudo ./install.sh --no-install-deps` when packages are managed elsewhere.

## License and Redistribution

routerd itself is released under the [BSD 3-Clause License](LICENSE). Release
archives and the live ISO include third-party software with their own licenses. The
Alpine-based live ISO is an aggregate distribution: GPL-licensed tools such as
dnsmasq, nftables, WireGuard tools, ppp, and iproute2 keep their own licenses
and source availability paths. The ISO as a whole is not relicensed as one GPL
work.

The release archive includes `share/doc/LICENSE` and
`share/doc/THIRD_PARTY_LICENSES.md`. The live ISO exposes the same notices
under `/usr/share/licenses/routerd/`. Regenerate the inventory with:

```sh
make third-party-licenses
```

Then create a configuration and validate it directly before a daemon exists:

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml

sudo routerd validate --config /usr/local/etc/routerd/router.yaml

workdir=$(mktemp -d)
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run \
  --state-file "$workdir/state.db" \
  --ledger-file "$workdir/ledger.db" \
  --status-file "$workdir/status.json"
rm -rf "$workdir"
```

Apply only from a console or independent management path after confirming it is
safe. This changes the host network:

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

## Developer Build

Go 1.24 or newer is expected.

```sh
make test
make build
make check-schema
make validate-example
make website-build
```

The Makefile is for development tasks.
End-user installation goes through the release archive and `install.sh`.

Important binaries built by `make build` include:

- `routerd`
- `routerctl`
- `routerd-dhcpv4-client`
- `routerd-dhcpv6-client`
- `routerd-pppoe-client`
- `routerd-healthcheck`
- `routerd-dns-resolver`
- `routerd-dhcp-event-relay`
- `routerd-firewall-logger`

Useful direct commands (the `routerctl` commands require a running local
`routerd.service`; use `sudo` unless your account has deliberately joined the
`routerd` group and started a new login session):

```sh
routerd validate --config examples/home-router.yaml
sudo routerctl get status
sudo routerctl get events --limit 20
sudo routerctl get connections --limit 50
sudo routerctl plugin list
sudo routerctl plugin run <name> --dry-run
```

## Runtime Layout

Default release-install paths:

- Config: `/usr/local/etc/routerd/router.yaml`
- Binaries: `/usr/local/sbin/routerd`, `/usr/local/sbin/routerctl`,
  `/usr/local/sbin/routerd-*`
- Plugin directory: `/usr/local/libexec/routerd/plugins`
- Linux runtime: `/run/routerd`
- Linux state: `/var/lib/routerd`
- FreeBSD runtime/state equivalents: `/var/run/routerd`, `/var/db/routerd`

Managed daemons expose the same local contract:

- `GET /v1/status`
- `GET /v1/healthz`
- `GET /v1/events?since=<cursor>&wait=<duration>`
- `POST /v1/commands/<command>`

## Platform Notes

Ubuntu Server is the most exercised deployment target. NixOS and FreeBSD share
the resource model but remain second-tier: some native renderers and service
integration are groundwork rather than feature parity. Alpine supports the live
ISO and `apk` package bootstrap, while OpenRC service parity is also
groundwork. See
`docs/platforms.md` for the current OS surface matrix.

The implementation is pre-release. v1alpha1 names and fields may still change
when a breaking cleanup makes the router safer or easier to operate.

## Non-goals for Now

- Remote plugin registry or remote plugin installation
- Full rollback of every OS-level mutation
- Interactive configuration editing in the Web Console
- Built-in LLM assistant
- Proxmox lab automation
- General-purpose firewall rule language

See `docs/design.md` for the authoritative design state.
