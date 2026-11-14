# ClusterNetworkRoute

[Resource configuration reference](../resources.md#clusternetworkroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declare IPv4 routes to Kubernetes Pod and Service networks through explicitly configured next hops. This does not discover cluster CIDRs or nodes.

## Conditions and references

- At least one CIDR across pods and services, and at least one via entry, are required. All CIDRs must be IPv4; duplicates and overlapping prefixes are rejected across both lists.
- Each CIDR × via pair expands to an IPv4StaticRoute named <name>-pods-NN-via-NN or <name>-services-NN-via-NN (one-based indices). An explicitly declared resource with the same API version, kind and name takes precedence over the generated route.
- weight becomes a route metric, not an ECMP traffic-share weight: omitted/0 → 100; 1–999 → 1000 − weight. Larger positive weights therefore produce lower metrics; weight 0 has the same metric as 900.
- Generated routes inherit the implementation limits of IPv4StaticRoute: FreeBSD renders them, but the current Linux path does not install generated static routes. Expansion and successful validation alone do not prove forwarding works.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    pods: # optional; object
      cidrs: # optional; array
        - "<string>" # string
    services: # optional; object
      cidrs: # optional; array
        - "<string>" # string
    via: # Required; array
      - # object
        interface: "<string>" # Required; string
        nextHop: "<string>" # Required; string
        weight: 0 # optional; integer; minimum=0; maximum=999
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.pods` | Optional group of Pod-network CIDRs; no implicit CIDRs. |
| `spec.pods.cidrs` | IPv4 prefixes for Pod networks. Empty by default; required if services.cidrs is empty. No duplicates or overlaps within or across groups. |
| `spec.services` | Optional group of Service-network CIDRs; no implicit CIDRs. |
| `spec.services.cidrs` | IPv4 prefixes for Service networks. Empty by default; required if pods.cidrs is empty. The same overlap restrictions apply across both groups. |
| `spec.via` | Required nonempty next-hop list. Each entry applies to every CIDR. Duplicate interface + nextHop pairs are rejected even when their weights differ. |
| `spec.via[].interface` | Required logical interface name declared in the same Router. It must resolve in the configuration interface index; this is not a status-value source. |
| `spec.via[].nextHop` | Required bare IPv4 next-hop address, not a CIDR or resource reference. No default or dynamic discovery. |
| `spec.via[].weight` | Optional integer 0–999, default 0. Converted to the metric described above; it is not proportional traffic weighting. |
| `spec.when` | Optional common state predicate; omitted means unconditional. See common fields for state/all/any semantics and schema limitations. Filtering takes place before generated routes are expanded. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/cluster_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/api/cluster_routes.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/render/networkd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/networkd.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `pods`, `services`

[Example](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)
