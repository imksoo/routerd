---
title: Resource API v1alpha1
slug: /reference/api-v1alpha1
---

# Resource API v1alpha1

![Diagram showing the Resource API v1alpha1 shape from apiVersion, kind, metadata, spec, and status through API groups and generated schema validation contracts](/img/diagrams/api-v1alpha1.png)

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
| `net.routerd.net/v1alpha1` | interfaces, `ManagementAccess`, reusable `IPAddressSet` resources, DHCP, DNS, routes, tunnels, VIP, BGP, events, traffic flow logs |
| `firewall.routerd.net/v1alpha1` | `FirewallZone`, `FirewallPolicy`, `FirewallRule`, `FirewallEventLog`, `ClientPolicy`, `PortForward`, `IngressService`, `LocalServiceRedirect` |
| `system.routerd.net/v1alpha1` | `Hostname`, `Sysctl`, `SysctlProfile`, `Package`, `NTPClient`, `NTPServer`, `LogSink`, `ObservabilityPipeline`, `RouterdCluster`, `LogRetention`, `WebConsole`, `ControlAPI` |
| `observability.routerd.net/v1alpha1` | `Telemetry` |
| `plugin.routerd.net/v1alpha1` | plugin manifests |
| `hybrid.routerd.net/v1alpha1` | `TunnelInterface`, `OverlayPeer`, `HybridRoute`, `AddressMobilityDomain`, `CloudProviderProfile`, `RemoteAddressClaim` |
| `mobility.routerd.net/v1alpha1` | `MobilityPool`, `MobilityMemberSet`, `SAMNodeSet`, `SAMRRSet`, `SAMEnrollmentPolicy`, `SAMEnrollmentClaim`, `SAMEnrollmentClient`, `SAMTransportProfile` |

## System Bootstrap

| Kind | Role |
| --- | --- |
| `Package` | Optional narrow override for OS packages that cannot yet be derived from router resources. Normal runtime dependencies are derived automatically. |
| `Sysctl` | Narrow escape hatch for one sysctl value that cannot yet be derived from router resources. Readback comparison can be `exact` or `atLeast`. |
| `SysctlProfile` | Narrow escape hatch for router-oriented sysctl defaults. Normal router sysctls are derived automatically. |
| `Hostname` | Sets the host name. |
| `NTPClient` | Enables the OS NTP client. It can use static servers or derive servers from DHCPv4 / DHCPv6 status with public fallback servers. |
| `NTPServer` | Runs a local LAN NTP server. Client allow ranges can be static `allowCIDRs` or derived with `allowCIDRFrom` from status fields such as `IPv6DelegatedAddress/<name>.address` or `DHCPv6PrefixDelegation/<name>.currentPrefix`. |
| `LogSink` | Routes log events to syslog, OTLP, webhook, file, or journald sinks. |
| `ObservabilityPipeline` | Configures OTLP environment and built-in routerd event forwarding to stdout, syslog, or Loki. |
| `RouterdCluster` | Uses a file lease so only the leader mutates host configuration while standby nodes observe status. |
| `LogRetention` | Manages retention for events, DNS queries, traffic flows, and firewall event logs. |
| `WebConsole` | Enables the read-only management Web Console. |
| `ControlAPI` | Configures the optional TCP mutation/control API listener used by local automation and SAM enrollment bootstrap. |

`ControlAPI` defaults to `127.0.0.1:65432` with source admission limited to
`127.0.0.1/32` and `::1/128`. This is intentionally narrow as a network
exposure, but it is still the mutation/control API and it is restricted by
source IP, not by Unix socket filesystem permissions. On multi-user hosts, or
where local process isolation matters, disable the TCP listener and use the
Unix socket instead:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: ControlAPI
metadata:
  name: local
spec:
  enabled: false
```

RR-side SAM enrollment over a protected private underlay can enable TCP
explicitly with a narrow source allowlist:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: ControlAPI
metadata:
  name: sam-enrollment
spec:
  enabled: true
  listenAddress: 10.30.0.10
  port: 65432
  allowCIDRs:
    - 10.30.0.0/24
  tokenFrom:
    file: /usr/local/etc/routerd/secrets/control-api-token
```

`ControlAPI.spec.allowCIDRs` rejects malformed CIDRs, `0.0.0.0/0`, and `::/0`.
The HTTP admission check uses the TCP remote address; forwarded headers are not
trusted for source admission. When `spec.tokenFrom` is set, HTTP clients must
send the token as an `Authorization: Bearer <token>` header. The token source
uses the common secret-source shape (`file`, `env`, and optional `base64`) and
is trimmed before comparison. The Unix socket control API does not use this
HTTP bearer-token check and should remain the default boundary for local
multi-user isolation.

## Observability

| Kind | Role |
| --- | --- |
| `Telemetry` | Declares an external OTLP endpoint and injects OpenTelemetry environment variables into generated service units. |

`Telemetry` describes routerd's own signal export endpoint for metrics, traces,
and logs emitted by managed daemons. `LogSink` describes log forwarding routes
for operational events and observed network logs. When a log sink uses OTLP,
prefer `LogSink.spec.otlp.telemetryRef` so the sink reuses a `Telemetry`
resource instead of duplicating collector endpoints.

## Interfaces

| Kind | Role |
| --- | --- |
| `Interface` | Binds a stable routerd name to an OS interface name and publishes link/address status for downstream resources. |
| `ManagementAccess` | Declares management interfaces and apply-time lockout checks. When present, apply fails if declared interfaces are missing, blocked by firewall zoning, or an enabled WebConsole is bound to all addresses unless `--allow-mgmt-lockout` is set. |
| `PPPoESession` | Defines PPPoE lower-interface settings. |
| `PPPoESession` | Represents a `routerd-pppoe-client` session. |
| `WireGuardInterface` | Represents a WireGuard interface. It can import peer definitions from `SAMNodeSet`, `SAMEnrollmentPolicy`, or `SAMRRSet` with `peersFrom` when WireGuard encryption is selected. |
| `WireGuardPeer` | Represents a WireGuard peer. |
| `TailscaleNode` | Configures a local Tailscale node for exit-node and subnet-router advertisement through a managed systemd unit. |
| `IPsecConnection` | Defines a cloud VPN oriented strongSwan connection. |
| `VRF` | Represents a Linux VRF device and route table. |
| `VXLANTunnel` | Represents a VXLAN tunnel. |

`PPPoESession.spec.enabled: false` keeps the PPPoE definition renderable but stops
routerd from starting the managed pppd unit. This is useful for a fallback path
that should remain available for manual testing without consuming a line's
PPPoE session slot during normal operation.

`TailscaleNode` can use `authKey` for one-shot bootstrap, but production
configs should prefer `authKeyEnv` and `authKeyFile` so the secret value stays
outside the YAML and the Git history. If neither is set, routerd assumes
`tailscaled` is already logged in and only reapplies the advertised node
options. Tailscale's default UDP/41641 port is reserved when this kind is
present, so WireGuard listen ports must use a different value. The Tailscale
how-to covers the full setup flow.

