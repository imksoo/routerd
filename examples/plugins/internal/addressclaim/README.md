# Cloud `*-address-claim` example plugins (aws / azure / oci)

These are EXAMPLE / REFERENCE routerd plugins for CloudEdge Event Federation
(ADR 0006, **Phase 4.1**). They are **DRY-RUN ONLY** and make **no cloud
calls**: they import no provider CLI/SDK and no `os/exec`, perform no network
access, and mutate nothing. Each reads a routerd `PluginRequest` JSON on stdin
(matched `routerd.client.ipv4.observed` events plus the allowlisted,
secret-redacted context resources) and writes a `PluginResult` on stdout
carrying:

- one `RemoteAddressClaim` resource (capture `provider-secondary-ip`,
  `configureOSAddress: false` — dry-run intent only), and
- two display-only `ActionPlan`s, each with an `undo`:
  - `assign-secondary-ip` (undo `unassign-secondary-ip`)
  - `ensure-forwarding-enabled` (undo `ensure-forwarding-disabled`)

routerd validates and persists action plans but **never executes** them.
Executing the provider operation is **out of scope** (Phase 5).

## Shared core

All three plugins are thin `main`s over the shared
`examples/plugins/internal/addressclaim` package, so they share the same input
handling, the same output shape, and the same test matrix. Each `main` only
supplies a small `ProviderProfile` (provider id + forwarding param +
provider-specific target keys). The core mirrors the routerd wire JSON with
local structs and depends only on the Go standard library; it does **not**
import `pkg/plugin` or `pkg/api`.

## Per-provider specifics

| Provider | `provider` | Forwarding parameter   | Provider target keys (besides provider/providerRef/nicRef/address) |
| -------- | ---------- | ---------------------- | ------------------------------------------------------------------ |
| AWS      | `aws`      | `sourceDestCheck=false`| `region`, `account`, `subnetRef` (from event payload, omitted if absent) |
| Azure    | `azure`    | `ipForwarding=true`    | `subscriptionId`, `resourceGroup` (CloudProviderProfile spec, else payload), `region`, `ipConfigName` (payload) |
| OCI      | `oci`      | `skipSourceDestCheck=true` | `compartmentId`, `region` (from event payload, omitted if absent) |

### Where values come from

- `address` — `payload.address`, else the event `subject` (required).
- `domainRef` — `payload.domain`, else the `AddressMobilityDomain` context
  resource name (required).
- `ownerSide` — `payload.ownerSide` (default `onprem`).
- `providerRef` — the `CloudProviderProfile` context resource name.
- `delivery.peerRef` — the `OverlayPeer` context resource name, else
  `payload.peerRef`.
- `nicRef` (ENI id / NIC id / VNIC OCID) — `payload.nicRef` (**required**; the
  plugin never invents a cloud resource id).

The `CloudProviderProfile`, `AddressMobilityDomain`, and `OverlayPeer` context
resources are **required**: a missing one is a hard error, not a silent default.

## Build and install

Build to a temporary location (do not commit binaries):

```sh
for p in aws azure oci; do
  go build -o "/tmp/${p}-address-claim" "./examples/plugins/${p}-address-claim"
  install -D "/tmp/${p}-address-claim" \
    "/usr/local/libexec/routerd/plugins/${p}-address-claim/bin/${p}-address-claim"
done
```

## Try it standalone

```sh
go build -o /tmp/aws-address-claim ./examples/plugins/aws-address-claim
cat <<'JSON' | /tmp/aws-address-claim
{
  "spec": {
    "events": [
      {"id":"e1","type":"routerd.client.ipv4.observed","subject":"10.88.60.9/32",
       "payload":{"domain":"cloudedge-same-subnet","ownerSide":"onprem",
                  "nicRef":"eni-0abc123","region":"ap-northeast-1"}}
    ],
    "context": {
      "resources": [
        {"apiVersion":"hybrid.routerd.net/v1alpha1","kind":"CloudProviderProfile","name":"aws-tokyo","spec":{"provider":"aws"}},
        {"apiVersion":"hybrid.routerd.net/v1alpha1","kind":"AddressMobilityDomain","name":"cloudedge-same-subnet","spec":{"prefix":"10.88.60.0/24"}},
        {"apiVersion":"hybrid.routerd.net/v1alpha1","kind":"OverlayPeer","name":"onprem-main","spec":{"role":"onprem"}}
      ]
    }
  }
}
JSON
```
