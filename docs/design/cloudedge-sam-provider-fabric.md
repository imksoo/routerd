# CloudEdge SAM provider fabric and capture lifecycle

This design note records the CloudEdge Selective Address Mobility (SAM)
provider-fabric model from issue #446. It is intentionally design-only: it does
not change the SAM planner, ownership resolver, generated schema, or provider
executors.

The immediate purpose is PR A from #446: define the state planes, convergence
vocabulary, capture-mode lifecycles, BGP advertisement gates, doctor/status
expectations, and follow-up PR boundaries before adding more SAM-core code.

## Problem

The PR #445 lab evidence showed that CloudEdge SAM can report
`ownershipResolverPhase=Resolved` while the real dataplane is still not usable.
That is not a small stale-secondary-IP cleanup bug. It means too many different
facts are currently represented as if they were one decision:

- home ownership: where the `/32` originally or authoritatively belongs
- cloud provider fabric holder: which provider object captures ingress for the
  `/32`
- OS capture state: whether the router VM owns the local `/32` or is
  forwarding-ready
- FIB/BGP reachability: whether routes are installed and BGP paths are admitted
- advertisement authority: whether this node may advertise `/32 owner`
- HA role and fencing: active, standby, draining, stale, or fenced epoch

Promiscuous mode is not a substitute for OS capture. The clean observer VM
experiments behind #446 found that provider API-only secondary IP delivery can
put packets on the NIC, but a router VM still does not answer or forward
correctly unless the required OS state is present for that capture mode.

## Core rule: Resolved is not Converged

`ownershipResolverPhase=Resolved` means the resolver did not find a blocking
ownership-evidence conflict for its current input set. It is a necessary
condition for SAM convergence, but it is not sufficient.

T1, A1, and A2 acceptance must key on `samConvergencePhase=Ready`, not on
`ownershipResolverPhase=Resolved`.

A SAM address is converged only when these facts agree for the current
mobility path signature, ownership epoch, capture epoch, or fence token used by
the implementation generation:

- home ownership evidence is known and non-conflicting
- provider cloud claim matches the desired holder or route target
- OS local `/32` or forwarding evidence matches the capture mode
- host forwarding, rp_filter, firewall, and policy route prerequisites are ready
- exact FIB state has no local/link/BGP conflict and points to the expected path
- BGP advertisement/import is allowed by the advertisement gate
- stale action journal entries cannot resurrect an older holder

Unknown, stale, or conflicting evidence is fail-closed.

## Terms

These identities are separate and must stay separate in status, doctor output,
and future implementation:

| Term | Meaning |
| --- | --- |
| `home owner` | The node, site, or provider where the `/32` naturally belongs. |
| `homeProviderRef` | Provider reference for the home owner, if the address is cloud-owned. |
| `cloud holder` | The provider API object that currently captures cloud ingress. |
| `capture provider` | The provider/site where local ingress capture is being attempted. |
| `OS holder` | The router OS that locally owns the `/32` in secondary-IP mode. |
| `route target` | Provider route-table/UDR target in route-table mode. |
| `forwarding-ready` | Host state proving packets can be forwarded without local `/32` ownership. |
| `BGP advertiser` | The node whose `/32 owner` route may be advertised to RR/BGP. |
| `HA role holder` | The active, standby, draining, inactive, or fenced node role. |
| `epoch` / `fenceToken` | Monotonic token or path signature used to reject stale actions and stale advertisements. |

BGP/RR may distribute candidate ownership and evidence metadata, but it is not
the final database for provider fabric ownership. Final `/32 owner`
advertisement is gated by provider observed holder, OS or forwarding evidence,
and the current epoch or fence token.

## State planes

### HomeOwnershipEvidence

`HomeOwnershipEvidence` records the authoritative or observed home of an
address.

Expected fields:

- `pool`
- `address`
- `homeOwnerNode`
- `homeProviderRef`
- `site`
- `source`: `providerInventory`, `onpremEvent`, `static`, `dhcp`, or `bgp`
- `resourceRef`, `nicRef`, `subnetRef`, or equivalent provider identifiers
- `freshness`
- `conflictReason`

