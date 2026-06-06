# Cloud Edge SAM transport layering

Cloud Edge SAM should keep address mobility routes out of WireGuard
`AllowedIPs`.

WireGuard treats `AllowedIPs` as cryptokey routing state. Outbound packets use
the inner destination address to select a peer, and inbound packets are accepted
only when the decrypted inner source address is allowed for that peer. This is
the right behavior for WireGuard, but it conflicts with SAM mobility when BGP,
route reflectors, or ECMP can move a `/32` between peers.

## Recommended layers

Trusted on-prem or home underlay can use direct IPIP or GRE:

```text
physical underlay
  IPIP or GRE tunnel
    SAM overlay packets
```

Encrypted transport should keep WireGuard as an endpoint-only layer and put IPIP
or GRE above it:

```text
physical underlay
  WireGuard endpoint transport
    IPIP or GRE tunnel
      SAM overlay packets
```

In that model, WireGuard peers should only contain router-to-router endpoint
prefixes such as `10.252.0.2/32`. SAM prefixes such as
`192.168.123.10/32` remain in BGP, the kernel FIB, and SAM resources.

## Protocol choice

Use IPIP first when SAM carries IPv4 mobility prefixes and the underlay is
trusted or already encrypted. It adds the least tunnel overhead while preserving
the separation between WireGuard cryptokey routing and SAM route mobility.

Use GRE when the deployment needs protocol identification beyond IPv4, a GRE
key, or stronger FreeBSD interoperability.

Avoid VXLAN, Geneve, or GRETAP unless L2 semantics are explicitly required. SAM
is selective L3 address mobility, so L2 overlay headers are usually unnecessary.

FOU and GUE can help when UDP encapsulation is useful on the physical underlay,
but using them inside WireGuard usually adds overhead without improving physical
underlay load balancing, because the physical network only sees WireGuard UDP.

## Configuration ergonomics

Low-level resources must still support DHCP/IPAM-safe endpoints. A
`TunnelInterface` should be able to derive `local` or `remote` from resource
status so operators do not duplicate DHCP-managed addresses in static config.
When the underlay address is managed outside routerd, such as a live image
DHCP client, use the adopted interface status:

```yaml
apiVersion: hybrid.routerd.net/v1alpha1
kind: TunnelInterface
metadata:
  name: tun-k8s-rt02
spec:
  mode: ipip
  localFrom:
    resource: Interface/mgmt
    field: primaryIPv4
  remote: 192.168.1.53
  trustedUnderlay: true
```

`primaryIPv4` may include a prefix length, for example `192.168.1.32/24`; the
tunnel controller resolves it to the address form required by `ip tunnel`.
When using an `Interface/...` status source, the link controller must publish the
interface status into the same state database. Normal `routerd serve` runs this
controller, but isolated tunnel tests should include both `link` and `tunnel`.

Common SAM topologies can use `SAMTransportProfile` to generate the low-level
`TunnelInterface`, endpoint `/32` route, and `BGPPeer` resources while preserving
these invariants:

- WireGuard `AllowedIPs` contains only transport endpoint prefixes.
- SAM mobility `/32`s are never injected into WireGuard peers.
- IPIP/GRE endpoint addresses can come from DHCP/IPAM-derived status fields.
- MTU and MSS behavior is explicit for each transport mode.

`spec.selfNodeRef` is required on every router. It is the stable identity used
for deterministic `/31` inner address derivation; routerd does not infer it from
hostname or BGP router ID. Each edge is keyed by the sorted pair of
`selfNodeRef` and peer `nodeRef`, then allocated from `innerPrefix` in sorted
edge order, so both ends derive the same `/31` with local and remote addresses
reversed.

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: SAMTransportProfile
metadata:
  name: lab-sam-transport
spec:
  selfNodeRef: pve-rt
  mode: ipip
  innerPrefix: 10.255.1.0/24
  underlayInterface: wg-hybrid
  localEndpointFrom:
    resource: Interface/wg-hybrid
    field: primaryIPv4
  bgp:
    routerRef: BGPRouter/mobility
    peerASN: 64512
    timersPreset: fast
  peers:
    - nodeRef: k8s-rt
      remoteEndpoint: 10.99.0.2
```

Explicit peer overrides can pin generated resource names, the per-peer underlay
interface, or the local/remote inner addresses. If either `localInner` or
`remoteInner` is overridden, both must be supplied and the pair must be a valid
`/31` inside `innerPrefix`.

The controller writes one `DynamicConfigPart` per profile and self node. Peer
removal replaces that part with the new generated resource set. Profile deletion
replaces the old part with an empty active part, causing the effective config to
drop generated tunnels, BGP peers, and endpoint routes. The existing
`TunnelInterface`, `BGPPeer`, and `IPv4Route` controllers then perform their
normal stale-resource teardown; this is the current GC boundary until the broader
resource lifecycle work is completed.

Related issues:

- #194: decouple SAM mobility prefixes from WireGuard `AllowedIPs`.
- #196: allow `TunnelInterface` endpoints to come from resource status.
- #197: add compact SAM underlay transport profiles.

References:

- WireGuard conceptual overview and cryptokey routing:
  https://www.wireguard.com/
- Linux tunnel link types:
  https://man7.org/linux/man-pages/man8/ip-link.8.html
- FreeBSD GRE:
  https://man.freebsd.org/gre/4
