# FirewallZone

[Resource configuration reference](../resources.md#firewallzone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Group configured interfaces under a named trust role for firewall generation. This does not create an OS interface or a separate routing/bridge domain.

## Conditions and references

- role and at least one interface are required; repeated identical interface strings within a zone are rejected. FirewallRule uses the zone metadata.name; self is the special router-local endpoint rather than an ordinary interface zone.
- Implicit policy allows mgmt toward all roles and self; trust toward self and roles other than mgmt; untrust is denied except same-role forwarding is enabled by the current shared policy. Established/related traffic and control-protocol exceptions are evaluated separately. See FirewallPolicy and FirewallRule for OS-specific ordering limits.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces:
      - <string>
    role: ("untrust" | "trust" | "mgmt")
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.role` | **Required** | string; enum=["untrust", "trust", "mgmt"] | Required untrust, trust or mgmt; no default. Selects implicit firewall behavior, not interface ownership or authentication. |
| `spec.interfaces` | **Required** | array | Required nonempty logical interface references. Bare names default to Interface; explicit Interface/name, PPPoESession/name, WireGuardInterface/name and DSLiteTunnel/name forms are accepted. References must resolve in the configuration interface/tunnel indexes and to nonempty kernel interface names for rendering. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interfaces`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
