# SAMNodeSet

[Resource configuration reference](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Declare shared SAM node identity/topology used by transport, event peers, WireGuard and MobilityPool member resolution. Local provider capture/discovery details belong in a MobilityPool self overlay, not here.

## Conditions and references

- nodes must be nonempty and nodeRef unique after trimming. Role/site are not inferred from the nodeRef. Consumers such as MobilityPool impose additional role/site requirements on the shared entries.
- Any nonempty WireGuard data requires publicKey and a nonempty allowedIPs list. endpoint and transportEndpoint remain optional in shape validation but must suit the chosen transport. samEndpoint and samEndpointFrom are mutually exclusive.
- Placement groups require role cloud and a consistent site across the group. priority, drain=true and positive maxSecondaryIPs all require a nonempty placement.group.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # Required; array
      - # object
        eventEndpoint: "<string>" # optional; string
        macAddresses: # optional; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # optional; object
          drain: true # optional; boolean
        maxSecondaryIPs: 0 # optional; integer; minimum=0
        nodeRef: "<string>" # Required; string
        placement: # optional; object
          group: "<string>" # optional; string
          priority: 0 # optional; integer; minimum=0; maximum=1000000
        role: "" # optional; string; enum=["", "onprem", "cloud"]
        routeReflector: true # optional; boolean
        samEndpoint: "<string>" # optional; string
        samEndpointFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        site: "<string>" # optional; string
        wireGuard: # optional; object
          allowedIPs: # optional; array
            - "<string>" # string
          endpoint: "<string>" # optional; string
          persistentKeepalive: 0 # optional; integer; minimum=0; maximum=65535
          publicKey: "<string>" # optional; string
          transportEndpoint: "<string>" # optional; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.nodes` | Required nonempty shared node list. No implicit local node is added. |
| `spec.nodes[].nodeRef` | Required stable node identity, unique in this NodeSet. Consumers compare it with EventGroup local identity or SAMTransportProfile.selfNodeRef; it is not a Kind/name reference. |
| `spec.nodes[].site` | Optional topology/site label, default empty; not derived from role. MobilityPool membership and placement rules may require an explicit compatible site. |
| `spec.nodes[].role` | Optional empty/onprem/cloud in NodeSet validation; no inferred default. A MobilityPool member needs its actual onprem/cloud role, and placement is cloud-only. |
| `spec.nodes[].routeReflector` | Default false. Marks a node eligible as an RR identity in enrollment topology selection; does not alone create a BGPRouter or establish BGP. |
| `spec.nodes[].eventEndpoint` | Optional absolute HTTP(S) URL with host for generated event peers. No default or status-value source. |
| `spec.nodes[].samEndpoint` | Optional IPv4 address or IPv4 CIDR carrying the node outer transport endpoint; no DNS hostname. Exclusive with samEndpointFrom. With WireGuard encryption, wireGuard.transportEndpoint takes precedence. |
| `spec.nodes[].samEndpointFrom` | Optional runtime status-value source following common resource/field rules. A missing value remains an unresolved transport source; optional does not supply an endpoint or bypass malformed-address errors. |
| `spec.nodes[].samEndpointFrom.resource` | Kind/name of a declared source resource, required when the source is used. Exclusive with literal samEndpoint. |
| `spec.nodes[].samEndpointFrom.field` | Explicit provided status field, required with resource; for example currentAddress from DHCPv4Client. |
| `spec.nodes[].samEndpointFrom.optional` | Default false. Does not bypass existence/field validation; this transport consumer still reports a missing endpoint as unresolved. |
| `spec.nodes[].macAddresses` | Optional MAC-address list, default empty. The ARP-observer intent collects these as ignored sender MACs to avoid rediscovering SAM router identities as clients. Not a MAC-change request. |
| `spec.nodes[].wireGuard` | Optional public WireGuard transport identity. Any nonempty subfield requires publicKey and allowedIPs. Private keys never belong in NodeSet topology. |
| `spec.nodes[].wireGuard.publicKey` | Required when wireGuard is configured; peer public key passed to generated WireGuardPeer. No derived default. |
| `spec.nodes[].wireGuard.endpoint` | Optional WireGuard outer UDP host:port, [IPv6]:port for IPv6; port 1–65535. Distinct from the inner transportEndpoint. |
| `spec.nodes[].wireGuard.transportEndpoint` | Optional IPv4 address/CIDR for the inner SAM tunnel endpoint when encryption=wireguard. Preferred over samEndpoint/samEndpointFrom; not the UDP socket address. |
| `spec.nodes[].wireGuard.allowedIPs` | Required nonempty IPv4/IPv6 CIDRs when WireGuard is configured; passed to generated peers. Does not automatically create ordinary routes for every prefix. |
| `spec.nodes[].wireGuard.persistentKeepalive` | Integer seconds 0–65535, default 0 disables persistent keepalive; copied to generated peers. |
| `spec.nodes[].placement` | Optional shared capture placement metadata. Only cloud role may declare a group; group members must share site/role and compatible provider identity in consumers. |
| `spec.nodes[].placement.group` | Optional placement group name, default empty means no grouped selection. Required for priority/drain/positive capacity settings. |
| `spec.nodes[].placement.priority` | Integer 0–1000000. 0/omitted is assigned 10,20,… by resolved member order, skipping explicit values; smaller priority wins eligible single-active selection. Requires group when nonzero. |
| `spec.nodes[].maintenance` | Optional declarative maintenance intent; default no drain. Shared topology metadata, not local provider credentials. |
| `spec.nodes[].maintenance.drain` | Default false. True excludes the grouped member from active capture selection and requires placement.group. |
| `spec.nodes[].maxSecondaryIPs` | Nonnegative integer, default 0. Positive requires placement.group and enables capacity-aware distributed capture for that group; 0 has no per-member capacity limit. Not a cloud quota change. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/placement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/placement.go) · [pkg/controller/mobility/capture_distribution.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/capture_distribution.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `nodeCount`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
