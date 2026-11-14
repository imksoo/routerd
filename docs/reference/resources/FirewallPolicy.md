# FirewallPolicy

[Resource configuration reference](../resources.md#firewallpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Set shared implicit firewall options used alongside zones and explicit rules. Multiple resources are aggregated rather than applied independently.

## Conditions and references

- No spec field is mandatory. logDeny is ORed across resources. sameRoleAccept starts true in the renderer and no false assignment exists, so setting false does not disable same-role forwarding in the current implementation.
- Linux nftables and FreeBSD PF have different implicit/explicit ordering. PF emits broad implicit passes before explicit deny rules for accepted paths; Linux emits explicit zone-pair rules before that pair’s implicit action. Do not assume an explicit deny overrides every implicit/control/state exception.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    [ logDeny: <boolean> ]
    [ sameRoleAccept: <boolean> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.logDeny` | *Optional* | boolean | Default false; any true resource enables Linux implicit input/forward deny logging. It does not log explicit drops without their own log flag. Current PF generation does not read this option. |
| `spec.sameRoleAccept` | *Optional* | boolean | Decoded default false, but effective renderer default is always true and false has no effect. This includes untrust-to-untrust in Linux. PF skips its broad untrust-to-untrust forwarding pass. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
