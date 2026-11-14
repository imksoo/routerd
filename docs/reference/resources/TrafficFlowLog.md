# TrafficFlowLog

[Resource configuration reference](../resources.md#trafficflowlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Record connection snapshots in a SQLite flow database, optionally enriching them from the DPI cache. This is not a packet capture or a log of every packet.

## Conditions and references

- The observer selects the first decodable TrafficFlowLog resource, even if disabled; a later enabled resource does not override it. Configure one resource for predictable recording.
- Recording runs after successful conntrack observation; unavailable observation prevents that round of recording. The connection query is limited to 10000 entries. Sampling occurs on observer reconciliation, normally every 30s unless runtime options change it.
- DPI enrichment reads the first enabled FirewallEventLog database when available, matching connection tuples against cached classification within one hour. Enrichment is not guaranteed by either include flag; both false also do not prevent enrichment if that cache is available. Use LogRetention signal trafficFlows to manage flow retention.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: TrafficFlowLog
  metadata:
    name: "<name>"
  spec:
    [ enabled: <boolean> ]
    [ includeApplicationLayer: <boolean> ]
    [ includeTLSSNI: <boolean> ]
    [ path: <string> ]
    [ source: ("" | "conntrack") ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.enabled` | *Optional* | boolean | Default false. True enables recording for the selected resource; it does not make an unavailable connection source work. |
| `spec.path` | **Conditionally required** | string | SQLite database path on the router. Required when enabled by normal validation. The reader has an internal /var/lib/routerd/traffic-flows.db fallback, but it does not waive that enabled-path requirement. |
| `spec.source` | *Optional* | string; enum=["", "conntrack"] | Only omitted/conntrack is accepted; effective label defaults to conntrack. It does not select a different collector or allow arbitrary backend names. |
| `spec.includeApplicationLayer` | *Optional* | boolean | Default false. With enabled true, requests DPI helper services and Linux DPI observation rules. Also includes applicationLayer availability/status from the nDPI agent in resource status; it does not guarantee libndpi is loaded or classification exists. |
| `spec.includeTLSSNI` | *Optional* | boolean | Default false. With enabled true, requests the same DPI helpers/observation path. Not a TLS decryption option, guaranteed SNI capture, or a separate storage redaction/filter switch; encrypted/unobserved names may remain unavailable. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/conntrackobserver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/conntrackobserver/controller.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `path`, `sinks`
