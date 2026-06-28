# Dynamic RR/Leaf Enrollment Test Runbook

This runbook validates the dual-RR `SAMRRSet` enrollment flow before any
cloud/PVE full-topology test.

Primary target: private-underlay SAM transport without mandatory WireGuard.
The review shape also includes one encrypted public-underlay leaf so the same
dual-RR `SAMRRSet` proves both transport paths:

- `leaf-a`: `mode: ipip`, `encryption: wireguard`, connects to rr-a and rr-b.
- `leaf-b`: `mode: fou`, `encryption: none`, connects to rr-a and rr-b.

WireGuard remains an optional transport-specific path for public underlay; it
is not the default enrollment identity or the default private-underlay model.

## Resource Boundaries

- `SAMRRSet` is control-plane intent: it lists rr-a/rr-b members and shared
  admission references. It never lists leaves and is not a data-plane primitive.
- `SAMEnrollmentPolicy` and `SAMEnrollmentClaim` authorize leaf join data,
  tunnel address, endpoint, and MobilityPool `/32` claims. Join authentication
  is modeled with `joinTokenFrom` plus claim `joinNonce`, `joinTimestamp`, and
  `joinHMAC`.
- `SAMTransportProfile` consumes `SAMRRSet` or accepted enrollment claims and
  generates existing `TunnelInterface` and `BGPPeer` resources.
- On RRs, `SAMTransportProfile.spec.bgp.generatePeers: false` can generate
  tunnel/endpoint intent while leaving BGP neighbor admission to
  `BGPDynamicPeer`.
- `BGPDynamicPeer` is only the RR BGP acceptor. It owns listen source-prefix
  admission and BGP policy. It does not own leaf identity, tunnel assignment,
  WireGuard material, or MobilityPool authorization.
- `WireGuardInterface` / `WireGuardPeer` are used only when
  `encryption: wireguard` is selected.
- `MobilityPool` remains the `/32` ownership authority.

## Example Configs

Primary non-WG private-underlay examples:

- `examples/cloudedge-dynamic-rr-a-hub.yaml`
- `examples/cloudedge-dynamic-rr-b-hub.yaml`
- `examples/cloudedge-dynamic-leaf-pve.yaml`

Mixed transport review examples:

- `examples/cloudedge-dynamic-leaf-a-wg.yaml`
- `examples/cloudedge-dynamic-leaf-b-fou.yaml`

These configs model:

- rr-a and rr-b as members of `SAMRRSet/cloudedge-rrs`;
- no static RR-side `BGPPeer/leaf-*`;
- RR-side `BGPDynamicPeer/cloudedge-leaves`;
- RR-side `SAMTransportProfile.spec.bgp.generatePeers: false`;
- leaf-side `SAMTransportProfile/leaf-pve` consuming
  `SAMRRSet/cloudedge-rrs`;
- generated/effective leaf `TunnelInterface` and `BGPPeer` resources toward
  both rr-a and rr-b;
- no `WireGuardInterface`, `WireGuardPeer`, or WG public key requirement.

The mixed examples model:

- `leaf-a` consuming the same `SAMRRSet/cloudedge-rrs` and deriving both
  rr-a and rr-b transport/BGP peers through an IPIP-over-WireGuard path;
- `leaf-a` using `WireGuardInterface.spec.peersFrom:
  SAMRRSet/cloudedge-rrs`, with WG public keys only in WG-specific blocks;
- `leaf-b` consuming the same `SAMRRSet/cloudedge-rrs` and deriving both
  rr-a and rr-b transport/BGP peers through `TunnelInterface mode: fou`;
- `leaf-b` using `encryption: none`, `encapSport: 5555`, and
  `encapDport: 5555`, with no `WireGuardInterface` or `WireGuardPeer`.

The RR configs include the same example `SAMEnrollmentClaim/leaf-pve` so the
paired RR flow can be tested without an external enrollment service. In a real
deployment, the accepted claim should be fanned out to both RRs by the
enrollment service or config distribution path.

## Required Inputs

Replace example values before live testing:

