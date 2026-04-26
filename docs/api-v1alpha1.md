---
title: Resource API v1alpha1
slug: /reference/api-v1alpha1
---

# API v1alpha1

routerd uses Kubernetes-like API shapes:

- `apiVersion`
- `kind`
- `metadata.name`
- `spec`
- `status` where applicable

## API Groups

- `routerd.net/v1alpha1` for the top-level `Router` config
- `net.routerd.net/v1alpha1` for network resources
- `firewall.routerd.net/v1alpha1` for firewall resources
- `system.routerd.net/v1alpha1` for local system resources
- `plugin.routerd.net/v1alpha1` for plugin manifests

## MVP Resources

- `Interface`
- `PPPoEInterface`
- `IPv4StaticAddress`
- `IPv4DHCPAddress`
- `IPv4DHCPServer`
- `IPv4DHCPScope`
- `HealthCheck`
- `IPv4DefaultRoutePolicy`
- `IPv4PolicyRoute`
- `IPv4PolicyRouteSet`
- `IPv4ReversePathFilter`
- `PathMTUPolicy`
- `Zone`
- `FirewallPolicy`
- `ExposeService`
- `IPv4SourceNAT`
- `IPv6DHCPAddress`
- `IPv6PrefixDelegation`
- `IPv6DelegatedAddress`
- `IPv6DHCPServer`
- `IPv6DHCPScope`
- `SelfAddressPolicy`
- `DNSConditionalForwarder`
- `DSLiteTunnel`
- `Hostname`
- `LogSink`
- `NTPClient`
- `Sysctl`

The schema is intentionally small and will be implemented incrementally.

## Interface Ownership

`Interface` resources support `spec.managed`.

- `managed: false` means routerd observes the interface and resolves aliases, but does not change link or address state.
- `managed: true` means routerd may manage the interface after existing OS networking ownership has been reviewed.

Host-level ownership is tracked as artifact intents and, for adopted resources,
the local ledger at `/var/lib/routerd/artifacts.json`. See
[Resource Ownership](resource-ownership.md) for the common reconcile and orphan
cleanup model used across resource kinds.

When cloud-init or netplan is detected, routerd planning reports `RequiresAdoption` instead of taking over automatically.

## PPPoEInterface

`PPPoEInterface` declares a PPPoE interface built on top of another `Interface`.
On Linux routerd renders pppd/rp-pppoe peer configuration, CHAP/PAP secrets,
and an optional systemd unit.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: PPPoEInterface
metadata:
  name: wan-ppp
spec:
  interface: wan-ether
  ifname: ppp0
  username: user@example.jp
  passwordFile: /usr/local/etc/routerd/pppoe-password
  defaultRoute: true
  usePeerDNS: true
  managed: true
  mtu: 1492
  mru: 1492
```

`interface` references the lower Ethernet `Interface`. `ifname` defaults to
`ppp-<metadata.name>` and must fit the Linux 15-character interface name limit.
Exactly one of `password` or `passwordFile` is required. `passwordFile` is
preferred so credentials do not have to live in the main YAML file.

When `managed: true`, routerd enables and starts `routerd-pppoe-<name>.service`.
When `managed: false`, routerd still renders the local config files but does not
start the systemd unit.

## LogSink

`system.routerd.net/v1alpha1` `LogSink` declares where routerd sends internal operational events.

Local journald/syslog output:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogSink
metadata:
  name: local-syslog
spec:
  type: syslog
  minLevel: info
  syslog:
    facility: local6
    tag: routerd
```

Trusted local log plugin output:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogSink
metadata:
  name: external-log
spec:
  type: plugin
  minLevel: warning
  plugin:
    path: /usr/local/libexec/routerd/log-sinks/example
    timeout: 5s
```

`enabled` defaults to `true`. `minLevel` defaults to `info`. `syslog.facility` defaults to `local6`, and `syslog.tag` defaults to `routerd`.
For remote syslog, set `syslog.network` and `syslog.address`, for example
`network: udp` and `address: syslog.example.net:514`.

## NTPClient

`system.routerd.net/v1alpha1` `NTPClient` declares the local NTP client. The
initial implementation manages `systemd-timesyncd` with static servers.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: NTPClient
metadata:
  name: system-time
spec:
  provider: systemd-timesyncd
  managed: true
  source: static
  interface: wan
  servers:
    - pool.ntp.org
```

