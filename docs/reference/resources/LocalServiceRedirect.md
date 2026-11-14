# LocalServiceRedirect

[Resource configuration reference](../resources.md#localserviceredirect) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Redirect matching traffic received on a declared interface to a service port on the router itself, using nftables prerouting NAT.

## Conditions and references

- interface and a nonempty rules list are required. Each rule requires protocols, destinationSetRef and both ports. These are AND conditions per rule; selected protocol alternatives are OR.
- Matching uses the received interface, destination address set and destination port. It affects incoming packets, not locally originated OUTPUT traffic. The target is local redirect, not forwarding to a remote backend; use PortForward/IngressService for other use cases.
- Provide the local listening service and its firewall access separately. The redirect does not start DNSResolver or another daemon and does not add a universal firewall allow rule. IPv4/IPv6 rules are emitted according to the referenced set families; DNS-backed sets are refreshed by IPAddressSet. This implementation renders nftables rules, not FreeBSD PF redirects.
- Rules are sorted by resource ID and then rule name, not YAML list order. An omitted name becomes its zero-based decimal list index; lexical sorting can put 10 before 2. Use explicit ordered names when rules overlap.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: "<name>"
  spec:
    interface: <string>
    rules:
      -
        destinationPort: <integer>
        destinationSetRef: <string>
        [ name: <string> ]
        protocols:
          - <string>
        redirectPort: <integer>
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
| `spec.interface` | **Required** | string | Required logical interface declared in the same Router; resolves to an OS input-interface name. No default or status source. |
| `spec.rules` | **Required** | array | Required nonempty redirect list. Rules do not inherit missing protocol, address set or ports from another rule. |
| `spec.rules[].name` | *Optional* | string | Optional ordering/comment label, default decimal zero-based index. It is not a resource reference; duplicates are not rejected here. |
| `spec.rules[].protocols` | **Required** | array | Required nonempty subset of tcp/udp; duplicates and other protocols are rejected. No implicit both-protocol default. |
| `spec.rules[].destinationSetRef` | **Required** | string | Required bare IPAddressSet name or IPAddressSet/name, declared in the same Router. No field selector or automatic inline address list. |
| `spec.rules[].destinationPort` | **Required** | integer; minimum=1; maximum=65535 | Required original destination port 1–65535; no range syntax or default. |
| `spec.rules[].redirectPort` | **Required** | integer; minimum=1; maximum=65535 | Required local service port 1–65535. Must have a suitable listener reachable after redirect; YAML validation does not check that listener. |
| `spec.when` | *Optional* | object | Optional common activation predicate, default unconditional. See common fields for state/all/any and reference constraints. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
