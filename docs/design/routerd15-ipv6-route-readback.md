# Linux IPv6 static-route readback checkpoint

## Observed failure

An isolated PVE Linux guest ran the packaged revision `ec707055` with an
externally managed veth interface and an independent network namespace peer.
The release archive SHA-256 was
`19733237629375e62b18e13bd2a7587592cd2042341ff8dfa5c40f130efbe294`.
The installed binary reported `routerd v20261027.1620 (ec707055)` before
the service was restarted. This was a package replacement with test-state
reset, not a clean-install qualification.

The declared route was `fd42:150::99/128` via `fd42:149::2`, interface
`rd15-v6r-host`, metric `77`. The controller added the route. Linux returned
the following observation:

```json
[{"type":"unicast","dst":"fd42:150::99","gateway":"fd42:149::2","dev":"rd15-v6r-host","protocol":"boot","scope":"global","metric":77,"flags":[],"pref":"medium"}]
```

The peer reached the router's `fd42:149::1` address (3/3 ICMPv6 replies),
and the router reached the peer's `fd42:150::99` address (3/3 replies).
The resource nevertheless reported `ObserveFailed` because its readback
parser required a prefix length. The ownership receipt remained pending.
This is a failed lifecycle qualification, not a passing replacement test.
It does not establish forwarding from a separate client VM through a VLAN.

## Fix and verification scope

Normalize Linux observation destinations at the readback boundary: a bare
IPv6 address denotes a /128 route, and `default` denotes `::/0`. Keep the
configured route validation and exact route identity comparison unchanged.
The existing apply/withdraw unit test exercises prefix, host and default
route observations, including status publication and withdrawal.

The corrected binary still needs an isolated runtime rerun covering route
application, independent client forwarding, withdrawal, restart and recovery.
The earlier guest preparation attempts that still ran `a23970a` are not
evidence for the `ec707055` implementation.
