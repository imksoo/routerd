# BFD

[Resource configuration reference](../resources.md#bfd) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Configure FRR bfdd sessions through vtysh and observe their status for BGPPeer failure handling. This is a separate backend from GoBGP; BFD Down can trigger a BGP peer reset.

## Conditions and references

- Every declared BFD must be referenced by at least one BGPPeer.bfd. The BFD peer must match that BGPPeer name or one of its configured peer addresses. BGPDynamicPeer has no bfd field.
- Supported operating systems are Linux and FreeBSD with FRR bfdd/vtysh available. No interface means a multihop session using the referenced BGPRouter.routerID as local address; a direct IP peer without an interface requires exactly one BGPRouter. An interface selects a single-hop session.
- BFD detects its own forwarding/session failure; its Up status does not prove every routed destination is reachable. The GoBGP controller acts on observed Down after a previously Up BFD or established BGP session, rather than treating every unknown/startup state as Down.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    detectMultiplier: 1 # optional; integer; minimum=1; maximum=50
    interface: "<string>" # optional; string
    minRx: "<string>" # optional; string
    minTx: "<string>" # optional; string
    peer: "<string>" # Required; string
    profile: "" # optional; string; enum=["", "fast", "normal", "slow"]
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
| `spec.peer` | Required bare IP address or BGPPeer/name reference. A BGPPeer reference expands to all its peer addresses and takes the local address from its routerRef routerID. No hostname form for a direct peer. |
| `spec.interface` | Optional single-hop interface. Prefer Interface/name so the controller resolves the declared Interface.ifname; a plain name is validated as a declared logical interface but is passed literally to FRR. Omitted uses multihop. |
| `spec.profile` | fast/omitted: minRx=minTx=300ms, multiplier=3; normal: 500ms/500ms/3; slow: 1000ms/1000ms/5. Explicit individual settings override these defaults. |
| `spec.minRx` | Optional Go duration, 50ms–60000ms, overriding profile receive interval. Converted to integer milliseconds; omitted uses profile. This is a BFD protocol timer, not a router reboot deadline. |
| `spec.minTx` | Optional Go duration, 50ms–60000ms, overriding profile transmit interval; emitted as integer milliseconds. |
| `spec.detectMultiplier` | Optional integer 1–50. Decoded 0/omission selects profile default (3, or 5 for slow). Multiplies the negotiated detection interval; not a count of routerd reconcile attempts. |
| `spec.when` | Optional common activation predicate; omitted is unconditional. See common fields for state/all/any, reference validation and schema limitations. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `peer`

[Example](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