| Placeholder | Meaning |
| --- | --- |
| `/usr/local/etc/routerd/secrets/cloudedge-join-token` | Shared join token available on each RR. |
| `EXAMPLE_HMAC_SHA256_HEX` | Lowercase hex HMAC-SHA256 over the canonical claim join payload using the join token. |
| `10.10.0.2` | rr-a private underlay endpoint. |
| `10.10.0.3` | rr-b private underlay endpoint. |
| `10.20.0.21` | leaf private underlay endpoint. |
| `10.20.0.31` | leaf-a WireGuard overlay/local SAM endpoint. |
| `10.20.0.32` | leaf-b private FOU underlay endpoint. |
| `10.99.0.2/32` | rr-a SAM/BGP tunnel identity. |
| `10.99.0.3/32` | rr-b SAM/BGP tunnel identity. |
| `10.255.0.21/32` | leaf tunnel address, inside policy `tunnelAddressPrefixes`. |
| `10.255.0.31/32` | leaf-a tunnel address, inside policy `tunnelAddressPrefixes`. |
| `10.255.0.32/32` | leaf-b tunnel address, inside policy `tunnelAddressPrefixes`. |
| `10.77.60.21/32` | leaf-owned MobilityPool address. |
| `10.77.60.31/32` | leaf-a owned MobilityPool address. |
| `10.77.60.32/32` | leaf-b owned MobilityPool address. |
| `203.0.113.10:51820` / `203.0.113.11:51820` | rr-a/rr-b WG UDP endpoints for the public-underlay WG example. |
| UDP `5555` | FOU/GUE encapsulation port used by the leaf-b private-underlay example. |

When `joinTokenFrom` is configured, routerd requires `joinNonce`,
`joinTimestamp`, and `joinHMAC`. If the referenced secret is readable during
validation, routerd verifies the HMAC. If the secret is not present on the
authoring host, validation still checks field presence and policy scope so
example configs remain reviewable before secrets are installed. A loaded config
must not contain duplicate `joinNonce` values for the same enrollment policy.
In a live enrollment service, used nonces should also be persisted outside the
routerd config so replayed join requests can be rejected across config
generations.

The HMAC input is UTF-8 text with these newline-separated fields, in this
order:

```text
policyRef=<claim policyRef>
rrSetRef=<claim rrSetRef>
leafID=<claim leafID>
joinAudience=<claim joinAudience>
joinNonce=<claim joinNonce>
joinTimestamp=<claim joinTimestamp>
tunnelAddress=<claim tunnelAddress>
endpoint=<claim endpoint>
mobility.ownedAddresses=<sorted comma-separated owned /32s>
bgp.asn=<claim BGP ASN>
bgp.routerID=<claim BGP router ID>
wireGuard.publicKey=<optional WG public key>
wireGuard.endpoint=<optional WG endpoint>
wireGuard.allowedIPs=<sorted comma-separated optional WG allowed IPs>
wireGuard.persistentKeepalive=<optional WG keepalive seconds>
```

Generate real `joinHMAC` values from the reviewed config instead of hand-copying
the payload:

```sh
bin/linux/routerctl mobility enrollment-hmac \
  --config examples/cloudedge-dynamic-rr-a-hub.yaml \
  --claim leaf-pve \
  --secret-file /usr/local/etc/routerd/secrets/cloudedge-join-token

bin/linux/routerctl mobility enrollment-hmac \
  --config examples/cloudedge-dynamic-leaf-b-fou.yaml \
  --claim leaf-b \
  --secret-file /usr/local/etc/routerd/secrets/cloudedge-join-token \
  --show-payload
```

Use `--secret-env` when the join token is injected by the shell or deployment
tool. Use `--show-payload` when reviewing exactly what is signed. After
replacing `EXAMPLE_HMAC_SHA256_HEX`, validation will cryptographically verify
the claim whenever the configured `joinTokenFrom` source is readable.

## Local Verification

Run before any cloud/PVE topology test:

```sh
gofmt
git diff --check
make check-schema
make validate-example
go test ./pkg/controller/bgp ./pkg/controller/chain ./pkg/controller/mobility ./pkg/config ./pkg/api ./tests/golden
go test ./...
```

Build local binaries for sandbox validation:

```sh
make build-daemons
```

Validate and plan the examples:

```sh
scripts/routerd-sandbox-run.sh sh -c '
  for config do
    bin/linux/routerctl validate --socket "$ROUTERD_SANDBOX_STATUS_SOCKET" -f "$config" --replace >/dev/null
    bin/linux/routerctl plan --socket "$ROUTERD_SANDBOX_STATUS_SOCKET" -f "$config" --replace >/dev/null
  done
' sh \
  examples/cloudedge-dynamic-rr-a-hub.yaml \
  examples/cloudedge-dynamic-rr-b-hub.yaml \
  examples/cloudedge-dynamic-leaf-pve.yaml \
  examples/cloudedge-dynamic-leaf-a-wg.yaml \
  examples/cloudedge-dynamic-leaf-b-fou.yaml
```

Expected local evidence:

- rr-a and rr-b validate without `WireGuardInterface` or `WireGuardPeer`.
- rr-a and rr-b contain `BGPDynamicPeer/cloudedge-leaves`.
- rr-a and rr-b contain no static `BGPPeer/leaf-*`.
- leaf contains `SAMRRSet/cloudedge-rrs` and no static `BGPPeer/rr-a` or
  `BGPPeer/rr-b`.
