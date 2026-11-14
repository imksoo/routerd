# FirewallPolicy

[Resource configuration reference](../resources.md#firewallpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Set shared implicit firewall options used alongside zones and explicit rules. Multiple resources are aggregated rather than applied independently.

## Conditions and references

- No spec field is mandatory. logDeny is ORed across resources. sameRoleAccept starts true in the renderer and no false assignment exists, so setting false does not disable same-role forwarding in the current implementation.
- Linux nftables and FreeBSD PF have different implicit/explicit ordering. PF emits broad implicit passes before explicit deny rules for accepted paths; Linux emits explicit zone-pair rules before that pair’s implicit action. Do not assume an explicit deny overrides every implicit/control/state exception.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    logDeny: true # optional; boolean
    sameRoleAccept: true # optional; boolean
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.logDeny` | Default false; any true resource enables Linux implicit input/forward deny logging. It does not log explicit drops without their own log flag. Current PF generation does not read this option. |
| `spec.sameRoleAccept` | Decoded default false, but effective renderer default is always true and false has no effect. This includes untrust-to-untrust in Linux. PF skips its broad untrust-to-untrust forwarding pass. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
