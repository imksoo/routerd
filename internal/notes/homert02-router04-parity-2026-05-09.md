# homert02 and router04 resource parity audit, 2026-05-09

Scope: compare homert02 production YAML with the router04 FreeBSD fallback-chain YAML after the DS-Lite / PPPoE / HGW fallback work.

## Result

homert02 already has the same core resource semantics as router04 for the common pieces:

- `EgressRoutePolicy/ipv4-default` owns the selected WAN candidate.
- `IPv4Route/default` derives `deviceFrom` and `gatewayFrom` from `EgressRoutePolicy/ipv4-default`.
- `PathMTUPolicy/lan-to-dslite-mtu` advertises LAN RA MTU and enables IPv4 TCP MSS clamping.
- PPPoE is kept in YAML but disabled, so the production line does not leak a PPPoE session.
- DS-Lite primary remains preferred over RA DS-Lite, PPPoE, IX2215, and HGW direct fallback.

## NAT44 difference

router04 uses a single `NAT44Rule/lan-to-dslite` with `egressPolicyRef: ipv4-default` because its lab validation selects one active egress candidate at a time.

homert02 intentionally keeps per-interface NAT44 rules:

- `lan-to-dslite-a`
- `lan-to-dslite-b`
- `lan-to-dslite-c`
- `lan-to-dslite-ra`
- `lan-to-pppoe-flets`
- `lan-to-wan-hgw`

This is not a missing parity item. homert02 still has `IPv4PolicyRouteSet/dslite-pd-balanced` and `IPv4DefaultRoutePolicy/lan-forward-egress` for source-hash distribution across the three PD DS-Lite tunnels. A single NAT rule resolved from `EgressRoutePolicy/ipv4-default` would only NAT the currently selected default device and would not cover packets policy-routed to the other DS-Lite tunnels. The per-interface NAT rules are therefore the correct Linux production expression for the existing balanced path.

## Live validation

Before apply, homert02 reported:

- routerd status: `Healthy generation=43 resources=88`
- IPv4 default: `default dev ds-lite-a scope link`
- NAT table contained masquerade rules for `ds-lite-a`, `ds-lite-b`, `ds-lite-c`, `ds-lite-ra`, `pppoe-flets`, and `ens18`.

The local YAML validates and plans as `Healthy` for:

- `EgressRoutePolicy/ipv4-default`
- `IPv4DefaultRoutePolicy/lan-forward-egress`
- `IPv4Route/default`
- all six NAT44 rules
- `PathMTUPolicy/lan-to-dslite-mtu`

After applying the local YAML with the latest `routerd` binary, homert02
reported:

- routerd status: `Healthy generation=44 resources=88`
- IPv4 default: `default dev ds-lite-a scope link`
- PPPoE service: `inactive` and `disabled`
- PPPoE health check service: `inactive` and `disabled`
- NAT table still contained masquerade rules for `ds-lite-a`, `ds-lite-b`,
  `ds-lite-c`, `ds-lite-ra`, `pppoe-flets`, and `ens18`.
