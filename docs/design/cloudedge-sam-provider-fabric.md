# CloudEdge SAM Provider Fabric Lifecycle

This note records the post-T1/A1/A2 lifecycle model for CloudEdge Selective
Address Mobility. It is a design guardrail for cleanup work after the live
multi-cloud validation, not a new operator-authored API.

ADR 0012 remains the base decision: BGP `/32` best path is the overlay
reachability source of truth. Cloud provider secondary IPs, route-table entries,
and guest OS capture state are provider-fabric ingress reconciliation. They are
important for real packets entering through AWS/Azure/OCI/on-prem fabrics, but
they must not become a second overlay ownership database.

## Goals

- Keep `MobilityPool` as the only operator-authored mobility intent.
- Explain each mobile `/32` with one ownership model across provider API state,
  guest OS state, routerd status, BGP advertisement, FIB import, and doctor.
- Gate BGP owner advertisement on observed ownership evidence, not on stale
  action journal rows.
- Keep cloud and on-prem evidence normalized so AWS, Azure, OCI, and on-prem
  sources feed the same resolver.
- Prefer read-time reconciliation from observed facts over destructive cleanup
  when evidence is stale or ambiguous.

## Separation Of Responsibilities

```text
MobilityPool
  Declarative pool, member, capture, placement, and discovery intent.

ownership resolver
  Normalizes static owners, provider discovery events, on-prem ownership
  events, local provider inventory, BGP best paths, and action/capture evidence
  into one per-address decision table.

BGP delivery planner
  Advertises only addresses this node is allowed to own, and imports remote
  best paths into the Linux FIB through the BGP controller.

provider-action planner/executor
  Reconciles provider-fabric ingress artifacts such as secondary private IPs,
  UDRs, route-table routes, source/destination checks, and forwarding flags.
  It does not decide overlay ownership.

Linux capture controllers
  Reflect local provider/on-prem capture into OS state: proxy ARP, forwarding,
  route residue cleanup, or the absence/presence of a local `/32` when that is
  part of the capture strategy.

doctor / routerctl mobility owners
  Explain the same state tables to operators and detect drift between intended
  ownership, provider/OS evidence, and the host FIB.
```

This split is deliberately narrower than introducing a dedicated RR ownership
database. The current control-plane owner table is federation/BGP-derived and
inspectable from nodes with the aggregated state, including hub/on-prem style
nodes. If a dedicated RR management daemon becomes necessary, it should be a
separate design and should consume the same normalized owner rows rather than
introducing a parallel ownership model.

## Address State Model

Each `(pool, address)` should be explainable with these facts:

| Fact | Meaning |
| --- | --- |
| `homeOwner` | The node/provider/resource that appears to own the endpoint address, from static config, provider discovery, on-prem discovery, or BGP. |
| `localEvidence` | Evidence observed by this node that the address is local to its fabric: provider private IP inventory, on-prem ARP/IPAM/PVE evidence, or local self/router addresses. |
| `captureHolder` | The provider/on-prem capture artifact holder, such as a secondary IP holder, route-table target, or proxy-ARP master. |
| `captureState` | Whether capture is confirmed, stale, absent, or unknown for this node. |
| `advertiseOwner` | The node whose BGP owner advertisement is allowed for the address. |
| `suppressionReason` | Why advertisement or provider claim is withheld. |
| `conflictReason` | Why ownership evidence is inconsistent, for example duplicate provider owners or remote owner overlapping local evidence. |

`ownershipResolverControlPlaneOwnerTable` is the operator-facing snapshot of
these facts. `ownershipResolverFIBVerdicts` is the corresponding route action
view: local route, deliver remote, or withhold.

## Provider Secondary-IP Mode

Secondary-IP capture is valid only when the provider fabric confirms that the
secondary private IP belongs to this node's provider attachment.

Expected steady state for an active cloud holder:

1. Provider discovery observes the secondary IP on this node's NIC/VNIC/ipConfig.
2. Provider forwarding prerequisites are true:
   - AWS source/destination check disabled.
   - Azure `enableIPForwarding=true`.
   - OCI source/destination check skipped.
