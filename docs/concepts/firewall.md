# Stateful firewall

routerd manages a stateful nftables filter table named `inet routerd_filter`.
The firewall model has three resource kinds:

- `FirewallZone` assigns interfaces to a named zone and gives that zone a role.
- `FirewallPolicy` sets global behavior such as deny logging.
- `FirewallRule` adds explicit exceptions to the role matrix.

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
