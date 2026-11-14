# DHCPv6Information

[Resource configuration reference](../resources.md#dhcpv6information) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Projects AFTR, DNS, SNTP and domain-search information from the DHCPv6-PD daemon that uses the same interface. It does not start a standalone information-request client.

## Conditions and references

- A DHCPv6PrefixDelegation resource on the exact same logical interface must reach Bound before this controller reads its daemon socket. Without such a PD resource there is no source to project. Info-request is triggered on bootstrap/transition to Bound, not as a separate lease lifecycle.
- The information controller uses the declared Router rather than the when-filtered effective view and does not evaluate dependsOn. Its Ready status reflects a daemon status read, not proof of a fresh information reply. ready_when is rejected; use supported declarations with the runtime limitations below.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata:
    name: "<name>"
  spec:
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    interface: <string>
    [ request:
        - <string>
    ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.interface` | **Required** | string | Required interface identifier matched exactly against DHCPv6PrefixDelegation.spec.interface. No default. Use the same logical interface name in both declarations. |
| `spec.request` | *Optional* | array | Requested-option declaration, default empty. The current controller does not use it: the daemon InfoRequest sends its fixed ORO for AFTR name, DNS servers, SNTP servers and domain search. |
| `spec.when` | *Optional* | object | Conditional declaration, default absent. Validated using the shared when rules, but the current information controller does not filter its own work by this field. Do not use it as an information-request stop switch. |
| `spec.dependsOn` | *Optional* | array | Dependency declaration, default empty. Reference targets/fields are validated, but this controller does not enforce these predicates before reading or requesting information. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/pdclient/client.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/client.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `source`, `aftrName`, `dnsServers`, `sntpServers`, `domainSearch`
