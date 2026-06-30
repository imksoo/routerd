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
- `BGPDynamicPeer` status reports the configured peer group/source prefixes,
  discovered dynamic peers, best-effort routerd-side accepted/rejected route
  counters, and enrollment correlation when the peer address matches an
  accepted claim tunnel address. The counters are measured while routerd
  observes GoBGP paths for FIB/admission filtering; they are not GoBGP's own
  import-policy rejection counters.
- `WireGuardInterface` / `WireGuardPeer` are used only when
  `encryption: wireguard` is selected.
- `MobilityPool` remains the `/32` ownership authority.
- `SAMEnrollmentClaim.spec.mobility.ownedAddresses` is bound to dynamic BGP
  admission by the BGP neighbor/tunnel address, not by the route's FIB
  next-hop. A dynamic leaf can advertise only its accepted claim-owned `/32`;
  another leaf's `/32`, an unclaimed pool `/32`, pool aggregate, subprefix,
  default route, or underlay route is rejected before FIB installation.
- RR-side enrollment admission supports both deployment shapes:
  - use `SAMEnrollmentPolicy.spec.mobilityPoolRefs` when the RR also declares a
    valid local `MobilityPool` member because RR and leaf/capture duties are
    colocated, or because the RR is an actual mobility-planning participant;
  - use `SAMEnrollmentPolicy.spec.mobilityPrefixes` and
    `SAMRRSet.spec.mobilityPrefixes` when the RR is only serving enrollment,
    RRSet delivery, WireGuard/dynamic peer admission, and BGP route reflection.
    In this separated shape, the RR must not declare a placeholder
    `MobilityPool` just to authorize leaf-owned `/32` claims.
- `SAMEnrollmentClaim.spec.expiresAt` and `spec.revoked` are
  RR/controller/admin-owned admission state. They are intentionally not part of
  the leaf-authored join HMAC payload, so an operator can revoke or shorten
  admission without leaf re-signing; `expiresAt` remains bounded by policy TTL.

## Leaf-Side RRSet Fetch

Leaf-side automatic enrollment uses `SAMEnrollmentClient`:

1. read a local `SAMEnrollmentClaim` from the leaf config;
2. submit it to a bootstrap RR control API endpoint;
3. fetch the `SAMRRSet` allowed by that accepted claim; and
4. persist the fetched RRSet into the leaf's local state DB as
   `DynamicConfigPart` source `SAMRRSet/<name>`.

The leaf startup config can then keep only the bootstrap claim/policy
reference and `peersFrom: SAMRRSet/<name>`. It does not need the full rr-a/rr-b
inventory as hand-edited static YAML for the automatic path.

`routerctl mobility enrollment-join` remains available as a manual/script path,
but it is not the normal every-reconcile operation. `SAMEnrollmentClient`
refreshes only when the fetched RRSet is missing, near expiry, or the local
claim material changes. Failed attempts use exponential backoff and transport
or BGP degradation does not trigger immediate rejoin loops.

## Revoke, Rotate, And Re-Enroll

RR-side revocation is an admin operation against accepted dynamic enrollment
state. It replaces the accepted `SAMEnrollmentClaim/<name>` dynamic part with a
revoked claim whose `expiresAt` is the revoke time. After that point, RRSet
fetch for the old claim fails and dynamic BGP admission stops treating the
claim as active.

```sh
routerctl mobility enrollment-revoke \
  --claim pve-leaf-b \
  --rr-url https://10.30.0.10:65432 \
  --rr-token-file /usr/local/etc/routerd/secrets/control-api-token \
  --rr-ca-file /usr/local/etc/routerd/secrets/rr-ca.pem \
  --rr-client-cert-file /usr/local/etc/routerd/secrets/admin.crt \
  --rr-client-key-file /usr/local/etc/routerd/secrets/admin.key \
  --reason rotated
```

Use `--rr-socket /run/routerd/routerd.sock` for local RR maintenance instead
of `--rr-url`. The same bearer-token and mTLS hardening used by enrollment
submit/fetch applies to revoke over TCP.

To rotate and re-enroll a leaf:

1. revoke the old accepted claim on every RR that accepted it;
2. update the leaf `SAMEnrollmentClaim.spec.joinNonce` and
   `spec.joinTimestamp`;
