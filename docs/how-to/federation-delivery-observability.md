# Federation delivery observability

Event Federation (ADR 0006, Phase 2) pushes events between routerd nodes via
the outbox. This guide explains how to verify that deliveries are healthy, spot
problems, and act on them using `routerctl`.

## Quick check

```sh
routerctl doctor federation --state-file /var/lib/routerd/routerd.db
```

A healthy system prints all `PASS`:

```
DOCTOR  PASS  pass=7 warn=0 fail=0 skip=0
AREA        STATUS  CHECK                                        DETAIL                                  REMEDY
federation  PASS    cloudedge/leaf-az failed deliveries           no failed deliveries
federation  PASS    cloudedge/leaf-az pending deliveries          no pending deliveries
federation  PASS    cloudedge/leaf-az stale TTL                   no stale TTL deliveries
federation  PASS    cloudedge/leaf-az delivery lag                max delivery lag 2s
federation  PASS    cloudedge/leaf-az event expiry                nearest event expires in 1740s
federation  PASS    cloudedge/leaf-az expected delivery           all 3 active event(s) have delivery rows
federation  SKIP    expected peers                                no self-emitted active events to deliver
```

## Commands

### Delivery summary

Per-(group, peer) aggregate of all active delivery rows:

```sh
routerctl federation deliveries summary \
  --group cloudedge \
  --state-file /var/lib/routerd/routerd.db
```

Output:

```
GROUP      PEER     EVENTS  DELIVERED  STALE_TTL  FAILED  PENDING  MAX_LAG  MIN_EXPIRES_IN
cloudedge  leaf-az  3       3          0          0       0        2s       29m0s
cloudedge  leaf-oci 3       2          0          1       0        4s       29m0s
```

Add `-o json` or `-o yaml` for machine-readable output. Use `--include-expired`
to include events whose TTL has already passed.

### Doctor federation

`routerctl doctor federation` runs two categories of checks:

1. **Recorded-delivery checks** — examine existing delivery rows per (group,
   peer) for failures, pending expiry, stale TTL, and delivery lag.
2. **Expected-peer audit** — derive the expected peer set from `EventGroup` and
   `EventPeer` resources in the startup config, then verify that every
   self-emitted active event has a delivery row for each expected peer.

Run against a specific area:

```sh
routerctl doctor federation \
  --state-file /var/lib/routerd/routerd.db \
  --config /usr/local/etc/routerd/router.yaml
```

`--config` is needed for expected-peer checks because the audit reads
`EventGroup` (for the self node name) and `EventPeer` (for declared peers) from
the startup config.

## Reading the summary table

| Column | Meaning |
|--------|---------|
| **GROUP** | EventGroup name (e.g. `cloudedge`) |
| **PEER** | Target peer node name |
| **EVENTS** | Total active event-delivery pairs |
| **DELIVERED** | Successfully pushed to peer |
| **STALE_TTL** | Delivered but the event TTL was refreshed since delivery (`event.expires_at > delivery.event_expires_at`); the peer holds a stale copy |
| **FAILED** | All retry attempts exhausted |
| **PENDING** | Enqueued but not yet delivered |
| **MAX_LAG** | Worst-case time between event observation and delivery |
| **MIN_EXPIRES_IN** | Time until the soonest event expires; negative means already expired |

Healthy state: `DELIVERED == EVENTS`, `FAILED == 0`, `PENDING == 0`,
`STALE_TTL == 0`.

## Doctor check reference

### Recorded-delivery checks

These run per (group, peer) pair that has delivery rows in the state store.

| Check | FAIL | WARN | PASS |
|-------|------|------|------|
| **failed deliveries** | Any delivery row in `failed` status | — | `FAILED == 0` |
| **pending deliveries** | Pending events whose TTL expires within 120 s | Pending exists but no imminent expiry | `PENDING == 0` |
| **stale TTL** | All delivered events have stale TTL (`STALE_TTL == DELIVERED`) | Some deliveries have stale TTL | `STALE_TTL == 0` |
| **delivery lag** | Max lag >= 180 s | Max lag >= 60 s | Below 60 s |
| **event expiry** | Nearest expiry < 120 s with pending/failed deliveries | Nearest expiry < 120 s, all delivered | Comfortable margin |

