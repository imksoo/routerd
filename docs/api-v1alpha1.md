---
title: Resource API v1alpha1
slug: /reference/api-v1alpha1
---

# Resource API v1alpha1

routerd configuration is a top-level `Router` resource with a list of typed
resources. This page summarizes the current implemented API surface.

Since Phase 1.6, DHCP names follow RFC spelling: `DHCPv4*` and `DHCPv6*`.
There are no compatibility aliases for the earlier names.

## Common Shape

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: wan
spec:
  ifname: ens18
  adminUp: true
```

| Field | Meaning |
| --- | --- |
| `apiVersion` | API group and version. |
| `kind` | Resource kind. |
| `metadata.name` | Name inside the kind. |
| `spec` | Desired intent declared by the user. |
| `status` | Observed state written by routerd or a managed daemon. |

## API Groups

| API group | Main kinds |
| --- | --- |
| `routerd.net/v1alpha1` | `Router` |
| `net.routerd.net/v1alpha1` | interfaces, DHCP, DNS, routes, tunnels, events, traffic flow logs |
| `firewall.routerd.net/v1alpha1` | `FirewallZone`, `FirewallPolicy`, `FirewallRule`, `FirewallLog` |
| `system.routerd.net/v1alpha1` | `Hostname`, `Sysctl`, `Package`, `NetworkAdoption`, `SystemdUnit`, `NTPClient`, `LogSink`, `LogRetention`, `WebConsole`, `NixOSHost` |
| `plugin.routerd.net/v1alpha1` | plugin manifests |

## System Bootstrap

| Kind | Role |
| --- | --- |
| `Package` | Declares OS-specific packages and installs missing packages where the platform supports it. |
| `Sysctl` | Sets one sysctl value. Readback comparison can be `exact` or `atLeast`. |
| `SysctlProfile` | Applies router-oriented sysctl defaults. |
| `NetworkAdoption` | Adjusts OS DHCP clients and systemd-resolved listeners so routerd can own the interface role. |
| `SystemdUnit` | Generates, installs, and enables systemd units used by routerd. |
| `Hostname` | Sets the host name. |
| `NTPClient` | Enables the OS NTP client. |
| `LogSink` | Sends routerd events to syslog or another local sink. |
| `LogRetention` | Manages retention for events, DNS queries, traffic flows, and firewall logs. |
| `WebConsole` | Enables the read-only management Web Console. |

## Interfaces and Links

| Kind | Role |
| --- | --- |
| `Interface` | Binds a stable routerd name to an OS interface name. |
| `Link` | Publishes link state for downstream resources. |
| `PPPoEInterface` | Defines PPPoE lower-interface settings. |
| `PPPoESession` | Represents a `routerd-pppoe-client` session. |
| `WireGuardInterface` | Represents a WireGuard interface. |
| `WireGuardPeer` | Represents a WireGuard peer. |
| `IPsecConnection` | Defines a cloud VPN oriented strongSwan connection. |
| `VRF` | Represents a Linux VRF device and route table. |
| `VXLANTunnel` | Represents a VXLAN tunnel. |

## WAN Addressing and Delegation

| Kind | Role |
| --- | --- |
| `IPv4StaticAddress` | Assigns a static IPv4 address. |
| `DHCPv4Address` | Legacy host DHCP client path. Prefer `DHCPv4Lease` for new configs. |
| `DHCPv4Lease` | DHCPv4 lease managed by `routerd-dhcpv4-client`. |
| `DHCPv6Address` | Groundwork for DHCPv6 IA_NA. |
| `DHCPv6PrefixDelegation` | DHCPv6-PD lease managed by `routerd-dhcpv6-client`. |
| `DHCPv6Information` | DHCPv6 information request result, including DNS, SNTP, domain search, and AFTR observations. |
| `IPv6DelegatedAddress` | Derives a LAN-side address from a delegated prefix. |
| `IPv6RAAddress` | Groundwork for IPv6 addresses learned from RA/SLAAC. |

`DHCPv6PrefixDelegation` no longer selects an OS DHCPv6 client. DHCPv6-PD is
owned by `routerd-dhcpv6-client`.

## LAN Services

| Kind | Role |
| --- | --- |
| `DHCPv4Server` | Provides a dnsmasq DHCPv4 pool. |
| `DHCPv4Scope` | Represents a DHCPv4 address range. |
| `DHCPv4Reservation` | Reserves an IPv4 address for a MAC address. |
| `DHCPv4Relay` | Represents dnsmasq DHCPv4 relay. |
| `IPv6RouterAdvertisement` | Generates RA, PIO, RDNSS, DNSSL, M/O flags, MTU, preference, and lifetimes. |
| `DHCPv6Server` | Provides dnsmasq DHCPv6 service in `stateless`, `stateful`, or `both` mode. |
| `DHCPv6Scope` | Represents a DHCPv6 address range. |
| `DNSZone` | Owns a local authoritative zone with manual and DHCP-derived records. |
| `DNSResolver` | Owns `routerd-dns-resolver` listen profiles, sources, upstreams, and cache. |

Android does not use DHCPv6 DNS configuration, so IPv6 LANs should publish
RDNSS through `IPv6RouterAdvertisement.spec.rdnss`.

dnsmasq is limited to DHCPv4, DHCPv6, relay, and RA. DNS answering and
forwarding belongs to `DNSResolver`.

`DNSResolver.spec.sources` lists local zones, conditional forwarding sources,
and default upstreams in priority order. `https://` is DoH, `tls://` is DoT,
`quic://` is DoQ, and `udp://` is plain DNS. `listen` can contain multiple
profiles, and each listener can choose a subset of sources.