3. recompute `spec.joinHMAC` with `routerctl mobility enrollment-hmac`, or
   regenerate the leaf config with `routerctl mobility leaf-config` and a join
   secret source;
4. let `SAMEnrollmentClient` refresh after the local claim material changes, or
   run `routerctl mobility enrollment-join` once to force submit/fetch and
   persist the new `SAMRRSet`; and
5. check `routerctl doctor sam-enrollment-client` on the leaf and
   `routerctl doctor bgp-dynamic-peer` on the RR.

Use `routerctl mobility leaf-config` to generate a minimal leaf startup config
for this automatic path. The generated config contains the local underlay
interface/address, owned mobility `/32`, `BGPRouter`, `SAMTransportProfile`,
`MobilityPool`, `SAMEnrollmentPolicy`, `SAMEnrollmentClaim`, and
`SAMEnrollmentClient`. It intentionally does not embed the fetched `SAMRRSet`;
the client submits the claim to one of the configured bootstrap endpoints and
persists the authorized RRSet as dynamic state.

## Example Configs

Primary non-WG private-underlay examples:

- `examples/cloudedge-dynamic-rr-a-hub.yaml`
- `examples/cloudedge-dynamic-rr-b-hub.yaml`
- `examples/cloudedge-dynamic-leaf-pve.yaml`

Mixed transport review examples:

- `examples/cloudedge-dynamic-leaf-a-wg.yaml`
- `examples/cloudedge-dynamic-leaf-b-fou.yaml`

PVE minimal automatic review examples:

- `examples/pve-minimal-rr.yaml`
- `examples/pve-minimal-rr-b.yaml`
- `examples/pve-minimal-leaf-a-wg.yaml`
- `examples/pve-minimal-leaf-b-fou.yaml`
- `examples/pve-minimal-leaf-c-wg.yaml`
- `examples/pve-minimal-leaf-d-fou.yaml`
- `tests/fixtures/pve-minimal-leaf-rrset-fetched.yaml`

These configs model:

- rr-a and rr-b as members of `SAMRRSet/cloudedge-rrs`;
- no static RR-side `BGPPeer/leaf-*`;
- RR-side `BGPDynamicPeer/cloudedge-leaves`;
- RR-side `SAMTransportProfile.spec.bgp.generatePeers: false`;
- RR-side private IPIP, public WG/IPIP, and private FOU enrollment policies
  for `leaf-pve`, `leaf-a`, and `leaf-b`;
- leaf-side `SAMTransportProfile/leaf-pve` consuming
  `SAMRRSet/cloudedge-rrs`;
- generated/effective leaf `TunnelInterface` and `BGPPeer` resources toward
  both rr-a and rr-b;
- no `WireGuardInterface`, `WireGuardPeer`, or WG public key requirement.

The dual-RR CloudEdge examples intentionally model separated RR/leaf roles:
`examples/cloudedge-dynamic-rr-a-hub.yaml` and
`examples/cloudedge-dynamic-rr-b-hub.yaml` do not declare `EventGroup` or
`MobilityPool`. They use `mobilityPrefixes: [10.77.60.0/24]` on RR-side
admission resources so local validation catches invalid claim addresses without
starting mobility planning on the RRs.

The PVE minimal examples use the same separated RR/leaf admission shape, reduced
to two local RRs and four leaves. `examples/pve-minimal-rr.yaml` models
`pve-rr-a`, `examples/pve-minimal-rr-b.yaml` models `pve-rr-b`, and both RR
configs authorize leaf-owned `/32` claims with
`mobilityPrefixes: [10.77.70.0/24]` instead of declaring a placeholder
`MobilityPool`. Leaf startup configs bootstrap against both RRs and consume a
fetched `SAMRRSet/pve-rrs` containing `pve-rr-a` and `pve-rr-b`.

The mixed examples model:

- `leaf-a` consuming the same `SAMRRSet/cloudedge-rrs` and deriving both
  rr-a and rr-b transport/BGP peers through an IPIP-over-WireGuard path;
- `leaf-a` using `WireGuardInterface.spec.peersFrom:
  SAMRRSet/cloudedge-rrs`, with WG public keys only in WG-specific blocks;
- `leaf-b` consuming the same `SAMRRSet/cloudedge-rrs` and deriving both
  rr-a and rr-b transport/BGP peers through `TunnelInterface mode: fou`;
