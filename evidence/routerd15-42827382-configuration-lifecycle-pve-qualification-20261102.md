# routerd 1.5 Linux PVE configuration lifecycle qualification

## Scope

This record covers the routerd 1.5 configuration lifecycle on an isolated
Linux PVE router and one independent VLAN client. It is limited to rejection,
validation and planning, replacement, client-visible DNS change, and rollback
of a fresh routerd 1.5 configuration. It is not an in-place routerd 1.0
upgrade or configuration migration test.

The router binary at `/usr/local/sbin/routerd` had SHA-256
`65d1f768e6f950fe8d2cf1b824cd65a39c0cb3880ae45e95aa935cfbfe06207c`.
The same SHA-256 was obtained by rebuilding source revision
`428273825467d5b23e9bf8d30baf579696cffcad` with
`CGO_ENABLED=0 go build -trimpath ./cmd/routerd`.

The router was VM219 and the independent client was VM220 on PVE VLAN 3131.
The starting fresh routerd 1.5 configuration had 21 resources and a healthy
generation 14. VM220 used a separate network namespace and queried the
router directly at `198.18.15.1`.

## Observed result

- `routerctl validate` accepted a 21-resource replacement candidate. Its
  `routerctl plan` result was healthy and described the intended resource
  effects before activation.
- Applying a malformed candidate failed at parsing with `bad request: parse
  config candidate`. The router remained healthy at generation 14 with 21
  resources.
- The valid replacement added the manual
  `lifecycle-check.home.arpa` record. It committed as healthy generation 15,
  and the independent VLAN client resolved that record to `198.18.15.1`.
- `routerctl rollback --to 16` created healthy generation 18 with 21
  resources. The independent client still resolved `router.home.arpa` to
  `198.18.15.1`; `lifecycle-check.home.arpa` no longer returned an address.

The rollback command commits the selected saved configuration as a new latest
generation. Therefore, the verified final generation is 18 rather than the
selected historical generation 16.

## Unverified scope

This is evidence only for the listed Linux configuration operations and DNS
visibility through an independent PVE VLAN client. It does not establish
interrupted replacement recovery, durable behavior across host reboot,
configuration changes for every resource kind, high availability, target
device qualification, or FreeBSD behavior. It also does not qualify all
routerd 1.0 functions.
