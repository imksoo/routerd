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
| `net.routerd.net/v1alpha1` | interfaces, reusable `IPAddressSet` resources, DHCP, DNS, routes, tunnels, VIP, BGP, events, traffic flow logs |
| `firewall.routerd.net/v1alpha1` | `FirewallZone`, `FirewallPolicy`, `FirewallRule`, `FirewallLog`, `ClientPolicy`, `PortForward`, `IngressService`, `LocalServiceRedirect` |
| `system.routerd.net/v1alpha1` | `Hostname`, `Sysctl`, `SysctlProfile`, `Package`, `NTPClient`, `LogSink`, `ObservabilityPipeline`, `RouterdCluster`, `LogRetention`, `WebConsole` |
| `observability.routerd.net/v1alpha1` | `Telemetry` |
| `plugin.routerd.net/v1alpha1` | plugin manifests |

## System Bootstrap

| Kind | Role |
| --- | --- |
| `Package` | Optional narrow override for OS packages that cannot yet be derived from router resources. Normal runtime dependencies are derived automatically. |
| `Sysctl` | Narrow escape hatch for one sysctl value that cannot yet be derived from router resources. Readback comparison can be `exact` or `atLeast`. |
| `SysctlProfile` | Narrow escape hatch for router-oriented sysctl defaults. Normal router sysctls are derived automatically. |
| `Hostname` | Sets the host name. |
| `NTPClient` | Enables the OS NTP client. It can use static servers or derive servers from DHCPv4 / DHCPv6 status with public fallback servers. |
| `LogSink` | Sends routerd events to syslog or another local sink. |
| `ObservabilityPipeline` | Configures OTLP environment and built-in routerd event forwarding to stdout, syslog, or Loki. |
| `RouterdCluster` | Uses a file lease so only the leader mutates host configuration while standby nodes observe status. |
| `LogRetention` | Manages retention for events, DNS queries, traffic flows, and firewall logs. |
| `WebConsole` | Enables the read-only management Web Console. |

## Observability

| Kind | Role |
| --- | --- |
| `Telemetry` | Declares an external OTLP endpoint and injects OpenTelemetry environment variables into generated service units. |

## Interfaces

| Kind | Role |
| --- | --- |
| `Interface` | Binds a stable routerd name to an OS interface name and publishes link/address status for downstream resources. |
| `PPPoESession` | Defines PPPoE lower-interface settings. |
| `PPPoESession` | Represents a `routerd-pppoe-client` session. |
| `WireGuardInterface` | Represents a WireGuard interface. |
| `WireGuardPeer` | Represents a WireGuard peer. |
| `TailscaleNode` | Configures a local Tailscale node for exit-node and subnet-router advertisement through a managed systemd unit. |
| `IPsecConnection` | Defines a cloud VPN oriented strongSwan connection. |
| `VRF` | Represents a Linux VRF device and route table. |
| `VXLANTunnel` | Represents a VXLAN tunnel. |

`PPPoESession.spec.disabled` keeps the PPPoE definition renderable but stops
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
the router YAML. `WireGuardPeer` also accepts `presharedKeyFile` for optional
peer PSKs; inline key fields are intended for examples and tests. On FreeBSD,
routerd renders an rc.d service that creates the
`wg` interface, loads the key from that file, applies peers, and then assigns
declared static addresses for the WireGuard interface.

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
| `DHCPv6Information` | DHCPv6 information request result, including DNS, SNTP, domain search, and AFTR observations. |
| `IPv6DelegatedAddress` | Derives a LAN-side address from a delegated prefix. |
| `IPv6RAAddress` | Represents IPv6 addresses learned from RA/SLAAC. |

`DHCPv6PrefixDelegation` no longer selects an OS DHCPv6 client. DHCPv6-PD is
owned by `routerd-dhcpv6-client`.

## LAN Services

