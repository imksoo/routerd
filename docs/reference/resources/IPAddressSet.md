# IPAddressSet

[Resource configuration reference](../resources.md#ipaddressset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Share literal and DNS-resolved IP addresses among supported nftables NAT, routing and firewall rules. It does not create routes or start a DNS service.

## Conditions and references

- At least one address or name is required; both lists may coexist. Addresses are individual IPv4/IPv6 literals, not CIDRs. Duplicates in each configured list are rejected; resolved results are combined and deduplicated.
- Consumers reference a bare name or IPAddressSet/name. Supported nftables consumers include LocalServiceRedirect, NAT44Rule/FlowDNATPinhole, EgressRoutePolicy and FirewallRule/FlowPinhole. Declaring an unused set only reports Observed/referenced:false and does not resolve names or create a kernel set.
- The consumer renderer must create the target nftables set before refresh; missing sets yield Pending/SetMissing. Name resolution uses A and AAAA queries through /etc/resolv.conf, not DNSUpstream resource references. Partial DNS failure is reported while usable remaining addresses can still be applied. Total resolution failure yields Pending/ResolveFailed and does not replace the existing set in that round.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: "<name>"
  spec:
    [ addresses:
        - <string>
    ]
    [ names:
        - <string>
    ]
    [ refreshInterval: <string> ]
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
| `spec.addresses` | **Conditionally required** | array | Optional literal IPv4/IPv6 list, default empty; required when names is empty. IPv4-mapped IPv6 is normalized to IPv4. No subnet expansion. |
| `spec.names` | **Conditionally required** | array | Optional DNS names, default empty; required when addresses is empty. Blank names and duplicates after trimming are rejected. A/AAAA results are merged with literals; no wildcard-name expansion. |
| `spec.refreshInterval` | *Optional* | string | Optional Go duration. Default schedule is half the smallest positive DNS TTL, with a 1m floor; without a positive TTL, 5m is assumed, giving 2m30s. A positive configured interval only shortens that schedule and can be below 1m. Zero/negative parses but has no shortening effect. Actual refresh occurs on controller reconciliation. |
| `spec.when` | *Optional* | object | Optional common activation predicate; default unconditional. Consult common fields for state/all/any and consumer activation behavior. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `updatedAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