`WireGuardInterface` accepts `privateKeyFile` so the private key can stay out of
the router YAML. If the file is absent, non-dry-run apply generates it with mode
`0600` and never overwrites an existing non-empty key. `WireGuardPeer` also
accepts `presharedKeyFile` for optional peer PSKs; inline key fields are
intended for examples and tests. On FreeBSD, routerd renders an rc.d service
that creates the
`wg` interface, loads the key from that file, applies peers, and then assigns
declared static addresses for the WireGuard interface.
`WireGuardInterface.spec.peersFrom` references `SAMNodeSet/<name>`,
`SAMEnrollmentPolicy/<name>`, or `SAMRRSet/<name>` and derives peers from
node-set WireGuard identity, accepted enrollment claims, or RRSet member
WireGuard identity. This is the WireGuard-specific materialization path only;
plain IPIP/GRE SAM transport uses `SAMTransportProfile` and does not require
WireGuard peers. Static `WireGuardPeer` resources with the same `metadata.name`
override generated peers.

Kernel modules and systemd-networkd/resolved adoption drop-ins are derived from
router resources. If a config still contains the removed `KernelModule`,
`NetworkAdoption`, `Link`, or `NixOSHost` kinds, routerd returns an error
instead of silently ignoring the input.

## WAN Addressing and Delegation

| Kind | Role |
| --- | --- |
| `IPv4StaticAddress` | Assigns a static IPv4 address. |
| `VirtualAddress` | Declares an IPv4 `/32` or IPv6 `/128` VIP. `spec.family` is `ipv4` or `ipv6`; `mode: vrrp` uses keepalived on Linux and CARP on FreeBSD. |
| `DHCPv4Client` | DHCPv4 lease, IPv4 address, and optional default route managed by `routerd-dhcpv4-client`. |
| `DHCPv6Address` | Represents DHCPv6 IA_NA intent for platform renderers. |
| `DHCPv6PrefixDelegation` | DHCPv6-PD lease managed by `routerd-dhcpv6-client`. |
| `DHCPv6PrefixDelegationLeaseSync` | Mirrors a `DHCPv6PrefixDelegation` client lease snapshot from an active node to standby nodes. |
| `DHCPv6Information` | DHCPv6 information request result, including DNS, SNTP, domain search, and AFTR observations. |
| `IPv6DelegatedAddress` | Derives a LAN-side address from a delegated prefix. |
| `IPv6RAAddress` | Represents IPv6 addresses learned from RA/SLAAC. |

`DHCPv6PrefixDelegation` no longer selects an OS DHCPv6 client. DHCPv6-PD is
owned by `routerd-dhcpv6-client`. Set `spec.clientDUID` to a plain hex DUID when
the DHCPv6 client identity must stay fixed across HA nodes.

## LAN Services

| Kind | Role |
| --- | --- |
| `DHCPv4Server` | Provides a dnsmasq DHCPv4 service and optional address pool. |
| `DHCPv4ServerLeaseSync` | Mirrors the dnsmasq lease file derived from a `DHCPv4Server` resource. |
| `DHCPv4Reservation` | Reserves an IPv4 address for a MAC address. |
| `DHCPv4Relay` | Represents dnsmasq DHCPv4 relay. |
| `IPv6RouterAdvertisement` | Generates RA, PIO, RDNSS, DNSSL, M/O flags, MTU, preference, and lifetimes. |
| `RogueRADetector` | Auto-derived status resource that reports non-self IPv6 Router Advertisements observed on an RA-serving interface. |
| `DHCPv6Server` | Provides dnsmasq DHCPv6/RA service in `stateless`, `stateful`, `both`, or `ra-only` mode. |
| `DHCPv6ServerLeaseSync` | Mirrors the dnsmasq lease file derived from a `DHCPv6Server` resource. |
| `DNSZone` | Owns a local authoritative zone with manual and DHCP-derived records. |
| `DNSResolver` | Owns a `routerd-dns-resolver` daemon instance, listen profiles, cache, metrics, and query logging. |
| `DNSForwarder` | Declares one DNS match rule for a resolver. It either serves one or more `DNSZone` resources or forwards to named `DNSUpstream` resources. |
| `DNSUpstream` | Declares one reusable upstream endpoint using `udp`, `tcp`, `dot`, or `doh`, with optional status-derived addresses, bootstrap resolvers, TLS name, and source interface. |

Android does not use DHCPv6 DNS configuration, so IPv6 LANs should publish
RDNSS through `IPv6RouterAdvertisement.spec.rdnss`.

dnsmasq is limited to DHCPv4, DHCPv6, relay, and RA. DNS answering and
forwarding belongs to `DNSResolver`.
LAN DNS suffixes can be tied to a local zone by referencing
`DNSZone/<name>.zone` from `DHCPv4Server.spec.domainFrom`,
`IPv6RouterAdvertisement.spec.dnsslFrom`, and
`DHCPv6Server.spec.domainSearchFrom`.

`DNSResolver.spec.listen[].sources` lists `DNSForwarder` names for that
listener. If the list is omitted, the listener uses every `DNSForwarder` that
references the resolver. `DNSResolver.spec.sources` is no longer accepted in
user YAML; split old inline entries into `DNSForwarder` and `DNSUpstream`.

`DNSForwarder.spec.match` contains domain matches such as `home.example` or
`.` for the default upstream. `spec.zoneRefs` serves local `DNSZone` resources;
`spec.upstreams` forwards to `DNSUpstream` resources. DNSSEC validation is
declared on `DNSForwarder.spec.dnssecValidate`.

`DNSUpstream.spec.protocol` is `udp`, `tcp`, `dot`, or `doh`. `addressFrom`
can derive UDP upstream addresses from resources such as
`DHCPv6Information/<name>.dnsServers`. `sourceInterface` binds outgoing DNS
queries to a Linux interface name, and `bootstrap` or `bootstrapFrom` supply
resolver addresses for DoH or DoT endpoint name resolution.

## DS-Lite, Routes, and NAT

