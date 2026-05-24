---
title: DNS resolver
slug: /concepts/dns-resolver
---

# DNS resolver

routerd splits DNS into small resources with a clear boundary between authoritative data, the resolver process, forwarding rules, and upstream endpoints.

`DNSZone` owns local authoritative data. It stores manual records and records derived from DHCP leases.

`DNSResolver` owns the daemon instance. It defines listen addresses, cache policy, metrics, and query logging. Each `DNSResolver` resource starts one `routerd-dns-resolver` process.

`DNSForwarder` owns one match rule for a resolver. It either serves `DNSZone` resources or forwards matching queries to `DNSUpstream` resources.

`DNSUpstream` owns one upstream endpoint. It can be plain UDP/TCP DNS, DoT, or DoH.

## Startup and partial bring-up

`DNSResolver` does not wait for every dependency before it serves. At startup it
brings up the daemon with whatever listen addresses and sources already resolve,
and converges later as the rest become ready:

- A listen entry binds the addresses that resolve now; an address whose `*From`
  source is not ready yet (for example a delegated-prefix address still waiting
  on DHCPv6 prefix delegation) is added on a later reconcile.
- A forward/upstream source whose dynamic upstream is unresolved (for example an
  AFTR forwarder whose upstream comes from a `DHCPv6Information` server) is
  omitted until that upstream appears. Zone sources, and sources with static or
  already-resolved upstreams, are served immediately.

While some parts are still waiting, the resource reports `phase: Degraded` with a
`waiting` list naming each listen/source and what it is waiting on. This is a
normal bootstrap state, not a failure: general DNS already answers. Once the
dependencies publish their status the controller re-reconciles and converges to
`phase: Applied` with the full configuration (identical to a fully-resolved
start). The resolver only reports `phase: Pending` (serving nothing) when no
listen address resolves at all, or no usable source remains.

This removes the boot-time window where DNS was refused while waiting on a
DHCPv6 prefix delegation (measured on a production router: general DNS answered
from the first second while the AFTR forwarder showed `Degraded`, then converged
to `Applied` once the delegated prefix arrived). A deliberate `routerd` restart
still has a sub-second gap while the process itself restarts.

## Source ordering

`DNSForwarder` resources that reference a resolver are evaluated in config order.
A forwarder with `zoneRefs` answers from `DNSZone`.
A forwarder with `upstreams` sends matching queries to selected upstreams.
Use `match: ["."]` for the default recursive path.

The resolver supports DoH, DoT, TCP DNS, and plain UDP DNS.
It tries upstreams by priority and falls back when a higher source fails.

## Multiple listen profiles

`spec.listen` is a list.
Each entry can select a subset of source names.
This allows LAN and VPN listeners to behave differently while sharing one resolver resource.
The names in `listen[].sources` refer to `DNSForwarder` resources. If omitted,
the listener uses every forwarder attached to the resolver.

Use `listen[].addressFrom` when a listen address comes from another
resource status. This keeps the dependency explicit and lets the controller
reconfigure the daemon when the source resource changes.

```yaml
listen:
  - name: lan
    addresses:
      - 192.0.2.1
    addressFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    port: 53
```

If a required address source is not available yet, the resolver stays
`Pending(AddressUnresolved)` instead of starting with a stale address.

## Dynamic zone records

`DNSZone.spec.records[].ipv4` and `ipv6` are literal addresses.
Use `ipv4From` or `ipv6From` when a record address comes from another
resource status.

```yaml
records:
  - hostname: router
    ipv4From:
      resource: IPv4StaticAddress/lan-base
      field: address
    ipv6From:
      resource: IPv6DelegatedAddress/lan-base
      field: address
```

If a required record source is not available yet, the record field is marked in
`DNSZone.status.pendingRecords`. The resolver is regenerated when the source
resource changes, and the record is published after the field resolves.

## Network-constrained upstreams

`DNSUpstream.spec.sourceInterface` binds outgoing queries to a specific interface on Linux.
Use a literal OS interface name, for example `ens18` or `wg0`.
When a tunnel or VRF resource creates that interface, make the dependency
explicit with resource ownership or ordering and keep the resolver pending
until the interface exists.

`DNSUpstream.spec.bootstrap` supplies DNS server addresses for resolving DoH and DoT endpoint names.
This is useful when the endpoint name is only resolvable inside an access network.

Use `addressFrom` when the upstream server list comes from another resource
status.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSForwarder
metadata:
  name: ngn-aftr
spec:
  resolver: DNSResolver/lan-resolver
  match:
    - transix.jp
  upstreams:
    - DNSUpstream/wan-dns
---
apiVersion: net.routerd.net/v1alpha1
kind: DNSUpstream
metadata:
  name: wan-dns
spec:
  protocol: udp
  addressFrom:
    - resource: DHCPv6Information/wan-info
      field: dnsServers
```

`DNSResolver.spec.sources` is not accepted in user YAML. Split old inline
source entries into `DNSForwarder` and `DNSUpstream`.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSForwarder
metadata:
  name: default
spec:
  resolver: DNSResolver/lan-resolver
  match:
    - "."
  upstreams:
    - DNSUpstream/cloudflare
---
apiVersion: net.routerd.net/v1alpha1
kind: DNSUpstream
metadata:
  name: cloudflare
spec:
  protocol: doh
  address: cloudflare-dns.com
  path: /dns-query
```

## dnsmasq boundary

dnsmasq is now limited to DHCPv4, DHCPv6, DHCP relay, and RA.
It does not generate `server=`, `local=`, or `host-record=` lines.
All DNS answering and forwarding goes through `routerd-dns-resolver`.