This plane answers "where does the address belong?" It does not answer whether
the cloud fabric has captured ingress or whether the router OS can forward.

### CloudCaptureClaim

`CloudCaptureClaim` records desired and observed provider fabric capture.

Expected fields:

- `pool`
- `address`
- `mode`: `secondary-ip` or `route-table`
- `captureProviderRef`
- `desiredHolder`
- `observedCloudHolder`
- `observedRouteTarget`
- `targetRef`
- `epoch` or `fenceToken`
- `phase`
- `reason`

This plane answers "what does the provider API currently deliver to?" In
secondary-IP mode it is the secondary private IP holder. In route-table/UDR mode
it is the provider route target.

### OSCaptureEvidence

`OSCaptureEvidence` records local host facts that make the provider claim usable.

Expected fields:

- `pool`
- `address`
- `node`
- `mode`
- `localAddressPresent`
- `forwardingReady`
- `ipForward`
- `rpFilter`
- `firewallVerdict`
- `routeGetVerdict`
- `observedAt`
- `phase`
- `reason`

In secondary-IP mode, the active cloud holder must reflect the captured `/32` as
a local OS address, and standby nodes must not hold it. In route-table/UDR mode,
the destination `/32` must not be local-owned; the host must instead prove it is
forwarding-ready.

### AdvertisementGate

`AdvertisementGate` is the final per-address decision for BGP `/32 owner`
advertisement or import.

Expected fields:

- `pool`
- `address`
- `allowed`
- `bgpOwnerNode`
- `requiredEvidence`
- `blockedBy`
- `epoch` or `fenceToken`
- `reason`

The gate must require current provider evidence plus OS/forwarding evidence for
the selected capture mode. It must not rely on `ownershipResolverPhase=Resolved`
alone.

### SAMConvergenceStatus

`SAMConvergenceStatus` is the operator-facing rollup for a pool/address or for a
pool summary.

Expected fields:

- `ownershipResolverPhase`
- `cloudClaimPhase`
- `osCapturePhase`
- `forwardingPhase`
- `fibConvergencePhase`
- `advertisementGatePhase`
- `samConvergencePhase`
- `splitBrainDetected`
- `staleEpochDetected`
- `blockingReasons`
- `lastObservedAt`

This status is the acceptance and doctor surface. It exists because individual
planes can be healthy while the end-to-end SAM dataplane is not.

## Phase vocabulary

### ownershipResolverPhase

- `Resolved`: ownership evidence has no known conflict.
- `Conflict`: two or more incompatible owner facts exist.
- `Unknown`: evidence is absent or insufficient.
- `Stale`: evidence exists but is outside the freshness window.

`Resolved` is only a prerequisite for convergence.

### cloudClaimPhase

- `NotApplicable`: this address does not need cloud provider capture.
- `Pending`: desired provider claim is not yet observed.
- `Claimed`: provider observed holder or route target matches desired state.
- `Conflict`: multiple holders or an incompatible holder/target exists.
- `Stale`: observed claim is older than the current epoch or path signature.
- `Failed`: provider operation or observation failed.

### osCapturePhase

- `NotApplicable`: no local OS capture state is required.
- `Missing`: required local `/32` or forwarding evidence is absent.
- `Reflected`: required local `/32` is present on the active holder.
- `ForwardingReady`: route-table mode forwarding evidence is ready.
- `Unexpected`: local `/32` exists when the mode requires it to be absent.
- `Conflict`: multiple OS holders or incompatible OS state exists.

### forwardingPhase

- `NotApplicable`: forwarding is not part of this address lifecycle.
- `Ready`: ip_forward, rp_filter, firewall, and policy route checks pass.
- `Disabled`: a required forwarding flag is disabled.
- `Rejected`: firewall, rp_filter, or policy routing rejects the path.
- `Unknown`: forwarding state is not observable.

### fibConvergencePhase

- `Ready`: exact route state is installed and points to the expected path.
- `MissingRoute`: expected route is absent.
- `ConflictingRoute`: local/link/static and BGP/SAM routes conflict.
- `WrongNextHop`: route exists but points to an unexpected device or next hop.
- `Unknown`: FIB state could not be observed.

### advertisementGatePhase

