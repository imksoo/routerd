# ClusterNetworkRoute

[Resource configuration reference](../resources.md#clusternetworkroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declare IPv4 routes to Kubernetes Pod and Service networks through explicitly configured next hops. This does not discover cluster CIDRs or nodes.

## Conditions and references

- At least one CIDR across pods and services, and at least one via entry, are required. All CIDRs must be IPv4; duplicates and overlapping prefixes are rejected across both lists.
- Each CIDR × via pair expands to an IPv4StaticRoute named <name>-pods-NN-via-NN or <name>-services-NN-via-NN (one-based indices). An explicitly declared resource with the same API version, kind and name takes precedence over the generated route.
- weight becomes a route metric, not an ECMP traffic-share weight: omitted/0 → 100; 1–999 → 1000 − weight. Larger positive weights therefore produce lower metrics; weight 0 has the same metric as 900.
- Generated routes inherit the implementation limits of IPv4StaticRoute: FreeBSD renders them, but the current Linux path does not install generated static routes. Expansion and successful validation alone do not prove forwarding works.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    [ pods:
        [ cidrs:
            - <string>
        ]
    ]
    [ services:
        [ cidrs:
            - <string>
        ]
    ]
    via:
      -
        interface: <string>
        nextHop: <string>
        [ weight: <integer> ]
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
| `spec.pods` | *Optional* | object | Optional group of Pod-network CIDRs; no implicit CIDRs. |
| `spec.pods.cidrs` | *Optional* | array | IPv4 prefixes for Pod networks. Empty by default; required if services.cidrs is empty. No duplicates or overlaps within or across groups. |
| `spec.services` | *Optional* | object | Optional group of Service-network CIDRs; no implicit CIDRs. |
| `spec.services.cidrs` | *Optional* | array | IPv4 prefixes for Service networks. Empty by default; required if pods.cidrs is empty. The same overlap restrictions apply across both groups. |
| `spec.via` | **Required** | array | Required nonempty next-hop list. Each entry applies to every CIDR. Duplicate interface + nextHop pairs are rejected even when their weights differ. |
| `spec.via[].interface` | **Required** | string | Required logical interface name declared in the same Router. It must resolve in the configuration interface index; this is not a status-value source. |
| `spec.via[].nextHop` | **Required** | string | Required bare IPv4 next-hop address, not a CIDR or resource reference. No default or dynamic discovery. |
| `spec.via[].weight` | *Optional* | integer; minimum=0; maximum=999 | Optional integer 0–999, default 0. Converted to the metric described above; it is not proportional traffic weighting. |
| `spec.when` | *Optional* | object | Optional common state predicate; omitted means unconditional. See common fields for state/all/any semantics and schema limitations. Filtering takes place before generated routes are expanded. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/cluster_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/api/cluster_routes.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/render/networkd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/networkd.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `pods`, `services`

[Example](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)
