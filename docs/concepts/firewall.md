# Stateful firewall

routerd manages a stateful nftables filter table named `inet routerd_filter`.
The firewall model has four resource kinds:

- `FirewallZone` assigns interfaces to a named zone and gives that zone a role.
- `FirewallPolicy` sets global behavior such as deny logging.
- `FirewallRule` adds explicit exceptions to the role matrix.
- `ClientPolicy` classifies LAN clients by MAC address for guest isolation.

The supported roles are `untrust`, `trust`, and `mgmt`. Zone names describe
topology, such as `wan`, `lan`, or `management`. Roles describe policy.

The implicit matrix is:

| From | To self | To mgmt | To trust | To untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | accept | accept | accept | accept |
| `trust` | accept | drop | accept | accept |
| `untrust` | drop | drop | drop | drop |

The generated rules always accept established and related connections, loopback
traffic, and required ICMPv6 traffic. Controllers also derive internal openings
for routerd-managed services such as DHCPv6 prefix delegation, DS-Lite,
dnsmasq DHCP service, and `routerd-dns-resolver`.

The firewall table is separate from NAT. NAT44 continues to use `ip
routerd_nat`.

## Guest client isolation

`ClientPolicy` is a MAC-based guest mode for shared LAN segments. It is meant
for the case where trusted and guest devices receive addresses from the same
DHCP server, but guest devices must not reach private networks.

The policy has two modes:

| Mode | Behavior |
| --- | --- |
| `include` | Listed MAC addresses are guests. All other clients remain trusted. |
| `exclude` | Listed MAC addresses are trusted. All other clients on the target interfaces are guests. |

Guest clients can use the router's local DNS, DHCP, and NTP services. By
default they cannot forward traffic to `10.0.0.0/8`, `172.16.0.0/12`,
`192.168.0.0/16`, or `fc00::/7`. Global internet egress still follows the
normal zone matrix and route policy.

`ClientPolicy` is currently implemented by the Linux nftables renderer with
Ethernet source address sets. FreeBSD pf does not expose the same MAC matching
model in the routed filter path, so the FreeBSD renderer reports the resource
as unsupported instead of silently applying a weaker policy.

## Logging

When `FirewallPolicy.spec.logDeny` is true and a `FirewallLog` resource is
enabled, generated nftables rules log denied packets to the configured NFLOG
group. On Linux, `routerd-firewall-logger` reads that group directly through
nfnetlink and stores rows in `firewall-logs.db`. This keeps NFLOG prefixes,
interfaces, packet family, protocol, addresses, and ports without running a
separate packet capture process.

The Web Console Firewall tab and `routerctl firewall-logs` read from that
database. The logger must be enabled as a managed `SystemdUnit`, for example
with `routerd-firewall-logger daemon --path /var/lib/routerd/firewall-logs.db
--nflog-group 1`.