- `Allowed`: all required evidence matches the current epoch/fence token.
- `Blocked`: at least one required evidence plane is missing, stale, or
  conflicting.

### samConvergencePhase

- `Ready`: all required phases are ready and advertisement is allowed.
- `Degraded`: ownership is resolved but one or more dataplane prerequisites are
  missing, stale, or unknown.
- `Failed`: a conflict, split brain, stale epoch resurrection, provider failure,
  or hard dataplane mismatch exists.

`Degraded` and `Failed` are not acceptable for T1/A1/A2.

## Capture-mode lifecycles

### Secondary-IP mode

Secondary-IP mode captures ingress by attaching the client `/32` to a router
NIC/VNIC as a provider secondary private address or equivalent provider object.

Expected provider usage:

- OCI: preferred current CloudEdge capture direction.
- AWS and Azure: supported when explicitly selected, but route-table/UDR is the
  separate lifecycle for local ingress route capture.

Provider facts to observe:

- address and prefix
- provider reference
- NIC/VNIC reference
- subnet and VPC/VNet/VCN reference
- instance or resource identifier
- primary versus secondary private IP
- instance state
- provider forwarding flag:
  - AWS ENI `SourceDestCheck`
  - Azure NIC `enableIPForwarding`
  - OCI VNIC `skipSourceDestCheck`

Lifecycle:

1. Determine that this node is the active capture candidate.
2. Plan or observe provider secondary-IP assignment for this node's NIC/VNIC.
3. Confirm `observedCloudHolder == desiredHolder` for the current epoch or path
   signature.
4. Reflect the captured `/32` into the active holder OS.
5. Confirm standby nodes do not hold the local `/32`.
6. Confirm forwarding, rp_filter, firewall, and FIB prerequisites.
7. Allow BGP `/32 owner` advertisement only after provider holder, OS holder,
   and current epoch/fence token agree.

Invariants:

- Standby nodes must not keep all capture `/32`s as local addresses.
- Provider claim without OS local `/32` is not converged.
- OS local `/32` without matching provider claim is drift.
- Multiple provider holders or multiple OS holders are hard failures.
- Stale secondary-IP action journals must not revive an old holder.

### Route-table / UDR mode

Route-table/UDR mode captures ingress by changing the provider route for the
client `/32` to target the router. The router does not own the destination `/32`
as a local address.

Expected provider usage:

- AWS: route table `/32` target is the router ENI.
- Azure: UDR `/32` target is `VirtualAppliance` with `nextHopIpAddress` set to
  the router NIC private IP.
- OCI: route-table capture is not the preferred current CloudEdge direction;
  OCI remains secondary-IP unless a future provider-specific design changes it.

Provider facts to observe:

- route destination `/32`
- route table or UDR reference
- route target:
  - AWS `NetworkInterfaceId`
  - Azure `nextHopType` and `nextHopIpAddress`
  - OCI private IP OCID if route-table mode is ever enabled
- route state where exposed by the provider
- target NIC/VNIC forwarding flag
- target instance state
- Azure effective routes and effective security rules when diagnosing drops

Lifecycle:

1. Determine that this node is the active capture candidate.
2. Plan or observe the provider route/UDR for the destination `/32`.
3. Confirm the route target points at this node's NIC, ENI, private IP, or VNIC.
4. Confirm the destination `/32` is not installed as a local OS address.
5. Confirm `forwardingReady`: provider forwarding flag, Linux forwarding,
   rp_filter, firewall, policy routing, and route-get checks pass.
6. Confirm exact FIB state has no local/link route conflict.
7. Allow BGP `/32 owner` advertisement only after provider route target,
   forwarding evidence, and current epoch/fence token agree.

Invariants:

- The destination `/32` must not be local-owned in route-table mode.
- A provider route target without forwarding readiness is not converged.
- A local/link route plus BGP/SAM route conflict is a hard failure unless an
  explicit design says otherwise.
- A route to a foreign ENI/NIC/private IP is a hard failure.

## BGP/RR role and advertisement gate

ADR 0012 makes BGP the source of truth for overlay reachability. This document
does not reverse that decision. It narrows the cloud-native ingress and
advertisement boundary:

- BGP/RR can distribute candidate `/32` reachability, owner attributes, path
  preferences, and evidence metadata.
- BGP/RR must not be treated as the final database for provider API holder,
  route-table target, local OS `/32`, or forwarding readiness.
- Provider fabric capture remains background reconciliation for overlay
  reachability, but cloud-native ingress must be reported as not converged until
  provider and OS/forwarding evidence agree.
- A `/32 owner` advertisement that implies local provider ingress readiness must
  be gated by provider observed holder plus OS/forwarding evidence plus current
  epoch/fence token.

Useful BGP metadata can include:

```text
routerd:cloud=aws|azure|oci|onprem
routerd:capture=secondary-ip|route-table|proxy-arp|local
routerd:scope=<vpc|vnet|vcn|site>
routerd:owner=<node-id>
routerd:intent=candidate|active|withdraw
routerd:epoch=<generation>
```

Those attributes are evidence distribution, not exclusive authority.

## Capture eligibility and providerRef risk

`decisionEligibleForCapture` must not use `homeProviderRef ==
captureProviderRef` as the capture eligibility axis.

The two references mean different things:

- `homeProviderRef`: where the client `/32` naturally belongs.
- `captureProviderRef`: where this router is trying to capture local ingress.

Cross-provider capture is valid when the topology, capture mode, HA role, and
evidence gates allow it. For example, an AWS router can need to capture an
Azure, OCI, or on-prem home `/32` for AWS-local ingress; an Azure router can need
to capture an AWS home `/32` for Azure-local ingress.

The eligibility axis should be:

```text
remote home address is needed for local ingress capture
AND this site/node is an active capture candidate
AND same-site local ownership is not being captured accidentally
AND the selected capture mode supports this topology
AND no ownership, cloud, OS, FIB, or HA conflict exists
AND the required evidence gate can be satisfied
```

Same-placement local-home suppression remains important. The bug risk is using
providerRef equality as a shortcut for that topology decision.

## Doctor and status drift checks

`routerctl status`, `routerctl doctor`, and incident dumps should make the state
planes visible and fail closed on these inconsistencies:

- `ownershipResolverPhase=Resolved` but `samConvergencePhase!=Ready`
- multiple home owner facts for one `/32`
- multiple cloud holders for one `/32`
- multiple OS holders for one `/32`
- cloud secondary IP is attached to this node but OS local `/32` is missing
- OS local `/32` exists but cloud holder is absent or different
- standby node holds a local `/32` that should be active-only
- route-table target is this node but `forwardingReady` is false
- provider route/UDR target is foreign, deleted, stopped, or stale
- provider forwarding flag is disabled:
  - AWS `SourceDestCheck`
  - Azure `enableIPForwarding`
  - OCI `skipSourceDestCheck`
- Azure effective routes do not choose the expected UDR/static route
- Azure effective security rules block the expected path
- exact `/32` route is missing
- exact `/32` route points to the wrong next hop or device
- local/link route conflicts with BGP/SAM route
- BGP owner and provider/OS holder disagree
- stale action journal or old epoch would resurrect an old capture
- old and new epochs are simultaneously active
- split brain is suspected from heartbeat, provider holder, or BGP evidence

Doctor output should identify which plane is blocking convergence instead of
collapsing all failures into "ownership not resolved".

## Acceptance gates

### Minimal fabric evidence

Before T1/A1/A2 is used as proof, each provider/capture mode needs minimal
fabric evidence:

- secondary-IP mode: provider attachment alone is not enough; OS local `/32`
  reflection must be proven.
- route-table/UDR mode: provider route target alone is not enough; forwarding
  readiness and absence of local `/32` ownership must be proven.
- BGP overlay success does not prove cloud-native ingress success.

### T1 baseline

T1 passes only when every expected SAM address reports:

- `ownershipResolverPhase=Resolved`
- `cloudClaimPhase=Claimed` or `NotApplicable`
- `osCapturePhase=Reflected`, `ForwardingReady`, or `NotApplicable` according
  to mode
- `forwardingPhase=Ready` or `NotApplicable`
- `fibConvergencePhase=Ready`
- `advertisementGatePhase=Allowed`
- `samConvergencePhase=Ready`
- doctor has no hard SAM drift findings