- leaf `SAMTransportProfile/leaf-pve` consumes `SAMRRSet/cloudedge-rrs`.
- controller tests show leaf-side generated `TunnelInterface` and `BGPPeer`
  resources for rr-a and rr-b.
- `TestCloudEdgeDynamicLeafExamplesMaterializeDualRRTransports` loads the
  leaf-a and leaf-b example YAML files and proves that `SAMTransportProfile`
  generates two RR-facing `TunnelInterface` resources and two `BGPPeer`
  resources from `SAMRRSet/cloudedge-rrs`.
- leaf-a shape test shows `SAMRRSet` consumption plus WG-specific
  `WireGuardInterface.peersFrom` toward both RRs.
- leaf-b controller test shows two generated `TunnelInterface` resources with
  `mode: fou` and encap ports `5555/5555`, two generated `BGPPeer` resources,
  and zero generated `WireGuardPeer` resources.
- controller tests show RR-side generated `TunnelInterface` resources can be
  created without generated per-leaf `BGPPeer` resources when
  `generatePeers: false`.
- WG materialization is covered only by WG-specific tests using optional
  `wireGuard` blocks; non-WG materialization is covered without WG resources.

For manual materialization evidence without touching cloud/PVE, run a sandbox
controller pass and render the effective config:

```sh
tmpdir=$(mktemp -d /tmp/routerd-leaf-b.XXXXXX)
bin/linux/routerd serve \
  --sandbox \
  --root "$tmpdir/root" \
  --config examples/cloudedge-dynamic-leaf-b-fou.yaml \
  --controllers sam-transport,wireguard \
  --apply-interval 0 &
pid=$!
for _ in $(seq 1 100); do
  test -S "$tmpdir/root/run/routerd/routerd-status.sock" && break
  sleep 0.1
done
sleep 1
bin/linux/routerctl dynamic list --state-file "$tmpdir/root/var/lib/routerd/routerd.db" -o yaml
bin/linux/routerctl dynamic render \
  --config examples/cloudedge-dynamic-leaf-b-fou.yaml \
  --state-file "$tmpdir/root/var/lib/routerd/routerd.db" \
  -o yaml
kill "$pid"
```

The dynamic list should include
`SAMTransportProfile/leaf-b/node/leaf-b` with six resources: two
`TunnelInterface`, two endpoint `IPv4Route`, and two `BGPPeer` resources. The
rendered `TunnelInterface` resources should use `mode: fou` and encap ports
`5555/5555`, and the rendered config should contain no `WireGuardPeer`.

Repeat the same command with `examples/cloudedge-dynamic-leaf-a-wg.yaml` to
check `SAMTransportProfile/leaf-a/node/leaf-a`. The SAM transport dynamic part
should again contain two `TunnelInterface`, two endpoint `IPv4Route`, and two
`BGPPeer` resources, with `mode: ipip`. The WG peer materialization path is
separate from the generic SAM transport dynamic part; in sandbox dry-run logs
the `wireguard` controller should report `peers:2` for
`WireGuardInterface/wg-cloudedge`, and controller tests verify the two
`WireGuardPeer` resources derived from `SAMRRSet/cloudedge-rrs`.

## Negative Tests

Local tests should cover:

- `BGPDynamicPeer.routeReflectorClient=true` rejects a peer ASN different from
  the referenced `BGPRouter` local ASN.
- `SAMRRSet` allows members without `wireGuard` blocks.
- `SAMEnrollmentClaim` is valid without `wireGuard.publicKey`.
- `SAMTransportProfile mode: fou` requires `encapSport` and `encapDport`.
- `SAMTransportProfile mode: ipip`/`gre` rejects FOU/GUE encap ports.
- a configured `joinTokenFrom` requires claim `joinNonce`, `joinTimestamp`, and
  `joinHMAC`.
- duplicate `joinNonce` values are rejected within the same enrollment policy.
- unauthorized MobilityPool `/32` claims are rejected.
- revoked or expired claims are skipped.
- route/default/underlay prefix rejection is enforced where current BGP and
  MobilityPool policy machinery supports it.

## Optional WireGuard Path

For public underlay or encrypted transport, use:

- `SAMTransportProfile.spec.encryption: wireguard`;
- `WireGuardInterface.spec.peersFrom` referencing `SAMEnrollmentPolicy` on the
  RR or `SAMRRSet` on the leaf;
- optional `wireGuard` blocks on enrollment claims and RRSet members.

WG credentials remain transport-specific. The leaf generates its private key
locally; only the leaf public key is accepted by the RR. The generic enrollment
identity is still `leafID` plus join-token/HMAC fields, not the WG public key.