| Kind | Role |
| --- | --- |
| `DSLiteTunnel` | Creates an `ip6tnl` tunnel to an AFTR. The AFTR can be static IPv6, FQDN, or DHCPv6 information. |
| `TunnelInterface` | Creates a trusted Linux L3 underlay tunnel device for hybrid overlay delivery. `mode` supports `ipip`, `gre`, and IPIP-over-UDP `fou`/`gue`; `fou`/`gue` require `encapSport` and `encapDport`. |
| `OverlayPeer` | Describes an on-prem or cloud overlay peer and the local underlay used to reach it. |
| `HybridRoute` | Lowers non-default remote IPv4 prefixes through an `OverlayPeer` into managed `IPv4Route` resources. |
| `MobilityPool` | Declares the CloudEdge mobility intent: pool prefix, federation group, node-to-site membership or `membersFrom` sources, BGP delivery policy, optional reusable cloud capture profiles, local value expansion, and provider trap placement. routerd derives BGP `/32` advertisements and provider trap action plans from observed facts and BGP best paths. |
| `MobilityMemberSet` | Groups shared identity-only MobilityPool members (`nodeRef`, `site`, `role`, optional placement/maintenance) so leaves can import them with `MobilityPool.spec.membersFrom` and keep only local capture/discovery details inline. |
| `SAMNodeSet` | Defines the shared SAM fabric node identity registry: node identity, optional site/role, Event Federation endpoint, SAM transport endpoint, and non-secret WireGuard peer identity. Follow-on controllers use it as the single source for generated EventPeer, WireGuardPeer, SAM transport peers, and MobilityPool members. |
| `SAMRRSet` | Declares a route-reflector set: RR member nodeRefs, generic endpoints, tunnel identities, optional WireGuard identity, and shared enrollment/MobilityPool/route-admission references. It never lists leaves. |
| `SAMEnrollmentPolicy` | Authorizes SAM transport enrollment claims for a hub/RR. It binds a transport profile, optional RRSet, join-token source, join audience, tunnel and endpoint prefixes, optional leaf ID pattern, TTL/revocation policy, optional WireGuard materialization settings, and authorized MobilityPool references. |
| `SAMEnrollmentClaim` | Carries one leaf enrollment payload: leaf identity, join nonce/timestamp/HMAC, RRSet reference, tunnel address, endpoint, optional BGP identity, optional MobilityPool-owned `/32`s, optional WireGuard credentials, expiry, and revocation state. |
| `SAMEnrollmentClient` | Runs leaf-side bootstrap/refresh: submits a local `SAMEnrollmentClaim` to bootstrap RR endpoints, fetches the allowed `SAMRRSet`, and persists it as dynamic state only when missing, near expiry, or claim material changes. |
| `SAMTransportProfile` | Declares this router's stable `selfNodeRef`, `mode` (`ipip`, `gre`, `fou`, or `gue`), `encryption` (`none` or `wireguard`), inner tunnel prefix, underlay interface, BGP router, and SAM transport peers. `fou`/`gue` use the existing `TunnelInterface` FOU/GUE path and require `encapSport` and `encapDport`. It can import topology and peer endpoints from `SAMNodeSet`, `SAMPeerGroup`, `SAMEnrollmentPolicy`, or `SAMRRSet` with `peersFrom`. routerd derives per-peer `TunnelInterface`, endpoint `/32` `IPv4Route`, and, unless `spec.bgp.generatePeers: false`, `BGPPeer` resources through a replace-on-reconcile `DynamicConfigPart`. |
| `AddressMobilityDomain` | Low-level compatibility SAM resource that defines an IPv4 prefix for hand-authored selective-address configs; full L2 extension is not supported. |
| `CloudProviderProfile` | Describes provider capabilities and external-command auth for declarative address capture planning. |
| `RemoteAddressClaim` | Low-level compatibility SAM resource that declares one mobile IPv4 `/32`, its capture mechanism, and legacy route delivery over an `OverlayPeer`. |
| `IPAddressSet` | Defines reusable IP address sets from literal addresses and FQDNs. Linux nftables renderers materialize these as named sets for firewall, redirect, NAT, and policy-routing consumers. |
| `IPv4Route` | Adds IPv4 routes, including DS-Lite defaults and explicit drop routes. |
| `ClusterNetworkRoute` | Expands Kubernetes Pod and Service CIDRs into static IPv4 routes through worker next hops. |
| `BGPRouter` | Declares a local BGP router. The current backend is a long-lived `routerd-bgp` GoBGP daemon with default-deny import policy. |
| `BGPPeer` | Declares GoBGP-managed BGP peers for a `BGPRouter`, for example Kubernetes BGP speakers. |
| `BGPDynamicPeer` | Declares a bounded GoBGP dynamic-neighbor accept range for hub/RR fabrics. It lets an RR accept passive BGP sessions from leaf source prefixes without pre-declaring every leaf as a `BGPPeer`. |
| `BFD` | Declares one BFD session intent. On Linux, routerd renders FRR `bfdd` configuration and records observed BFD state without deconfiguring referenced GoBGP peers. |
| `NAT44Rule` | Performs IPv4 NAPT in the nftables `routerd_nat` table. |
| `NAT44SessionSync` | Mirrors selected NAT44 conntrack sessions from an active node to standby nodes over SSH. |
| `PortForward` | Publishes one WAN-side IPv4 TCP/UDP port to one internal IPv4 target with DNAT. |
| `IngressService` | Publishes one WAN-side IPv4 TCP/UDP service. Multiple backends, TCP/HTTP health checks, and `failover`, `sourceHash`, or `random` backend selection are accepted. |
| `LocalServiceRedirect` | Redirects LAN-origin IPv4/IPv6 traffic for `IPAddressSet` destinations to a local router port. This is intended for plaintext DNS/NTP interception without touching DoH or DoT ports. |
| `EgressRoutePolicy` | Represents default-route selection, marked IPv4 policy routing, and hash-based multi-target egress routing. |

`EgressRoutePolicy` supports `destinationSetRefs` and `excludeDestinationSetRefs`
in addition to CIDR fields. Use them to steer or exclude FQDN-backed destination
sets without expanding addresses directly into the policy resource. Use
`mode: priority` for default-route failover, `mode: mark` for one marked route
table, and `mode: hash` or `candidates[].targets` for source/destination hash
distribution across multiple route tables.

`HybridRoute` is intentionally conservative in the MVP: it rejects default
routes, accepts only the main table, and lowers IPv4 destinations into the
existing `IPv4Route` controller path instead of installing routes directly.

CloudEdge Mobility keeps the operator-authored surface declarative:
`MobilityPool` is the high-level address/capture intent,
`MobilityMemberSet` is a reusable shared member list,
`SAMNodeSet` is the write-once node identity registry for generated peers,
`SAMRRSet` is the shared RR admission-set intent,
`SAMEnrollmentPolicy`/`SAMEnrollmentClaim` are the generic leaf transport
enrollment boundary, `SAMEnrollmentClient` is the leaf bootstrap/refresh
controller, `SAMTransportProfile` is the high-level transport/BGP
intent, federation events
are observed facts, and BGP best paths are the mobility ownership/delivery view.
When an RR HTTP `ControlAPI` requires `tokenFrom`, leaf
`SAMEnrollmentClient.spec.controlAPITokenFrom` carries the same bearer token on
claim submit and RRSet fetch requests.
The mobility planner derives BGP `/32` advertisements and provider trap action
plans; operators should not hand-author per-address paths or capture procedures
for the mobility control plane. `AddressMobilityDomain` and `RemoteAddressClaim`
remain supported as lower-level SAM compatibility Kinds outside the MobilityPool
BGP path.

`MobilityPool.spec.deliveryPolicy.mode` defaults to `bgp`; route-mode
MobilityPool planning has been removed from the mobility mainline.
For CloudEdge Mobility, write the self site completely and keep remote sites
identity-only: remote members normally need only `nodeRef`, `site`, `role`, and
optional `placement` / `maintenance`. This is the same shape as BGP peering:
each node needs to know who the peers are, not the remote provider NICs and
subnets. `spec.profiles.cloudCaptures` stores reusable self-site cloud capture
defaults; `spec.values` stores non-secret local identifiers; `capture.targetFrom`
and `ownershipDiscovery.subnetRefFrom` project those local values into generated
provider action targets and discovery scope. Explicit member fields override
profile defaults.
`members[].capture.target` carries non-secret provider target identifiers into
generated background provider action plans.
On-prem proxy-ARP members can set `members[].ownershipDiscovery.mode:
onprem-l2` with multiple `sources` such as `dhcpv4-lease`, `arp-observer`,
`on-demand-arp`, and `pve-svnet`. These event sources are merged into the same
`routerd.client.ipv4.observed` ownership facts consumed by the BGP mobility
planner, so PVE IPAM environments do not have to rely on DHCP lease visibility
alone. `on-demand-arp` uses source `scanInterval` for a low-rate proactive
prefix sweep, probing one target per interval so quiet existing clients can be
discovered without manual owner-side ARP traffic.
Passive on-prem sources are not authoritative by default. If operations accept
an empty L2 segment after the sources have been armed, set
`ownershipDiscovery.allowEmptyAfter`; the pool reports a non-authoritative
`Complete` discovery snapshot with `discoveryResultCount: 0` while it is fresh.
`members[].placement` can group same-provider cloud routers into deterministic
active/standby capture placement; `members[].maintenance.drain` removes that
member from active selection. All nodes in a mobility demo should receive the
same `MobilityPool` identity and placement set so they project the same placement
decision. The old remote-full inline style is still accepted for pre-release
compatibility, but `routerctl validate`, plan, and apply warn when a remote member
contains local capture or discovery details. Future pre-release configs may
require identity-only remote members.