When `interface` is set, routerd renders per-link `NTP=` through
systemd-networkd for that interface. When omitted, routerd writes global
`systemd-timesyncd` servers.

## IPv4 Overlap Safety

`IPv4StaticAddress` resources are checked against desired static addresses and observed IPv4 prefixes on other interfaces during planning.

Overlapping prefixes on different interfaces are blocked by default. Intentional overlap for NAT, HA, or lab cases must be explicit:

```yaml
spec:
  interface: lan
  address: 192.168.160.3/24
  allowOverlap: true
  allowOverlapReason: overlapping customer network for NAT lab
```

## Sysctl

`system.routerd.net/v1alpha1` `Sysctl` declares a kernel parameter.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: ipv4-forwarding
spec:
  key: net.ipv4.ip_forward
  value: "1"
  runtime: true
  persistent: false
```

`runtime: true` means routerd should manage the running kernel value. `persistent: true` is reserved for OS-specific rendering such as sysctl.d or rc.conf and is not applied yet.

## SelfAddressPolicy

`SelfAddressPolicy` defines how `dnsSource: self` selects a local address. This
keeps address selection explicit when an interface has multiple addresses, such
as a delegated LAN address and extra DS-Lite source addresses.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: SelfAddressPolicy
metadata:
  name: lan-ipv6-self
spec:
  addressFamily: ipv6
  candidates:
    - source: delegatedAddress
      delegatedAddress: lan-ipv6-pd-address
      addressSuffix: "::3"
    - source: interfaceAddress
      interface: lan
      matchSuffix: "::3"
    - source: interfaceAddress
      interface: lan
      ordinal: 1
```

`IPv6DHCPScope` can reference it:

```yaml
spec:
  dnsSource: self
  selfAddressPolicy: lan-ipv6-self
```

Candidate order is significant. The first candidate that can be resolved wins.
When omitted, IPv6 DHCP scopes use a default policy equivalent to delegated
address plus the `IPv6DelegatedAddress.addressSuffix`, then an observed address
matching that suffix, then the first observed global address.

## HealthCheck And IPv4DefaultRoutePolicy

`HealthCheck` declares a small reachability check. The default interval is `60s`;
shorter intervals are intentionally opt-in because route failover should not be
overly sensitive by default.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: dslite-v4
spec:
  type: ping
  role: next-hop
  targetSource: dsliteRemote
  interface: transix-a
```

`role` describes what the check means operationally. It does not change the
wire operation by itself; it makes route policy and status output easier to
read. Supported roles are:

- `link`: interface existence, carrier, or administrative link state.
- `next-hop`: nearby forwarding dependency such as a gateway, AFTR, or tunnel
  endpoint.
- `internet`: end-to-end public reachability, such as ping or TCP connect to a
  public target.
- `service`: service-specific dependency such as DNS resolution, DHCP, AFTR
  FQDN resolution, or a PPPoE session.
- `policy`: an aggregate answer to whether a route candidate may be selected.

The default role is `next-hop`, matching the current `targetSource: auto`
behavior.

`IPv4DefaultRoutePolicy` selects the healthy candidate with the lowest
`priority`. A candidate may be a direct interface route, or it may reference an
`IPv4PolicyRouteSet`. Direct candidates use dedicated routing tables and
firewall marks. New flows are marked for the active direct candidate. Existing
flows keep their conntrack mark while that candidate remains healthy; if the old
candidate becomes unhealthy, routerd's nftables policy rewrites that flow to the
currently active candidate.

When the active candidate references `routeSet`, routerd leaves new flows
unmarked so the referenced `IPv4PolicyRouteSet` can hash them across its
targets. Existing conntrack marks for healthy route-set targets are preserved.
If a stale mark belongs to a failed candidate, routerd clears it and lets the
route set select a target again.

When `target` is omitted, `targetSource: auto` chooses a nearby check target:
DS-Lite checks the AFTR IPv6 address, and ordinary/PPPoE interfaces check the
IPv4 default gateway for that interface. This is a `next-hop` check by default.
If you need end-to-end IPv4 Internet reachability, configure an explicit static
IPv4 target as a separate `role: internet` health check. A future aggregate
`role: policy` check can combine several lower-level checks into one answer for
route candidate selection. A route candidate with no `healthCheck` is always
treated as up.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DefaultRoutePolicy
metadata:
  name: default-v4
spec:
  mode: priority
  sourceCIDRs:
    - 192.168.160.0/24
  destinationCIDRs:
    - 0.0.0.0/0
  candidates:
    - name: dslite
      routeSet: lan-dslite-balance
      priority: 10
      healthCheck: dslite-v4
    - name: pppoe
      interface: wan-pppoe
      gatewaySource: none
      priority: 20
      table: 111
      mark: 273
      routeMetric: 60
      healthCheck: pppoe-v4
    - name: dhcp4
      interface: wan
      gatewaySource: dhcp4
      priority: 30
      table: 112
      mark: 274
      routeMetric: 100
      healthCheck: wan-dhcp4-v4
```

