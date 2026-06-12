# CloudEdge SAM Minimal

This directory is a minimal current-style CloudEdge SAM authoring example. It is
separate from `examples/cloudedge-mobility-demo`, which is an older full demo
with legacy scaffolding and provider-specific operational assumptions.

The first target is the transport substrate:

- one shared `SAMNodeSet/fabric` as the node identity registry;
- `WireGuardInterface.spec.peersFrom: SAMNodeSet/fabric` for bootstrap peers;
- RR/onprem `SAMTransportProfile.publishPeerGroup: true`;
- leaf/cloud `SAMTransportProfile.spec.peersFrom: SAMPeerGroup/cloudedge-transport`;
- leaf/cloud bootstrap `IPv4Route` entry so first-contact peer-group sync
  reaches the RR/onprem SAM endpoint over `wg-hybrid` on a fresh host;
- BGP over SAMTransportProfile-generated IPIP tunnels.

This does not implement first-contact WireGuard enrollment. Until ADR0015
exists, every node still needs a trusted copy of `SAMNodeSet/fabric` containing
the WireGuard public keys and reachable bootstrap endpoints.

## Validate

`routerctl validate` is a control API command. Use the sandbox wrapper from the
repository root:

```sh
scripts/routerd-sandbox-run.sh sh -c 'for config in "$@"; do go run ./cmd/routerctl validate --socket "$ROUTERD_SANDBOX_STATUS_SOCKET" -f "$config" --replace; done' sh \
  examples/cloudedge-sam-minimal/onprem.yaml \
  examples/cloudedge-sam-minimal/cloud.yaml
```

## Runtime shape

Use `onprem.yaml` on the RR/onprem node and `cloud.yaml` on one cloud leaf. This
is intentionally a two-node transport baseline. Expand the `SAMNodeSet` only
after the two-node dataplane is green on real VMs.

The cloud leaf includes an explicit `10.99.0.1/32 dev wg-hybrid` bootstrap
route. Do not remove it unless routerd grows an equivalent pre-sync route
installer: without it, a fresh cloud leaf can handshake WireGuard but still send
peer-group HTTP sync traffic for the RR/onprem SAM endpoint through its default
underlay route.

For a multi-leaf lab, the leaf still only needs the RR/onprem bootstrap route
when the shared `SAMNodeSet` marks that node with `routeReflector: true` and a
reachable `samEndpoint`. Peer-group sync prefers those RR endpoints before
falling back to legacy WireGuard peer probing, so non-RR leaf `samEndpoint`
routes can be derived later by `SAMTransportProfile` instead of being present
before sync.
