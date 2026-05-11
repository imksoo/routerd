# Phase 3.5 router03 recovery

Date: 2026-05-11

Scope: recover router03 management reachability and bring it onto current routerd binaries and schema. The local router03 YAML is kept under `local/`, which is intentionally ignored, so this note records the deployed state.

## Root causes

- `ens20` management link was down and had no IPv4 address.
- The current minimal router03 config uses routerd-supervised DHCPv4 client daemons for `ens18` and `ens20`.
- `routerd.service` did not allow `AF_PACKET`, so child `routerd-dhcpv4-client` processes could not open Linux packet sockets and failed with `address family not supported by protocol`.
- `/etc/resolv.conf` is a systemd-resolved stub symlink on router03, so direct file writes did not populate effective DNS. DHCPv4 DNS must be applied through `resolvectl`.

## Fixes

- Added `AF_PACKET` to the generated DHCPv4 client systemd unit address-family allowlist.
- Ensured systemd units are unmasked before enable/restart so previously masked units converge declaratively.
- Updated the DHCPv4 lease controller to use `resolvectl dns <ifname> ...` and `resolvectl domain <ifname> ~.` when `/etc/resolv.conf` points at systemd-resolved.
- Deployed rebuilt static Linux binaries and the current router03 YAML over IPv6 link-local, then verified via management IPv4.

## Evidence

router03:

```text
routerd v20260511.1428
routerd.service: active
routerctl status: phase=Healthy generation=9 resourceCount=17
ens18=192.168.1.32/24
ens20=192.168.123.125/24
default via 192.168.1.1 dev ens18 metric 100
resolvectl dns ens18: 192.168.1.66 192.168.1.67
curl -4 https://www.google.com/generate_204: 204
```

The recovery target for Phase 3.5 P1-2 is complete: management SSH over `192.168.123.125` works, DHCPv4 lease daemons are running, IPv4 default route is present, DNS resolution works, and routerd reports Healthy.

## Follow-up

The router03 config is intentionally minimal for recovery. Full DS-Lite/NAT/firewall parity with router05/homert02 can be handled in a later chain if router03 is promoted from lab recovery target to parity target.