`sources[].viaInterface` binds outgoing DNS queries to a Linux interface name.
`sources[].bootstrapResolver` supplies resolver addresses for DoH and DoT
endpoint name resolution. DNSSEC is configured with `DNSZone.spec.dnssec` and
`DNSResolver.spec.sources[].dnssecValidate`.

## DS-Lite, Routes, and NAT

| Kind | Role |
| --- | --- |
| `DSLiteTunnel` | Creates an `ip6tnl` tunnel to an AFTR. The AFTR can be static IPv6, FQDN, or DHCPv6 information. |
| `IPv4Route` | Adds IPv4 routes, including DS-Lite defaults and explicit drop routes. |
| `NAT44Rule` | Performs IPv4 NAPT in the nftables `routerd_nat` table. |
| `IPv4SourceNAT` | Legacy IPv4 source NAT groundwork. |
| `IPv4PolicyRoute` | Represents IPv4 policy routing. |
| `IPv4PolicyRouteSet` | Groups multiple policy routes. |
| `IPv4DefaultRoutePolicy` | Represents default-route policy. |
| `IPv4ReversePathFilter` | Manages reverse path filter settings. |
| `PathMTUPolicy` | Controls MTU and TCP MSS adjustment. `mtu.source: probe` can measure path MTU with DF probes. |

`IPv4PolicyRoute`, `IPv4PolicyRouteSet`, and `IPv4DefaultRoutePolicy` support
`excludeDestinationCIDRs`. Use it to keep LAN, management, HGW LAN, and RFC
1918 destinations out of policy routing.

`NAT44Rule` supports `destinationCIDRs` and `excludeDestinationCIDRs`. This
allows internet traffic to be masqueraded while private routed destinations
stay un-NATed.

## Coordination

| Kind | Role |
| --- | --- |
| `HealthCheck` | Measures reachability through `routerd-healthcheck` or the development embedded runner. |
| `EgressRoutePolicy` | Selects the highest-weight ready egress candidate. Candidates can include gateway fields and health checks. |
| `EventRule` | Evaluates event streams with all_of, any_of, sequence, window, absence, throttle, debounce, and count. |
| `DerivedEvent` | Emits virtual events derived from multiple resource states. |
| `SelfAddressPolicy` | Selects a self address for protocols that need one. |
| `StatePolicy` | Represents state-management policy. |

`HealthCheck.spec.sourceInterface` accepts a network resource name and resolves
it to the OS interface name at runtime. `via` and `sourceAddress` can also be
specified. `sourceAddressFrom` derives the probe source address from another
resource status.

## Firewall

| Kind | Role |
| --- | --- |
| `FirewallZone` | Assigns interfaces to zones with `untrust`, `trust`, and `mgmt` roles. |
| `FirewallPolicy` | Represents global firewall behavior such as deny logging. |
| `FirewallRule` | Represents exceptions that cannot be expressed by the role matrix. |

Stateful filtering renders into the nftables `inet routerd_filter` table.
Established traffic, loopback, and required ICMPv6 are always accepted.
routerd derives internal openings needed by DHCP, DNS, DS-Lite, and related
managed resources.

## Renamed Kinds

Phase 1.6 renamed DHCP resources.

| Old | Current |
| --- | --- |
| `IPv4DHCPAddress` | `DHCPv4Address` |
| `IPv4DHCPServer` | `DHCPv4Server` |
| `IPv4DHCPReservation` | `DHCPv4Reservation` |
| `IPv4DHCPScope` | `DHCPv4Scope` |
| `IPv6DHCPAddress` | `DHCPv6Address` |
| `IPv6PrefixDelegation` | `DHCPv6PrefixDelegation` |
| `IPv6DHCPServer` / `IPv6DHCPv6Server` | `DHCPv6Server` |
| `IPv6DHCPScope` | `DHCPv6Scope` |
| `DHCPRelay` | `DHCPv4Relay` |

The daemon binaries are `routerd-dhcpv4-client` and `routerd-dhcpv6-client`.