Selective Address Mobility does not configure firewall or NAT policy. Operators
compose firewall/NAT by referencing literal addresses in the existing firewall
and NAT resources. Provider action plans are review artifacts until they are
imported into the action journal and allowed by `ProviderActionPolicy`, approval,
allowlists, and an executor plugin.

routerd derives reverse path filter sysctls, tunnel MTU, RA MTU, and TCP MSS
clamping from router role, tunnel, firewall zone, and RA/DHCPv6 resources.
Configs should declare the tunnel and LAN/WAN intent rather than separate
`IPv4ReversePathFilter` or `PathMTUPolicy` resources.
For trusted overlay paths that need a non-TCP IPv4 PMTU black-hole workaround,
`OverlayPeer.spec.pathMTU.forceFragmentIPv4` or
`TunnelInterface.spec.pathMTU.forceFragmentIPv4` can enable a default-off Linux
nftables `routerd_forcefrag` table. It clears DF only for oversized IPv4 packets
on the derived forwarded path. It is supported only for `wireguard`, `ipip`,
`gre`, `fou`, and `gue` overlay underlays.
If an externally managed source interface has a lower MTU, such as `tailscale0`,
set `Interface.spec.mtu`; routerd uses it only for that source path instead of
lowering unrelated LAN paths.

`ClusterNetworkRoute` is a helper for Kubernetes nodes that need static routes
for Pod CIDRs and Service CIDRs instead of dynamic routing. routerd expands each
CIDR and each `spec.via[]` next hop into managed `IPv4StaticRoute` resources.
Equal `weight` values produce equal route metrics for ECMP-capable platforms;
different weights become different metrics so higher-weight next hops are
preferred and lower-weight next hops act as fallback routes.

`EgressRoutePolicy` supports `excludeDestinationCIDRs`. Use it to keep LAN,
management, HGW LAN, and RFC 1918 destinations out of policy routing.

`FirewallRule` supports `destinationSetRefs` and `excludeDestinationSetRefs`
in addition to destination CIDR narrowing. Use these fields to accept, drop, or
reject traffic for reusable FQDN-backed sets without expanding addresses into
each rule. Stateful rule expressions also support `sourcePorts`,
`destinationPorts`, ICMP / ICMPv6 type matching, `rateLimit`, and `connLimit`.
`port` remains accepted as a single destination port shorthand; new examples
prefer `destinationPorts`.

`NAT44Rule` supports simple source NAT with `outboundInterface`,
`sourceCIDRs`, and `translation`, and policy-aware NAT with `type`,
`egressInterface` or `egressPolicyRef`, and `sourceRanges`. It also supports
`destinationCIDRs`, `destinationSetRefs`, `excludeDestinationCIDRs`, and
`excludeDestinationSetRefs`. This allows internet traffic to be masqueraded
while private routed destinations or reusable address sets stay un-NATed.

`NAT44SessionSync` uses `conntrack --dump -o extended` for selected SNAT
addresses and restores the parsed tuple and conntrack mark on standby targets.
It is intended for active-to-standby HA sync and is usually gated with
`spec.when`.

`BGPRouter`, `BGPPeer`, and `BGPDynamicPeer` currently use the long-lived
`routerd-bgp` daemon.
routerd maps the resource specs directly to typed GoBGP API objects over a
local gRPC Unix socket and observes status through `ListPeer` and `ListPath`;
it does not render FRR text config, run `frr-reload.py`, parse `vtysh`, or use
GoBGP's file configuration format. `apply` renders host artifacts only
and reports BGP as serve-managed. `routerctl show bgp` summarizes routers,
peers, message counters, route selection state, and last errors from stored
GoBGP observation.
Prefix status includes `best`, `valid`, `installed`, `stale`, `nextHop`, and
observed communities. Learned IPv4 best paths that match
`spec.importPolicy.allowedPrefixes` are installed into the kernel FIB with
routerd-owned protocol and metric values. By default, GoBGP import policy
rewrites accepted eBGP next-hops to the learning peer address
(`spec.importPolicy.nextHopRewrite: peer-address`), matching the former FRR
`set ip next-hop peer-address` behavior so Kubernetes edge routes install as
peer-address ECMP even when the advertised next-hop is a downstream speaker.
Set `nextHopRewrite: unchanged` only when the advertised next-hop is meant to
be installed directly. Equal best paths for the same prefix are installed as
ECMP next hops.

`BGPDynamicPeer` is an RR-side BGP acceptor for GoBGP dynamic neighbors. Its
`spec.listen.sourcePrefixes` controls which source addresses may open BGP TCP
sessions. It is not route admission; accepted NLRI is still constrained by
`spec.importPolicy.allowedPrefixes`, and exported routes by
`spec.exportPolicy.allowedPrefixes`. `BGPDynamicPeer` does not carry WireGuard
public keys, allowed IPs, SAM tunnel addresses, leaf identity, TTL/revocation,
or MobilityPool ownership authorization.

`SAMRRSet`, `SAMEnrollmentPolicy`, and `SAMEnrollmentClaim` are the SAM
control-plane intent for dynamic hub/leaf fabrics. The primary private-underlay
path uses `SAMTransportProfile.spec.peersFrom` to consume `SAMRRSet` or
accepted enrollment claims and generate existing `TunnelInterface` and
`BGPPeer` resources. On an RR that accepts sessions through `BGPDynamicPeer`,
set `SAMTransportProfile.spec.bgp.generatePeers: false` so SAM creates transport
tunnels without generated per-leaf BGP peers. Enrollment can be authenticated
with a policy
`joinTokenFrom` and claim `joinNonce`, `joinTimestamp`, and `joinHMAC` fields.
When the referenced secret is readable at validation time, `joinHMAC` is
verified as lowercase hex HMAC-SHA256 over the canonical claim join payload:
policy/RRSet refs, leaf ID, audience, nonce, timestamp, tunnel address,
endpoint, owned `/32`s, BGP identity, and optional WireGuard credentials.
If `encryption: wireguard` is selected, a hub can also set
`WireGuardInterface.spec.peersFrom: [{resource: SAMEnrollmentPolicy/<name>}]`;
routerd then materializes `WireGuardPeer` entries only for non-revoked,
non-expired claims whose leaf ID and tunnel address satisfy the policy and that
carry WireGuard credentials. The claim's `spec.mobility.ownedAddresses` are
authorization input for `MobilityPool` ownership and must fall within a pool
referenced by the policy when that policy is present in the same config. BGP
route admission remains a `BGPDynamicPeer`/BGP policy concern.

`BGPRouter.spec.convergenceProfile: fast` is intended for Kubernetes/edge
routers that prefer quick convergence over graceful restart stale-path
retention: it derives fast peer timers and disables graceful restart unless
`spec.gracefulRestart.enabled` is explicitly set. Import policy is default
deny; add `spec.importPolicy.allowedPrefixes` for Kubernetes LoadBalancer pools.
`BGPPeer.spec.ebgpMultihop` enables non-direct eBGP sessions such as loopback
peering or lab-to-production validation across routed hops. Omit it or set `0`
or `1` for the direct eBGP default; set a value from `2` to `255` to configure
the GoBGP multihop TTL for that peer group.
`BGPRouter` can use a router ID that differs from the TCP source address, but
peer routers must still configure the address that the host actually uses as
its BGP source. Check `ip route get <peer-address>` on Linux when the LAN has
multiple addresses, and prefer a router ID that matches that operational source
unless there is a clear reason not to.

