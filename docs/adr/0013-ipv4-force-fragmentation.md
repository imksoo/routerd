# ADR 0013: IPv4 force fragmentation for trusted overlay paths

## Status

Accepted for pre-release implementation.

## Context

routerd already derives path MTU handling from tunnel and forwarding intent. The
normal mitigation is TCP MSS clamping: on Linux, `routerd_mss` rewrites TCP SYN
MSS for derived low-MTU forwarding paths without requiring a firewall zone.

MSS clamping does not help non-TCP traffic. Oversized UDP, QUIC, ICMP, or other
IPv4 packets with DF set can still black-hole when a trusted overlay or underlay
has lower effective MTU and PMTUD feedback is blocked or ignored.

Clearing DF is not a general Internet default. It violates the sender's explicit
path-MTU preference and can create fragments that are more expensive to forward
and easier to drop. The feature therefore must be explicit, path-scoped, and
default off.

## Decision

Add an explicit IPv4 force-fragment option to overlay path MTU intent:

- `OverlayPeer.spec.pathMTU.forceFragmentIPv4`
- `TunnelInterface.spec.pathMTU.forceFragmentIPv4`

The feature is supported only for trusted routerd overlay devices where routerd
can derive the forwarded path and effective MTU: `wireguard`, `ipip`, `gre`,
`fou`, and `gue`. Validation rejects `route`, `tailscale`, `ipsec`, or other
underlay types when force fragmentation is enabled.

On Linux, routerd renders a dedicated nftables table:

```text
table ip routerd_forcefrag {
  chain forward {
    type filter hook forward priority mangle; policy accept;
    iifname <capture> oifname <tunnel> ip length > <path-mtu> ip frag-off 0x4000 ip frag-off set 0
  }
}
```

The match is IPv4-only and scoped to derived forwarded paths. It clears DF only
for oversized, currently unfragmented DF packets. The kernel then fragments on
the egress device according to the normal interface MTU.

TCP MSS clamping remains the primary TCP mitigation. Force fragmentation is a
catch-all for non-TCP or incorrectly sized traffic on explicitly trusted paths.

## Alternatives

- **Route MTU lock.** More standard for routes that routerd owns, but it does not
  cover every route origin cleanly, including BGP-imported mobility paths, and
  would spread policy across route writers.
- **iptables.** Existing stock targets do not provide a cleaner cross-path
  expression than nftables for DF clearing.
- **FreeBSD pf in the first phase.** pf has `scrub ... no-df`, but routerd's
  live SAM/overlay dataplane is Linux-first. FreeBSD support is left for a later
  phase rather than silently pretending parity.

## Consequences

- Default behavior is unchanged.
- Linux receives a second router-owned path-MTU nftables table,
  `routerd_forcefrag`, next to `routerd_mss`.
- Operators must opt in per overlay path or tunnel interface.
- Fragmentation can reduce throughput and increase packet loss sensitivity; docs
  should describe it as a last-resort PMTU black-hole workaround for trusted
  overlays.
