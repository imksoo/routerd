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
