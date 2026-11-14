# FirewallZone

[Resource configuration reference](../resources.md#firewallzone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Group configured interfaces under a named trust role for firewall generation. This does not create an OS interface or a separate routing/bridge domain.

## Conditions and references

- role and at least one interface are required; repeated identical interface strings within a zone are rejected. FirewallRule uses the zone metadata.name; self is the special router-local endpoint rather than an ordinary interface zone.
- Implicit policy allows mgmt toward all roles and self; trust toward self and roles other than mgmt; untrust is denied except same-role forwarding is enabled by the current shared policy. Established/related traffic and control-protocol exceptions are evaluated separately. See FirewallPolicy and FirewallRule for OS-specific ordering limits.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces: # Required; array
      - "<string>" # string
    role: "untrust" # Required; string; enum=["untrust", "trust", "mgmt"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.role` | Required untrust, trust or mgmt; no default. Selects implicit firewall behavior, not interface ownership or authentication. |
| `spec.interfaces` | Required nonempty logical interface references. Bare names default to Interface; explicit Interface/name, PPPoESession/name, WireGuardInterface/name and DSLiteTunnel/name forms are accepted. References must resolve in the configuration interface/tunnel indexes and to nonempty kernel interface names for rendering. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