`BGPRouter` can advertise connected and static IPv4 routes with independent
`allowedPrefixes`; only prefixes explicitly listed in
`BGPRouter.spec.exportPolicy.allowedPrefixes` or the redistribute allow-lists
are added to GoBGP as local paths. BGP community policy can be declared on the
router or peer with `communities.send`, `communities.accept`, and
`communities.set.in/out`; observed route communities are stored in status when
GoBGP reports them. The watcher defaults to a 15 second controller interval and
4096 observed prefixes, and `BGPRouter.spec.watcher` can tune `pollInterval`,
`maxPrefixes`, and `peerStateChangeThrottle`; validation rejects intervals below
3 seconds and prefix caps of 1,000,000 or more. The GoBGP MVP supports one
`BGPRouter` per router and does not yet support `spec.vrf`; unsupported
multi-router or VRF resources are reported as Pending instead of being silently
ignored. BFD resources are applied through the Linux FRR `bfdd` bridge and
their observed status gates the referenced GoBGP peers. `spec.listen.address`
and `spec.listen.port` bind the `routerd-bgp` GoBGP listener.

`VirtualAddress` uses keepalived on Linux and CARP on FreeBSD for
`mode: vrrp`. `spec.family: ipv4` requires an IPv4 `/32`, and
`spec.family: ipv6` requires an IPv6 `/128`. IPv6 VIPs render keepalived VRRPv3 with
`family inet6`; FreeBSD renders `inet6` CARP aliases. Linux VRRP uses explicit
unicast peers and defaults to
`nopreempt`; FreeBSD CARP uses multicast advertisements on the parent interface,
so `spec.vrrp.peers` is ignored there. Set `spec.vrrp.preempt: true` only when
automatic failback is intended. Advertisement and failback timing use routerd
profile defaults rather than per-resource low-level timing fields.
The resource status records the rendered backend, VIP address, VRID, base
priority, track-adjusted priority, and generated config path when a file-backed
backend is used. `track` lowers priority when referenced resources such as
`BGPRouter`, `BGPPeer`, or `IngressService` are not healthy. Track entries use
hysteresis: by default three consecutive unhealthy observations are required to
apply a penalty and two consecutive healthy observations are required to clear
it. `spec.hostname` can publish VIPs into matching DNSResolver-served `DNSZone`
records; IPv4 VIPs create A records and IPv6 VIPs create AAAA records. Set
`spec.externalDNS: true` when the name is owned by an outside DNS system; routerd
will keep validating the hostname syntax but will not try to publish it or warn
about missing DNSZone coverage. `routerctl show vrrp` shows role, priority,
peers, and transition age. NixOS remains groundwork until a native
service-manager module owns the same host artifacts.

### VRRP production tuning

Use `preempt: true` only for control-plane VIPs where automatic failback is
worth the operational sensitivity. For home-router or DS-Lite/LAN service VIPs,
prefer the default non-preemptive behavior so the backup keeps the VIP until it
fails or is intentionally moved. See `examples/vrrp-tuning-presets.yaml`
for complete resource fragments.

`BGPPeer.spec.password` is passed to the GoBGP peer as the TCP MD5
authentication password. Prefer `BGPPeer.spec.passwordFrom` for production
configs so the routerd YAML does not contain the shared secret.
`passwordFrom.file` reads a local root-owned secret file and `passwordFrom.env`
reads an environment variable; `base64: true` decodes either source before
applying it to the long-lived BGP daemon.

`VirtualAddress.spec.vrrp.authentication` is rendered into keepalived as
`auth_pass` and into FreeBSD CARP as `pass`. Prefer
`VirtualAddress.spec.vrrp.authenticationFrom` for production configs.
`authenticationFrom.file` reads a local secret file and `authenticationFrom.env`
reads an environment variable; `base64: true` decodes either source before
rendering. Treat rendered keepalived config and host interface state as secrets. VRRP
authentication is deprecated in VRRPv3 (RFC 5798); routerd assumes L2 isolation
and recommends using authentication only when it is still required by the
surrounding network policy or to guard against simple misconfiguration.

`PortForward` and `IngressService` render DNAT on Linux nftables and FreeBSD pf.
Set `spec.hairpin.enabled: true` with `spec.hairpin.interfaces` to also allow
LAN clients to reach the service through the WAN address. Hairpin mode requires
`listen.address` or `listen.addressFrom`; routerd renders the LAN-side DNAT plus
the return-path masquerade/NAT reflection rule. `listen.addressFrom` and backend
`addressFrom` can reference statically rendered address resources such as
`IPv4StaticAddress/<name>.address` or `VirtualAddress/<name>.address`.
`IngressService` treats omitted `spec.hairpin.mode` as `auto`: when the listen
address and the selected backend are on a prefix declared for the same listen
interface, routerd automatically emits the same-interface return-path SNAT
needed for LAN clients to use the VIP. If no listen-interface prefix is
declared in YAML, auto mode also treats private IPv4 listen/backend addresses
in the same `/24` as hairpin-required, which covers live ISO deployments where
the interface address is inherited from the boot environment. Set
`spec.hairpin.mode: off` to suppress that behavior, or `manual` with
`interfaces` for explicit NAT reflection.
`IngressService` accepts multiple backends, TCP/HTTP health checks, and
`failover`, `sourceHash`, or `random` backend selection. The runtime controller
resolves backend FQDNs, falls back to the previous resolved IPv4 address when DNS
temporarily fails, records backend health in status, and writes either one
active backend or a healthy backend distribution. When only one backend remains
healthy, `sourceHash` and `random` degrade to failover. Linux nftables rendering
uses the status-selected backend set on the next NAT reconcile and emits
`jhash ip saddr` for `sourceHash` or `numgen random` for `random`. Existing
conntrack entries are not flushed, so established flows can stay on the old
backend while new flows use the selected backend. Validator checks reject
listen-port collisions between `IngressService`, `LocalServiceRedirect`, and
routerd-managed local daemons on the same protocol/interface. `spec.hostname`
can also publish the listen address into matching DNSResolver-served `DNSZone`
records. Set `spec.externalDNS: true` when AD DNS or another external DNS system
owns the name. `routerctl show ingress` shows active backend and per-backend
health; `routerctl show ingress --verbose` also samples the live dataplane
(`ip_forward`, nftables DNAT/SNAT rule counts, and matching conntrack flows).
The `DETAIL` column reports `hairpinMode`, whether hairpin is required, and
whether the expected nftables SNAT rule is present or missing.
Ingress, NAT-like resources, DS-Lite, IPv6 PD/RA, and routing resources derive
the runtime sysctls they need, including forwarding, redirect suppression,
reverse-path-filter exceptions, and per-interface RA acceptance. `routerd
apply` plans and renders those derived settings but mutates the host only
for explicit `Sysctl` / `SysctlProfile` escape hatches; `routerd serve` applies
the derived runtime settings during controller reconcile. During
maintenance, `routerctl drain ingress/<service> backend=<name>
--duration 10m` marks a backend as drained in the runtime state store. The
controller treats it as unhealthy with reason `Drained` until the duration
expires or `routerctl undrain ingress/<service> backend=<name>` clears the
state.

