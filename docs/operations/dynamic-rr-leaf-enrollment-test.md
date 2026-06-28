# Dynamic RR/Leaf Enrollment Test Runbook

This runbook validates the dual-RR `SAMRRSet` enrollment flow before any
cloud/PVE full-topology test.

Primary target: private-underlay SAM transport with `mode: ipip` and
`encryption: none`. WireGuard remains an optional transport-specific path for
public underlay, but it is not the default enrollment model.

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

The RR configs include the same example `SAMEnrollmentClaim/leaf-pve` so the
paired RR flow can be tested without an external enrollment service. In a real
deployment, the accepted claim should be fanned out to both RRs by the
enrollment service or config distribution path.

## Required Inputs

Replace example values before live testing:

| Placeholder | Meaning |
| --- | --- |
| `/usr/local/etc/routerd/secrets/cloudedge-join-token` | Shared join token available on each RR. |
| `EXAMPLE_HMAC_SHA256_HEX` | HMAC over the claim join payload using the join token. |
| `10.10.0.2` | rr-a private underlay endpoint. |
| `10.10.0.3` | rr-b private underlay endpoint. |
| `10.20.0.21` | leaf private underlay endpoint. |
| `10.99.0.2/32` | rr-a SAM/BGP tunnel identity. |
| `10.99.0.3/32` | rr-b SAM/BGP tunnel identity. |
| `10.255.0.21/32` | leaf tunnel address, inside policy `tunnelAddressPrefixes`. |
| `10.77.60.21/32` | leaf-owned MobilityPool address. |

Current implementation validates presence and scope of join fields when
`joinTokenFrom` is configured. Full cryptographic HMAC verification is a
follow-up controller/enrollment-service step before production use.

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
bin/linux/routerctl validate -f examples/cloudedge-dynamic-rr-a-hub.yaml --replace
bin/linux/routerctl validate -f examples/cloudedge-dynamic-rr-b-hub.yaml --replace
bin/linux/routerctl validate -f examples/cloudedge-dynamic-leaf-pve.yaml --replace

bin/linux/routerctl plan -f examples/cloudedge-dynamic-rr-a-hub.yaml --replace
bin/linux/routerctl plan -f examples/cloudedge-dynamic-rr-b-hub.yaml --replace
bin/linux/routerctl plan -f examples/cloudedge-dynamic-leaf-pve.yaml --replace
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
- controller tests show RR-side generated `TunnelInterface` resources can be
  created without generated per-leaf `BGPPeer` resources when
  `generatePeers: false`.
- WG materialization is covered only by WG-specific tests using optional
  `wireGuard` blocks.

## Negative Tests

Local tests should cover:

- `BGPDynamicPeer.routeReflectorClient=true` rejects a peer ASN different from
  the referenced `BGPRouter` local ASN.
- `SAMRRSet` allows members without `wireGuard` blocks.
- `SAMEnrollmentClaim` is valid without `wireGuard.publicKey`.
- a configured `joinTokenFrom` requires claim `joinNonce`, `joinTimestamp`, and
  `joinHMAC`.
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

Full topology pass criteria should include:

- branch binaries installed on rr-a, rr-b, and leaf;
- no static RR-side `BGPPeer/leaf-*`;
- leaf establishes transport toward both RRs;
- RRs accept BGP sessions through `BGPDynamicPeer`;
- only authorized MobilityPool `/32` routes are propagated;
- minimal connectivity over the authorized `/32`.
