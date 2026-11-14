# ManagementAccess

[Resource configuration reference](../resources.md#managementaccess) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declare the interfaces checked by the pre-apply management-lockout check. This resource does not create a management listener or firewall allow rule.

## Conditions and references

- The check runs before apply and is also exposed by routerctl doctor. Fail findings block real application unless --allow-mgmt-lockout is explicitly supplied; dry-run reports findings without blocking. It inspects configuration, not live management reachability.
- Each interface must be a declared Interface. If any FirewallZone exists, each management interface must belong to a zone whose role is trust or mgmt. A named Bridge/WireGuard resource alone does not satisfy this specific check.
- When no ManagementAccess is declared, this check adds no findings. requireWebConsoleBound checks explicit wildcard listen addresses; it does not require a WebConsole to exist or prove its address belongs to a listed management interface.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ManagementAccess
  metadata:
    name: "<name>"
  spec:
    allowSourceCIDRs: # optional; array
      - "<string>" # string
    interfaces: # Required; array
      - "<string>" # string
    requireWebConsoleBound: true # optional; boolean
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.interfaces` | Required nonempty list of bare Interface names or Interface/name. Duplicates are rejected after normalization. These identify declared resources, not arbitrary OS names. |
| `spec.allowSourceCIDRs` | Optional IPv4/IPv6 CIDR list, default empty. Syntax is validated, but the current management check and firewall renderers do not consume it. It does not enforce a source-address access list. |
| `spec.requireWebConsoleBound` | Default true. An enabled WebConsole explicitly listening on 0.0.0.0 or :: produces fail; false downgrades that finding to warn. With multiple ManagementAccess resources, any omitted/true value keeps this requirement enabled. A derived listen address produces only a warning; omitted listenAddress without a source is not flagged here. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/management.go](https://github.com/imksoo/routerd/blob/main/pkg/config/management.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go) · [cmd/routerctl/doctor.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
