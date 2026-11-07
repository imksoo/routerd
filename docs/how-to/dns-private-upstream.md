---
title: Private DNS upstreams
slug: /how-to/dns-private-upstream
---

# Private DNS upstreams

![Diagram showing DNSForwarder and DNSUpstream routing local, provider, and default DNS queries through UDP, TCP, DoT, DoH, and addressFrom sources](/img/diagrams/how-to-dns-private-upstream.png)

## Scenario

You want the resolver on the router to:

- Forward queries for the access network's internal zones (e.g. an ISP's AFTR FQDN, an enterprise intranet domain) to a specific DNS server learned dynamically.
- Use a private encrypted DNS provider (DoH / DoT) as the default upstream.
- Keep a fast plain-DNS fallback if the encrypted upstream becomes unhealthy.
- Avoid exposing provider account IDs or private endpoints in shared examples.

## How routerd solves it

`DNSResolver` runs `routerd-dns-resolver`. The daemon listens on UDP/TCP. `DNSForwarder` resources attached to the resolver define the ordered match rules, and `DNSUpstream` resources define reusable upstream endpoints.

| Scheme | Protocol | Default port |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL-dependent |
| `tls://` | DNS over TLS | 853 |
| `udp://` | Plain DNS over UDP | 53 |
| `tcp://` | Plain DNS over TCP | 53 |

The order in `DNSForwarder.spec.upstreams` is the priority order. routerd tries the highest-priority upstream that is healthy, and falls back through the list when one fails.

`DNSUpstream.spec.addressFrom` lets the upstream address list come from another resource's status. This is the mechanism for using DNS servers learned through DHCPv6 information request.

A non-empty `DNSResolver.spec.listen[].sources` list restricts that listener to
the named forwarders. When a selected forwarder becomes ineligible through
`when` or loses an eligible dependency, the listener must not gain access to
other forwarders. The declared selection remains in force when none of its
sources are available. Omitting `sources` selects all of the resolver's sources.

If a selected forwarder is waiting for a dynamic upstream address or required
bootstrap resolver, its match rule stays in place. Matching queries receive
`SERVFAIL` until the dependency resolves; they are not sent to another forwarder.
Unrelated queries can still use ready sources. Resolver status reports
`WaitingForDependencies`, and worker upstream status marks the source `Pending`.

## Conditional forwarding example

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: lan-resolver
  spec:
    listen:
      - name: lan
        addresses:
          - 192.0.2.1
        port: 53
        sources:
          - local-zone
          - access-network
          - provider-bootstrap
          - default
    cache:
      enabled: true
      maxEntries: 10000
      minTTL: 60s
      maxTTL: 24h
      negativeTTL: 30s
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: local-zone
  spec:
    resolver: DNSResolver/lan-resolver
    match: [lan.example.org]
    zoneRefs: [DNSZone/lan]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: access-network
  spec:
    resolver: DNSResolver/lan-resolver
    match: [transix.jp, corp.example.com]
    upstreams: [DNSUpstream/access-network]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: access-network
  spec:
    protocol: udp
    addressFrom:
      - resource: DHCPv6Information/wan-info
        field: dnsServers
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: provider-bootstrap
  spec:
    resolver: DNSResolver/lan-resolver
    match: [dns.example-provider.net]
    upstreams: [DNSUpstream/cloudflare-udp, DNSUpstream/google-udp]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: default
  spec:
    resolver: DNSResolver/lan-resolver
    match: ["."]
    upstreams: [DNSUpstream/provider-doh, DNSUpstream/provider-dot, DNSUpstream/cloudflare-udp]
    healthcheck:
      interval: 15s
      timeout: 3s
      failThreshold: 3
      passThreshold: 2
    dnssecValidate: true
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: provider-doh
  spec:
    protocol: doh
    address: dns.example-provider.net
    path: /dns-query
    sourceInterface: ens18
    bootstrap: [1.1.1.1, 2606:4700:4700::1111]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: provider-dot
  spec:
    protocol: dot
    address: dns.example-provider.net
    sourceInterface: ens18
    bootstrap: [1.1.1.1, 2606:4700:4700::1111]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: cloudflare-udp
  spec:
    protocol: udp
    address: 1.1.1.1
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: google-udp
  spec:
    protocol: udp
    address: 8.8.8.8
```

On the routerd 1.5 proposal branch, `minTTL` is a cache admission threshold:
answers with a shorter authoritative lifetime are served without caching.
It never extends an upstream TTL. `maxTTL` and `negativeTTL` cap retention;
negative answers also require an SOA and respect the lesser of its TTL and
MINIMUM field. Cache hits count down the remaining lifetime, including authority
and additional records, while preserving EDNS flags.

## NextDNS with a one-second query deadline

Use the following resources with an existing `DNSResolver/lan-resolver`.
The complete, loopback-only example is
[`examples/dns-nextdns-failover.yaml`](https://github.com/imksoo/routerd/blob/main/examples/dns-nextdns-failover.yaml).
Replace `YOUR_PROFILE_ID` only in your local configuration.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: default
  spec:
    resolver: DNSResolver/lan-resolver
    match: ["."]
    queryTimeout: 1s
    upstreams: [DNSUpstream/nextdns, DNSUpstream/cloudflare]
    healthcheck:
      interval: 15s
      timeout: 3s
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: nextdns
  spec:
    protocol: doh
    address: dns.nextdns.io
    path: /YOUR_PROFILE_ID
    bootstrap: [1.1.1.1]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: cloudflare
  spec:
    protocol: doh
    address: cloudflare-dns.com
    path: /dns-query
    bootstrap: [1.1.1.1]
```

