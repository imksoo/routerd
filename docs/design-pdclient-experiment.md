# DHCPv6-PD client experiment

This note records the replacement direction for DHCPv6 prefix delegation.
It is intentionally experimental and does not promise compatibility with the
current `IPv6PrefixDelegation.spec.client` implementations.

## Direction

`IPv6PrefixDelegation` should be owned by a small routerd PD client service,
not by systemd-networkd, dhcpcd, or dhcp6c plus hook scripts and packet
recorders.

The target shape is:

```text
routerd-pdclient process
  owns DHCPv6-PD session state
  sends Solicit / Request / Renew / Rebind / Release
  writes one lease snapshot to routerd state DB

routerd process
  reads currentPrefix from state DB
  applies LAN-side addresses, RA, DHCPv6, DNS, firewall, and routes
```

The process boundary is deliberate. A normal OS service is easier to supervise,
restart, observe, and replace than a renderer that generates another DHCP
client's configuration and then infers what happened from hooks.

## Package Boundary

The first package is `pkg/pdclient`.

It is OS-independent:

- DHCPv6 payload encode/decode.
- IA_PD state machine.
- timers for T1, T2, and valid lifetime expiry.
- a `Transport` interface for platform-specific packet IO.
- a `Store` interface and plain `Snapshot` for DB integration.

It does not:

- bind sockets directly,
- call `ip`, `ifconfig`, `systemctl`, or `service`,
- render LAN-side config,
- mutate addresses,
- know about dnsmasq.

Linux and FreeBSD transports should be separate packages or build-tagged files.
Tests should be able to run the same state machine against an in-memory fake
transport and fake DHCPv6 server.

## LAN Reflection

LAN reflection must stay separate. The PD client publishes:

- resource name,
- uplink interface,
- state,
- current prefix,
- server DUID,
- IAID,
- T1/T2/preferred/valid timing,
- expiry timestamps.

The LAN-side controller consumes `currentPrefix` in the same way static address
logic consumes a configured prefix. That keeps delegated LAN addresses, static
IPv6 addresses, RA, DHCPv6, and DNS service on the same apply path.

## Initial MVP

The first useful MVP is NTT-oriented but not Linux-specific:

- DUID-LL or explicit DUID.
- IA_PD only; no IA_NA.
- Rapid Commit disabled.
- Solicit -> Advertise -> Request -> Reply.
- Renew at T1.
- Rebind at T2.
- Clear `currentPrefix` at valid lifetime expiry.
- No active fallback packets outside the state machine.

After that works with a fake server, add platform transports and then connect
the snapshot to the routerd SQLite state DB.