IPv6 default gateway behavior is intentionally left for a later design pass.

## IPv6 Prefix Delegation

`IPv6PrefixDelegation` requests a delegated prefix on an uplink interface. `IPv6DelegatedAddress` assigns an address on a downstream interface by combining one delegated subnet with a static interface identifier.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6PrefixDelegation
metadata:
  name: wan-pd
spec:
  interface: wan
  client: networkd
  profile: ntt-hgw-lan-pd
```

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6DelegatedAddress
metadata:
  name: lan-ipv6-pd-address
spec:
  prefixDelegation: wan-pd
  interface: lan
  subnetID: "0"
  addressSuffix: "::3"
  sendRA: true
  announce: true
```

On Linux with systemd-networkd, routerd renders drop-ins under `/etc/systemd/network/10-netplan-<ifname>.network.d/`. The downstream `addressSuffix` maps to networkd `Token=`, so `::3` means the LAN interface receives the delegated prefix with host identifier `::3`.

`profile` tunes DHCPv6-PD behavior for known upstream environments:

- `ntt-ngn-direct-hikari-denwa`: the router is connected directly to the NTT NGN/ONU side on a Hikari Denwa contract.
- `ntt-hgw-lan-pd`: the router is connected to the LAN side of an NTT home gateway that delegates `/60` prefixes to downstream routers.

Both NTT PD profiles currently request IA_PD only, disable rapid commit, use a link-layer DUID, force DHCPv6 Solicit when necessary, and default the prefix delegation hint to `/60` unless `prefixLength` is set explicitly.

Some NTT home gateway LAN-side environments only advertise IPv6 by RA/SLAAC and do not answer DHCPv6-PD. That mode should not be modeled as `IPv6PrefixDelegation`; it needs a separate RA/SLAAC resource design.

## IPv4DHCPServer And IPv4DHCPScope

`IPv4DHCPServer` declares a DHCPv4 server instance. `IPv4DHCPScope` binds that server to an interface and declares one address pool plus DHCP options. This split keeps multi-scope DHCP readable.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPServer
metadata:
  name: dhcp4
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
  dns:
    enabled: true
    upstreamSource: dhcp4
    upstreamInterface: wan
    cacheSize: 1000
```

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPScope
metadata:
  name: lan-dhcp4
spec:
  server: dhcp4
  interface: lan
  rangeStart: 192.168.160.100
  rangeEnd: 192.168.160.199
  leaseTime: 12h
  routerSource: interfaceAddress
  dnsSource: self
  authoritative: true
```

`routerSource` may be `interfaceAddress`, `static`, or `none`. `dnsSource` may be `dhcp4`, `static`, `self`, or `none`.

For `server: dnsmasq`, `dnsSource: self` advertises the router's LAN IPv4 address as the DNS server and runs dnsmasq as a DNS forwarder/cache. `spec.dns.upstreamSource` on `IPv4DHCPServer` controls where dnsmasq forwards queries:

- `dhcp4`: use DNS servers observed from DHCPv4 on `upstreamInterface`.
- `static`: use `upstreamServers`.
- `system`: use the host resolver configuration.
- `none`: run without upstream forwarders.

If `dnsSource` is `dhcp4` or `static` on the scope, routerd writes those DNS server addresses directly into the DHCPv4 option and dnsmasq does not need to listen on DNS port 53 for that scope.

`spec.interface` must refer to an `Interface`. If that interface still requires adoption because cloud-init or netplan is present, planning blocks DHCP scope management as well.

## IPv6DHCPServer And IPv6DHCPScope

`IPv6DHCPServer` declares a DHCPv6/RA server instance. `IPv6DHCPScope` binds it to an `IPv6DelegatedAddress`, so the LAN prefix follows the WAN DHCPv6-PD lease.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6DHCPServer
metadata:
  name: dhcp6
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
```

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6DHCPScope
metadata:
  name: lan-dhcp6
spec:
  server: dhcp6
  delegatedAddress: lan-ipv6-pd-address
  mode: stateless
  leaseTime: 12h
  defaultRoute: true
  dnsSource: self
```

`mode: stateless` means clients use SLAAC for addresses and DHCPv6 for options such as DNS. `mode: ra-only` sends RA without DHCPv6 address assignment. IPv6 default routes are advertised by RA; DHCPv6 itself has no default gateway option. With `dnsSource: self`, routerd advertises the delegated LAN IPv6 address, for example `pd-prefix::3`, as the DNS server. Static IPv6 DNS servers can be advertised with `dnsSource: static` and `dnsServers`.

When dnsmasq RA is enabled, routerd uses the same IPv6 DNS server list for
DHCPv6 DNS and RA RDNSS. This matters for Android clients, which should be
treated as SLAAC/RDNSS clients rather than DHCPv6 clients.

For dnsmasq-backed DHCP and DNS, `listenInterfaces` is the allow-list of
interfaces where dnsmasq may serve. Scopes must bind to an interface listed by
their server. Interfaces not listed are rendered as `except-interface`, so a WAN
is not served unless it is explicitly configured.

## IPv4SourceNAT

`IPv4SourceNAT` declares outbound source NAT without using Linux-specific API names. On Linux this may render to masquerade when `translation.type` is `interfaceAddress`. `outboundInterface` may reference an `Interface`, `PPPoEInterface`, or `DSLiteTunnel` resource.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4SourceNAT
metadata:
  name: lan-to-wan
spec:
  outboundInterface: transix
  sourceCIDRs:
    - 192.168.160.0/24
  translation:
    type: interfaceAddress
    portMapping:
      type: range
      start: 1024
      end: 65535
```

Other translation forms are reserved for static SNAT and pools:

```yaml
translation:
  type: address
  address: 203.0.113.10
```

```yaml
translation:
  type: pool
  addresses:
    - 203.0.113.10
    - 203.0.113.11
```

`translation.portMapping` controls source port handling:

- `auto`: let the platform choose source ports.
- `preserve`: preserve source ports when the platform can.
- `range`: map source ports into `start` through `end`.

## IPv4PolicyRoute

`IPv4PolicyRoute` marks forwarded IPv4 packets that match source and/or destination CIDRs, installs an `ip rule` for that mark, and installs a default route in a dedicated routing table. `outboundInterface` may reference an `Interface`, `PPPoEInterface`, or `DSLiteTunnel` resource.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4PolicyRoute
metadata:
  name: lan-via-transix
spec:
  outboundInterface: transix
  table: 100
  priority: 10000
  mark: 256
  sourceCIDRs:
    - 192.168.160.0/24
  destinationCIDRs:
    - 0.0.0.0/0
  routeMetric: 50
```

This is the first building block for multiple DS-Lite tunnels. Several policies can route different client or destination prefixes through different tunnel resources. Automatic load balancing and conntrack-aware tunnel selection are intentionally separate future resources.

## IPv4PolicyRouteSet