`IPAddressSet` writes literal IPv4/IPv6 addresses into nftables named sets when
the ruleset is rendered. FQDN `A`/`AAAA` records are resolved by the runtime
controller, which refreshes referenced nftables sets in place without reloading
the whole firewall, NAT, or policy table. The next refresh is scheduled at half of the
observed minimum DNS TTL, but never sooner than 60 seconds. `refreshInterval`
can cap that delay when a set should be refreshed more aggressively.

Entries in `IPAddressSet.spec.names` are exact DNS names only. `microsoft.com`
means the `A`/`AAAA` records for `microsoft.com` itself; it does not include
`www.microsoft.com`, `login.microsoft.com`, `*.microsoft.com`, or deeper
subdomains. Wildcard and suffix-style service matching needs a DNS-observation
or provider endpoint-feed resource rather than plain FQDN resolution.

`LocalServiceRedirect` renders Linux nftables `redirect` rules in `prerouting`
only. It matches packets arriving from one declared interface and an
`IPAddressSet` destination. Router-originated traffic and health checks do not
traverse this hook.

Example:

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: PortForward
metadata:
  name: web-admin
spec:
  listen:
    interface: wan
    addressFrom:
      resource: IPv4StaticAddress/wan-ip
      field: address
    protocol: tcp
    port: 8443
  target:
    address: 172.18.1.88
    port: 443
  hairpin:
    enabled: true
    mode: manual
    interfaces:
      - lan
```

## Coordination

| Kind | Role |
| --- | --- |
| `HealthCheck` | Measures reachability through `routerd-healthcheck` or the development embedded runner. |
| `EgressRoutePolicy` | Selects the highest-weight ready egress candidate. Candidates can include gateway fields and health checks. |
| `EventGroup` | Declares one CloudEdge Event Federation bus identity. It can derive push peers from `SAMNodeSet` with `peersFrom`, while static `EventPeer` resources remain available as overrides. |
| `EventPeer` | Declares a static Event Federation push peer for bootstrap or explicit override use. |
| `EventSubscription` | Consumes matched federation events and invokes a plugin that emits a `DynamicConfigPart`. |
| `EventRule` | Evaluates event streams with all_of, any_of, sequence, window, absence, throttle, debounce, and count. |
| `DerivedEvent` | Emits virtual events derived from multiple resource states. |
| `SelfAddressPolicy` | Selects a self address for protocols that need one. |

`HealthCheck.spec.enabled: false` renders the daemon unit but disables and stops it.
`EgressRoutePolicy` candidates also accept `enabled: false`; disabled
candidates are not selected even if their last observed health status is still
Healthy. In `mode: priority`, candidate `weight` remains the first selection
key, `priority` is the tie-breaker and policy-rule priority, and stale
ledger-owned policy-route rules/tables are removed when candidates are removed.

## `spec.when`

Resources that support `spec.when` are included only when the predicate matches
routerd's local state store. The existing single-predicate form remains valid:

```yaml
when:
  state:
    wan.ipv6.mode:
      equals: pd-ready
```

Use `all` for AND and `any` for OR. They can be nested to any depth:

```yaml
when:
  any:
    - all:
        - state:
            dslite.a.health:
              status: set
        - state:
            wan.ipv6.mode:
              in: [pd-ready, address-only]
    - state:
        pppoe.health:
          equals: healthy
