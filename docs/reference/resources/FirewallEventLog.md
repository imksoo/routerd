# FirewallEventLog

[Resource configuration reference](../resources.md#firewalleventlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Enable the firewall packet-log collector and configure its database, Linux NFLOG group and renderer logging options. Several accepted filter/archive fields currently have no runtime consumer.

## Conditions and references

- enabled defaults false. On Linux the collector uses NFLOG; FreeBSD uses pflog0. This does not turn every accept/deny rule into a logged rule: FirewallRule.log and FirewallPolicy.logDeny also determine which packets are logged.
- Use one enabled resource: renderer options come from the first enabled resource, while service generation uses a shared routerd-firewall-logger service name. Multiple independent collector instances are not provided by multiple resource names.
- fromZones/toZones must reference existing FirewallZone names or self; rules accepts FirewallRule/name or a bare rule name; sinks accepts LogSink/name or a bare sink name; retention accepts LogRetention/name or a bare retention name. These references are validated even though these fields currently do not filter, export or select retention at runtime.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # optional; boolean
    events: # optional; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # optional; array
      - "<string>" # string
    log: # optional; object
      acceptSampleRate: 0 # optional; integer; minimum=0
      copyRange: 0 # optional; integer; minimum=0
      dropSampleRate: 0 # optional; integer; minimum=0
      reject: true # optional; boolean
    nflogGroup: 0 # optional; integer; minimum=0; maximum=65535
    path: "<string>" # optional; string
    retention: "<string>" # optional; string
    rules: # optional; array
      - "<string>" # string
    sampleRate: 0 # optional; integer; minimum=0
    sinks: # optional; array
      - "<string>" # string
    toZones: # optional; array
      - "<string>" # string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.enabled` | Default false; true requests the collector service and enables grouped logging options. Also requests the DPI classifier helper in the current service renderer. |
| `spec.path` | Optional SQLite database path. Default /var/lib/routerd/firewall-logs.db on Linux and /var/db/routerd/firewall-logs.db on FreeBSD. This is a database, not a JSON-lines output file. |
| `spec.events` | Accepted values deny, allow, rateLimit, connLimit; default empty. Currently validated but not used to filter collector input or generated firewall logging. |
| `spec.fromZones` | Default empty. Source-zone references validated against FirewallZone names or self, but not used as a logging filter at runtime. |
| `spec.fromZones[]` | Existing FirewallZone metadata.name or the special self zone; not Kind/name. No runtime filtering effect currently. |
| `spec.toZones` | Default empty. Destination-zone references validated against FirewallZone names or self, but not used as a logging filter at runtime. |
| `spec.rules` | Default empty. Existing FirewallRule references (bare name or FirewallRule/name) are required when supplied; currently not used to select logged rules. Set FirewallRule.log on the actual rule. |
| `spec.sampleRate` | Nonnegative integer, default 0; accepted but unused by the collector and renderer. Use log.acceptSampleRate/log.dropSampleRate for the supported renderer controls. |
| `spec.sinks` | Default empty. Existing LogSink references are validated but the collector does not export to them; its output is the path database. |
| `spec.retention` | Optional existing LogRetention reference, default empty; currently not read to select a retention policy. Configure LogRetention with the firewallEvents signal to perform retention on this database. |
| `spec.nflogGroup` | Integer 0–65535; omitted or 0 selects group 1 on Linux for both nftables logging and the collector. FreeBSD uses pflog0 and ignores this group. |
| `spec.log` | Optional renderer logging options; omitted fields decode to zero/false. These options do not implement the unused top-level event/zone/rule filters. |
| `spec.log.acceptSampleRate` | Default 0 disables extra implicit-accept logging. On nftables, positive N logs approximately 1/N of eligible packets (1 logs all). PF uses a positive value only as an enable switch, not a 1/N sampler. Explicit FirewallRule.log is separate. |
| `spec.log.dropSampleRate` | Default 0; nftables rates 0/1 log all already-enabled implicit deny logs, positive N>1 samples approximately 1/N. This does not itself enable FirewallPolicy.logDeny. PF does not use this sampling value. |
| `spec.log.copyRange` | Nonnegative NFLOG snaplen in bytes; default 0 omits the nftables snaplen argument. Positive values cap copied packet bytes. Not applied by the PF renderer. |
| `spec.log.reject` | Default false; decoded but not read by the current renderer or collector. It does not change a rule verdict or turn on reject logging. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/render/firewall_logger.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_logger.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `path`, `sinks`