### Expected-peer audit

These check that the config-declared peers actually have delivery rows.

| Check | FAIL | SKIP | PASS |
|-------|------|------|------|
| **expected delivery** | Self-emitted active events exist but no delivery row for this peer | No self-emitted events in the group | All active events have delivery rows |
| **empty endpoint** | `EventPeer.spec.endpoint` is empty | — | — |

The audit excludes the self node: if `EventGroup.spec.nodeName` matches
`EventPeer.spec.nodeName`, that peer is skipped (a node does not push events to
itself).

## Common failures and remedies

### EventPeer endpoint not set

```
FAIL  cloudedge/leaf-oci expected delivery  EventPeer endpoint is empty
      set spec.endpoint on EventPeer/leaf-oci for group cloudedge
```

The `EventPeer` resource declares the peer but has no endpoint URL. The outbox
cannot push events without a target. Set `spec.endpoint` to the peer's federation
listener (e.g. `https://10.252.0.3:8443/v1/federation/events`).

### Missing delivery rows for expected peer

```
FAIL  cloudedge/leaf-oci expected delivery  2 of 3 active event(s) have no delivery row: evt-abc, evt-def
      outbox never enqueued delivery for this peer; check EventPeer config and outbox peer filter
```

The outbox creates delivery rows only for events where `SourceNode` matches the
local `EventGroup.spec.nodeName`. If the self node name in the EventGroup does
not match the `source_node` column in `federation_events`, no delivery is
enqueued. Verify:

```sh
routerctl federation event list --group cloudedge --state-file /var/lib/routerd/routerd.db
```

Check the `SOURCE` column matches the EventGroup `nodeName`. Also confirm that
the outbox controller is running (`eventd` daemon) and that no `types` /
`subjectPrefixes` filter on the `EventPeer` silently excludes the events.

### HMAC authentication mismatch

The outbox push returns HTTP 403 or 401. Check `journalctl -u routerd-eventd`
for authentication errors. Verify that both ends share the same
`EventGroup.spec.auth.hmacSecretRef` and that the referenced Secret exists and
contains the correct key.

### Outbox not running

```
FAIL  cloudedge/leaf-az pending deliveries  3 pending; 2 event(s) expire within 120s without delivery
      outbox may be stalled or peer unreachable; check eventd logs and peer endpoint
```

Delivery rows were created but nothing was pushed. Confirm `routerd-eventd` is
running:

```sh
systemctl status routerd-eventd
journalctl -u routerd-eventd --since "10 min ago"
```

### Stale TTL after refresh

```
WARN  cloudedge/leaf-az stale TTL  1 of 3 delivered event(s) have stale TTL (event.expiresAt > delivery.eventExpiresAt)
      outbox should re-push refreshed events on next tick; if this persists, check outbox interval and delivery filtering
```

