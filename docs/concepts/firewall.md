# Stateful firewall

![Diagram showing routerd firewall zones, policy matrix, explicit rules, client policy, and generated nftables filter output](/img/diagrams/concept-firewall.png)

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

## Rule expressions

`FirewallRule` supports CIDR source/destination matches, reusable
`IPAddressSet` destination references, TCP/UDP `sourcePorts` and
`destinationPorts`, ICMP / ICMPv6 type names, nftables `rateLimit`, and
per-source `connLimit`. `rateLimit` and `connLimit` match over-limit traffic,
so they are usually paired with `drop` or `reject` rules for dampening scans or
brute-force attempts.

## Guest client isolation

`ClientPolicy` is a MAC-based guest mode for shared LAN segments. It is meant
for the case where trusted and guest devices receive addresses from the same
DHCP server, but guest devices must not reach private networks.

The policy has two modes:

| Mode | Behavior |
| --- | --- |
| `include` | Listed MAC addresses are guests. All other clients remain trusted. |
| `exclude` | Listed MAC addresses are trusted. All other clients on the target interfaces are guests. |

`ClientPolicy.spec.macs` is the short form for the common case. `interfaces`
can be omitted when the policy should target every interface in a `trust`
`FirewallZone`. `spec.isolation` provides readable intent for guest mode:
internet, LAN, management, and local discovery can be allowed or denied without
hand-writing CIDR lists.

Guest clients can use the router's local DNS, DHCP, and NTP services. By
default they cannot forward traffic to `10.0.0.0/8`, `172.16.0.0/12`,
`192.168.0.0/16`, or `fc00::/7`. Global internet egress still follows the
normal zone matrix and route policy.

`ClientPolicy` is implemented by the Linux nftables renderer with Ethernet
source address sets. FreeBSD pf cannot provide that MAC/L2 matching model in
the routed filter path. On FreeBSD, routerd instead renders IPv4 rules from a
`DHCPv4Reservation` and IPv6 rules only from explicit
`classification[].ipv6Addresses`. It never derives IPv6 identity from a MAC or
IPv4 reservation. This preserves a safe, address-based guest-deny model but
does not cover privacy or otherwise unlisted IPv6 addresses; use separate
network segmentation when that is required.

## Logging

When `FirewallPolicy.spec.logDeny` is true and a `FirewallEventLog` resource is
enabled, generated nftables rules log denied packets to the configured NFLOG
group. On Linux, `routerd-firewall-logger` reads that group directly through
nfnetlink and stores rows in `firewall-logs.db`. This keeps NFLOG prefixes,
interfaces, packet family, protocol, addresses, and ports without running a
separate packet capture process.

The Web Console Firewall tab and `routerctl get firewall-logs` read from that
database. When `FirewallEventLog.spec.enabled` is true, routerd derives the
`routerd-firewall-logger` service artifact and passes the configured database
path and NFLOG group to it.

For accepted-flow DPI observation, set `FirewallEventLog.spec.log.copyRange` to cap
the NFLOG payload copied from each packet. Values such as `1536` or `2048`
bytes keep the first payload visible for TLS/HTTP/DNS classification without
copying full packets into user space.