```

Each `when` node must be exactly one of `state`, `all`, or `any`. `state` maps
state variable names to `exists`, `equals`, `in`, `contains`, `status`, and
`for` matches. One-element `all` is equivalent to the single-predicate form.
State-management resources are not exposed as config kinds; express conditional
activation directly on the dependent resources with `spec.when`.

`HealthCheck` declares probe intent: target, protocol, cadence, and thresholds.
When a health check is referenced by an `EgressRoutePolicy` candidate or target,
routerd derives the health-check daemon, source binding, and socket mark from
that route target automatically. Platform-specific socket mechanics stay inside
the controller and renderer.

`WebConsole.spec.listenAddressFrom` derives the HTTP listener address from
another resource status, for example `Interface/mgmt.status.ipv4Addresses`.
Use it instead of a literal `listenAddress` when the management address comes
from DHCP, IPAM, or another declarative resource.

## Status Provides Contract

Fields such as `addressFrom`, `gatewayFrom`, `dnsServerFrom`, and
`dependsOn[].field` can reference only fields that the
target kind declares in this contract. The validator rejects missing resources
and fields outside the target kind's `provides` set.

| Kind | Provides |
| --- | --- |
| `BFD` | `peer` (string), `phase` (string) |
| `BGPDynamicPeer` | `acceptedRouteCount` (int), `discoveredPeerCount` (int), `discoveredPeers` (objectList), `observedAt` (timestamp), `peerGroup` (string), `phase` (string), `rejectedRouteCount` (int), `rejectedRouteSummary` (object), `sourcePrefixCount` (int), `sourcePrefixes` (stringList) |
| `BGPPeer` | `acceptedPrefixes` (int), `address` (string), `observedAt` (timestamp), `phase` (string), `state` (string) |
| `BGPRouter` | `acceptedPrefixes` (int), `establishedPeers` (int), `observedAt` (timestamp), `peers` (objectList), `phase` (string), `prefixes` (int) |
| `Bridge` | `ifname` (string), `members` (stringList), `phase` (string) |
| `ClientPolicy` | `phase` (string) |
| `ClusterNetworkRoute` | `phase` (string), `pods` (stringList), `services` (stringList) |
| `DHCPv4Client` | `currentAddress` (string), `defaultGateway` (string), `device` (string), `dnsServers` (stringList), `domain` (string), `expiresAt` (timestamp), `gateway` (string), `interface` (string), `leaseTime` (int), `ntpServers` (stringList), `phase` (string), `rebindAt` (timestamp), `renewAt` (timestamp) |
| `DHCPv4Relay` | `phase` (string) |
| `DHCPv4Reservation` | `address` (string), `hostname` (string), `phase` (string) |
| `DHCPv4Server` | `configPath` (string), `dnsServers` (stringList), `domain` (string), `dryRun` (bool), `interface` (string), `ntpServers` (stringList), `phase` (string) |
| `DHCPv4ServerLeaseSync` | `command` (string), `dryRun` (bool), `phase` (string), `sourceCount` (int), `sources` (objectList), `syncedAt` (timestamp), `targetCount` (int), `targets` (objectList) |
| `DHCPv6Address` | `address` (string), `interface` (string), `phase` (string) |
| `DHCPv6Information` | `aftrName` (string), `dnsServers` (stringList), `domainSearch` (stringList), `phase` (string), `sntpServers` (stringList), `source` (string) |
| `DHCPv6PrefixDelegation` | `aftrName` (string), `currentPrefix` (string), `dnsServers` (stringList), `domainSearch` (stringList), `interface` (string), `phase` (string), `sntpServers` (stringList) |
| `DHCPv6PrefixDelegationLeaseSync` | `command` (string), `dryRun` (bool), `phase` (string), `sourceCount` (int), `sources` (objectList), `syncedAt` (timestamp), `targetCount` (int), `targets` (objectList) |
| `DHCPv6Server` | `configPath` (string), `dnsServers` (stringList), `dryRun` (bool), `interface` (string), `phase` (string), `sntpServers` (stringList) |
| `DHCPv6ServerLeaseSync` | `command` (string), `dryRun` (bool), `phase` (string), `sourceCount` (int), `sources` (objectList), `syncedAt` (timestamp), `targetCount` (int), `targets` (objectList) |
| `DNSForwarder` | `phase` (string), `resolver` (string), `upstreams` (stringList) |
| `DNSResolver` | `listenAddresses` (stringList), `listeners` (int), `phase` (string), `sources` (int), `updatedAt` (timestamp) |
| `DNSUpstream` | `address` (string), `phase` (string), `url` (string) |
| `DNSZone` | `pendingRecords` (objectList), `phase` (string), `records` (int), `updatedAt` (timestamp), `zone` (string) |
| `DSLiteTunnel` | `aftrIPv6` (string), `aftrName` (string), `device` (string), `dryRun` (bool), `innerLocalIPv4` (string), `innerRemoteIPv4` (string), `interface` (string), `localIPv6` (string), `localInterface` (string), `mtu` (int), `phase` (string), `tunnelName` (string) |
| `AddressMobilityDomain` | `mode` (string), `peerRef` (string), `phase` (string), `prefix` (string) |
| `CloudProviderProfile` | `capabilities` (stringList), `phase` (string), `provider` (string) |
| `DerivedEvent` | `phase` (string), `topic` (string) |
| `EgressRoutePolicy` | `advisory` (bool), `candidates` (objectList), `dryRun` (bool), `family` (string), `lastTransitionAt` (timestamp), `phase` (string), `role` (string), `selectedCandidate` (string), `selectedDevice` (string), `selectedGateway` (string), `selectedGatewaySource` (string), `selectedInterface` (string), `selectedMetric` (int), `selectedRouteTable` (int), `selectedSource` (string), `selectedTargets` (int), `selectedWeight` (int), `updatedAt` (timestamp) |
| `EventGroup` | `group` (string), `listenAddress` (string), `listenPort` (int), `nodeName` (string), `peers` (int), `peersFrom` (objectList), `pendingSources` (stringList), `phase` (string) |
| `EventRule` | `phase` (string), `topic` (string) |
| `FirewallEventLog` | `path` (string), `phase` (string), `sinks` (stringList) |
| `FirewallPolicy` | `phase` (string) |
| `FirewallRule` | `action` (string), `phase` (string) |
| `FirewallZone` | `interfaces` (stringList), `phase` (string) |
| `HealthCheck` | `consecutiveFailed` (int), `lastCheckedAt` (timestamp), `phase` (string), `protocol` (string), `role` (string), `sourceAddress` (string), `sourceInterface` (string), `target` (string) |
| `Hostname` | `hostname` (string), `phase` (string) |
| `HybridRoute` | `defaultRouteUntouched` (bool), `estimatedMTU` (int), `peerRef` (string), `phase` (string), `routes` (objectList) |
| `IPAddressSet` | `addresses` (stringList), `ipv4Addresses` (stringList), `ipv6Addresses` (stringList), `phase` (string), `updatedAt` (timestamp) |
| `IPsecConnection` | `phase` (string) |
| `IPv4Route` | `destination` (string), `device` (string), `dryRun` (bool), `gateway` (string), `metric` (int), `phase` (string), `type` (string) |
| `IPv4StaticAddress` | `address` (string), `dryRun` (bool), `ifname` (string), `interface` (string), `phase` (string) |
| `IPv4StaticRoute` | `destination` (string), `gateway` (string), `interface` (string), `phase` (string) |
| `IPv6DelegatedAddress` | `address` (string), `dryRun` (bool), `interface` (string), `phase` (string), `prefixSource` (string) |
| `IPv6RAAddress` | `address` (string), `interface` (string), `phase` (string) |
| `IPv6RouterAdvertisement` | `configPath` (string), `dryRun` (bool), `interface` (string), `phase` (string), `prefix` (string), `rdnss` (stringList) |
| `RogueRADetector` | `interface` (string), `observedRouters` (string), `packetsSeen` (string), `phase` (string), `rogueCount` (string), `selfMAC` (string) |
| `IPv6StaticRoute` | `destination` (string), `gateway` (string), `interface` (string), `phase` (string) |
| `IngressService` | `activeBackend` (object), `activeBackends` (objectList), `backends` (objectList), `dryRun` (bool), `healthyBackends` (int), `hostname` (string), `listenAddress` (string), `observedAt` (timestamp), `phase` (string), `totalBackends` (int) |
| `Interface` | `addresses` (stringList), `ifname` (string), `ipv4Addresses` (stringList), `ipv6Addresses` (stringList), `macAddress` (string), `phase` (string) |
| `Inventory` | `host` (object), `phase` (string) |
| `LocalServiceRedirect` | `phase` (string) |
| `LogRetention` | `phase` (string), `targets` (objectList), `updatedAt` (timestamp) |
| `LogSink` | `phase` (string), `type` (string) |
| `ManagementAccess` | `interfaces` (stringList), `phase` (string) |
| `MobilityMemberSet` | `memberCount` (int) |
| `MobilityPool` | `addresses` (object), `dynamicSource` (string), `generatedActions` (int), `generatedBGPPaths` (int), `generatedBGPTraps` (int), `groupRef` (string), `memberSet` (object), `membersFrom` (objectList), `pendingSources` (stringList), `placementActive` (bool), `placementActiveNode` (string), `placementGroup` (string), `plannerPhase` (string), `plannerReason` (string), `prefix` (string), `resolvedMemberCount` (int), `deliveryMode` (string), `discoverySelfPrivateIPs` (stringList), `providerActionPhase` (string), `providerActionFailedCount` (int), `providerActionFailedAddresses` (stringList), `providerActionSupersededFailureCount` (int), `providerActionSupersededFailureAddresses` (stringList), `providerActionSupersededFailureReason` (string), `ownershipResolverPhase` (string), `ownershipResolverReason` (string), `ownershipResolverConflictCount` (int), `ownershipResolverConflicts` (objectList), `ownershipResolverOwnerTable` (objectList), `ownershipResolverControlPlaneOwnerTable` (objectList) |
| `SAMNodeSet` | `nodeCount` (int) |
| `SAMRRSet` | `enrollmentPolicyRef` (string), `memberCount` (int), `members` (stringList), `phase` (string) |
| `SAMEnrollmentClaim` | `endpoint` (string), `expiresAt` (timestamp), `leafID` (string), `phase` (string), `revoked` (bool), `rrSetRef` (string), `tunnelAddress` (string) |
| `SAMEnrollmentClient` | `backoff` (string), `claimRef` (string), `lastAttempt` (timestamp), `lastSuccess` (timestamp), `nextAttempt` (timestamp), `observedRRSet` (string), `phase` (string), `reason` (string) |
| `SAMEnrollmentPolicy` | `acceptedClaims` (int), `leafIDs` (stringList), `phase` (string), `skippedClaims` (int) |
| `NAT44Rule` | `dryRun` (bool), `egressInterface` (string), `phase` (string), `snatAddress` (string) |
| `NAT44SessionSync` | `deleteFailed` (int), `deleteOK` (int), `dryRun` (bool), `insertFailed` (int), `insertOK` (int), `mode` (string), `phase` (string), `sessionCount` (int), `snatAddresses` (stringList), `syncedAt` (timestamp), `targetCount` (int), `targets` (objectList) |
| `NTPClient` | `phase` (string), `servers` (stringList), `source` (string), `updatedAt` (timestamp) |
| `NTPServer` | `allowCIDRs` (stringList), `listenAddresses` (stringList), `phase` (string), `servers` (stringList), `source` (string), `updatedAt` (timestamp) |
| `ObservabilityPipeline` | `phase` (string), `signals` (stringList) |
| `OverlayPeer` | `nodeID` (string), `phase` (string), `role` (string), `underlayInterface` (string), `underlayType` (string) |
| `TunnelInterface` | `dryRun` (bool), `encapDport` (int), `encapSport` (int), `ifname` (string), `interface` (string), `local` (string), `mode` (string), `mtu` (int), `phase` (string), `remote` (string), `ttl` (int) |
| `PPPoESession` | `connectedAt` (timestamp), `currentAddress` (string), `device` (string), `dnsServers` (stringList), `dryRun` (bool), `gateway` (string), `interface` (string), `peerAddress` (string), `phase` (string) |
| `Package` | `dryRun` (bool), `packages` (stringList), `phase` (string) |
| `PortForward` | `dryRun` (bool), `listenAddress` (string), `phase` (string), `target` (object) |
| `RouterdCluster` | `leader` (string), `leaseExpiresAt` (timestamp), `phase` (string) |
| `SelfAddressPolicy` | `address` (string), `phase` (string), `source` (string) |
| `RemoteAddressClaim` | `address` (string), `captureType` (string), `deliveryMode` (string), `domainRef` (string), `ownerSide` (string), `peerRef` (string), `phase` (string) |
| `SAMTransportProfile` | `dynamicSource` (string), `generatedBGPPeers` (int), `generatedEndpointRoutes` (int), `generatedTunnels` (int), `innerPrefix` (string), `peers` (objectList), `peersFrom` (objectList), `pendingSources` (stringList), `phase` (string), `selfNode` (string), `topologyNodeRefs` (stringList) |
| `Sysctl` | `dryRun` (bool), `key` (string), `phase` (string), `value` (string) |
| `SysctlProfile` | `dryRun` (bool), `phase` (string), `profile` (string) |
| `TailscaleNode` | `advertiseRoutes` (stringList), `peerCount` (int), `phase` (string), `tailnetName` (string) |
| `Telemetry` | `phase` (string), `signals` (stringList) |
| `TrafficFlowLog` | `path` (string), `phase` (string), `sinks` (stringList) |
| `VRF` | `ifname` (string), `members` (stringList), `phase` (string), `routeTable` (int) |
| `VXLANSegment` | `ifname` (string), `phase` (string), `vni` (int) |
| `VXLANTunnel` | `ifname` (string), `phase` (string), `vni` (int) |
| `VirtualAddress` | `address` (string), `dryRun` (bool), `hostname` (string), `ifname` (string), `phase` (string), `priority` (int), `role` (string), `virtualRouterID` (int) |
| `WebConsole` | `listenAddress` (string), `phase` (string), `port` (int) |
| `WireGuardInterface` | `fwmark` (int), `hostFirewall` (object), `listenPort` (int), `peerCount` (int), `peersFrom` (objectList), `pendingSources` (stringList), `phase` (string), `publicKey` (string), `selfNodeRef` (string) |
| `WireGuardPeer` | `handshakeAgeSeconds` (int), `latestEndpoint` (string), `latestHandshake` (timestamp), `phase` (string), `transferRxBytes` (int), `transferTxBytes` (int) |

## Firewall

| Kind | Role |
| --- | --- |
| `FirewallZone` | Assigns interfaces to zones with `untrust`, `trust`, and `mgmt` roles. |
| `FirewallPolicy` | Represents global firewall behavior such as deny logging. |
| `FirewallRule` | Represents exceptions that cannot be expressed by the role matrix. Supports source CIDRs, destination CIDRs, and `IPAddressSet` destination refs. |
| `ClientPolicy` | Classifies clients by MAC address for guest isolation on Linux nftables. |
| `PortForward` | Adds a single-target ingress DNAT rule and, when routerd manages the firewall table, an internal forward accept rule. Optional hairpin mode adds LAN-side DNAT and return-path SNAT. |
| `IngressService` | Adds the same ingress DNAT path as `PortForward`; multiple backends, `failover` / `sourceHash` / `random` selection, and health-check intent are accepted, with runtime backend state handled by the controller path. Optional hairpin mode matches `PortForward`. |
| `LocalServiceRedirect` | Adds local service redirect rules for `IPAddressSet` destinations. The firewall renderer opens the matching local input ports for the source zone. |

Stateful filtering renders into the nftables `inet routerd_filter` table.
Established traffic, loopback, and required ICMPv6 are always accepted.
routerd derives internal openings needed by DHCP, DNS, DS-Lite, and related
managed resources.

`ClientPolicy` supports `mode: include` for "listed MAC addresses are guests"
and `mode: exclude` for "listed MAC addresses are trusted, everything else on
the interface is guest." `spec.macs` is the short form for guest/trusted MAC
lists. `classification[]` is the structured form; each entry has
`mode: trusted|guest|isolated` and a `match` selector with `macs`,
`ouiPrefixes`, `hostnamePatterns`, or `dhcpFingerprints`. Match fields are ORed.
`ipv4Reservation` can keep address-based rendering stable on platforms that
cannot match Ethernet source addresses. `spec.isolation` can express the common
guest shape: internet allowed, LAN and management denied, and mDNS/SSDP/NetBIOS
discovery blocked. The FreeBSD pf renderer reports this resource as unsupported
because pf does not provide the
same MAC-based routed filtering model.

## Management plane

`ManagementAccess` declares the interfaces and admin source CIDRs that
routerd must keep reachable so a non-dry-run `apply` cannot accidentally
lock the operator out. When at least one `ManagementAccess` is present, the
apply preflight runs the checks below and **fails the apply** unless
`--allow-mgmt-lockout` is set. `validate`, `plan`, and `show` are not
affected, and dry-run apply only reports the findings without blocking.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: ManagementAccess
metadata:
  name: home-mgmt
spec:
  interfaces: [mgmt0]
  allowSourceCIDRs:
    - 192.168.100.0/24
    - fd00:100::/64
  requireWebConsoleBound: true  # default
```

