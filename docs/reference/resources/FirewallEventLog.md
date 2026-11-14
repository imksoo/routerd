# FirewallEventLog

[Resource configuration reference](../resources.md#firewalleventlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Enable the firewall packet-log collector and configure its database, Linux NFLOG group and renderer logging options. Several accepted filter/archive fields currently have no runtime consumer.

## Conditions and references

- enabled defaults false. On Linux the collector uses NFLOG; FreeBSD uses pflog0. This does not turn every accept/deny rule into a logged rule: FirewallRule.log and FirewallPolicy.logDeny also determine which packets are logged.
- Use one enabled resource: renderer options come from the first enabled resource, while service generation uses a shared routerd-firewall-logger service name. Multiple independent collector instances are not provided by multiple resource names.
- fromZones/toZones must reference existing FirewallZone names or self; rules accepts FirewallRule/name or a bare rule name; sinks accepts LogSink/name or a bare sink name; retention accepts LogRetention/name or a bare retention name. These references are validated even though these fields currently do not filter, export or select retention at runtime.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    [ enabled: <boolean> ]
    [ events:
        - ("deny" | "allow" | "rateLimit" | "connLimit")
    ]
    [ fromZones:
        - <string>
    ]
    [ log:
        [ acceptSampleRate: <integer> ]
        [ copyRange: <integer> ]
        [ dropSampleRate: <integer> ]
        [ reject: <boolean> ]
    ]
    [ nflogGroup: <integer> ]
    [ path: <string> ]
    [ retention: <string> ]
    [ rules:
        - <string>
    ]
    [ sampleRate: <integer> ]
    [ sinks:
        - <string>
    ]
    [ toZones:
        - <string>
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.enabled` | *Optional* | boolean | Default false; true requests the collector service and enables grouped logging options. Also requests the DPI classifier helper in the current service renderer. |
| `spec.path` | **Conditionally required** | string | Optional SQLite database path. Default /var/lib/routerd/firewall-logs.db on Linux and /var/db/routerd/firewall-logs.db on FreeBSD. This is a database, not a JSON-lines output file. |
| `spec.events` | *Optional* | array | Accepted values deny, allow, rateLimit, connLimit; default empty. Currently validated but not used to filter collector input or generated firewall logging. |
| `spec.fromZones` | *Optional* | array | Default empty. Source-zone references validated against FirewallZone names or self, but not used as a logging filter at runtime. |
| `spec.fromZones[]` | **Conditionally required** | string | Existing FirewallZone metadata.name or the special self zone; not Kind/name. No runtime filtering effect currently. |
| `spec.toZones` | *Optional* | array | Default empty. Destination-zone references validated against FirewallZone names or self, but not used as a logging filter at runtime. |
| `spec.rules` | *Optional* | array | Default empty. Existing FirewallRule references (bare name or FirewallRule/name) are required when supplied; currently not used to select logged rules. Set FirewallRule.log on the actual rule. |
| `spec.sampleRate` | *Optional* | integer; minimum=0 | Nonnegative integer, default 0; accepted but unused by the collector and renderer. Use log.acceptSampleRate/log.dropSampleRate for the supported renderer controls. |
| `spec.sinks` | *Optional* | array | Default empty. Existing LogSink references are validated but the collector does not export to them; its output is the path database. |
| `spec.retention` | *Optional* | string | Optional existing LogRetention reference, default empty; currently not read to select a retention policy. Configure LogRetention with the firewallEvents signal to perform retention on this database. |
| `spec.nflogGroup` | *Optional* | integer; minimum=0; maximum=65535 | Integer 0–65535; omitted or 0 selects group 1 on Linux for both nftables logging and the collector. FreeBSD uses pflog0 and ignores this group. |
| `spec.log` | *Optional* | object | Optional renderer logging options; omitted fields decode to zero/false. These options do not implement the unused top-level event/zone/rule filters. |
| `spec.log.acceptSampleRate` | *Optional* | integer; minimum=0 | Default 0 disables extra implicit-accept logging. On nftables, positive N logs approximately 1/N of eligible packets (1 logs all). PF uses a positive value only as an enable switch, not a 1/N sampler. Explicit FirewallRule.log is separate. |
| `spec.log.dropSampleRate` | *Optional* | integer; minimum=0 | Default 0; nftables rates 0/1 log all already-enabled implicit deny logs, positive N>1 samples approximately 1/N. This does not itself enable FirewallPolicy.logDeny. PF does not use this sampling value. |
| `spec.log.copyRange` | *Optional* | integer; minimum=0 | Nonnegative NFLOG snaplen in bytes; default 0 omits the nftables snaplen argument. Positive values cap copied packet bytes. Not applied by the PF renderer. |
| `spec.log.reject` | *Optional* | boolean | Default false; decoded but not read by the current renderer or collector. It does not change a rule verdict or turn on reject logging. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/render/firewall_logger.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_logger.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `path`, `sinks`
