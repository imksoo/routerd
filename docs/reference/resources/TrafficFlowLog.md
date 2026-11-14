# TrafficFlowLog

[Resource configuration reference](../resources.md#trafficflowlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Record connection snapshots in a SQLite flow database, optionally enriching them from the DPI cache. This is not a packet capture or a log of every packet.

## Conditions and references

- The observer selects the first decodable TrafficFlowLog resource, even if disabled; a later enabled resource does not override it. Configure one resource for predictable recording.
- Recording runs after successful conntrack observation; unavailable observation prevents that round of recording. The connection query is limited to 10000 entries. Sampling occurs on observer reconciliation, normally every 30s unless runtime options change it.
- DPI enrichment reads the first enabled FirewallEventLog database when available, matching connection tuples against cached classification within one hour. Enrichment is not guaranteed by either include flag; both false also do not prevent enrichment if that cache is available. Use LogRetention signal trafficFlows to manage flow retention.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TrafficFlowLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # optional; boolean
    includeApplicationLayer: true # optional; boolean
    includeTLSSNI: true # optional; boolean
    path: "<string>" # optional; string
    source: "" # optional; string; enum=["", "conntrack"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.enabled` | Default false. True enables recording for the selected resource; it does not make an unavailable connection source work. |
| `spec.path` | SQLite database path on the router. Required when enabled by normal validation. The reader has an internal /var/lib/routerd/traffic-flows.db fallback, but it does not waive that enabled-path requirement. |
| `spec.source` | Only omitted/conntrack is accepted; effective label defaults to conntrack. It does not select a different collector or allow arbitrary backend names. |
| `spec.includeApplicationLayer` | Default false. With enabled true, requests DPI helper services and Linux DPI observation rules. Also includes applicationLayer availability/status from the nDPI agent in resource status; it does not guarantee libndpi is loaded or classification exists. |
| `spec.includeTLSSNI` | Default false. With enabled true, requests the same DPI helpers/observation path. Not a TLS decryption option, guaranteed SNI capture, or a separate storage redaction/filter switch; encrypted/unobserved names may remain unavailable. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/conntrackobserver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/conntrackobserver/controller.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go)