| Kind | Role |
| --- | --- |
| `DHCPv4Server` | Provides a dnsmasq DHCPv4 service and optional address pool. |
| `DHCPv4Reservation` | Reserves an IPv4 address for a MAC address. |
| `DHCPv4Relay` | Represents dnsmasq DHCPv4 relay. |
| `IPv6RouterAdvertisement` | Generates RA, PIO, RDNSS, DNSSL, M/O flags, MTU, preference, and lifetimes. |
| `DHCPv6Server` | Provides dnsmasq DHCPv6/RA service in `stateless`, `stateful`, `both`, or `ra-only` mode. |
| `DNSZone` | Owns a local authoritative zone with manual and DHCP-derived records. |
| `DNSResolver` | Owns `routerd-dns-resolver` listen profiles, sources, upstreams, and cache. |

Android does not use DHCPv6 DNS configuration, so IPv6 LANs should publish
RDNSS through `IPv6RouterAdvertisement.spec.rdnss`.

dnsmasq is limited to DHCPv4, DHCPv6, relay, and RA. DNS answering and
forwarding belongs to `DNSResolver`.
LAN DNS suffixes can be tied to a local zone by referencing
`DNSZone/<name>.zone` from `DHCPv4Server.spec.domainFrom`,
`IPv6RouterAdvertisement.spec.dnsslFrom`, and
`DHCPv6Server.spec.domainSearchFrom`.

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
| `IPAddressSet` | Defines reusable IP address sets from literal addresses and FQDNs. Linux nftables renderers materialize these as named sets for firewall, redirect, NAT, and policy-routing consumers. |
| `IPv4Route` | Adds IPv4 routes, including DS-Lite defaults and explicit drop routes. |
| `ClusterNetworkRoute` | Expands Kubernetes Pod and Service CIDRs into static IPv4 routes through worker next hops. |
| `BGPRouter` | Declares a local BGP router. The initial backend is FRR with default-deny import policy. |
| `BGPPeer` | Declares FRR-managed BGP peers for a `BGPRouter`, for example Kubernetes BGP speakers. |
| `NAT44Rule` | Performs IPv4 NAPT in the nftables `routerd_nat` table. |
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

routerd derives reverse path filter sysctls, tunnel MTU, RA MTU, and TCP MSS
clamping from router role, tunnel, firewall zone, and RA/DHCPv6 resources.
Configs should declare the tunnel and LAN/WAN intent rather than separate
`IPv4ReversePathFilter` or `PathMTUPolicy` resources.

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

`BGPRouter` and `BGPPeer` currently target FRR. routerd renders FRR config,
validates it with `vtysh -C -f`, applies deltas with
`frr-reload.py --reload`, watches FRR JSON status through `BGPStateWatcher`,
and stores peer/prefix status for `routerctl`, Web Console resources, events,
and OTel metrics. `routerctl show bgp` summarizes routers, peers, message
counters, BFD status, and last errors. `BGPPeer.spec.bfd` enables FRR BFD for
peers that need sub-second failure detection; when any managed peer uses BFD,
routerd also keeps `bgpd=yes` and `bfdd=yes` in the FRR daemons file and
restarts `frr.service` only when those daemon toggles change. The
watcher defaults to a 15 second controller interval and 4096 observed prefixes,
and `BGPRouter.spec.watcher` can tune `pollInterval`, `maxPrefixes`, and
`peerStateChangeThrottle`; validation rejects intervals below 3 seconds and
prefix caps of 1,000,000 or more. Import policy is default deny; add
`spec.importPolicy.allowedPrefixes` for Kubernetes LoadBalancer pools. Accepted
imports set `ip next-hop peer-address` so Kubernetes-advertised
`/32` routes remain reachable through the advertising speaker. `BGPRouter` can
redistribute connected and static IPv4 routes with independent
`allowedPrefixes`; routerd renders FRR `redistribute connected/static route-map`
statements and keeps the peer outbound route-map default-deny unless exported
prefixes are explicitly listed in `BGPRouter.spec.exportPolicy.allowedPrefixes`
or a peer override at `BGPPeer.spec.exportPolicy.allowedPrefixes`. BGP
community policy can be declared on the router or peer with `communities.send`, `communities.accept`, and
`communities.set.in/out`. The watcher records observed route communities in
status when FRR exposes them in JSON output. Multiple `BGPRouter` resources can
run as separate FRR BGP instances by assigning additional routers to
`spec.vrf`, which references a routerd `VRF` resource and renders
`router bgp <asn> vrf <ifname>`. routerd stores observed BGP status per
`BGPRouter` by following `BGPPeer.spec.routerRef`. `spec.listen.address` is
validated and used for routerd-side listen collision checks; FRR address binding
itself remains a bgpd daemon invocation option rather than an integrated config
stanza.