3. Guest OS state matches the capture policy:
   - If `configureOSAddress=true`, the `/32` is present on the declared local
     interface.
   - If `configureOSAddress=false`, the provider-captured address is absent from
     guest interfaces and packets are forwarded to the BGP-imported owner path.
4. BGP owner advertisement is allowed only for addresses the ownership resolver
   classifies as local/static/on-prem owned or as a valid ownership event. A
   mere provider capture holder is not a home owner by itself.

Failed provider assign actions are active failures only while provider discovery
does not observe the address as self-captured. Once current provider truth
observes self-capture, the old failed action is superseded and retained only as
diagnostic breadcrumb.

## Route-Table / UDR Mode

Route-table capture is a separate lifecycle from secondary-IP capture. It should
not be treated as proof that the guest owns the destination address.

Expected steady state:

1. Provider discovery confirms the cloud route/UDR target points at this router
   node or its NIC.
2. Provider forwarding prerequisites are true.
3. Guest OS has forwarding-ready state, but does not add the destination `/32`
   as a local address merely because the route-table target points here.
4. BGP advertisement is gated on route-table target confirmation plus forwarding
   readiness, not on local address presence.

Secondary-IP and route-table modes should share normalized owner/capture rows,
but their OS reflection checks differ. Cleanup code must branch on capture
strategy instead of assuming every provider capture is a local OS `/32`.

## On-Prem Capture

On-prem capture remains local-fabric authority:

- `proxy-arp` capture requires the configured capture interface and proxy
  neighbor state.
- VRRP-master or equivalent local guard decides whether the node may answer on
  the LAN.
- On-prem ownership discovery events can conflict with remote cloud provider
  owner events and must appear in the same control-plane owner table.

BGP still decides remote overlay delivery. It does not replace the local L2/ARP
authority guard.

## Fencing And Stale Work

The clean model is a level projection from current observed facts. Provider
actions may be slow, retried, or left in historical journal rows; those rows
must not resurrect old ownership after the desired BGP/provider state changes.

Current implementation uses `mobilityPathSig` plus desired holder/action target
to fence provider actions. Cleanup should move toward these rules:

- A stale action journal row is diagnostic history unless its path signature and
  holder still match the current desired row.
- Destructive unassign/delete should require positive evidence that the local
  capture is stale and not a current delivery/capture candidate.
- Empty or failed provider discovery should fail safe: keep active failures red,
  and avoid claiming success or deleting ambiguous state.
- Route/FIB cleanup should compare the exact intended artifact and Linux route
  selection, not just the presence of any route for the same prefix.

Avoid adding new ad hoc cleanup paths that encode one observed lab failure. If a
cleanup is necessary, it should be derivable from the normalized owner table,
provider evidence, and capture strategy.

## Doctor Expectations

`routerctl doctor sam` should continue to treat doctor/status as diagnostic
evidence, not as the dataplane authority. The checks should explain at least:

- cloud holder exists but OS/forwarding state is missing;
- OS `/32` exists but provider holder is absent or belongs to another node;
- route-table target points to this node but forwarding is not ready;
- provider forwarding flag is disabled;
- duplicate provider owners or overlapping cloud/on-prem evidence;
- BGP owner and provider/OS evidence disagree;
- stale action journal history is active, superseded, or safely ignored;
- Linux FIB route selection contradicts the owner table.

Actual acceptance still requires provider API state, OS state, BGP/FIB state,
and real dataplane probes to agree.

## Cleanup Backlog

The following code areas should be reviewed against this model before adding
more feature work:

- ownership classes around `ConfirmedCapture` and `StaleCapture`;
- observed-self stale capture cleanup and its hold timers;
- provider action import/execution gates keyed by `mobilityPathSig`;
- OS address cleanup for provider-secondary captures;
- route-table/UDR capture strategy handling;
- duplicate owner conflict reporting and FIB verdicts;
- doctor checks that may be treating diagnostic RED as dataplane RED.

Each cleanup PR should state which rule above it simplifies and should include
focused tests plus live cloud evidence when it changes provider/OS behavior.
