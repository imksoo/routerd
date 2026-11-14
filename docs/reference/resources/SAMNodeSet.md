# SAMNodeSet

[Resource configuration reference](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Declare shared SAM node identity/topology used by transport, event peers, WireGuard and MobilityPool member resolution. Local provider capture/discovery details belong in a MobilityPool self overlay, not here.

## Conditions and references

- nodes must be nonempty and nodeRef unique after trimming. Role/site are not inferred from the nodeRef. Consumers such as MobilityPool impose additional role/site requirements on the shared entries.
- Any nonempty WireGuard data requires publicKey and a nonempty allowedIPs list. endpoint and transportEndpoint remain optional in shape validation but must suit the chosen transport. samEndpoint and samEndpointFrom are mutually exclusive.
- Placement groups require role cloud and a consistent site across the group. priority, drain=true and positive maxSecondaryIPs all require a nonempty placement.group.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes:
      -
        [ eventEndpoint: <string> ]
        [ macAddresses:
            - <string>
        ]
        [ maintenance:
            [ drain: <boolean> ]
        ]
        [ maxSecondaryIPs: <integer> ]
        nodeRef: <string>
        [ placement:
            [ group: <string> ]
            [ priority: <integer> ]
        ]
        [ role: ("" | "onprem" | "cloud") ]
        [ routeReflector: <boolean> ]
        [ samEndpoint: <string> ]
        [ samEndpointFrom:
            field: <string>
            [ optional: <boolean> ]
            resource: <string>
        ]
        [ site: <string> ]
        [ wireGuard:
            [ allowedIPs:
                - <string>
            ]
            [ endpoint: <string> ]
            [ persistentKeepalive: <integer> ]
            [ publicKey: <string> ]
            [ transportEndpoint: <string> ]
        ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.nodes` | **Required** | array | Required nonempty shared node list. No implicit local node is added. |
| `spec.nodes[].nodeRef` | **Required** | string | Required stable node identity, unique in this NodeSet. Consumers compare it with EventGroup local identity or SAMTransportProfile.selfNodeRef; it is not a Kind/name reference. |
| `spec.nodes[].site` | *Optional* | string | Optional topology/site label, default empty; not derived from role. MobilityPool membership and placement rules may require an explicit compatible site. |
| `spec.nodes[].role` | *Optional* | string; enum=["", "onprem", "cloud"] | Optional empty/onprem/cloud in NodeSet validation; no inferred default. A MobilityPool member needs its actual onprem/cloud role, and placement is cloud-only. |
| `spec.nodes[].routeReflector` | *Optional* | boolean | Default false. Marks a node eligible as an RR identity in enrollment topology selection; does not alone create a BGPRouter or establish BGP. |
| `spec.nodes[].eventEndpoint` | *Optional* | string | Optional absolute HTTP(S) URL with host for generated event peers. No default or status-value source. |
| `spec.nodes[].samEndpoint` | *Optional* | string | Optional IPv4 address or IPv4 CIDR carrying the node outer transport endpoint; no DNS hostname. Exclusive with samEndpointFrom. With WireGuard encryption, wireGuard.transportEndpoint takes precedence. |
| `spec.nodes[].samEndpointFrom` | *Optional* | object | Optional runtime status-value source following common resource/field rules. A missing value remains an unresolved transport source; optional does not supply an endpoint or bypass malformed-address errors. |
| `spec.nodes[].samEndpointFrom.resource` | **Conditionally required** | string | Kind/name of a declared source resource, required when the source is used. Exclusive with literal samEndpoint. |
| `spec.nodes[].samEndpointFrom.field` | **Conditionally required** | string | Explicit provided status field, required with resource; for example currentAddress from DHCPv4Client. |
| `spec.nodes[].samEndpointFrom.optional` | *Optional* | boolean | Default false. Does not bypass existence/field validation; this transport consumer still reports a missing endpoint as unresolved. |
| `spec.nodes[].macAddresses` | *Optional* | array | Optional MAC-address list, default empty. The ARP-observer intent collects these as ignored sender MACs to avoid rediscovering SAM router identities as clients. Not a MAC-change request. |
| `spec.nodes[].wireGuard` | *Optional* | object | Optional public WireGuard transport identity. Any nonempty subfield requires publicKey and allowedIPs. Private keys never belong in NodeSet topology. |
| `spec.nodes[].wireGuard.publicKey` | *Optional* | string | Required when wireGuard is configured; peer public key passed to generated WireGuardPeer. No derived default. |
| `spec.nodes[].wireGuard.endpoint` | *Optional* | string | Optional WireGuard outer UDP host:port, [IPv6]:port for IPv6; port 1–65535. Distinct from the inner transportEndpoint. |
| `spec.nodes[].wireGuard.transportEndpoint` | *Optional* | string | Optional IPv4 address/CIDR for the inner SAM tunnel endpoint when encryption=wireguard. Preferred over samEndpoint/samEndpointFrom; not the UDP socket address. |
| `spec.nodes[].wireGuard.allowedIPs` | *Optional* | array | Required nonempty IPv4/IPv6 CIDRs when WireGuard is configured; passed to generated peers. Does not automatically create ordinary routes for every prefix. |
| `spec.nodes[].wireGuard.persistentKeepalive` | *Optional* | integer; minimum=0; maximum=65535 | Integer seconds 0–65535, default 0 disables persistent keepalive; copied to generated peers. |
| `spec.nodes[].placement` | *Optional* | object | Optional shared capture placement metadata. Only cloud role may declare a group; group members must share site/role and compatible provider identity in consumers. |
| `spec.nodes[].placement.group` | *Optional* | string | Optional placement group name, default empty means no grouped selection. Required for priority/drain/positive capacity settings. |
| `spec.nodes[].placement.priority` | *Optional* | integer; minimum=0; maximum=1000000 | Integer 0–1000000. 0/omitted is assigned 10,20,… by resolved member order, skipping explicit values; smaller priority wins eligible single-active selection. Requires group when nonzero. |
| `spec.nodes[].maintenance` | *Optional* | object | Optional declarative maintenance intent; default no drain. Shared topology metadata, not local provider credentials. |
| `spec.nodes[].maintenance.drain` | *Optional* | boolean | Default false. True excludes the grouped member from active capture selection and requires placement.group. |
| `spec.nodes[].maxSecondaryIPs` | *Optional* | integer; minimum=0 | Nonnegative integer, default 0. Positive requires placement.group and enables capacity-aware distributed capture for that group; 0 has no per-member capacity limit. Not a cloud quota change. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/placement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/placement.go) · [pkg/controller/mobility/capture_distribution.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/capture_distribution.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `nodeCount`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