`VirtualAddress` uses keepalived on Linux and CARP on FreeBSD for
`mode: vrrp`. `spec.family: ipv4` requires an IPv4 `/32`, and
`spec.family: ipv6` requires an IPv6 `/128`. IPv6 VIPs render keepalived VRRPv3 with
`family inet6`; FreeBSD renders `inet6` CARP aliases. Linux VRRP uses explicit
unicast peers and defaults to
`nopreempt`; FreeBSD CARP uses multicast advertisements on the parent interface,
so `spec.vrrp.peers` is ignored there. Set `spec.vrrp.preempt: true` only when
automatic failback is intended, and pair it with `spec.vrrp.preemptDelay` when
Linux failback should wait. FreeBSD has no direct `preemptDelay` equivalent.
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

Use shorter advertisements only for control-plane VIPs where fast failover is
worth the extra L2 chatter and operational sensitivity. A Kubernetes API VIP is
a typical case: `advertInterval: 1s`, `preempt: true`, and
`preemptDelay: 30s` lets the preferred router take the VIP back, but only after
it has stayed healthy long enough to avoid a quick failback loop.

Use slower, non-preemptive settings for home-router or DS-Lite/LAN service VIPs
where stability matters more than returning to a preferred owner. A conservative
preset is `advertInterval: 3s` with `preempt: false`; the backup keeps the VIP
until it fails or is intentionally moved. See `examples/vrrp-tuning-presets.yaml`
for complete resource fragments.

`BGPPeer.spec.password` is rendered into FRR as `neighbor ... password ...`.
Prefer `BGPPeer.spec.passwordFrom` for production configs so the routerd YAML
does not contain the shared secret. `passwordFrom.file` reads a local root-owned
secret file and `passwordFrom.env` reads an environment variable; `base64: true`
decodes either source before rendering.
FRR listen-address binding is a bgpd daemon invocation option (`-l` /
`--listenon`), not a normal BGP config stanza in the managed FRR config. Keep
host firewall zones and service-manager bgpd options aligned when BGP must be
limited to a specific interface address.

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
apply --once` plans and renders those derived settings but mutates the host only
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
| `EventRule` | Evaluates event streams with all_of, any_of, sequence, window, absence, throttle, debounce, and count. |
| `DerivedEvent` | Emits virtual events derived from multiple resource states. |
| `SelfAddressPolicy` | Selects a self address for protocols that need one. |

`HealthCheck.spec.disabled` renders the daemon unit but disables and stops it.
`EgressRoutePolicy` candidates also accept `disabled: true`; disabled
candidates are not selected even if their last observed health status is still
Healthy.

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

`HealthCheck.spec.sourceInterface` accepts a network resource name and resolves
it to the OS interface name at runtime. `via`, `fwmark`, and `sourceAddress`
can also be specified. `sourceAddressFrom` derives the probe source address
from another resource status. On Linux, `routerd-healthcheck` uses
`SO_BINDTODEVICE` and can set `SO_MARK`. When a health check is referenced by an
`EgressRoutePolicy` candidate or target, routerd derives `SO_MARK` from that
route target's mark automatically; direct `fwmark` is intended for standalone
low-level probes. On FreeBSD, it selects a source address from the named
interface because FreeBSD does not provide the Linux socket options.

`WebConsole.spec.listenAddressFrom` derives the HTTP listener address from
another resource status, for example `Interface/mgmt.status.ipv4Addresses`.
Use it instead of a literal `listenAddress` when the management address comes
from DHCP, IPAM, or another declarative resource.

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
lists, while `classification[]` can keep names and reservation references.
`spec.isolation` can express the common guest shape: internet allowed, LAN and
management denied, and mDNS/SSDP/NetBIOS discovery blocked. The FreeBSD pf
renderer reports this resource as unsupported because pf does not provide the
same MAC-based routed filtering model.

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
