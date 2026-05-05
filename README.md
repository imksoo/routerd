# routerd

[Project site and documentation: routerd.net](https://routerd.net/)

routerd is a pre-release declarative router control plane for people who want a
general-purpose host to behave like an understandable router.

Instead of spreading intent across netplan, systemd-networkd, dnsmasq,
nftables, sysctl files, custom scripts, and one-off daemon units, routerd keeps
the router shape in typed YAML resources. It then validates the configuration,
shows a plan, applies the required host artifacts, starts managed daemons, and
keeps status visible through `routerctl`, a local API, logs, and a read-only Web
Console.

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

## Current Scope

Implemented resource areas include:

- interface aliases, links, bridges, VRF, VXLAN, WireGuard, and cloud-oriented
  IPsec groundwork
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
- `HealthCheck`, `EgressRoutePolicy`, `EventRule`, and `DerivedEvent`
  coordination
- `Package`, `Sysctl`, `SysctlProfile`, `NetworkAdoption`, `SystemdUnit`,
  `NTPClient`, `LogSink`, `LogRetention`, and `WebConsole`
- local NAPT/conntrack inspection through `routerctl`
- read-only Web Console for status, events, connections, DNS queries, traffic,
  firewall logs, and the active configuration
- OpenTelemetry SDK hooks for logs, metrics, and traces when exporters are
  configured

Stateful firewall filtering is still evolving. NAT44 and narrowly scoped
firewall/logging groundwork exist, but routerd is not yet a general-purpose
firewall rule language.

## Example Shape

The production-style examples show how the pieces fit together:

- `examples/homert02.yaml`: Ubuntu home-router style configuration with OS
  bootstrap, DHCPv6-PD, DS-Lite, routed HGW LAN, DNS resolver, DHCP server,
  RA, NAT44, log storage, and Web Console.
- `examples/router-lab.yaml`: smaller Linux lab configuration.
- `examples/nixos-edge.yaml`: NixOS-oriented rendering path.
- `examples/freebsd-edge.yaml`: FreeBSD service-manager and package groundwork.

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

## Build

Go 1.24 or newer is expected.

```sh
make test
make build
make check-schema
make validate-example
make website-build
```

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
routerd validate --config examples/homert02.yaml
routerd plan --config examples/homert02.yaml
routerd apply --config examples/homert02.yaml --once --dry-run
routerctl status
routerctl events --limit 20
routerctl connections --limit 50
```

## Runtime Layout

Default source-install paths:

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

Ubuntu Server is the primary implementation target. NixOS and FreeBSD are
active second-tier targets with working binaries and service-manager
groundwork. Do not assume full renderer parity yet; see `docs/platforms.md` for
the current matrix.

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
