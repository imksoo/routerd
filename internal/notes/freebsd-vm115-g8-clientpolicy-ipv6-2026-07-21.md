# FreeBSD VM115 G8 ClientPolicy IPv6 acceptance — 2026-07-21

Commit: `78fec5b241e59450f1eab4ca973909fac2ff24ed` (production change
`77f58fa046a6fe0d62c63f62198c463d21e01c9f`).

FreeBSD ClientPolicy now uses only explicit
`classification[].ipv6Addresses` as IPv6 identity. It does not infer IPv6
identity from a DHCPv4 reservation, MAC address, OUI, hostname, or DHCP
fingerprint. This is deliberately not Linux MAC/L2 parity: routed FreeBSD PF
does not expose the Ethernet-source matching model that the Linux nftables
renderer uses. Unlisted or privacy IPv6 addresses require separate network
segmentation.

## VM acceptance

Owner: `/tmp/vm115-g8-clientpolicy-accept.6g96HH`.

The production runner rendered its own Router fixture through `routerd
validate` and `routerd render freebsd`, loaded that generated `pf.conf`, and
used three disposable VNET jails/epairs:

- source: `fd00:1::10`;
- allowed sink: `2001:db8:3::2`;
- denied ULA sink: `fd00:2::2`.

Persisted original-resolution console frames prove:

- `runner.rc=0`, empty runner stderr, and
  `freebsd-clientpolicy-ipv6-dataplane=ok`;
- generated rules contain the explicit `inet6 from fd00:1::10` deny for
  `fd00:2::/64` before the broad ICMPv6 pass, and the explicit allow for
  `2001:db8:3::/64`;
- the allowed sink captured three echo requests from `fd00:1::10`;
- the denied sink captured no packets from `fd00:1::10` (only local NDP
  traffic); the source's denied ping had zero replies;
- PF rules contain the generated deny label, and PF state output contains the
  allowed source flow; and
- post-cleanup PF rule/state observations are empty and `pfctl` reports
  `pf disabled`.

Frame hashes are recorded in
`/tmp/vm115-g8-clientpolicy-accept.6g96HH/frames/sha256sum.txt`: runner rc
`bcf9dcb2d5ee2dc56a818012b7488cbe54d5d610bf8d944c5e5bd89ad82a950c`, result
`188ed139545a016cf690fb21d9de37903e2eb33b4ed8f890b2d9b5b2af99653d`, summary
`0e0baa73c6c994cf93d01aada8d17fd7b50dfdb8994db9511f3de4de5a2c79b5`, allowed
sink `3c771612e10d5dad9eca1af1a2fe691e10a30f52a7a9b8c99d8bb186be551bd7`, and
denied sink `f66dcc288098bcd7353adaa5dba68396ecd72183f00bff8dd50a71368bd61e51`.

Postflight is exact: VM115 stopped, `args` absent, retained
`routerd-plan-v4.iso`, `vmbr404 NO-CARRIER`, and the two retained snapshots
unchanged. This closes only G8's explicit-IPv6 FreeBSD ClientPolicy sub-AC;
it does not claim MAC/L2 equivalence.
