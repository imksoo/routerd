# TunnelInterface

[Resource configuration reference](../resources.md#tunnelinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

Create and reconcile an unencrypted IPv4 outer tunnel and an optional IPv4 inner address. metadata.name is the actual OS tunnel name.

## Conditions and references

- Each endpoint requires exactly one literal or common status source. Both are IPv4-only. Missing dynamic endpoint state pauses reconciliation even when optional true; existing forwarding is not automatically withdrawn by that missing value. No when/dependsOn fields exist here.
- Linux supports all four modes using ip link/address and FOU listeners. FreeBSD supports IPIP/gifN and GRE/greN only, rejects explicit TTL and requires an explicit inner peerAddress whenever address is set. The controller forces the link up; there is no managed/enabled opt-out.
- Configured address replaces other IPv4 addresses on the tunnel; empty removes them. Linux cleanup also removes unconfigured IPIP/GRE interfaces except built-in or otherwise retained configured devices, and unused FOU listeners; FreeBSD removal uses recorded TunnelInterface status and type checks. Changes are configuration-wide, not ownership-limited.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    [ address: <string> ]
    [ encapDport: <integer> ]
    [ encapSport: <integer> ]
    [ key: <integer> ]
    [ local: <string> ]
    [ localFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    mode: ("ipip" | "gre" | "fou" | "gue")
    [ mtu: <integer> ]
    [ pathMTU:
        [ forceFragmentIPv4: <boolean> ]
    ]
    [ peerAddress: <string> ]
    [ remote: <string> ]
    [ remoteFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    trustedUnderlay: <boolean>
    [ ttl: <integer> ]
    [ underlayInterface: <string> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.mode` | **Required** | string; enum=["ipip", "gre", "fou", "gue"] | Required ipip/gre/fou/gue, no default. Linux FOU/GUE use an IPIP link with UDP encapsulation. FreeBSD supports only ipip (gifN) and gre (greN); FOU/GUE are rejected. |
| `spec.local` | **Conditionally required** | string | Local outer bare IPv4 endpoint. Exactly one of local or localFrom is required; literal CIDR/hostname/IPv6 is rejected. No implicit interface-address selection. |
| `spec.localFrom` | **Conditionally required** | object | Optional common status source with explicit existing Kind/name and provided field, exclusive with local. Runtime reads status only; a single IPv4 address or CIDR is accepted and normalized. Missing values yield Pending/EndpointSourcePending even with optional true; no endpoint fallback. |
| `spec.remote` | **Conditionally required** | string | Remote outer bare IPv4 endpoint. Exactly one of remote or remoteFrom is required; literal hostnames, IPv6 and CIDR are rejected. |
| `spec.remoteFrom` | **Conditionally required** | object | Optional common status source, exclusive with remote. Same required field, IPv4/CIDR normalization and ignored optional flag as localFrom. Missing status pauses reconciliation without automatically tearing down an already existing tunnel. |
| `spec.address` | **Conditionally required** | string | Optional inner IPv4 host/CIDR. Controller removes other IPv4 addresses from this tunnel; omitted/empty therefore removes existing IPv4 addresses. No automatic inner address. FreeBSD requires peerAddress when this is set. |
| `spec.peerAddress` | **Conditionally required** | string | FreeBSD-only explicit inner bare IPv4 peer paired with address. Both must be supplied together; no derivation from CIDR. Rejected on Linux. |
| `spec.underlayInterface` | *Optional* | string | Optional declared resource name for MTU estimation (Interface, WireGuardInterface or nested TunnelInterface). Not required or globally checked for existence here; unknown uses the 1500 fallback. Current tunnel commands do not bind to this device: host routing selects outer egress. Avoid recursive references. |
| `spec.mtu` | *Optional* | integer; minimum=576; maximum=9216 | Optional explicit 576–9216, used unchanged. Omitted/0 derives configured underlay MTU (fallback 1500) minus overhead: ipip 20, gre 24 (+4 if keyed), fou 28, gue 32. Thus fallback defaults are 1480/1476/1472/1468 (keyed GRE 1472). Not live PMTU discovery. |
| `spec.ttl` | *Optional* | integer; minimum=1; maximum=255 | Linux outer TTL 1–255, omitted/0 becomes 64. FreeBSD rejects nonzero explicit TTL; its ifconfig tunnel path does not apply the internal reported default 64. |
| `spec.key` | *Optional* | integer; minimum=0; maximum=4294967295 | GRE-only integer 0–4294967295; default 0 means no key. Nonzero with other modes is rejected. FreeBSD uses ifconfig grekey and Linux key/nokey. It is an identifier, not an encryption/authentication secret. |
| `spec.encapSport` | **Conditionally required** | integer; minimum=1; maximum=65535 | Required UDP source/local receive port 1–65535 for fou/gue; no default. Controller also creates a local FOU/GUE listener on this port. Nonzero rejected for ipip/gre. |
| `spec.encapDport` | **Conditionally required** | integer; minimum=1; maximum=65535 | Required remote UDP destination port 1–65535 for fou/gue; must match peer receive port. No default; nonzero rejected in ipip/gre. |
| `spec.trustedUnderlay` | **Required** | boolean | Required true; false/omitted is rejected. IPIP/GRE/FOU/GUE do not provide encryption or peer authentication, and this acknowledgement does not add either. |
| `spec.pathMTU` | *Optional* | object | Optional forwarded-path policy options, default no forced IPv4 fragmentation. Independent of explicit tunnel MTU. |
| `spec.pathMTU.forceFragmentIPv4` | *Optional* | boolean | Default false. Linux path-MTU policy can clear DF for oversized IPv4 on matching derived forwarded paths; not a per-tunnel ignore-df option or an IPv6 feature. FreeBSD skips this nftables controller without an equivalent action in this path. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/tunnel.go) · [internal/ipiptunnel/ipip.go](https://github.com/imksoo/routerd/blob/main/internal/ipiptunnel/ipip.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/render/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/render/mtu.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/mtu.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `mode`, `local`, `remote`, `peerAddress`, `observedAddress`, `observedPeerAddress`, `underlayInterface`, `underlayMTU`, `tunnelOverhead`, `mtu`, `ttl`, `encapSport`, `encapDport`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