### A1 failover

A1 starts only after T1 is green. It adds graceful or abrupt active movement and
requires:

- old holder stops advertising or is fenced
- new cloud holder or route target is observed
- new OS capture or forwarding state is observed
- old epoch/path-signature actions are invalidated
- no standby local `/32` leak remains
- T1 readiness returns after the transition

### A2 split-brain and fencing

A2 starts only after T1 and A1 are green. It requires:

- split-brain suspicion is surfaced
- stale epoch or stale path-signature operations are blocked
- provider holder and OS holder do not diverge silently
- BGP advertisement is blocked until the new holder has current provider and
  OS/forwarding evidence
- old owner revival cannot resurrect stale capture through the action journal

If T1 fails, A1 and A2 are blocked.

## Relation to existing ADRs

### ADR 0008: Capture Coordination via Fencing Tokens

ADR 0008 defines `captureEpoch` as a per-capture-domain fencing token for
provider action projection and stale-action rejection. This document extends the
same safety idea across cloud holder, OS reflection, forwarding readiness, and
BGP advertisement. The key point is that a provider action being fenced does not
prove dataplane convergence; convergence also needs observed provider and host
evidence.

### ADR 0010: Capture Ownership Arbitration

ADR 0010 defines `ownershipEpoch` and a deterministic owner map without adding
consensus. This document treats that map as the ownership-evidence input, not as
the final convergence result. The owner map must feed cloud claims, OS capture
evidence, FIB convergence, and advertisement gates.

### ADR 0011: Generalized Failover

ADR 0011 records that AWS, Azure, OCI, and on-prem reassignment semantics differ
and that cloud-inventory observe, drift/orphan/conflict detection, and doctor
hardening are required. This document makes those provider-specific differences
visible as separate secondary-IP and route-table/UDR lifecycles, then adds the
OS reflection or forwarding-ready plane needed by the #446 evidence.

### ADR 0012: BGP /32 Address Mobility

ADR 0012 moves overlay reachability to BGP and demotes provider capture to
background reconciliation. This document is compatible with that split. BGP can
be the overlay delivery source of truth while provider fabric convergence
remains a separate cloud-native ingress status. BGP best path, by itself, does
not prove secondary-IP attachment, UDR target correctness, OS local `/32`
reflection, forwarding readiness, or provider forwarding flags.

## Follow-up PR slicing

The work should be split so each PR has a narrow review and acceptance boundary.

1. Design-only
   - Add or update this document.
   - No schema, Go code, planner, resolver, or executor changes.
   - No lab requirement.
2. Status vocabulary
   - Add `SAMConvergenceStatus` and phase fields.
   - Preserve existing behavior.
   - Add synthetic status/doctor coverage for "Resolved but not Converged".
3. Capture eligibility fix
   - Replace `homeProviderRef == captureProviderRef` capture eligibility with a
     topology/capture-mode/HA-role/evidence decision.
   - Add cross-provider capture tests and same-site local suppression tests.
4. Cloud/OS evidence separation
   - Separate home ownership, cloud holder, OS holder, forwarding evidence, and
     advertisement gate status.
   - Stop using one ownership decision as the implicit BGP gate.
5. Secondary-IP lifecycle
   - Enforce provider secondary attachment plus active-only OS local `/32`.
   - Gate advertisement on provider holder plus OS holder plus current
     epoch/fence token.
   - Require clean observer VM evidence.
6. Route-table/UDR lifecycle
   - Enforce provider route target plus forwarding-ready evidence.
   - Require destination `/32` absence as a local address.
   - Surface provider forwarding flags and Azure effective route/security checks.
7. HA/fencing
   - Add or harden active/standby/draining/inactive/fenced state.
   - Invalidate old epoch/path-signature actions.
   - Run clean T1 -> A1 -> A2; A1/A2 remain blocked while T1 is not green.

## Non-goals for this design-only PR

- Do not change the SAM planner.
- Do not change the ownership resolver.
- Do not change provider executors.
- Do not change generated schema.
- Do not implement remote plugin install or remote plugin registry.
- Do not claim NixOS or FreeBSD provider-fabric parity beyond existing
  groundwork.
