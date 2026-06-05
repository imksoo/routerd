# Event Federation reference

> Experimental (CloudEdge). See [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md)
> for the design and invariants, and the how-to
> [Event Federation subscription](../how-to/event-federation-subscription.md) for a
> worked example.

Event Federation lets routerd nodes exchange **typed, observed facts** (e.g. "this
client IPv4 was observed", "this address expired") over the overlay, and lets a
subscriber turn matched events into derived configuration via a plugin. It is the
control-plane substrate beneath
[Selective Address Mobility](./selective-address-mobility): an observed address
on one node becomes a `RemoteAddressClaim` (capture) on another.

The model is **at-least-once delivery with idempotent, observed-fact events**.
Events are immutable statements about the world ("observed"), never imperative
commands; a receiver re-deriving the same state from the same events is a no-op.

## Kinds

### `EventGroup`

The bus a node participates in. One node has one identity per group.

| Field | Meaning |
|---|---|
| `nodeName` | This node's identity in the group; stamped as `sourceNode` on emitted events. |
| `retention` | Bounds how many events / how long the local store keeps them. Empty/zero = unlimited. |
| `auth` | HMAC secret material for peer delivery (push). |
| `listen` | Receiver bind (`address`) for inbound peer pushes. Empty = push-only (no receiver). |
| `replayWindow` | Go duration bounding accepted message timestamp skew for replay protection (default `5m`). |

### `EventPeer`

A remote node this node pushes events to.

| Field | Meaning |
|---|---|
| `groupRef` | The `EventGroup` this peer belongs to (required). |
| `nodeName` | Remote peer node identity (required). |
| `endpoint` | Base URL to push to, e.g. `http://10.99.0.7:8787` (required for push). |
| `direction` | Delivery direction; only `push` is supported. Empty defaults to `push`. |
| `types` | Optional event-type allowlist; empty delivers all. |
| `subjectPrefixes` | Optional subject-prefix allowlist; empty delivers all. |

### `EventSubscription`

Turns matched events into a plugin invocation that emits a `DynamicConfigPart`.

| Field | Meaning |
|---|---|
| `groupRef` | The `EventGroup` to consume from. |
| `match` | Which events to act on (by type / subject). |
| `trigger.pluginRef` | The `Plugin` invoked for matched events. |
| `trigger.batchWindow` | Coalesce matched events into one invocation (Go duration). |
| `trigger.debounce` | Delay invocation until after the last matched event (Go duration). |

## `routerctl federation` CLI

```
routerctl federation event emit  --group <g> --type <topic> --subject <entity> [--source-node <n>] [--ttl <dur>] [--payload k=v ...]
routerctl federation event list  --group <g>
routerctl federation event deliveries --group <g>
```

`emit` records an observed fact into the local store (e.g.
`--type routerd.client.ipv4.observed --subject 10.88.60.9/32`). `list` shows
recorded events; `deliveries` shows per-peer push delivery state.

> Self-capture guard (ADR 0006 no-feedback-loop invariant): a node must not emit
> `routerd.client.ipv4.observed` for an address it is itself capturing via a local
> `RemoteAddressClaim`, or the delivered capture address would loop back as a fresh
> observation.

## Transport — `routerd-eventd`

`routerd-eventd@<group>` is a long-lived per-group daemon (supervised by a
generated systemd unit on Linux, rc.d on FreeBSD) that:

- **pushes** locally-recorded events to each `EventPeer` over HTTP, signed with the
  group HMAC; the receiver verifies the signature and rejects messages outside the
  `replayWindow`.
- records **deliveries** (per peer, per event) so at-least-once retry is bounded and
  observable.
- **prunes** the local event store per the group `retention`.

The outbox carries a `sourceNode` guard so a received event is not re-forwarded back
to its origin (no delivery loop).

## Subscription → plugin → DynamicConfigPart flow

1. A node emits an observed fact (`routerctl federation event emit`, or a future
   observer).
2. `routerd-eventd` delivers it to peers; each peer records it in its event store.
3. A peer's `EventSubscription` matches the event and invokes `trigger.pluginRef`,
   coalescing per `batchWindow` / `debounce`.
4. The plugin returns a `DynamicConfigPart` (e.g. a `RemoteAddressClaim`), which the
   [dynamic-config](./dynamic-config.md) chain unions into the effective config and
   reconciles into the dataplane.

This keeps the operator-authored intent declarative: the operator declares the
group/peers/subscription; the claims, captures, and action plans are **derived**.