For an uncached query, NextDNS is preferred when healthy. If it does not respond
within one second, or the attempt fails, routerd tries the explicitly listed
Cloudflare DoH endpoint. It does not wait for the background health-check
failure threshold. A valid `NXDOMAIN` or other valid answer does not trigger a
switch. Cloudflare does **not** apply your NextDNS filtering or logging policy.

`queryTimeout` applies separately to each candidate; two attempts can take about
two seconds plus local processing. Without it, `healthcheck.timeout` remains the
query deadline (default `3s`). In a LAN → core chain, configure the outer timeout
with room for the inner attempts. Exhausting the candidates returns uncached
`SERVFAIL` without trying a different forwarder or public default source.

## Provider bootstrap

Some private DNS providers serve their resolver endpoint from a domain that the resolver itself is going to use. If a host tries to resolve that provider name through the provider, the query loops or fails before the resolver is healthy.

Add a conditional source for the provider domain that points to a public resolver or to access-network DNS. Keep account IDs (e.g. provider profile IDs) out of shared examples; put them only in your local secrets file or in a per-host YAML overlay.

The `DNSUpstream.spec.bootstrap` field provides the same protection at a finer granularity: it specifies which resolvers to use for resolving the upstream endpoint name itself, before the encrypted transport is established. Use `bootstrapFrom` when those resolver addresses come from another resource status, such as DHCPv6-provided DNS servers.

## Interface binding

`DNSUpstream.spec.sourceInterface` binds outgoing DNS queries to a specific Linux interface name. Use a literal OS interface name such as `ens18`. If the interface is created by another resource (e.g. a tunnel), declare the relationship with `ownerRefs` or resource ordering and keep the resolver pending until the interface exists.

FreeBSD does not currently provide the same `SO_BINDTODEVICE` enforcement, so platform-specific docs do not promise identical behaviour there.

## See also

- [Local DNS zones](./dns-local-zone.md)
- [DNS resolver concept](../concepts/dns-resolver.md)

## Verify listener isolation during source withdrawal

The local regression exercises when filtering, controller-generated runtime
configuration, worker reload and actual UDP/TCP questions:

```sh
go test ./cmd/routerd-dns-resolver -run '^TestWhenFilteredListenerCannotSelectAnotherDNSView$' -count=1
```

For a native package check, use an isolated Linux guest with two DNS listeners.
Select only `public` on the first and only `private` on the second. Give each
forwarder a separate zone and record. Gate the public forwarder on a HealthCheck
against a disposable TCP service; keep the private forwarder unconditional.

1. With the health peer available, verify each listener's own record using
   `dig` over UDP and TCP. The public listener must not return the private record.
2. Stop only the health peer. Wait for `HealthCheck` to report `Unhealthy` and
   for the generated resolver configuration to omit the public source. Repeat
   the private-record queries against both listeners. Only the private listener
   may answer it.
3. Restart the health peer. Wait for `Healthy` and the public source to return;
   verify the original answers and isolation without restarting routerd.
4. Withdraw the test configuration. Require a healthy empty resource set and
   no remaining resolver helper or test DNS listener. Stop the health peer.

Use a bounded wait for the configured probe thresholds and reconciliation.
A timeout, a leaked private answer, or recovery requiring product-state repair
fails the check. Record the executed package revision and outcome; retain raw
output only while investigating a failure. This check covers explicit listener
source selection, not every conditional-domain or dynamic-upstream policy.

## Verify DHCP-derived private DNS withdrawal

Use an isolated Linux guest and an independent DHCP/DNS peer. Advertise that
peer as the DHCP DNS server, select it through `DNSUpstream.addressFrom`, and
place its private-domain forwarder before a separate default forwarder. Gate the
DHCP client on a disposable HealthCheck peer. Log queries at the default DNS
peer so a private question sent there is detectable, even if no answer returns.

1. With the health peer stopped and no WAN lease, require `Unhealthy` and the
   private source in runtime `pendingSources`. Private UDP/TCP questions must
   return SERVFAIL; public questions must still resolve.
2. Start the health peer. Within 90 seconds, require an actual DHCP lease and
   its DNS server in status, then verify private and public UDP/TCP answers.
3. Stop the health peer. Within 60 seconds, require `Unhealthy`, removal of the
   DHCP-owned IPv4 address, and the private source pending again. Repeat step 1.
4. Withdraw the configuration. Require healthy empty status, no managed helpers,
   no test DNS listener, and no DHCP-owned address **before** deleting the test
   network. Stop independent peers and remove their isolated network afterward.

Any private question in the default peer's log, retained DHCP address, timeout,
or manual repair of product state fails the check. Record the package revision
and stage that failed. The local ownership regression also verifies that a failed
address deletion retains its receipt and error until a later retry succeeds:

```sh
go test ./pkg/controller/chain -run '^TestWhenStatusPublicationCannotEraseDHCPv4WithdrawalOwnership$' -count=1
```
