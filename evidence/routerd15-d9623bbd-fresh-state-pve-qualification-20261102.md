# routerd 1.5 current Linux PVE qualification

## Scope

This record qualifies the Linux binary built from source revision
`d9623bbd867d450ff8d1be03c59222592dbf43ad`. The revision removes the discarded
routerd 1.0 handoff implementation. It does not add, test, or require an
in-place 1.0-to-1.5 upgrade, old configuration reader, database import, or
cross-version cutback.

The router fixture used a converted routerd 1.5 configuration and state created
by a prior fresh routerd 1.5 installation. Its configuration SHA-256 was
`2659c5125c9099aec6075b7107df000178ff3c0698fcc63c5d0da5cd96b636af`.

## Executed setup

- Package: `routerd-v20261027.1620-linux-amd64.tar.gz`
- Package SHA-256:
  `50a8f4f721caa8c586a2c39a2c433acc88e98be68712fb5ffd453b273b581e34`
- Router: isolated Linux PVE VM, actual system service and kernel networking
- Client and peers: separate isolated Linux PVE VM, with independent LAN, WAN,
  and management test paths

The package was installed with the ordinary installer while retaining the
already-fresh 1.5 configuration and state. No 1.0 configuration, database, or
handoff marker was supplied to routerd.

## Observed result

The router reported the expected binary revision and healthy state:

```text
routerd v20261027.1620 (d9623bbd)
STATUS  HEALTHY  generation=0  resources=21
active
```

The independent client/peer checks passed:

```text
PASS: live lease, DNS UDP/TCP, NAT and management positive control
PASS: WAN/LAN/management and DNS isolation
```

For the configured DHCP reservation, the independent client was temporarily
given the reservation MAC address. It received `198.18.15.150`, and
`printer.home.arpa` resolved to that address over both UDP and TCP. The client
then returned to its original MAC address and reacquired the ordinary dynamic
lease `198.18.15.107`.

The router test VM was left running on `d9623bbd`. The legacy cutback ISO,
legacy state archives, and package transfer ISO were detached and removed after
the check.

## Unverified scope

This is evidence for the listed Linux DHCP, DNS, IPv4 NAT, management, and
isolation paths only. DHCP lease expiry, conflicts, and DHCP server restart
remain unverified. This record does not establish every routerd 1.0 function,
IPv6 functions, all WAN modes, high availability behavior, or FreeBSD behavior.
