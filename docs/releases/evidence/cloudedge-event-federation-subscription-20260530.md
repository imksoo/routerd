# CloudEdge Event Federation Phase 3 subscription smoke

Result: PASS

Date: 2026-05-30
Branch/build: event-federation / 515fe7e8d086
Build command: `make dist`

Evidence bundle:
`/home/imksoo/routerd-labs/event-federation/evidence/20260530T111612Z-phase3-subscription-515fe7e8`

## Topology

The smoke used the same PVE-only pair as Phase 2. No cloud VM was started.

- Sender: router03 / 192.168.123.125 / `router03.lain.local`
- Receiver: router05 / 192.168.123.127 / `router05.lain.local`
- EventGroup: `cloudedge-event-smoke`
- Sender EventGroup nodeName: `onprem-event-node`
- Receiver EventGroup nodeName: `cloud-event-node`
- Receiver listen: `169.254.250.5:9443`
- Sender EventPeer endpoint: `http://169.254.250.5:9443`
- AddressMobilityDomain: `cloudedge-same-subnet`
- Plugin executable path: `/usr/local/libexec/routerd/plugins/event-to-remote-claim/bin/event-to-remote-claim`

The sender and receiver configs were adapted from:

- `examples/event-federation/sender-onprem.yaml`
- `examples/event-federation/receiver-cloud.yaml`

The receiver plugin path was instrumented with a wrapper that logged stdin and
then executed the built example plugin binary as `event-to-remote-claim.real`.
This provided the `PluginRequest.spec.events` evidence without changing plugin
output.

## Deployment

- `make dist` completed for `515fe7e8`.
- `routerd`, `routerctl`, and `routerd-eventd` were deployed to both nodes.
- The example plugin was built separately with `CGO_ENABLED=0 GOOS=linux` and
  installed on router05.
- Both generated configs passed `routerd check`.
- `routerd-eventd@cloudedge-event-smoke.service` was active on both nodes.
- Receiver `ss` showed a listener on `169.254.250.5:9443`.
- Overlay reachability passed both directions:
  - router03 -> `169.254.250.5`: 3/3 ping, 0% loss
  - router05 -> `169.254.250.3`: 3/3 ping, 0% loss

## Main Assertion

Event:

- ID: `evt-phase3-smoke-20260530T112250Z`
- Type: `routerd.client.ipv4.observed`
- Subject: `10.88.60.9/32`
- SourceNode: `onprem-event-node`
- Payload domain: `cloudedge-same-subnet`
- Payload ownerSide: `onprem`

Result:

- Sender delivery to `cloud-event-node`: `delivered`, attempts `1`
- Receiver federation store contained the same event ID
- EventSubscription run:
  - subscription: `EventSubscription/cloud-claims`
  - plugin: `event-to-remote-claim`
  - status: `succeeded`
  - attempts: `1`
  - dynamic source: `EventSubscription/cloud-claims/07634fdff9b3235c`
- Plugin run:
  - trigger type: `federation-subscription`
  - trigger topic: `cloud-claims`
  - exit code: `0`
  - status: `succeeded`
- `routerctl dynamic list -o json` showed one active DynamicConfigPart.
- `routerctl dynamic render -o yaml` showed:
  - kind: `RemoteAddressClaim`
  - name: `onprem-10-88-60-9`
  - address: `10.88.60.9/32`
  - domainRef: `cloudedge-same-subnet`
  - ownerSide: `onprem`
  - capture.type: `provider-secondary-ip`
  - capture.providerRef: `example-provider`
  - capture.nicRef: `example-nic-ref`
  - delivery.peerRef: `onprem-main`
  - delivery.tunnelInterface: `wg-hybrid`

The rendered claim carried provenance annotations:

- `routerd.net/dynamic-source: EventSubscription/cloud-claims`
- `routerd.net/event-group: cloudedge-event-smoke`
- `routerd.net/event-id: evt-phase3-smoke-20260530T112250Z`
- `routerd.net/event-subject: 10.88.60.9/32`

The captured PluginRequest contained the main event under `spec.events` with the
same ID, subject, source node, and payload.

## Negative Checks

Duplicate idempotency: PASS

- Re-emitting `evt-phase3-smoke-20260530T112250Z` did not create another
  subscription run.
- The main event delivery remained attempts `1`.
- DynamicConfigPart count remained `1`.
- Rendered `RemoteAddressClaim/onprem-10-88-60-9` count remained `1`.
- Plugin request log stayed at one successful request.

Non-match event: PASS

- Event ID: `evt-phase3-nonmatch-20260530T112250Z`
- ownerSide: `cloud`
- Transport delivery: `delivered`
- Receiver stored the event.
- No subscription run was created for it.
- No DynamicConfigPart or rendered claim for `10.88.60.10/32`.

Expired event: PASS

- Event ID: `evt-phase3-expired-20260530T112250Z`
- ObservedAt: `2026-05-30T11:14:07Z`
- ExpiresAt: `2026-05-30T11:14:08Z`
- Sender delivery query: `null`
- Receiver did not receive the expired event.
- No subscription run or rendered claim for `10.88.60.11/32`.

Plugin failure retry cap: PASS

- Event ID: `evt-phase3-pluginfail-20260530T112250Z`
- Matched a lab-only `EventSubscription/cloud-claims-fail`.
- Failure plugin exited `42`.
- `event_subscription_runs` ended at `status=failed`, `attempts=3`.
- Three failed plugin run rows were recorded.
- No DynamicConfigPart was created for `10.88.60.66/32`.

## Scope Checks

- No provider action was executed.
- No cloud resource was created, started, stopped, or mutated.
- No Phase 4 actionPlan execution was attempted.
- No SAM dataplane apply was performed; the RemoteAddressClaim exists only in
  `routerctl dynamic render`.
- No ARP observer, provider-specific plugin, or DynamicConfigPart consumer path
  was used.

## Verdict

Phase 3 control-plane automation passed:

manual emit -> transport -> EventSubscription match -> plugin.Run ->
DynamicConfigPart -> `routerctl dynamic render` RemoteAddressClaim.

Phase 4 was not started.

## Pre-flight note

Pre-flight was requested after the smoke had already entered execution. The main
path passed, and the generated PluginResult/DynamicConfigPart confirmed
retroactively:

- payload domain matched `AddressMobilityDomain.metadata.name` (`cloudedge-same-subnet`)
- plugin executable was present and invoked (`event-to-remote-claim`, exit 0)
- receiver hybrid context was complete (the rendered `RemoteAddressClaim` resolved
  `domainRef` / `delivery.peerRef` / `capture.providerRef` against the receiver
  config and passed `dynamic render` validation)
- provider mutation was not attempted

i.e. pre-flight was not skipped — the main path PASS is what proved the
config/context were correct.