- `leaf-b` using `encryption: none`, `encapSport: 5555`, and
  `encapDport: 5555`, with no `WireGuardInterface` or `WireGuardPeer`.

The RR configs do not include example claims for `leaf-pve`, `leaf-a`, or
`leaf-b`. Review claim fixtures live under `tests/fixtures/` and must be
submitted through the enrollment API or injected into dynamic admission state
for local controller tests. `leaf-a` is admitted through
`SAMEnrollmentPolicy/cloudedge-public-wg-leaves` and
`SAMTransportProfile/rr-*-wg`; `leaf-b` is admitted through
`SAMEnrollmentPolicy/cloudedge-private-fou-leaves` and
`SAMTransportProfile/rr-*-fou`. In a real deployment, accepted claims should be
persisted as admission state and fanned out to both RRs by the enrollment
service or config distribution path.

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
  --config examples/cloudedge-dynamic-leaf-pve.yaml \
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
  examples/pve-minimal-rr.yaml \
  examples/pve-minimal-rr-b.yaml \
  examples/pve-minimal-leaf-a-wg.yaml \
  examples/pve-minimal-leaf-b-fou.yaml \
  examples/pve-minimal-leaf-c-wg.yaml \
  examples/pve-minimal-leaf-d-fou.yaml \
  examples/cloudedge-dynamic-leaf-pve.yaml \
  examples/cloudedge-dynamic-leaf-a-wg.yaml \
  examples/cloudedge-dynamic-leaf-b-fou.yaml
```

Expected local evidence:

- rr-a and rr-b validate without static `WireGuardPeer`; their
  `WireGuardInterface` is present only for the optional WG admission path.
- rr-a and rr-b contain `BGPDynamicPeer/cloudedge-leaves`.
- rr-a and rr-b contain no static `BGPPeer/leaf-*`.
- rr-a and rr-b contain no static `SAMEnrollmentClaim/leaf-*`.
- `tests/fixtures/cloudedge-rr-claims-seed.yaml` and
  `tests/fixtures/pve-minimal-rr-claims-seed.yaml` contain submitted-claim
  examples only; controller tests load them as dynamic admission state.
- rr-a and rr-b materialize one `TunnelInterface` for `leaf-a` through
  `SAMTransportProfile/rr-*-wg`, one `TunnelInterface` for `leaf-b` through
  `SAMTransportProfile/rr-*-fou`, and zero generated RR-side `BGPPeer`
  resources for those profiles.
- the RR-side `WireGuardInterface/wg-cloudedge` derives only
  `WireGuardPeer/leaf-a`; `leaf-b` remains non-WG.
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
- `TestCloudEdgeDynamicRRExamplesMaterializeMixedAdmissionWithoutBGPPeers`
  loads the rr-a and rr-b examples and proves the private IPIP, public WG/IPIP,
  and private FOU RR-side admission profiles generate tunnels while keeping RR
  generated `BGPPeer` count at zero.
- `TestCloudEdgeRRExamplesDeriveOnlyWGAdmissionPeers` proves the RR-side WG
  materialization path derives only `WireGuardPeer/leaf-a` and does not turn the
  non-WG `leaf-b` FOU claim into a WG peer.
- `examples/pve-minimal-leaf-a-wg.yaml`,
  `examples/pve-minimal-leaf-b-fou.yaml`,
  `examples/pve-minimal-leaf-c-wg.yaml`, and
  `examples/pve-minimal-leaf-d-fou.yaml` contain no static
  `SAMRRSet/pve-rrs`; tests inject
  `tests/fixtures/pve-minimal-leaf-rrset-fetched.yaml` as fetched dynamic
  state and prove the leaf generates RR-facing `TunnelInterface` and `BGPPeer`
  resources toward both `pve-rr-a` and `pve-rr-b`.
- `SAMEnrollmentClient` submits the leaf claim, fetches the allowed RRSet, and
  writes the fetched RRSet to local dynamic state only when refresh is needed.
- `routerctl mobility enrollment-join` performs the same submit/fetch/write
  path for manual bootstrap and troubleshooting.
- WG materialization is covered only by WG-specific tests using optional
  `wireGuard` blocks; non-WG materialization is covered without WG resources.

Example leaf bootstrap command:

```sh
routerctl mobility leaf-config \
  --leaf-id pve-leaf-b \
  --underlay-ifname vmbr0 \
  --underlay-address 10.30.0.22/24 \
  --local-endpoint 10.30.0.22 \
  --endpoint-prefix 10.30.0.0/24 \
  --inner-prefix 10.255.10.0/24 \
  --tunnel-address 10.255.10.22/32 \
  --mobility-pool pve-mobility \
  --mobility-pool-prefix 10.77.70.0/24 \
  --owned-address 10.77.70.22/32 \
  --rr-set pve-rrs \
  --policy pve-fou-leaves \
  --join-token-file /usr/local/etc/routerd/secrets/pve-join-token \
  --join-audience pve-private-underlay \
  --bootstrap-endpoint https://10.30.0.10:65432 \
  --bootstrap-endpoint https://10.30.0.11:65432 \
  --control-api-token-file /usr/local/etc/routerd/secrets/control-api-token \
  --control-api-ca-file /usr/local/etc/routerd/secrets/rr-ca.pem \
  --control-api-client-cert-file /usr/local/etc/routerd/secrets/leaf.crt \
  --control-api-client-key-file /usr/local/etc/routerd/secrets/leaf.key \
  --secret-file /usr/local/etc/routerd/secrets/pve-join-token \
  > /usr/local/etc/routerd/router.yaml