Preflight checks:

| Check | Failure condition |
| --- | --- |
| Interface exists | A declared `interfaces[]` member is not present as an `Interface` resource (the management interface is being removed or renamed). |
| Firewall self-access | The firewall is enabled (at least one `FirewallZone` resource exists), but a declared management interface is not a member of a `FirewallZone` with role `mgmt` or `trust` — the input chain's `policy drop` would block SSH to the router. |
| WebConsole binding | `WebConsole` is enabled and binds to `0.0.0.0` / `::`. With `requireWebConsoleBound: true` (default) this is a fail; otherwise a warn. |

The same checks are surfaced by `routerctl doctor mgmt`, which never
applies anything.

`spec.allowSourceCIDRs` is informational today (recorded in status and
shown by doctor) and is not yet enforced by the firewall guard.

`--allow-mgmt-lockout` is an **emergency override**, intended for the case
where you must apply a config that would otherwise be blocked and have a
console-side recovery path lined up (e.g. migrating the management
interface to a new VLAN with PVE console access ready). It is not a
default-operations flag; routine apply should not need it.

## Renamed Kinds

Phase 1.6 renamed DHCP resources.

| Old | Current |
| --- | --- |
| `IPv4DHCPServer` | `DHCPv4Server` |
| `IPv4DHCPReservation` | `DHCPv4Reservation` |
| `IPv4DHCPScope` | `DHCPv4Server` |
| `IPv6DHCPAddress` | `DHCPv6Address` |
| `IPv6PrefixDelegation` | `DHCPv6PrefixDelegation` |
| `IPv6DHCPServer` / `IPv6DHCPv6Server` | `DHCPv6Server` |
| `IPv6DHCPScope` | `DHCPv6Server` |
| `DHCPRelay` | `DHCPv4Relay` |

The daemon binaries are `routerd-dhcpv4-client` and `routerd-dhcpv6-client`.
