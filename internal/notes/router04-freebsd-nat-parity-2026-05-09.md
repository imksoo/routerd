# router04 FreeBSD NAT parity check - 2026-05-09

## Scope

Mature router04 NAT toward the homert02 model without changing the high-level
WAN fallback chain.

## Changes

- Replaced the single dynamic `NAT44Rule` tied to `EgressRoutePolicy/ipv4-default`
  with explicit per-interface NAT44 rules.
- Covered all router04 IPv4 egress candidates:
  - `ds-lite-a` / `gif41`
  - `ds-lite-b` / `gif42`
  - `ds-lite-c` / `gif43`
  - `ds-lite-ra` / `gif44`
  - `pppoe-flets` / `ppp-flets`
  - `wan` / `vtnet0`
- Kept RFC 1918 destination exclusions so private routed destinations are not
  translated.
- Fixed NAT44 controller alias resolution for `PPPoEInterface`, so runtime pf
  anchor rendering resolves `pppoe-flets` to `ppp-flets`.
- Changed NAT44 artifact reporting on FreeBSD from `nft.table` to `pf.anchor`.

## Validation

- `go test ./...`: passed.
- `make check-schema`: passed.
- `routerd validate --config local/router04.yaml`: passed.
- router04 apply:
  - status: `Healthy`
  - generation: `1081`
  - resource count: `73`
- `pfctl -s nat` includes NAT rules for `gif41`, `gif42`, `gif43`, `gif44`,
  `ppp-flets`, and `vtnet0`.
- `pfctl -a routerd_nat -sn` matches the per-interface rule set after restarting
  routerd with the updated binary.
- NAT smoke test from `nwadmin03` over `ens19` through router04:
  - temporary host route to a Google IPv4 address via `192.168.160.4`
  - `curl --interface ens19 --resolve www.google.com:443:<target> https://www.google.com/generate_204`
  - result: HTTP `204`, about `0.055s`
- pf state showed translated flow:
  - original: `192.168.160.182:<port> -> <google-ip>:443`
  - translated: `192.0.0.2:<port> -> <google-ip>:443`

## Remaining gap

router04 still does not implement homert02-style Linux `IPv4PolicyRouteSet`
source-hash distribution. FreeBSD has the NAT coverage needed for every egress
candidate and the default route fallback chain works. A future parity step can
map `IPv4DefaultRoutePolicy` to pf `route-to` rules if we want FreeBSD to
perform per-client balancing rather than selected-default failover.