## RR-to-RR Peering Decision

RR-to-RR peering is not required for the primary test when every leaf connects
to both rr-a and rr-b. If leaf A can reach only rr-a and leaf B can reach only
rr-b, then RR-to-RR BGP peering or another synchronization path is required for
complete route propagation.

This failure mode must be decided before a production topology. Do not infer
route consistency from one reachable RR.

## Full Topology Gate

Do not start the cloud/PVE full topology test until the user reviews:

- final rr-a, rr-b, and leaf configs;
- expected generated `TunnelInterface`, `BGPPeer`, optional `WireGuardPeer`,
  and `BGPDynamicPeer` state;
- required hosts, underlay addresses, secrets, and artifacts;
- pass/fail criteria;
- which transport profile is being tested;
- what remains untested.

Concrete host roles for the first reviewed topology:

| Host role | Config | Purpose |
| --- | --- | --- |
| rr-a | `examples/cloudedge-dynamic-rr-a-hub.yaml` | First RR admission point. |
| rr-b | `examples/cloudedge-dynamic-rr-b-hub.yaml` | Second RR admission point. |
| leaf-b | `examples/cloudedge-dynamic-leaf-b-fou.yaml` | Primary private-underlay non-WG test. |
| leaf-a | `examples/cloudedge-dynamic-leaf-a-wg.yaml` | Optional encrypted public-underlay test. |

Required live-test artifacts:

- routerd binaries or packages built from this branch, installed on every test
  host;
- reviewed configs with example addresses, endpoints, and interface names
  replaced by the real topology values;
- shared join token installed on rr-a and rr-b at the `joinTokenFrom` path;
- real `joinHMAC` values generated with `routerctl mobility enrollment-hmac`
  after final config edits;
- firewall/underlay reachability for BGP TCP/179 over the generated tunnel
  addresses;
- UDP `5555` permitted between leaf-b and both RRs for the FOU path;
- for the optional WG path only, leaf-a local WG private key, RR WG public keys,
  reachable RR WG UDP endpoints, and UDP `51820` permitted;
- rollback artifacts: previous routerd binary/package, previous config, and
  service restart commands for each host.

Preflight on each host before enabling the live topology:

```sh
routerctl validate -f /etc/routerd/routerd.yaml
routerctl plan -f /etc/routerd/routerd.yaml
```

On leaf-a and leaf-b, run a local controller materialization check before
starting live forwarding:

```sh
routerd serve --sandbox --root /tmp/routerd-sam-preflight \
  --config /etc/routerd/routerd.yaml \
  --controllers sam-transport,wireguard \
  --apply-interval 0

routerctl dynamic list \
  --state-file /tmp/routerd-sam-preflight/var/lib/routerd/routerd.db -o yaml

routerctl dynamic render \
  --config /etc/routerd/routerd.yaml \
  --state-file /tmp/routerd-sam-preflight/var/lib/routerd/routerd.db -o yaml
```

Expected preflight state:

- rr-a and rr-b configs contain `BGPDynamicPeer/cloudedge-leaves` and no static
  `BGPPeer/leaf-*`;
- leaf-b renders two RR-facing `TunnelInterface` resources with `mode: fou`,
  `encapSport: 5555`, `encapDport: 5555`, and no WireGuard resources;
- leaf-a renders two RR-facing `TunnelInterface` resources with `mode: ipip`
  and the WG controller derives two RR `WireGuardPeer` resources;
- both leaf configs render two generated `BGPPeer` resources, one for rr-a and
  one for rr-b.

Full topology pass criteria:

- branch binaries installed on rr-a, rr-b, and leaf;
- no static RR-side `BGPPeer/leaf-*`;
- leaf-b establishes FOU transport toward both rr-a and rr-b without any
  WireGuard peer requirement;
- if the optional WG test is selected, leaf-a establishes WG plus IPIP transport
  toward both rr-a and rr-b;
- both RRs accept BGP sessions through `BGPDynamicPeer/cloudedge-leaves`;
- each RR learns only the authorized MobilityPool `/32` routes;
- default routes, underlay/management prefixes, and unauthorized `/32` claims
  are not accepted;
- minimal connectivity succeeds over the authorized `/32` between test leaves
  and RR-side test targets;
- stopping one RR leaves the leaf connected to the other RR, with the expected
  route-convergence behavior documented.

Items intentionally not covered unless selected for the first live run:

- every supported transport combination; the first required live run should
  prioritize leaf-b `fou` plus `encryption:none`, while leaf-a WG remains the
  optional encrypted path;
- long-running expiry/revocation behavior beyond local validation/controller
  tests;
- RR-to-RR peering behavior when a leaf can reach only one RR;
- provider action side effects outside the chosen test providers/hosts.