```

When `--secret-file`, `--secret-env`, or `--secret` is supplied, the generator
computes `SAMEnrollmentClaim.spec.joinHMAC` from the same canonical payload used
by `routerctl mobility enrollment-hmac`. Without a secret source, it writes the
placeholder `EXAMPLE_HMAC_SHA256_HEX` so the config can still be reviewed
before secrets are installed.

```sh
routerctl mobility enrollment-join \
  --config /usr/local/etc/routerd/router.yaml \
  --claim pve-leaf-b \
  --rr-url http://10.30.0.10:65432 \
  --state-file /var/lib/routerd/routerd.db
```

For local Unix-socket review against a sandbox RR:

```sh
routerctl mobility enrollment-join \
  --config examples/pve-minimal-leaf-b-fou.yaml \
  --claim pve-leaf-b \
  --rr-socket /run/routerd/routerd.sock \
  --state-file /tmp/routerd-leaf/routerd.db
```

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
- `BGPDynamicPeer` rejects configs without an effective
  `importPolicy.allowedPrefixes` allowlist.
- static `BGPPeer` reconcile does not delete live peers from
  `routerd-dynamic-*` peer groups.
- watch-triggered BGP observation includes dynamic peer import allowlists.
- `SAMRRSet` allows members without `wireGuard` blocks.
- `SAMEnrollmentClaim` is valid without `wireGuard.publicKey`.
- missing `SAMEnrollmentPolicy` references are validation errors.
- `policy.ttl` with `claim.joinTimestamp` expires claims during materialization.
- `claim.expiresAt` must not exceed `claim.joinTimestamp + policy.ttl`.
- `policy.endpointPrefixes` or `policy.wireGuard.endpointPrefixes` is enforced
  against `claim.wireGuard.endpoint` host addresses.
- duplicate `leafID`, `tunnelAddress`, `wireGuard.publicKey`,
  `mobility.ownedAddresses`, and `bgp.routerID` values are rejected within the
  same enrollment policy.
- `SAMTransportProfile mode: fou` requires `encapSport` and `encapDport`.
- `SAMTransportProfile mode: ipip`/`gre` rejects FOU/GUE encap ports.
- a configured `joinTokenFrom` requires claim `joinNonce`, `joinTimestamp`, and
  `joinHMAC`.
- duplicate `joinNonce` values are rejected within the same enrollment policy.
- unauthorized MobilityPool `/32` claims are rejected.
- revoked or expired claims are skipped.
- BGP import policy can require exact host routes with
  `allowedPrefixLengthMin: 32` and `allowedPrefixLengthMax: 32`.
- dynamic SAM route admission rejects pool aggregates, non-/32 subprefixes,
  default routes, underlay prefixes, MobilityPool-outside `/32`s, another
  claim's `/32`, and unclaimed pool `/32`s.
- `BGPDynamicPeer` status exposes discovered dynamic peers, accepted route
  count, rejected route count, and rejected route summary from routerd-side
  observation.

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
- UDP `5555` permitted between leaf-b/leaf-d and both RRs for the FOU path;
- for the optional WG path only, leaf-a/leaf-c local WG private key, RR WG public keys,
  reachable RR WG UDP endpoints, and UDP `51820` permitted;
- rollback artifacts: previous routerd binary/package, previous config, and
  service restart commands for each host.

Preflight on each host before enabling the live topology:

```sh
routerctl validate -f /etc/routerd/routerd.yaml
routerctl plan -f /etc/routerd/routerd.yaml
```

On each PVE minimal leaf, run a local controller materialization check before
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
- rr-a and rr-b render RR-side tunnels for `leaf-a` and `leaf-b` through their
  respective admission profiles, while RR-side generated `BGPPeer` count
  remains zero because BGP admission is handled by `BGPDynamicPeer`;
- rr-a and rr-b derive a WG peer only for `leaf-a`; `leaf-b` has no WG peer;
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
- `routerctl status BGPDynamicPeer/cloudedge-leaves` shows the connected leaf
  under `discoveredPeers`, maps it to `enrollmentClaimRef`, and reports zero
  rejected routes for the positive path;
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

## PVE Live Redundancy Evidence - 2026-06-29

The PVE cloud-SAM redundancy topology passed live validation on 2026-06-29.

Freeze commit:

```text
4a6dad6b8786ed01e63381dcf77230467b8a5021
```

Evidence archive:

```text
/home/imksoo/routerd-labs-archive/evidence/samred-20260629T035652Z/
```

Archive checksum:

```text
77277d94e9c1b097ff0e9b7158b1cdeed772b27300c3a7c58bc007db9c1c92f4  routerd-samred-20260629T035652Z.tar.gz
```

Checksum verification:

```text
sha256sum -c: OK
repo state: main...origin/main clean
```

Validated assertions:

- RR base configs contained no static leaf claims.
- RR base configs contained no per-leaf `BGPPeer` resources.
- leaf-a, leaf-b, leaf-c, and leaf-d enrolled through `SAMEnrollmentClient`.
- Each leaf submitted claims to both rr-1 and rr-2.
- rr-1 and rr-2 each established 4 dynamic peers through `BGPDynamicPeer/samred-leaves`.
- Each leaf established FOU tunnel and BGP sessions to both RRs.
- client-999 `10.99.9.10` and client-998 `10.99.8.10` passed bidirectional ping.
- client-999 and client-998 passed bidirectional SSH using a temporary test key.

Cleanup status:

```text
complete
```

## Cleanup Evidence - 2026-06-29

Cleanup evidence was captured and archived after the PVE live redundancy test.
For future redundant Cloud-SAM test cleanup, use the generalized
[SAM redundancy cleanup runbook](./sam-redundancy-cleanup.md) and keep the
archive outside the repository.

Archive directory:

```text
/home/imksoo/routerd-labs-archive/evidence/samred-20260629T035652Z/
```

Added cleanup evidence:

```text
pre-cleanup/
cleanup/cleanup-session.log
post-cleanup/
CLEANUP-EVIDENCE-SHA256SUMS.txt
routerd-samred-20260629T035652Z-cleanup-evidence.tar.gz
routerd-samred-20260629T035652Z-cleanup-evidence.tar.gz.sha256
```

Cleanup evidence tarball checksum:

```text
c760850291690cac94549ec9730af3fa0545c017a3fdde5f0bfc7d7dae9a3591  routerd-samred-20260629T035652Z-cleanup-evidence.tar.gz
```

Verification:

```text
sha256sum -c routerd-samred-20260629T035652Z-cleanup-evidence.tar.gz.sha256: OK
sha256sum -c CLEANUP-EVIDENCE-SHA256SUMS.txt: OK
```

Post-cleanup assertions:

- VMID 9601-9608 are absent on pve05, pve06, and pve07.
- Bridges `rsam999`, `rsam998`, and `rsamclnt` are absent on pve05, pve06, and pve07.
- `/mnt/pve/qnap/template/iso/routerd-samred-*-cidata.iso` is absent on pve05, pve06, and pve07.

Repository state after cleanup evidence capture:

```text
main...origin/main clean
```
