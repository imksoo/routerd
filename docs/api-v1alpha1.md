---
title: Resource API v1alpha1
slug: /reference/api-v1alpha1
---

# Resource API v1alpha1

A routerd config is a list of declarative resources. Each resource describes a
single behavior the router should exhibit — an interface that should come up,
an address pool that should be served, a tunnel that should reach an AFTR, a
default route that should follow a healthy uplink. Apply compares those
intents against the host and brings the host into shape.

The resource shape is intentionally Kubernetes-like:

- `apiVersion`
- `kind`
- `metadata.name`
- `spec`
- `status` where applicable

This page describes what each resource makes the router do, the spec fields
that change that behavior, and the host artifacts that come out of it.

## Quick index

This is a long page. Jump to the kind you need:

**Top-level**
- `Router` (the top-level resource — see ["API groups"](#api-groups) below)
- [Inventory](#inventory) (observation only)

**Interfaces**
- [Interface](#interface)
- [PPPoEInterface](#pppoeinterface)

**IPv4 addressing**
- [IPv4StaticAddress](#ipv4staticaddress)
- [IPv4DHCPAddress](#ipv4dhcpaddress)

**IPv4 DHCP and DNS service (LAN-side)**
- [IPv4DHCPServer / IPv4DHCPScope](#ipv4dhcpserver-and-ipv4dhcpscope)

**IPv6 addressing and prefix delegation**
- [IPv6PrefixDelegation](#ipv6prefixdelegation)
- [IPv6DelegatedAddress](#ipv6delegatedaddress)
- [IPv6RAAddress](#ipv6raaddress)
- [IPv6DHCPAddress](#ipv6dhcpaddress)
- [IPv6DHCPServer / IPv6DHCPScope](#ipv6dhcpserver-and-ipv6dhcpscope)
- [SelfAddressPolicy](#selfaddresspolicy)
- [DNSConditionalForwarder](#dnsconditionalforwarder)

**DS-Lite (IPv6-only WAN with IPv4 over IPv6)**
- [DSLiteTunnel](#dslitetunnel)

**IPv4 routing, NAT, policy**
- [HealthCheck](#healthcheck)
- [IPv4DefaultRoutePolicy](#ipv4defaultroutepolicy)
- [IPv4SourceNAT](#ipv4sourcenat)
- [IPv4PolicyRoute](#ipv4policyroute)
- [IPv4PolicyRouteSet](#ipv4policyrouteset)
- [IPv4ReversePathFilter](#ipv4reversepathfilter)

**MTU**
- [PathMTUPolicy](#pathmtupolicy)

**Firewall**
- [Zone](#zone)
- [FirewallPolicy](#firewallpolicy)
- [ExposeService](#exposeservice)

**System and host integration**
- [NixOSHost](#nixoshost)
- [Hostname](#hostname)
- [Sysctl](#sysctl)
- [NTPClient](#ntpclient)
- [LogSink](#logsink)

If you are new, read [Concepts](../concepts/what-is-routerd) first; this
page is the field reference, not the introduction.

## API groups

- `routerd.net/v1alpha1` for the top-level `Router`.
- `net.routerd.net/v1alpha1` for interfaces, addressing, DNS, route policy,
  and tunnels.
- `firewall.routerd.net/v1alpha1` for firewall zones and policy.
- `system.routerd.net/v1alpha1` for hostname, sysctl, NTP client, and the
  routerd event sink.
- `plugin.routerd.net/v1alpha1` for plugin manifests.

## Available resource kinds

Networking:
`Interface`, `PPPoEInterface`, `IPv4StaticAddress`, `IPv4DHCPAddress`,
`IPv4DHCPServer`, `IPv4DHCPScope`, `IPv6DHCPAddress`, `IPv6PrefixDelegation`,
`IPv6DelegatedAddress`, `IPv6DHCPServer`, `IPv6DHCPScope`,
`SelfAddressPolicy`, `DNSConditionalForwarder`, `DSLiteTunnel`,
`StatePolicy`, `HealthCheck`, `IPv4DefaultRoutePolicy`, `IPv4SourceNAT`,
`IPv4PolicyRoute`, `IPv4PolicyRouteSet`, `IPv4ReversePathFilter`,
`PathMTUPolicy`.

Firewall:
`Zone`, `FirewallPolicy`, `ExposeService`.

System:
`Hostname`, `Sysctl`, `NTPClient`, `NixOSHost`, `LogSink`.

Observed-only:
`Inventory` is written by routerd into the state database as
`routerd.net/v1alpha1/Inventory/host`. It is not normally written in
`spec.resources`; inspect it with `routerctl describe inventory/host`.

The set is small on purpose. New kinds are added when the router gains a new
behavior, not as a generic platform.

### Inventory

`Inventory` records what routerd can observe about the local host before an
apply run changes router resources. It has no desired `spec`; its `status`
contains OS and kernel information, virtualization detection, best-effort DMI
fields, the detected service manager, and whether selected commands are
available.

```sh
routerctl describe inventory/host
routerctl show inventory/host -o yaml
```

The first implementation records the value only. Renderers do not consume it
yet. Later platform-specific render decisions can use the same state object
without guessing whether the host is physical, virtual, systemd-based, or
rc.d-based.

## Top-Level Apply Policy

The top-level `spec.reconcile` block controls how routerd behaves when one
part of an apply fails.

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lab-router
spec:
  reconcile:
    mode: progressive
    protectedInterfaces:
      - mgmt
    protectedZones:
      - mgmt
```

- `mode: strict` is the default. routerd stops at the first apply error and
  returns that error.
- `mode: progressive` applies independent stages where it can, records stage
  errors as warnings, and reports the result as `Degraded`. Destructive
  orphan cleanup and ownership recording are skipped when a stage fails.
- `protectedInterfaces` names interfaces that carry the management path.
  routerd treats them as safety anchors when deciding whether to continue
  after an error.
- `protectedZones` names firewall zones that must keep router access open.
  nftables rendering automatically keeps SSH open from these zones even if a
  firewall policy forgets to list them explicitly.

This does not make every host operation transactional. It gives routerd a
clear rule: keep the management path, apply what can be applied safely, and
leave failed data-plane work visible for the next apply pass.

## State And Conditions

### StatePolicy

`StatePolicy` evaluates host observations into a named state variable. State
variables have three statuses:

- `unknown`: routerd has not evaluated the value, or observation failed.
- `unset`: routerd evaluated the source and found no value.
- `set`: routerd evaluated the source and recorded a concrete value.

`Set(name, "")` is normalized to `unset`. Only an explicit reset/forget
operation returns a value to `unknown`.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: StatePolicy
metadata:
  name: wan-ipv6-mode
spec:
  variable: wan.ipv6.mode
  values:
    - value: pd-ready
      when:
        ipv6PrefixDelegation:
          resource: wan-pd
          available: true
    - value: address-only
      when:
        ipv6PrefixDelegation:
          resource: wan-pd
          available: false
          unavailableFor: 180s
        ipv6Address:
          interface: wan
          global: true
        dnsResolve:
          name: gw.transix.jp
          type: AAAA
          upstreamSource: static
          upstreamServers:
            - 2404:1a8:7f01:a::3
            - 2404:1a8:7f01:b::3
```

Resources that support `spec.when` are applied only when the expression is
true. `unknown` and `unset` are false for ordinary comparisons; they only match
when explicitly requested by `status` or `exists: false`.

```yaml
when:
  state:
    wan.ipv6.mode:
      in:
        - pd-ready
        - address-only
```

Supported state match operators:

- `exists: true`: true when the variable is `set`.
- `exists: false`: true when the variable is `unset`; `unknown` remains false.
- `equals`: true when the variable is `set` and equal to the value.
- `in`: true when the variable is `set` and equal to one listed value.
- `contains`: true when the variable is `set` and contains the string.
- `status`: explicitly match `set`, `unset`, or `unknown`.
- `for`: require the matched status/value to have held for the duration.

Fields within a single match (`equals`, `for`, etc.) combine with AND, and
multiple `spec.when.state` entries also combine with AND. There is no OR
operator in `spec.when`: `in: [a, b, c]` covers OR across the values of a
single variable, and broader OR is expressed by deriving a synthetic state
variable through a `StatePolicy`.

`StatePolicy.values` is evaluated top to bottom, and the first matching entry
wins; if nothing matches, the variable becomes `unset`. Two entries that
record the same value therefore behave as OR over their `when` conditions:

```yaml
kind: StatePolicy
spec:
  variable: wan.ready
  values:
    - value: ready
      when:
        ipv6PrefixDelegation:
          resource: wan-pd
          available: true
    - value: ready
      when:
        ipv6Address:
          interface: wan
          global: true
```

Resources can then guard themselves with
`when: { state: { wan.ready: { equals: ready } } }`, which matches whenever
either source condition holds.

`spec.when` is currently available on DHCP scopes, IPv6 delegated addresses,
DS-Lite tunnels, health checks, IPv4 NAT, IPv4 policy route sets, and
IPv4 default route candidates.

## Interfaces

### Interface

`Interface` declares one networking interface that routerd should know about
and, optionally, manage.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: lan
spec:
  ifname: ens19
  adminUp: true
  managed: true
```

How routerd behaves:

- `spec.ifname` resolves the alias `lan` to a real link on the host. Other
  resources reference `lan`, never the kernel name.
- `spec.adminUp: true` keeps the link administratively up.
- `spec.managed: true` means routerd may change link and address state. If
  cloud-init or netplan already owns the interface, planning reports it as
  requiring adoption instead of taking it over.
- `spec.managed: false` keeps routerd in observe-only mode for that
  interface: alias resolution still works, but link and address state are
  left alone.

Host ownership decisions, including the local ledger at
the `artifacts` table in `/var/lib/routerd/routerd.db`, are described in
[Resource Ownership](resource-ownership.md).

### PPPoEInterface

`PPPoEInterface` brings up a PPPoE session on top of an existing
`Interface`. On Linux, routerd renders pppd / rp-pppoe peer configuration,
the CHAP/PAP secret, and a managed systemd unit. On FreeBSD, routerd renders
an `mpd5` configuration and starts the `mpd5` rc.d service for managed
sessions.

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

How routerd behaves:

- `spec.interface` references the underlying Ethernet `Interface`.
- `spec.ifname` defaults to `ppp-<metadata.name>` and must fit Linux's
  15-character interface name limit.
- Exactly one of `spec.password` and `spec.passwordFile` must be set; using
  `passwordFile` keeps credentials out of the main YAML.
- `spec.managed: true` enables and starts `routerd-pppoe-<name>.service`.
  `spec.managed: false` renders the config files but leaves the unit alone.
  On FreeBSD, the same flag controls whether the session is loaded by the
  generated `mpd5` default label.
- `spec.defaultRoute: true` lets pppd install a default route through the
  PPP link; combine with `IPv4DefaultRoutePolicy` if multiple uplinks need
  to coexist.
- `spec.usePeerDNS: true` accepts DNS servers the PPP peer advertises.
- `spec.mtu` and `spec.mru` are useful when the upstream session uses a
  smaller MTU than 1500 (PPPoE typically caps at 1492).

## IPv4 addressing

### IPv4StaticAddress

`IPv4StaticAddress` makes a fixed IPv4 prefix appear on an interface.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4StaticAddress
metadata:
  name: lan-ipv4
spec:
  interface: lan
  address: 192.168.10.3/24
  exclusive: true
```

How routerd behaves:

- routerd assigns `192.168.10.3/24` to the LAN interface and treats it as
  the router's own address.
- `spec.exclusive: true` makes routerd remove other static IPv4 addresses on
  that interface during apply, so the LAN side does not end up with two
  conflicting prefixes after a renumber.
- During planning, routerd checks the desired static addresses and observed
  IPv4 prefixes on other interfaces. Overlapping prefixes on different
  interfaces are blocked unless explicitly allowed:

  ```yaml
  spec:
    interface: lan
    address: 192.168.10.3/24
    allowOverlap: true
    allowOverlapReason: overlapping customer network for NAT lab
  ```

### IPv4DHCPAddress

`IPv4DHCPAddress` asks routerd to obtain an IPv4 address from upstream DHCP.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPAddress
metadata:
  name: wan-dhcp4
spec:
  interface: wan
  client: dhclient
  required: true
```

How routerd behaves:

- routerd manages a DHCPv4 client binding on `interface`. `spec.client`
  picks the client implementation (currently `dhclient`).
- `spec.required: true` means apply fails if no lease is acquired —
  useful when the rest of the config depends on a working WAN address.
- `spec.useRoutes: false` tells supported renderers to ignore DHCP-provided
  routes. `spec.useDNS: false` ignores DHCP-provided DNS servers. This is
  useful for management interfaces that should receive an address from IPAM
  without changing the router's default route or resolver.
- `spec.routeMetric` sets the metric for DHCP-provided IPv4 routes when routes
  are accepted.

Management-interface example:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPAddress
metadata:
  name: mgmt-dhcp4
spec:
  interface: mgmt
  client: networkd
  required: false
  useRoutes: false
  useDNS: false
```

## IPv4 DHCP and DNS service

### IPv4DHCPServer and IPv4DHCPScope

The DHCPv4 service is split into a server resource and one or more scope
resources. The server represents a single dnsmasq instance; each scope binds
that server to one downstream interface.

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
  rangeStart: 192.168.10.100
  rangeEnd: 192.168.10.199
  leaseTime: 12h
  routerSource: interfaceAddress
  dnsSource: self
  authoritative: true
```

How routerd behaves:

- `spec.listenInterfaces` is the allow-list for dnsmasq. Scopes may only
  bind to interfaces listed by their server. Anything not listed is
  rendered as `except-interface`, so a WAN never serves DHCP/DNS unless it
  is explicitly named.
- `IPv4DHCPScope.routerSource` controls the gateway option:
  `interfaceAddress` advertises the router's LAN address, `static` uses
  `spec.router`, `none` omits the option.
- `IPv4DHCPScope.dnsSource` controls the DNS server option:
  - `dhcp4` and `static` write the DNS servers directly into the DHCPv4
    option; dnsmasq does not need to listen on port 53 for that scope.
  - `self` advertises the router's own LAN IPv4 address and runs dnsmasq as
    a DNS forwarder/cache. The forwarder behavior is then controlled by
    `IPv4DHCPServer.spec.dns`:
    - `upstreamSource: dhcp4` forwards to the DNS servers learned through
      DHCPv4 on `upstreamInterface`.
    - `upstreamSource: static` uses `upstreamServers`.
    - `upstreamSource: system` follows the host resolver configuration.
    - `upstreamSource: none` runs without upstream forwarders.
  - `none` omits the DNS option entirely.
- If `spec.interface` still requires adoption — for example because
  cloud-init owns it — planning blocks the DHCP scope as well, since
  serving DHCP without owning the interface would race with another
  manager.

## IPv6 addressing and prefix delegation

### IPv6PrefixDelegation

`IPv6PrefixDelegation` requests a delegated prefix on an uplink interface.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6PrefixDelegation
metadata:
  name: wan-pd
spec:
  interface: wan
  client: dhcp6c
  profile: ntt-hgw-lan-pd
  prefixLength: 60
```

Breaking note: obsolete pre-release DHCPv6-PD workaround fields were removed.
If an older config still contains unknown DHCPv6-PD keys, delete those keys.

How routerd behaves:

- `spec.client` selects the OS DHCPv6-PD client. `networkd` uses
  systemd-networkd drop-ins on Linux. `dhcp6c` uses a managed WIDE/KAME-style
  `dhcp6c.conf` and service. `dhcpcd` uses a managed per-resource
  `dhcpcd.conf` and service and is currently an evaluation path for unifying
  WAN DHCPv4, RA/SLAAC, IA_NA, and IA_PD handling. NTT home-gateway profiles
  should not use systemd-networkd as the preferred PD path; use `dhcp6c`
  today, or `dhcpcd` only when intentionally running the lab evaluation.
  Do not also declare an `IPv6DHCPAddress` on the same interface when an
  external client such as `dhcp6c` or `dhcpcd` owns prefix delegation there;
  only one DHCPv6 client should bind the WAN side.
- `spec.profile` selects a known upstream environment:
  - `default` is generic DHCPv6-PD.
  - `ntt-ngn-direct-hikari-denwa` is for a router connected directly to
    NTT NGN/ONU on a Hikari Denwa contract.
  - `ntt-hgw-lan-pd` is for a router behind an NTT home gateway that
    delegates `/60` prefixes to the LAN side.
  Both NTT profiles request IA_PD only, disable rapid commit, use a
  link-layer DUID, force DHCPv6 Solicit when needed, and default the expected
  delegated length to `/60`. routerd does not derive or render IAID unless
  `spec.iaid` is explicitly set. For systemd-networkd, routerd deliberately
  omits `PrefixDelegationHint=`, disables hostname sending, and disables the
  option-use knobs that are not needed for PD where networkd exposes them.
  Some protocol-maintenance request options may still be emitted by
  systemd-networkd itself.
- During apply, routerd records observed prefix-delegation state in
  `ipv6PrefixDelegation.<name>.lease` in the local state store. The lease JSON
  holds the current prefix, last known prefix, observed DUID, IAID, expected
  DUID, and last observed time. Older pre-release state files that used
  separate keys such as `ipv6PrefixDelegation.<name>.lastPrefix` are no longer
  migrated automatically. Interface names, configured prefix length, client
  type, and profile remain separate state entries because
  they describe routerd configuration rather than the DHCPv6 lease itself.
  `currentPrefix` inside the lease is cleared when no downstream delegated
  prefix is visible. `lastPrefix` is kept for operator visibility, not fed
  back as an exact DHCPv6 hint.
  Use `routerctl get ipv6pd` to see the desired resource definition,
  `routerctl describe ipv6pd/<name>` to inspect current and last delegated
  prefixes without mixing them up, and `routerctl show ipv6pd -o yaml
  --events` for the combined machine-readable view.
- For systemd-networkd and FreeBSD `dhcp6c` clients, routerd records observed
  DHCP identity into the lease when available. With `dhcp6c`, the DUID is read
  from `/var/db/dhcp6c_duid`. For NTT profiles, routerd records an expected
  DUID derived from the uplink MAC as a DHCPv6 link-layer DUID. These values
  are state memory for display and identity checks. routerd does not derive or
  render an IAID by default; use `spec.iaid` only when an operator explicitly
  needs to pin the DHCP identity.
- The OS DHCPv6 client remains responsible for Renew/Rebind before the lease
  expires. routerd should not normally restart that client during apply,
  because a restart can turn a renewal path into a fresh Solicit or Release.
  If there is no current observable prefix, `plan`, `apply`, and daemon
  status include a warning so the operator can fix the DHCPv6 client before
  the upstream lease expires.
- `spec.iaid` pins the DHCPv6 IAID. It may be written as decimal, `0x`
  prefixed hex, or 8 hex digits. systemd-networkd renders it as a decimal
  `IAID=` value; FreeBSD `dhcp6c` uses it as the `ia-pd` / `id-assoc pd`
  identifier. Leave it unset for the normal NTT profile path.
- `spec.duidType` defaults to `link-layer` for NTT profiles when omitted.
  This keeps systemd-networkd away from its default machine-id based DUID and
  keeps FreeBSD/KAME `dhcp6c` aligned with NTT home-gateway expectations.
- `spec.duidType` and `spec.duidRawData` pin the DHCPv6 DUID when an operator
  intentionally needs a stable identity that is not derived from the uplink
  MAC, for example during HA failover, router replacement, or a staged
  migration. `duidRawData` accepts either `00:01:...` byte notation or compact
  hex. Leave it unset for the normal real MAC-derived DUID-LL behavior.
- `spec.serverID`, `spec.priorPrefix`, and `spec.acquisitionStrategy` are
  manual overrides for the DHCPv6-PD active-control path. The normal path is
  for routerd to learn the upstream server identifier and previously delegated
  prefix from `IPv6PrefixDelegation` status, then pass that state to renderers
  and active DHCPv6 helpers. Set these fields only when recovering or migrating
  a binding and the observed status is missing or known to be wrong.
  `acquisitionStrategy` is one of `hybrid`, `solicit-only`, or
  `request-claim-only`.
- `routerd dhcp6 request|renew|release --resource <name>` is the low-level
  active-control entry point for lab recovery. It reads DUID, IAID, server
  identifier, and prefix data from the resource and state DB, then sends a raw
  DHCPv6 packet on the uplink. Request/Renew packets use a fresh transaction ID,
  non-zero T1/T2 and IA Prefix lifetimes, and Reconfigure Accept; Release sends
  zero IA_PD lifetimes and omits Reconfigure Accept.
  Lab packets can override the requested lifetimes with `--t1`, `--t2`,
  `--preferred-lifetime`, and `--valid-lifetime`. Use this only to measure
  whether an upstream server honours shorter requested leases.
- On FreeBSD with KAME `dhcp6c`, routerd manages `/var/db/dhcp6c_duid` for
  NTT profiles whose effective DUID type is `link-layer`. If the existing file
  differs from the desired DUID, routerd backs it up as `.bak.<timestamp>` and
  writes the desired DUID before starting `dhcp6c`.
- With `client: dhcpcd`, routerd manages the dhcpcd DUID file for NTT
  profiles whose effective DUID type is `link-layer`, renders
  `dhcpcd-<name>.conf`, and starts a per-resource service. On Linux this is
  `routerd-dhcpcd-<name>.service`; on FreeBSD this is a managed rc.d script
  under `/usr/local/etc/rc.d`. This path is present so the lab can measure
  dhcpcd before changing any profile defaults.

Some NTT home-gateway environments only advertise IPv6 by RA/SLAAC and never
answer DHCPv6-PD. Those should not be modeled as `IPv6PrefixDelegation`;
that mode needs a separate RA/SLAAC resource design.

### IPv6DelegatedAddress

`IPv6DelegatedAddress` carves a downstream subnet out of a delegated prefix
and gives the router a stable address inside it.

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

How routerd behaves:

- routerd combines one delegated subnet with the static suffix to assign
  the LAN-side address. With systemd-networkd, the suffix is rendered as
  `Token=`, so `::3` means the LAN interface receives the delegated prefix
  with host identifier `::3`.
- On FreeBSD, routerd observes the currently visible delegated prefix and can
  add the stable LAN-side suffix with
  `ifconfig <ifname> inet6 <address> prefixlen 64 alias`.
- `spec.sendRA: true` lets dnsmasq advertise the prefix through RA.
- `spec.announce: true` exposes this address as a candidate for
  `dnsSource: self` and for DS-Lite local-address selection.

### IPv6RAAddress

`IPv6RAAddress` accepts IPv6 Router Advertisements on an uplink interface so
the router itself receives an upstream SLAAC address and RA-provided default
route. This is separate from DHCPv6-PD: DS-Lite AFTR DNS lookups and other
WAN-side IPv6 control traffic need a usable WAN IPv6 address even before a
delegated LAN prefix is applied.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6RAAddress
metadata:
  name: wan-ra
spec:
  interface: wan
  required: true
```

On Linux renderers this enables `IPv6AcceptRA=yes`. On FreeBSD it enables
`accept_rtadv` and `rtsold` for the interface.

### IPv6DHCPAddress

`IPv6DHCPAddress` runs a DHCPv6 client on an uplink interface to obtain an
IA_NA address. It is independent from prefix delegation, which is requested
by `IPv6PrefixDelegation`.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6DHCPAddress
metadata:
  name: wan-dhcp6
spec:
  interface: wan
  client: networkd
  required: true
```

### IPv6DHCPServer and IPv6DHCPScope

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

How routerd behaves:

- The scope binds to `IPv6DelegatedAddress`, so the LAN prefix automatically
  follows whatever WAN-side DHCPv6-PD hands out.
- `spec.mode: stateless` lets clients pick their own address through SLAAC
  while still receiving DHCPv6 options such as DNS.
- `spec.mode: ra-only` sends RA without DHCPv6 address assignment.
- IPv6 default routes are advertised by RA; DHCPv6 itself has no default
  gateway option.
- If the delegated LAN prefix is not observable yet, routerd omits the
  dnsmasq IPv6 scope temporarily. IPv4 DHCP and DNS scopes can continue
  running while DHCPv6-PD is still converging.
- `spec.dnsSource: self` advertises the router's delegated LAN IPv6 address
  (for example `pd-prefix::3`) as the DNS server. `dnsSource: static` plus
  `dnsServers` advertises a fixed list instead.
- When dnsmasq RA is enabled, routerd uses the same IPv6 DNS server list
  for DHCPv6 DNS and RA RDNSS. This matters for Android clients, which
  should be treated as SLAAC/RDNSS clients rather than DHCPv6 clients.

### SelfAddressPolicy

`SelfAddressPolicy` defines how `dnsSource: self` selects a local address
when an interface has more than one — for example a delegated LAN address
plus extra DS-Lite source addresses.

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

`IPv6DHCPScope` references it through `spec.selfAddressPolicy`. Candidates
are evaluated in order; the first one routerd can resolve wins. When no
policy is referenced, IPv6 DHCP scopes fall back to a default that prefers
the delegated address with `IPv6DelegatedAddress.addressSuffix`, then a
matching observed address, then the first observed global address.

### DNSConditionalForwarder

`DNSConditionalForwarder` forwards a single domain to specific upstream
servers. With dnsmasq this becomes `server=/domain/upstream`.

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

`upstreamSource` controls where the forwarders come from:

- `static`: use `upstreamServers`.
- `dhcp4`: use DNS servers learned by DHCPv4 on `upstreamInterface`.
- `dhcp6`: use DNS servers learned by DHCPv6 on `upstreamInterface`.

This makes it possible to run a global ad-blocking resolver as the default,
while keeping provider-specific names (such as DS-Lite AFTR FQDNs) on
provider DNS so they resolve to the correct AAAA records.

## DS-Lite

### DSLiteTunnel

`DSLiteTunnel` brings up a DS-Lite B4 tunnel toward an AFTR.

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

How routerd behaves:

- routerd creates an `ipip6` tunnel with the IPv6 underlay reaching the
  AFTR through `spec.interface`.
- If `spec.remoteAddress` is omitted, routerd resolves `aftrFQDN` as AAAA.
  `aftrDNSServers` is consulted when only specific DNS servers return the
  AFTR record. AAAA answers are sorted alphabetically;
  `aftrAddressOrdinal` selects the 1-based record.
- `aftrAddressSelection` controls what happens when the ordinal is outside
  the current AAAA record count:
  - `ordinal`: apply fails for this tunnel.
  - `ordinalModulo`: the ordinal wraps around the current count.
- `localAddressSource` chooses the tunnel's local IPv6 source address:
  - `interface`: use the first global IPv6 address on `spec.interface`.
  - `static`: use `localAddress`.
  - `delegatedAddress`: derive an address from the
    `IPv6DelegatedAddress` named in `localDelegatedAddress`;
    `localAddressSuffix` overrides the suffix for this tunnel.

  With `delegatedAddress`, routerd also adds the derived local address as
  `/128` on the delegated address interface when missing. This keeps the
  DS-Lite underlay on WAN while letting multiple tunnels use distinct
  LAN-PD-derived source addresses.
- `defaultRoute: true` adds an IPv4 default route through the tunnel, with
  `routeMetric` controlling the priority among multiple uplinks.

For multiple DS-Lite tunnels with `ordinalModulo`, keep `localAddressSuffix`
distinct per tunnel so two tunnels remain unique even if the AFTR set
shrinks. Health-based failover still requires an active `HealthCheck`
resource.

## IPv4 routing

### HealthCheck

`HealthCheck` declares one reachability check.

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

How routerd behaves:

- `spec.interval` defaults to 60 seconds. Shorter intervals are opt-in
  because route failover should not be overly sensitive by default.
- `spec.target` may be an explicit address; if omitted, `targetSource: auto`
  picks a nearby check target. DS-Lite tunnels check the AFTR IPv6
  address; ordinary or PPPoE interfaces check the IPv4 default gateway.
- `spec.role` describes what the check means operationally. It does not
  change the wire operation by itself, but it makes route policy and
  status output easier to read:
  - `link`: interface presence, carrier, or administrative state.
  - `next-hop`: nearby forwarding dependency such as a gateway, AFTR, or
    tunnel endpoint. This is the default.
  - `internet`: end-to-end public reachability — for example a ping or TCP
    connect to a public address.
  - `service`: a service-specific dependency such as DNS resolution, DHCP,
    AFTR FQDN resolution, or a PPPoE session.
  - `policy`: an aggregate answer to whether a route candidate may be
    selected. Reserved for future use.

If you need end-to-end IPv4 Internet reachability, configure an explicit
static IPv4 target as a separate `role: internet` health check rather than
overloading the next-hop check.

### IPv4DefaultRoutePolicy

`IPv4DefaultRoutePolicy` selects which uplink the IPv4 default route
follows. The healthy candidate with the lowest `priority` becomes active.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DefaultRoutePolicy
metadata:
  name: default-v4
spec:
  mode: priority
  sourceCIDRs:
    - 192.168.10.0/24
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

How routerd behaves:

- A candidate may point directly at an interface or reference an
  `IPv4PolicyRouteSet` through `routeSet`.
- For direct candidates, routerd installs a dedicated routing table and a
  firewall mark per candidate. New flows are marked for the active
  candidate. Established flows keep their conntrack mark while that
  candidate stays healthy; if the old candidate becomes unhealthy, routerd
  rewrites that flow to the currently active candidate.
- For route-set candidates, routerd leaves new flows unmarked so the
  referenced `IPv4PolicyRouteSet` can hash them across its targets.
  Existing conntrack marks for healthy route-set targets are preserved;
  marks belonging to a failed target are cleared so the route set picks
  again.
- A candidate without `healthCheck` is always treated as up.

IPv6 default gateway behavior is intentionally left for a later design pass.

### IPv4SourceNAT

`IPv4SourceNAT` declares outbound source NAT in terms of source ranges and
the desired translation.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4SourceNAT
metadata:
  name: lan-to-wan
spec:
  outboundInterface: transix
  sourceCIDRs:
    - 192.168.10.0/24
  translation:
    type: interfaceAddress
    portMapping:
      type: range
      start: 1024
      end: 65535
```

How routerd behaves:

- `outboundInterface` may be an `Interface`, `PPPoEInterface`, or
  `DSLiteTunnel`.
- `translation.type: interfaceAddress` translates to whatever IPv4 address
  is currently on the egress interface. Linux renders this as masquerade.
- `translation.type: address` pins translation to a single address:

  ```yaml
  translation:
    type: address
    address: 203.0.113.10
  ```
- `translation.type: pool` distributes across a pool:

  ```yaml
  translation:
    type: pool
    addresses:
      - 203.0.113.10
      - 203.0.113.11
  ```
- `translation.portMapping`:
  - `auto`: let the platform pick source ports.
  - `preserve`: preserve original source ports when possible.
  - `range`: limit translated source ports to `[start, end]`.

### IPv4PolicyRoute

`IPv4PolicyRoute` sends matching forwarded traffic out a specific egress.

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
    - 192.168.10.0/24
  destinationCIDRs:
    - 0.0.0.0/0
  routeMetric: 50
```

How routerd behaves: routerd marks IPv4 packets matching the source and
destination CIDRs, installs an `ip rule` for that mark, and installs a
default route in the dedicated routing table. This is the building block
for routing different LAN prefixes through different uplinks. Hash-based
load balancing is a separate resource (`IPv4PolicyRouteSet`).

### IPv4PolicyRouteSet

`IPv4PolicyRouteSet` selects between multiple egress targets by hashing the
flow.

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
    - 192.168.10.0/24
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

How routerd behaves:

- routerd renders nftables rules that restore an existing conntrack mark,
  pick a mark with `jhash` for new flows, save the chosen mark back into
  conntrack, and install one `ip rule` plus one routing table per target.
- Established flows stay on the same target through their conntrack mark.
- `hashFields` currently supports `sourceAddress` and `destinationAddress`.
- This is the recommended way to load balance multiple DS-Lite tunnels
  with different local IPv6 source addresses; each target usually points
  at a different `DSLiteTunnel`.

### IPv4ReversePathFilter

`IPv4ReversePathFilter` controls Linux `rp_filter` for cases where reverse
path checks would drop legitimate asymmetric traffic — common with policy
routing and multiple DS-Lite tunnels.

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

`spec.target` may be `all`, `default`, or `interface`. With
`target: interface`, `spec.interface` may reference an `Interface`,
`PPPoEInterface`, or `DSLiteTunnel`. `spec.mode` is `disabled`, `strict`,
or `loose`, mapping to Linux values 0, 1, and 2.

## PathMTUPolicy

`PathMTUPolicy` works out the effective path MTU between one downstream
interface and one or more upstream interfaces, then advertises that MTU and
clamps forwarded TCP MSS so end hosts converge on it.

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

How routerd behaves:

- `mtu.source: minInterface` takes the smallest configured MTU among
  `toInterfaces`. Plain `Interface` defaults to 1500, `PPPoEInterface`
  defaults to 1492, and `DSLiteTunnel` defaults to 1454. Any explicit
  `spec.mtu` on those resources wins.
- `mtu.source: static` uses `mtu.value` directly.
- `ipv6RA.enabled: true` advertises the resulting MTU through the
  referenced `IPv6DHCPScope` — for example, dnsmasq emits
  `ra-param=ens19,1454`.
- `tcpMSSClamp.enabled: true` installs nftables forward-chain MSS rules.
  The clamped MSS is derived from the effective MTU: IPv4 subtracts 40
  bytes and IPv6 subtracts 60 bytes. Omitting `families` enables both
  `ipv4` and `ipv6`.

## Firewall

The first firewall API is intentionally smaller than a general rule
language. It models home-router safety defaults and explicit service
exposure.

### Zone

A `Zone` names a group of router interfaces.

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
```

### FirewallPolicy

`FirewallPolicy` applies a preset and explicit chain defaults.

```yaml
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

The `home-router` preset installs:

- input default drop and forward default drop.
- invalid drop, established/related accept, loopback input accept.
- IPv6 control-plane input needed by the router itself: ICMPv6 and
  DHCPv6 client replies to UDP destination port 546 on WAN interfaces.
  The DHCPv6 rule intentionally does not constrain the server source
  port, because some home gateways reply from an ephemeral UDP port.
- LAN-to-WAN forward allow when both `lan` and `wan` zones are defined.
- SSH, DNS, and DHCP access to the router only from the configured
  `routerAccess` zones; SSH from WAN is gated by `routerAccess.ssh.wan`.

### ExposeService

`ExposeService` publishes one internal IPv4 service to the outside.

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
  internalAddress: 192.168.10.20
  internalPort: 443
  sources:
    - 203.0.113.0/24
  hairpin: true
```

How routerd behaves: routerd renders a DNAT rule plus a matching
forward-chain accept. `spec.sources`, when present, restricts the source
prefixes that may reach the published port. `spec.hairpin` is accepted in
the resource shape, but the first renderer does not yet synthesize
external-address hairpin rules — the external-address selection model is
still being designed.

## System resources

### NixOSHost

`NixOSHost` declares NixOS host-level settings for `routerd render nixos`.
It is not applied by runtime apply. The generated
`routerd-generated.nix` is meant to be imported from a small
`configuration.nix` and applied with `nixos-rebuild switch`.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: NixOSHost
metadata:
  name: nixos-edge
spec:
  hostname: nixos-edge
  domain: example.net
  stateVersion: "25.11"
  boot:
    loader: grub
    grubDevice: /dev/sda
  routerdService:
    enabled: true
    binaryPath: /usr/local/sbin/routerd
    configFile: /usr/local/etc/routerd/router.yaml
    applyInterval: 60s
  debugSystemPackages: true
  ssh:
    enabled: true
    passwordAuthentication: true
    permitRootLogin: "no"
  sudo:
    wheelNeedsPassword: false
  users:
    - name: admin
      groups:
        - wheel
      initialPassword: change-me
      sshAuthorizedKeys:
        - ssh-ed25519 AAAA...
```

How routerd behaves:

- `spec.hostname` and `spec.domain` render `networking.hostName` and
  `networking.domain`.
- `spec.boot.loader: grub` and `spec.boot.grubDevice` render the minimal
  GRUB boot loader settings needed by a generated NixOS host module.
- `spec.users` renders `users.users.<name>` entries, including SSH
  authorized keys.
- `spec.ssh` and `spec.sudo` render OpenSSH and sudo policy.
- `spec.routerdService.enabled: true` renders a local systemd unit for
  `routerd serve`. This is useful for simple NixOS hosts that use a
  source-installed `/usr/local/sbin/routerd` binary instead of importing
  the flake module. The service defaults are:
  `/usr/local/sbin/routerd`, `/usr/local/etc/routerd/router.yaml`,
  `/run/routerd/routerd.sock`, and a `60s` apply interval.
- `spec.debugSystemPackages` adds operational tools to
  `environment.systemPackages`. The package set is derived from resources,
  for example `dnsmasq`, `nftables`, `ppp`, `iproute2`, `dnsutils`,
  `iputils`, `tcpdump`, and `traceroute`.
- `Sysctl` resources with `persistent: true` render into
  `boot.kernel.sysctl`. Runtime-only sysctl resources remain the daemon's
  responsibility.
- `spec.additionalPackages` and `spec.additionalServicePath` allow
  explicit package additions.

### Hostname

`Hostname` declares the system hostname.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Hostname
metadata:
  name: system
spec:
  hostname: router03.example.net
  managed: true
```

`managed: false` keeps the resource as observation only and does not change
host state.

### Sysctl

`Sysctl` declares one kernel parameter.

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

`runtime: true` reflects the value into the running kernel during
apply. `persistent: true` is reserved for OS-specific rendering such as
sysctl.d or rc.conf and is not applied yet.

### NTPClient

`NTPClient` declares the local NTP client. The first implementation manages
`systemd-timesyncd` with a static server list.

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

How routerd behaves: when `interface` is set, routerd writes a per-link
`NTP=` drop-in through systemd-networkd for that interface. When omitted,
routerd writes the global `systemd-timesyncd` server list.

### LogSink

`LogSink` declares where routerd sends its own internal events — config
load, plan output, apply result, plugin errors, and so on.

Local journald or syslog:

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

Trusted local plugin:

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

Defaults: `enabled: true`, `minLevel: info`, `syslog.facility: local6`,
`syslog.tag: routerd`. For remote syslog, set `syslog.network` (`udp`,
`tcp`, `unix`, or `unixgram`) and `syslog.address` (for example
`syslog.example.net:514`).
