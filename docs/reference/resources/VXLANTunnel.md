# VXLANTunnel

[Resource configuration reference](../resources.md#vxlantunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Manage a Linux VXLAN device with static remote flood destinations and optional bridge attachment. No multicast discovery or dynamic MAC learning is configured.

## Conditions and references

- The operational controller returns without applying on non-Linux systems. Configure a Linux target for this resource; do not assume schema acceptance implies FreeBSD operation.
- when is fail-closed: false, unresolved, stale or pre-boot state tears down the device (down then delete), rather than only skipping updates. Freshness defaults to 30s per referenced state, overridden by maxAge. Unknown-accepting predicates are rejected by validation.
- tcpMSSClamp requires bridge plus explicit mtu >=1280. Rendering also requires that bridge to be an actual Bridge resource, with resolvable member interfaces; global reference validation alone only checks the shared interface index.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    bridge: "<string>" # optional; string
    ifname: "<string>" # optional; string
    localAddress: "<string>" # Required; string
    mtu: 0 # optional; integer; minimum=0
    outerDF: "" # optional; string; enum=["", "inherit", "set", "unset"]
    peers: # optional; array
      - "<string>" # string
    tcpMSSClamp: true # optional; boolean
    udpPort: 1 # optional; integer; minimum=1; maximum=65535
    underlayInterface: "<string>" # Required; string
    vni: 1 # Required; integer; minimum=1; maximum=16777215
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
| `spec.ifname` | OS VXLAN name; defaults to metadata.name. Must be <=15 characters without whitespace or slash. |
| `spec.vni` | Required VXLAN network identifier, integer 1–16777215. Must match the intended remote layer-2 segment. |
| `spec.localAddress` | Required local outer IP address; no dynamic status source. Validator accepts an IP address but does not verify local assignment or peer-family consistency. |
| `spec.peers` | Optional list of remote outer IP addresses, default empty. Reconciled as permanent self flood FDB entries for 00:00:00:00:00:00; undesired flood entries are removed. Empty provides no static remote flood destinations. |
| `spec.underlayInterface` | Required declared logical interface in the same Router; resolved to its OS device name. No status-value source or auto-selection. |
| `spec.udpPort` | Outer UDP destination port, default 4789 when omitted/0; explicit range 1–65535. |
| `spec.mtu` | Optional inner-device MTU 576–9216; omitted/0 leaves OS/current MTU. tcpMSSClamp requires an explicit value >=1280 even though schema shows minimum 0. |
| `spec.bridge` | Optional declared bridge name, resolved to its OS name and attached as the VXLAN master. Default detached. Required for tcpMSSClamp; use a Bridge resource rather than another interface kind. |
| `spec.outerDF` | Outer IPv4 DF policy: inherit (default), set or unset, passed to ip link VXLAN df. Does not set an inner address or route. |
| `spec.when` | Optional common state/all/any predicate with additional fail-closed freshness rules above. Omitted means unconditional; conditions that accept unknown cannot enable this tunnel. |
| `spec.tcpMSSClamp` | Default false. True creates Linux bridge-family IPv4/IPv6 TCP SYN MSS rules between this VXLAN and Bridge member ports. Uses the minimum positive VXLAN/bridge/member MTU, minus 40 for IPv4 or 60 for IPv6; does not clamp arbitrary routed traffic. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/vxlan_tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/vxlan_tunnel.go) · [pkg/vxlan/vxlan.go](https://github.com/imksoo/routerd/blob/main/pkg/vxlan/vxlan.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `ifname`, `vni`
