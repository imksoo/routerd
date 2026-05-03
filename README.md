# routerd

[Project site and documentation: routerd.net](https://routerd.net/)

routerd is a pre-release declarative router control plane. A router is described
as typed YAML resources, and routerd turns that intent into local daemon
processes, generated configuration files, kernel network state, and observable
status.

The current implementation is being rebuilt around small managed daemons:

- `routerd-dhcpv6-client` for DHCPv6 prefix delegation and information request
- `routerd-dhcpv4-client` for WAN DHCPv4 leases
- `routerd-pppoe-client` for PPPoE sessions
- `routerd-healthcheck` for out-of-process health probes

routerd then connects daemon events to controllers through an in-process bus and
SQLite event store. The actively tested lab target is pve05-pve07 with Ubuntu,
NixOS, and FreeBSD router VMs.

## Current Scope

Implemented resource areas include:

- Interface aliases, links, bridges, VXLAN, VRF, WireGuard, and cloud-oriented
  IPsec configuration skeletons
- WAN acquisition through DHCPv6-PD, DHCPv6 information request, DHCPv4 leases,
  PPPoE sessions, and DS-Lite tunnels
- LAN service through a managed dnsmasq instance: DHCPv4 server/reservations,
  DHCPv6 stateless/stateful/both modes, RA options, host records, local
  domains, DNSSEC flagging, DDNS, conditional forwarding, and local DNS proxy
  upstreams for DoH, DoT, DoQ, and plain UDP fallback
- IPv6 delegated LAN address derivation from a DHCPv6-PD prefix
- egress route selection with `EgressRoutePolicy`, `HealthCheck`, `EventRule`, and
  `DerivedEvent`
- IPv4 default routes, NAT44 through nftables, and aggregate conntrack
  observation
- Local HTTP+JSON control APIs over Unix sockets for routerd and the managed
  daemons
- NixOS module rendering, including a declarative
  `routerd-dhcpv6-client@wan-pd` unit

Stateful firewall policy rendering is intentionally postponed. Current nftables
work is focused on NAT44 and narrowly scoped lab rules; do not treat routerd as a
general-purpose firewall rule language yet.

## Naming

Phase 1.6 made a clean RFC-style naming break. There are no compatibility
aliases for the old names.

Use:

- `DHCPv4Address`, `DHCPv4Lease`, `DHCPv4Server`, `DHCPv4Scope`,
  `DHCPv4Reservation`, `DHCPv4Relay`
- `DHCPv6Address`, `DHCPv6PrefixDelegation`, `DHCPv6Information`,
  `DHCPv6Server`, `DHCPv6Scope`
- `routerd-dhcpv4-client` and `routerd-dhcpv6-client`
- `/run/routerd/dhcpv4-client/...`, `/run/routerd/dhcpv6-client/...`
- `/var/lib/routerd/dhcpv4-client/...`, `/var/lib/routerd/dhcpv6-client/...`

External terms keep their native spelling where required, such as Netplan
`dhcp4` / `dhcp6` fields or package names that contain `dhcp6`.

## Build

Go 1.24 or newer is expected.

```sh
make test
make build
make check-schema
```

Important binaries built by `make build`:

- `bin/linux/routerd`
- `bin/linux/routerctl`
- `bin/linux/routerd-dhcpv4-client`
- `bin/linux/routerd-dhcpv6-client`
- `bin/linux/routerd-healthcheck`

Useful direct commands:

```sh
go test ./...
go build ./cmd/routerd
go build ./cmd/routerctl
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
routerd apply --config examples/router-lab.yaml --once --dry-run --status-file /tmp/routerd-status.json
routerctl status
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

Daemon examples:

```sh
/usr/local/sbin/routerd-dhcpv6-client \
  --resource wan-pd \
  --interface ens18 \
  --socket /run/routerd/dhcpv6-client/wan-pd.sock \
  --lease-file /var/lib/routerd/dhcpv6-client/wan-pd/lease.json \
  --event-file /var/lib/routerd/dhcpv6-client/wan-pd/events.jsonl
```

The daemon contract exposes:

- `GET /v1/status`
- `GET /v1/healthz`
- `GET /v1/events?since=<cursor>&wait=<duration>`
- `POST /v1/commands/<command>`

## Tested Lab State

As of Phase 1.7 on 2026-05-03:

| Host | OS | DHCPv6-PD daemon | Prefix | Status |
|---|---|---|---|---|
| router01 | FreeBSD | `routerd-dhcpv6-client` | `2409:10:3d60:1250::/60` | Bound |
| router02 | NixOS | declarative `routerd-dhcpv6-client@wan-pd` | `2409:10:3d60:1230::/60` | Bound |
| router03 | Ubuntu | `routerd-dhcpv6-client` | `2409:10:3d60:1240::/60` | Bound |
| router04 | FreeBSD | `routerd-dhcpv6-client` | `2409:10:3d60:1260::/60` | Bound |
| router05 | Ubuntu | `routerd-dhcpv6-client` + controller chain | `2409:10:3d60:1220::/60` | Bound |

router05 also has a real DS-Lite tunnel installed by routerd:
`ds-routerd-test@ens18`, AFTR resolved from `gw.transix.jp` through the HGW
conditional DNS path, IPv4 default route through the tunnel, NAT44 via
`routerd_nat`, and confirmed `curl -4` connectivity.

## Platform Notes

Ubuntu Server is still the primary implementation target. NixOS and FreeBSD are
active second-tier targets with working binaries and service-manager paths, but
do not imply full renderer parity. Platform-dependent code should go through
`pkg/platform` or explicit feature checks rather than reading `runtime.GOOS`
directly.

The healthy implementation lab is pve05-pve07. Earlier pve01-pve04 vmbr0 VLAN
1901 behavior is treated as a broken lab path and should not drive design
decisions.

## Non-goals for Now

- Remote plugin registry or remote plugin installation
- Full rollback of every OS-level mutation
- Interactive router console
- Built-in LLM assistant
- Proxmox lab automation
- General-purpose firewall rule language

See `docs/design.md` for the authoritative design state.
