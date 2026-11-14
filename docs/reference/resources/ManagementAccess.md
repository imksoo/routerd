# ManagementAccess

[Resource configuration reference](../resources.md#managementaccess) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declare the interfaces checked by the pre-apply management-lockout check. This resource does not create a management listener or firewall allow rule.

## Conditions and references

- The check runs before apply and is also exposed by routerctl doctor. Fail findings block real application unless --allow-mgmt-lockout is explicitly supplied; dry-run reports findings without blocking. It inspects configuration, not live management reachability.
- Each interface must be a declared Interface. If any FirewallZone exists, each management interface must belong to a zone whose role is trust or mgmt. A named Bridge/WireGuard resource alone does not satisfy this specific check.
- When no ManagementAccess is declared, this check adds no findings. requireWebConsoleBound checks explicit wildcard listen addresses; it does not require a WebConsole to exist or prove its address belongs to a listed management interface.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: ManagementAccess
  metadata:
    name: "<name>"
  spec:
    [ allowSourceCIDRs:
        - <string>
    ]
    interfaces:
      - <string>
    [ requireWebConsoleBound: <boolean> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.interfaces` | **Required** | array | Required nonempty list of bare Interface names or Interface/name. Duplicates are rejected after normalization. These identify declared resources, not arbitrary OS names. |
| `spec.allowSourceCIDRs` | *Optional* | array | Optional IPv4/IPv6 CIDR list, default empty. Syntax is validated, but the current management check and firewall renderers do not consume it. It does not enforce a source-address access list. |
| `spec.requireWebConsoleBound` | *Optional* | boolean | Default true. An enabled WebConsole explicitly listening on 0.0.0.0 or :: produces fail; false downgrades that finding to warn. With multiple ManagementAccess resources, any omitted/true value keeps this requirement enabled. A derived listen address produces only a warning; omitted listenAddress without a source is not flagged here. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/management.go](https://github.com/imksoo/routerd/blob/main/pkg/config/management.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go) · [cmd/routerctl/doctor.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interfaces`

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
