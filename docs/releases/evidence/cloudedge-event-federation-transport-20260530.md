# CloudEdge Event Federation Phase 2 transport smoke

Result: PASS

Date: 2026-05-30
Branch/build: event-federation / f951fd471a7e
Build command: `make dist`

Evidence bundle:
`/home/imksoo/routerd-labs/event-federation/evidence/20260530T091652Z-phase2-transport-f951fd47`

## Topology

Transport-only smoke used a PVE-only pair. No Azure, AWS, or OCI VM was started.

- Sender: router03 / 192.168.123.125 / `router03.lain.local`
- Receiver: router05 / 192.168.123.127 / `router05.lain.local`
- EventGroup: `cloudedge-event-smoke`
- Sender EventGroup nodeName: `onprem-event-node`
- Receiver EventGroup nodeName: `cloud-event-node`
- Overlay: `wg-hybrid`
- Sender overlay address: `169.254.250.3/32`
- Receiver overlay address: `169.254.250.5/32`
- Receiver eventd listen: `169.254.250.5:9443`
- Sender EventPeer endpoint: `http://169.254.250.5:9443`

The emitted event used `--source-node onprem-event-node`, matching the sender
EventGroup `spec.nodeName`.

## Deployment Evidence

- `make dist` completed with static Linux artifacts.
- `routerd`, `routerctl`, and `routerd-eventd` from build `f951fd471a7e` were deployed to both nodes.
- Both generated configs passed `routerd check`.
- Receiver `routerd-eventd@cloudedge-event-smoke.service` listened on `169.254.250.5:9443`.
- Sender `routerd-eventd@cloudedge-event-smoke.service` ran push/prune only, as expected.
- Overlay reachability passed both directions:
  - router03 -> `169.254.250.5`: 3/3 ping, 0% loss
  - router05 -> `169.254.250.3`: 3/3 ping, 0% loss
  - router03 curl to `http://169.254.250.5:9443/`: HTTP 404 from eventd, proving listener reachability

## Assertions

### A. Sender local store

PASS. Sender stored event:

- ID: `evt-phase2-smoke-20260530T092231Z`
- Group: `cloudedge-event-smoke`
- SourceNode: `onprem-event-node`
- Type: `routerd.client.ipv4.observed`
- Subject: `10.88.60.9/32`

### B. Sender delivery

PASS. Sender delivery reached the receiver peer:

- EventID: `evt-phase2-smoke-20260530T092231Z`
- Peer: `cloud-event-node`
- Status: `delivered`
- Attempts: `1`
- DeliveredAt: `2026-05-30T09:22:41Z`

### C. Receiver store

PASS. Receiver stored the same event ID:

- EventID: `evt-phase2-smoke-20260530T092231Z`
- RecordedAt on receiver: `2026-05-30T09:22:43Z`
- Receiver status after first delivery: `received=1 duplicate=0 rejected=0 storedEvents=1`

### D. Idempotent duplicate

PASS. Re-emitting the same event ID did not create a second receiver event.

- Sender delivery remained `attempts=1`
- Receiver still had one event with ID `evt-phase2-smoke-20260530T092231Z`
- Receiver status remained `received=1 duplicate=0 storedEvents=1`

### E. Bad HMAC

PASS. Synthetic POST with an invalid `X-Routerd-Signature` returned:

- HTTP status: `401 Unauthorized`
- Body: `bad signature`
- Receiver store unchanged
- Receiver status advanced to `rejected=1`

### F. Expired event

PASS. Expired event was stored locally on the sender but was not pushed.

- Expired EventID: `evt-expired-20260530T092347Z`
- ObservedAt: `2026-05-30T09:14:02Z`
- ExpiresAt: `2026-05-30T09:14:03Z`
- Sender delivery query: `null`
- Receiver did not receive the expired event

### G. Restart-resume

PASS. A fresh event emitted while sender eventd was stopped was delivered after
the sender eventd service restarted.

- Resume EventID: `evt-resume-20260530T092347Z`
- Sender eventd before emit: `inactive`
- Receiver before sender restart: only the original main event
- Delivery after restart: `delivered`, `attempts=1`, `deliveredAt=2026-05-30T09:24:18Z`
- Receiver status after resume: `received=2 duplicate=0 rejected=1 storedEvents=2`

## Known Lab Notes

- The PVE receiver already had a firewall default-drop policy. To let eventd
  accept traffic on the new overlay interface, `WireGuardInterface/wg-hybrid`
  was added to router05's existing management firewall zone for this smoke.
- Explicit `/32` route resources were added for the overlay peer addresses:
  `169.254.250.5 dev wg-hybrid metric 120` on router03 and
  `169.254.250.3 dev wg-hybrid metric 120` on router05.
- The runbook used `routerctl federation event deliveries --group ...`, but the
  current CLI supports delivery lookup by `--event-id`. Assertions used
  `--event-id`.
- `make dist` initially did not include `routerd-eventd` in the release payload.
  The working tree adds it to the Makefile release build/install list.

## Verdict

CloudEdge Event Federation Phase 2 transport-only smoke passed:

- local emit persisted in sender SQLite
- outbox loop pushed to EventPeer
- receiver HMAC verification accepted the valid event
- receiver persisted the same event ID
- sender delivery became `delivered`
- duplicate ID was idempotent
- bad HMAC was rejected with 401
- expired event was not delivered
- restart-resume proved SQLite-backed outbox delivery
- no EventSubscription, plugin trigger, DynamicConfigPart, ARP observer, provider action, or cloud mutation was used
