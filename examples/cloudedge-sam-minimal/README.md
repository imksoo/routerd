# CloudEdge SAM Minimal

This directory is a minimal current-style CloudEdge SAM authoring example. It is
separate from `examples/cloudedge-mobility-demo`, which is an older full demo
with legacy scaffolding and provider-specific operational assumptions.

The first target is the transport substrate:

- one shared `SAMNodeSet/fabric` as the node identity registry;
- `WireGuardInterface.spec.peersFrom: SAMNodeSet/fabric` for bootstrap peers;
- RR/onprem `SAMTransportProfile.publishPeerGroup: true`;
- leaf/cloud `SAMTransportProfile.spec.peersFrom: SAMPeerGroup/cloudedge-transport`;
- leaf/cloud bootstrap `IPv4Route` entries so first-contact peer-group sync
  reaches SAM endpoints over `wg-hybrid` on a fresh host;
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

For a multi-leaf lab, add the same kind of bootstrap route for every other
node's `samEndpoint` in the shared `SAMNodeSet`. A 5-node Azure/AWS/OCI run
showed that peer-group sync may probe non-RR peers before it reaches the
publisher; if those endpoint /32 routes are missing, the probes fall back to
the cloud VM's default route and time out even while WireGuard handshakes are
healthy. This should become automatic pre-sync route generation, but until then
the declarative route entries are part of the minimal working config.