`IPv4PolicyRouteSet` selects one of multiple policy-route targets by hashing packet fields. On Linux, routerd renders nftables rules that restore an existing conntrack mark, choose a mark with `jhash` for new flows, save the selected mark back to conntrack, and install one `ip rule` plus routing table per target.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4PolicyRouteSet
metadata:
  name: lan-dslite-balance
spec:
  mode: hash
  hashFields:
    - sourceAddress
    - destinationAddress
  sourceCIDRs:
    - 192.168.160.0/24
  destinationCIDRs:
    - 0.0.0.0/0
  targets:
    - name: transix-a
      outboundInterface: transix-a
      table: 100
      priority: 10000
      mark: 256
      routeMetric: 50
    - name: transix-b
      outboundInterface: transix-b
      table: 101
      priority: 10001
      mark: 257
      routeMetric: 50
```

`hashFields` currently supports `sourceAddress` and `destinationAddress`. This is meant for multiple DS-Lite tunnels with different local IPv6 source addresses; each target usually points at a different `DSLiteTunnel`.

## IPv4ReversePathFilter

`IPv4ReversePathFilter` controls Linux `rp_filter` for policy-routing cases where reverse path checks can drop valid asymmetric traffic.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4ReversePathFilter
metadata:
  name: rp-filter-transix-a
spec:
  target: interface
  interface: transix-a
  mode: disabled
```

`target` may be `all`, `default`, or `interface`. When `target: interface`, `interface` may reference an `Interface`, `PPPoEInterface`, or `DSLiteTunnel` resource. `mode` may be `disabled`, `strict`, or `loose`, corresponding to Linux values `0`, `1`, and `2`.

## PathMTUPolicy

`PathMTUPolicy` derives an effective path MTU for traffic leaving one interface
toward one or more upstream interfaces. It can advertise that MTU to IPv6 LAN
clients through RA and clamp forwarded TCP MSS for IPv4 and IPv6.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: PathMTUPolicy
metadata:
  name: lan-wan-mtu
spec:
  fromInterface: lan
  toInterfaces:
    - wan
    - transix
  mtu:
    source: minInterface
  ipv6RA:
    enabled: true
    scope: lan-dhcp6
  tcpMSSClamp:
    enabled: true
    families:
      - ipv4
      - ipv6
```

`mtu.source: minInterface` uses the minimum configured MTU among
`toInterfaces`. Plain `Interface` resources currently default to `1500`,
`PPPoEInterface` defaults to `1492`, and `DSLiteTunnel` defaults to `1454`
unless their resource sets `mtu`. `mtu.source: static` uses `mtu.value`.

When `ipv6RA.enabled` is true, `ipv6RA.scope` references an `IPv6DHCPScope`.
dnsmasq then renders an RA MTU option such as `ra-param=ens19,1454`.

When `tcpMSSClamp.enabled` is true, routerd renders nftables forward-chain MSS
rules. The fixed MSS is derived from the effective MTU: IPv4 subtracts 40 bytes
and IPv6 subtracts 60 bytes. If `families` is omitted, both `ipv4` and `ipv6`
are enabled.

## Minimal Firewall

Firewall resources use `firewall.routerd.net/v1alpha1`. The first firewall API is
intentionally smaller than a general rule language. It models home-router safety
defaults and service exposure:

- `Zone`: names a set of router interfaces, such as `lan` or `wan`.
- `FirewallPolicy`: applies the `home-router` preset with explicit input and
  forward defaults.
- `ExposeService`: publishes one internal IPv4 service with DNAT plus a matching
  forward allow rule.

The `home-router` preset renders input default drop, forward default drop,
invalid drop, established/related accept, loopback input accept, LAN-to-WAN
forward allow when `lan` and `wan` zones exist, and router SSH/DNS/DHCP access
only from configured `routerAccess` zones.

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: Zone
metadata:
  name: lan
spec:
  interfaces:
    - lan
---
apiVersion: firewall.routerd.net/v1alpha1
kind: Zone
metadata:
  name: wan
spec:
  interfaces:
    - wan-pppoe
---
apiVersion: firewall.routerd.net/v1alpha1
kind: FirewallPolicy
metadata:
  name: default-home
spec:
  preset: home-router
  input:
    default: drop
  forward:
    default: drop
  routerAccess:
    ssh:
      fromZones:
        - lan
      wan:
        enabled: false
    dns:
      fromZones:
        - lan
    dhcp:
      fromZones:
        - lan
```

