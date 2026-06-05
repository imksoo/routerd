# How-to: turn a federated event into a RemoteAddressClaim

CloudEdge Event Federation (ADR 0006) lets one routerd node observe a fact and
have another node react to it declaratively. Phase 3 closes the loop on the
receiver side: a received event matches an `EventSubscription`, which runs a
trusted local plugin, whose output becomes a `DynamicConfigPart` you can inspect
with `routerctl dynamic render`.

This guide uses the shipped, provider-agnostic example plugin
`event-to-remote-claim`.

## The flow

```
on-prem routerd                         cloud routerd
---------------                         -------------
observe LAN client
  -> emit federation event   --push-->  receive event (EventGroup)
     routerd.client.ipv4.observed         |
                                          v
                                   EventSubscription match
                                          |
                                          v
                                   run Plugin (event-to-remote-claim)
                                          |
                                          v
                                   PluginResult -> DynamicConfigPart
                                          |
                                          v
                                   routerctl dynamic render
                                     shows RemoteAddressClaim
```

1. **Emit** — an on-prem node observes a client and emits a
   `routerd.client.ipv4.observed` event into a shared `EventGroup`.
2. **Transport (Phase 2)** — the event is pushed over the overlay to the cloud
   node's `EventGroup` receiver.
3. **Match** — the cloud node's `EventSubscription` matches the event by type
   (and optionally subject prefix / source node).
4. **Plugin** — the subscription's `trigger.pluginRef` Plugin runs with the
   matched events on stdin and returns a `PluginResult`.
5. **DynamicConfigPart** — routerd validates the result and stores it as a
   dynamic config part, stamped with provenance annotations
   (`routerd.net/event-id`, `routerd.net/event-group`,
   `routerd.net/dynamic-source`).
6. **Render** — `routerctl dynamic render` shows the effective config, including
   the new `RemoteAddressClaim`.

## Example resources

- Receiver (cloud) wiring: [`examples/event-federation/receiver-cloud.yaml`](https://github.com/imksoo/routerd/blob/main/examples/event-federation/receiver-cloud.yaml)
  — `EventGroup`, `EventSubscription`, `Plugin`, plus the hybrid context
  (`OverlayPeer`, `AddressMobilityDomain`, `CloudProviderProfile`) that the
  resulting `RemoteAddressClaim` resolves against.
- Sender (on-prem) wiring: [`examples/event-federation/sender-onprem.yaml`](https://github.com/imksoo/routerd/blob/main/examples/event-federation/sender-onprem.yaml)
  — `EventGroup` + `EventPeer` push target.
- Example plugin: [`examples/plugins/event-to-remote-claim/`](https://github.com/imksoo/routerd/tree/main/examples/plugins/event-to-remote-claim).

## Try it

Build and install the example plugin:

```sh
go build -o bin/event-to-remote-claim ./examples/plugins/event-to-remote-claim
install -D bin/event-to-remote-claim \
  /usr/local/libexec/routerd/plugins/event-to-remote-claim/bin/event-to-remote-claim
```

Apply the receiver config, then emit a test event (normally Phase 2 delivers it
from the on-prem node):

```sh
routerctl federation event emit \
  --state-file /var/lib/routerd/routerd.db \
  --group cloudedge --type routerd.client.ipv4.observed \
  --subject 10.88.60.9/32 --source-node onprem-router \
  --payload address=10.88.60.9/32 \
  --payload domain=cloudedge-same-subnet \
  --payload ownerSide=onprem \
  --payload peerRef=onprem-main \
  --payload providerRef=example-provider \
  --ttl 30m
```

After the EventSubscription controller reconciles, render the effective config:

```sh
routerctl dynamic render \
  --config /usr/local/etc/routerd/router.yaml \
  --state-file /var/lib/routerd/routerd.db
```

You will see a `RemoteAddressClaim` for `10.88.60.9/32` carrying the event
provenance annotations.

## Scope and safety

- The example plugin is **provider-agnostic** and performs **no cloud
  mutation**. Its `capture` block is a dry-run-intent placeholder
  (`configureOSAddress: false`).
- Executing a provider operation to actually claim the address (the
  `actionPlan`) is **Phase 4/5**; the MVP never executes action plans.
- routerd never passes config or secrets to a plugin — only observed events.
- `EventSubscription.match.types` is required so a subscription cannot
  blanket-trigger a plugin on every event in the group; use `subjectPrefixes`
  and `sourceNodes` to narrow further and guard against loops.
