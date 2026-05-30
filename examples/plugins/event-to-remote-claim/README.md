# event-to-remote-claim — provider-agnostic example plugin

This is an EXAMPLE / REFERENCE routerd plugin for CloudEdge Event Federation
(ADR 0006, Phase 3). It is intentionally provider-INDEPENDENT and dry-run only.

It reads a routerd `PluginRequest` JSON from stdin, and for each matched
`routerd.client.ipv4.observed` federation event under `spec.events` it emits one
`RemoteAddressClaim` resource in a `PluginResult` on stdout. It performs **no
network or cloud calls** and depends only on the Go standard library.

## What it derives from each event

| Claim field                  | Source                                         |
| ----------------------------- | ---------------------------------------------- |
| `metadata.name`               | deterministic from owner side + address (`onprem-10-88-60-9`) |
| `spec.address`                | `payload.address`, else the event `subject`    |
| `spec.domainRef`              | `payload.domain`                               |
| `spec.ownerSide`              | `payload.ownerSide` (default `onprem`)         |
| `spec.capture.type`           | `provider-secondary-ip` (provider-agnostic placeholder) |
| `spec.capture.providerRef`    | `payload.providerRef` (default `example-provider`) |
| `spec.capture.nicRef`         | `payload.nicRef` (default `example-nic-ref`)   |
| `spec.delivery.peerRef`       | `payload.peerRef` (default `onprem-main`)      |
| `spec.delivery.tunnelInterface` | `wg-hybrid`                                  |

`spec.capture.configureOSAddress` is always `false`: the capture spec is
dry-run intent only. Actually claiming the address on a provider is a Phase 4/5
`actionPlan`, which routerd never executes in the MVP.

## Build and install

```sh
go build -o bin/event-to-remote-claim ./examples/plugins/event-to-remote-claim
install -D bin/event-to-remote-claim \
  /usr/local/libexec/routerd/plugins/event-to-remote-claim/bin/event-to-remote-claim
```

## Try it standalone

```sh
echo '{"spec":{"events":[{"id":"e1","type":"routerd.client.ipv4.observed","subject":"10.88.60.9/32","payload":{"domain":"cloudedge-same-subnet","ownerSide":"onprem"}}]}}' \
  | ./bin/event-to-remote-claim
```

See `examples/event-federation/` for the receiver-side `EventSubscription` +
`Plugin` wiring and `docs/how-to/event-federation-subscription.md` for the
end-to-end flow.