`ExposeService` is the first building block for IPv4 port publishing. `sources`
is optional; when present, only those IPv4 source prefixes are accepted. `hairpin`
is accepted in the resource shape, but the first renderer does not synthesize
external-address hairpin rules yet because the external address selection model
is not defined.

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: ExposeService
metadata:
  name: nas-https
spec:
  family: ipv4
  fromZone: wan
  viaInterface: wan-pppoe
  protocol: tcp
  externalPort: 443
  internalAddress: 192.168.160.20
  internalPort: 443
  sources:
    - 203.0.113.0/24
  hairpin: true
```

## DNSConditionalForwarder

`DNSConditionalForwarder` declares domain-specific DNS forwarding. With dnsmasq this renders to `server=/domain/upstream`.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSConditionalForwarder
metadata:
  name: transix-aftr
spec:
  domain: gw.transix.jp
  upstreamSource: static
  upstreamServers:
    - 2404:1a8:7f01:a::3
    - 2404:1a8:7f01:b::3
```

`upstreamSource` may be:

- `static`: use `upstreamServers`.
- `dhcp4`: use DNS servers learned on `upstreamInterface` by DHCPv4.
- `dhcp6`: use DNS servers learned on `upstreamInterface` by DHCPv6.

This allows a default DNS policy such as an ad-blocking resolver while keeping provider-specific names, such as DS-Lite AFTR names, on provider DNS.

## DSLiteTunnel

`DSLiteTunnel` declares a DS-Lite B4 tunnel. On Linux routerd creates an `ipip6` tunnel and can install an IPv4 default route through it.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DSLiteTunnel
metadata:
  name: transix
spec:
  interface: wan
  tunnelName: ds-transix
  aftrFQDN: gw.transix.jp
  aftrDNSServers:
    - 2404:1a8:7f01:a::3
    - 2404:1a8:7f01:b::3
  aftrAddressOrdinal: 1
  aftrAddressSelection: ordinalModulo
  localAddressSource: delegatedAddress
  localDelegatedAddress: lan-ipv6-pd-address
  localAddressSuffix: "::100"
  defaultRoute: true
  routeMetric: 50
  mtu: 1454
```

If `remoteAddress` is omitted, routerd resolves `aftrFQDN` as AAAA. `aftrDNSServers` may be used when the provider only returns the AFTR address from specific DNS servers. AAAA answers are sorted alphabetically; `aftrAddressOrdinal` selects the 1-based record to use. If omitted, routerd uses the first sorted address.

`aftrAddressSelection` controls what happens when `aftrAddressOrdinal` is outside the current AAAA record count:

- `ordinal`: fail reconcile for this tunnel.
- `ordinalModulo`: wrap around the current record count.

For multiple DS-Lite tunnels, configure different `aftrAddressOrdinal` values and run reconcile periodically. If the provider changes the AAAA set, each reconcile observes the new sorted set and recreates tunnels whose selected AFTR changed. When using `ordinalModulo`, keep `localAddressSuffix` distinct per tunnel so two tunnels can still coexist if the AFTR set shrinks. Health-based failover still needs an active health check resource.

`interface` is the underlay interface used to reach the AFTR. `localAddressSource` controls the tunnel's local IPv6 source address:

- `interface`: use the first global IPv6 address on `interface`.
- `static`: use `localAddress`.
- `delegatedAddress`: derive an address from an `IPv6DelegatedAddress` resource referenced by `localDelegatedAddress`; `localAddressSuffix` overrides that delegated address suffix for this tunnel.

With `delegatedAddress`, routerd adds the derived local address as `/128` on the delegated address interface when it is missing. This keeps the DS-Lite underlay on WAN while allowing multiple tunnels to use distinct LAN-PD-derived source addresses.
