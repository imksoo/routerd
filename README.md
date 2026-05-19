# routerd

[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

[Project site and documentation: routerd.net](https://routerd.net/)

Prebuilt release archives for Linux amd64 and FreeBSD amd64 are published on
the [GitHub Releases page](https://github.com/imksoo/routerd/releases).
Installation and upgrade are documented in
[`docs/install-and-upgrade.md`](docs/install-and-upgrade.md).
Release automation for maintainers is documented in
[`docs/operations/release-process.md`](docs/operations/release-process.md).

routerd is a pre-release declarative router control plane for people who want a
general-purpose host to behave like an understandable router.

Instead of spreading intent across netplan, systemd-networkd, dnsmasq,
nftables, sysctl files, custom scripts, and one-off daemon units, routerd keeps
the router shape in typed YAML resources. It then validates the configuration,
shows a plan, writes the required host artifacts, and lets
`routerd serve --controller-chain` own managed daemon lifecycle. Status stays
visible through `routerctl`, a local API, logs, and a read-only Web Console.

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
strongest when the same network intent must move between a Proxmox lab, a NixOS
or FreeBSD router, an Ubuntu home gateway, and a diskless mini PC booted from
the live ISO.

The project focuses on a few independent strengths:

- **Cross-OS declarative resources** for Ubuntu, NixOS, and FreeBSD host
  integration, with Alpine Linux groundwork for the live ISO and minimal hosts.
- **Live ISO plus USB persistence** for diskless mini PC routers.
- **Observable routing decisions** through events, generation diffs,
  health checks, Web Console, and OpenTelemetry.
- **Multi-stage WAN fallback** across DS-Lite, PPPoE, DHCP WAN, and local
  route policy without flushing conntrack.
- **Client-aware LAN policy** through DHCP reservations, neighbor inventory,
  and MAC-based guest isolation on supported platforms.

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
- Kubernetes edge building blocks: dual-stack FRR-backed BGP peers with
  optional BFD, static Pod/Service CIDR route helpers, keepalived-backed IPv4
  and IPv6 VIPs, and multi-backend `IngressService` health/failover
- guest-device isolation with `ClientPolicy`, DHCPv4 reservations, and
  MAC-based nftables filtering on shared LAN segments
- `HealthCheck`, `EgressRoutePolicy`, `EventRule`, and `DerivedEvent`
  coordination
- `Package`, `Sysctl`, `SysctlProfile`, `NetworkAdoption`, `SystemdUnit`,
  `NTPClient`, `LogSink`, `ObservabilityPipeline`, `RouterdCluster`,
  `LogRetention`, and `WebConsole`
- local NAPT/conntrack inspection through `routerctl`
- read-only Web Console for status, events, connections, DNS queries, traffic,
  firewall logs, and the active configuration
- OpenTelemetry SDK hooks and built-in event log forwarding to stdout, syslog,
  or Loki when exporters are configured

Stateful firewall filtering is intentionally scoped. routerd renders NAT44,
zone policy, service holes, denial logging, and traffic inspection, but it is
not a general-purpose firewall rule language.

## Example Shape

The production-style examples show how the pieces fit together:

- `examples/home-router.yaml`: Ubuntu home-router style configuration with OS
  bootstrap, DHCPv6-PD, DS-Lite, routed HGW LAN, DNS resolver, DHCP server,
  RA, NAT44, log storage, and Web Console.
- `examples/router-lab.yaml`: smaller Linux lab configuration.
- `examples/nixos-edge.yaml`: NixOS-oriented rendering path.
- `examples/freebsd-edge.yaml`: FreeBSD-native rc.d, pf, mpd5, dnsmasq, DS-Lite,
  package, and service examples.
- `examples/tailscale-exit-subnet.yaml`: Tailscale exit-node and subnet-router
  advertisement through a managed systemd unit.
- `examples/guest-mode.yaml`: MAC-based guest-device isolation on a shared
  LAN.
- `examples/README.md`: an index of focused templates, including minimal
  Tailscale, WireGuard hub-spoke, VRF lab, and multi-WAN home patterns.

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
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

For FreeBSD, download `routerd-freebsd-amd64.tar.gz` from the latest release and
run the same `./install.sh`.
Use `routerd-linux-arm64.tar.gz` or `routerd-freebsd-arm64.tar.gz` on arm64 hosts.
Versioned archives remain available on each release page when you need an exact
release.

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
`share/doc/THIRD_PARTY_LICENSES.md`. The live ISO exposes the same notices under
`/usr/share/licenses/routerd/`. Regenerate the inventory with:

```sh
make third-party-licenses
```

Then create and validate the configuration:

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml

routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

Apply only after confirming that the management path is safe:

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

Useful direct commands:

```sh
routerd validate --config examples/home-router.yaml
routerd plan --config examples/home-router.yaml
routerd apply --config examples/home-router.yaml --once --dry-run
routerctl status
routerctl events --limit 20
routerctl connections --limit 50
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

Ubuntu Server is the most exercised deployment target. NixOS and FreeBSD use
the same resource model through their native activation paths. Alpine supports
the live ISO and `apk` package bootstrap, while OpenRC service parity is still
tracked as groundwork. See
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