An event's TTL was extended (e.g. by re-emitting with `--ttl`) but the delivery
record still holds the old `event_expires_at`. The outbox should detect this on
its next tick and re-push (PR #531). If the warning persists, check the outbox
interval and that the re-push logic is working:

```sh
routerctl federation event deliveries \
  --group cloudedge --peer leaf-az \
  --state-file /var/lib/routerd/routerd.db
```

Compare `EVENT_EXPIRES_AT` in the delivery row against the event's `EXPIRES`
column in `routerctl federation event list`.

### SourceNode does not match EventGroup nodeName

Events emitted with a `--source-node` that differs from the local
`EventGroup.spec.nodeName` are not pushed by the outbox (the outbox only pushes
self-emitted events). The expected-peer audit will show these events as missing
delivery rows. Verify the source node matches:

```sh
routerctl federation event list --group cloudedge -o json \
  --state-file /var/lib/routerd/routerd.db | jq '.[].sourceNode'
```

## SAMSubnetPolicy delivery verification

For CloudEdge SAM deployments, `routerd.mobility.shard.assigned` events carry
the shard assignment from the hub to leaf nodes. Verify delivery:

```sh
# Hub node: check deliveries to all leaf peers
routerctl federation deliveries summary --group cloudedge \
  --state-file /var/lib/routerd/routerd.db

# Hub node: doctor check including expected-peer audit
routerctl doctor federation \
  --state-file /var/lib/routerd/routerd.db \
  --config /usr/local/etc/routerd/router.yaml

# Leaf node: confirm the event was received and the subscription processed it
routerctl federation event list --group cloudedge \
  --state-file /var/lib/routerd/routerd.db
routerctl dynamic list --state-file /var/lib/routerd/routerd.db
```

On the leaf, `routerctl dynamic list` should show a `DynamicConfigPart` with
provenance `routerd.net/event-group: cloudedge`. If the event is delivered but
no dynamic config appears, the issue is on the subscription/plugin side, not
delivery — see [Event Federation Subscription](./event-federation-subscription).

## JSON output for automation

Both commands support JSON output for integration with monitoring or scripts:

```sh
# Summary as JSON
routerctl federation deliveries summary --group cloudedge -o json \
  --state-file /var/lib/routerd/routerd.db

# Doctor as JSON
routerctl doctor federation -o json \
  --state-file /var/lib/routerd/routerd.db \
  --config /usr/local/etc/routerd/router.yaml
```

The doctor JSON includes `summary.overall` (`"pass"`, `"warn"`, or `"fail"`)
for alerting thresholds. A non-`"pass"` overall status indicates at least one
check that warrants investigation.

### Federation summary in doctor JSON

When the `federation` area is checked, the doctor JSON includes a
`federation` object with aggregated delivery statistics:

```json
{
  "federation": {
    "severityCounts": {"pass": 7, "warn": 0, "fail": 0, "skip": 1},
    "failedDeliveryCount": 0,
    "staleTTLCount": 0,
    "pendingDeliveryCount": 0,
    "missingExpectedPeerCount": 0,
    "maxDeliveryLagSeconds": 2,
    "minExpiresInSeconds": 1740,
    "totalEvents": 3,
    "totalDelivered": 3,
    "subscriptionRunsTotal": 3,
    "subscriptionRunsSucceeded": 3,
    "subscriptionRunsFailed": 0,
    "subscriptionRunsPending": 0
  }
}
```

### jq examples

```sh
# Overall status for alerting
routerctl doctor federation -o json \
  --state-file /var/lib/routerd/routerd.db \
  --config /usr/local/etc/routerd/router.yaml \
  | jq -r '.summary.overall'

# Failed delivery count (exit 1 if > 0)
routerctl doctor federation -o json \
  --state-file /var/lib/routerd/routerd.db \
  --config /usr/local/etc/routerd/router.yaml \
  | jq -e '.federation.failedDeliveryCount == 0'

# Missing expected peers
routerctl doctor federation -o json \
  --state-file /var/lib/routerd/routerd.db \
  --config /usr/local/etc/routerd/router.yaml \
  | jq '.federation.missingExpectedPeerCount'

# All failing checks with remedies
routerctl doctor federation -o json \
  --state-file /var/lib/routerd/routerd.db \
  --config /usr/local/etc/routerd/router.yaml \
  | jq '[.checks[] | select(.status == "fail") | {name, detail, remedy}]'

# Stale TTL and pending counts for monitoring
routerctl doctor federation -o json \
  --state-file /var/lib/routerd/routerd.db \
  --config /usr/local/etc/routerd/router.yaml \
  | jq '{stale: .federation.staleTTLCount, pending: .federation.pendingDeliveryCount, lag: .federation.maxDeliveryLagSeconds}'

# Subscription run failures (exit 1 if > 0)
routerctl doctor federation -o json \
  --state-file /var/lib/routerd/routerd.db \
  --config /usr/local/etc/routerd/router.yaml \
  | jq -e '.federation.subscriptionRunsFailed == 0'
```

## Subscription monitoring

When `EventSubscription` resources are configured, `routerctl doctor federation`
also checks subscription processing health:

| Check | FAIL | WARN | PASS |
|-------|------|------|------|
| **subscription runs** | Any run in `failed` status | Pending runs exist | All runs succeeded |

### Inspecting subscription runs

List processing records for a specific subscription:

```sh
routerctl federation subscription runs \
  --subscription EventSubscription/cloud-claims \
  --state-file /var/lib/routerd/routerd.db
```

Output:

```
ID  SUBSCRIPTION                      EVENT_ID  GROUP      PLUGIN        STATUS     ATTEMPTS  STARTED                    COMPLETED                  ERROR
1   EventSubscription/cloud-claims    evt-abc   cloudedge  cloud-claims  succeeded  1         2026-06-17T12:00:00Z       2026-06-17T12:00:01Z
2   EventSubscription/cloud-claims    evt-def   cloudedge  cloud-claims  failed     3         2026-06-17T12:01:00Z       2026-06-17T12:01:05Z       plugin exited 1
```

Add `-o json` for machine-readable output. Failed runs indicate the plugin
returned an error; check the plugin output and event payload.

### End-to-end delivery verification

To verify the full pipeline from event emission to DynamicConfigPart:

```sh
# 1. Verify events are delivered
routerctl federation deliveries summary --group cloudedge \
  --state-file /var/lib/routerd/routerd.db

# 2. Verify subscriptions processed events
routerctl federation subscription runs \
  --subscription EventSubscription/cloud-claims \
  --state-file /var/lib/routerd/routerd.db

# 3. Verify DynamicConfigParts were generated
routerctl dynamic list --state-file /var/lib/routerd/routerd.db
```

If events are delivered but subscription runs show failures, the issue is in the
plugin or its inputs. If subscription runs succeed but no DynamicConfigPart
appears, check the plugin output format.

## OpenTelemetry metrics reference

`routerd-eventd` emits the following metrics when an OTLP endpoint is configured
(see [Send telemetry to an OTLP collector](./opentelemetry)):

### Outbox delivery metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `routerd_eventd_outbox_delivery_total` | counter | group, peer, event_type, status | Delivery results (delivered/failed) |
| `routerd_eventd_outbox_delivery_attempts_total` | counter | group, peer, event_type, status | Total push attempts |
| `routerd_eventd_outbox_delivery_lag_seconds` | histogram | group, peer, event_type | Time between event observation and delivery |
| `routerd_eventd_outbox_repush_total` | counter | group, peer, event_type | Re-pushes after TTL refresh |
| `routerd_eventd_outbox_stale_ttl_delivery_total` | counter | group, peer, event_type | Stale TTL detections |

### Outbox loop health metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `routerd_eventd_outbox_tick_total` | counter | group | Outbox loop iterations |
| `routerd_eventd_outbox_tick_errors_total` | counter | group | Failed outbox iterations |
| `routerd_eventd_outbox_tick_duration_seconds` | histogram | group | Time per outbox iteration |

### Receiver metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `routerd_eventd_receiver_accepted_total` | counter | group | Events accepted |
| `routerd_eventd_receiver_duplicate_total` | counter | group | Duplicate events received |
| `routerd_eventd_receiver_reject_total` | counter | group, reason | Events rejected (reason: bad_timestamp, stale_timestamp, bad_signature, bad_body, validation, read_body, store_error) |

### Pruner metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `routerd_eventd_pruner_tick_total` | counter | group | Pruner loop iterations |
| `routerd_eventd_pruner_pruned_total` | counter | group | Events pruned by retention |
| `routerd_eventd_pruner_tick_errors_total` | counter | group | Failed pruner iterations |

### Label policy

Allowed labels: `group`, `peer`, `event_type`, `status`, `reason`. High-cardinality
values (`event_id`, `subject`, `address`, `dedupe_key`, `endpoint`, raw `error`
strings) are never used as metric labels.

### Alerting thresholds

Recommended Prometheus alerting rules:

```
# Outbox loop stalled (no tick in 5 minutes)
rate(routerd_eventd_outbox_tick_total[5m]) == 0

# High delivery failure rate
rate(routerd_eventd_outbox_delivery_total{status="failed"}[5m]) > 0

# Outbox loop errors
rate(routerd_eventd_outbox_tick_errors_total[5m]) > 0

# Receiver rejecting events
rate(routerd_eventd_receiver_reject_total[5m]) > 0.1

# Pruner errors
rate(routerd_eventd_pruner_tick_errors_total[5m]) > 0

# Delivery lag p99 above 60s
histogram_quantile(0.99, rate(routerd_eventd_outbox_delivery_lag_seconds_bucket[5m])) > 60
```
