# IPv6RAAddress

[Resource configuration reference](../resources.md#ipv6raaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declares host-side receipt of IPv6 router advertisements/SLAAC. This is an RA client, not an IPv6RouterAdvertisement server for the LAN.

## Conditions and references

- managed controls host RA setup. FreeBSD renders accept_rtadv/rtsold. Linux networkd adoption preserves/enables RA; when DHCPv6 is moved away from networkd, the drop-in also suppresses RA-triggered DHCPv6 startup. This declaration does not guarantee usable upstream RAs.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RAAddress
  metadata:
    name: "<name>"
  spec:
    interface: <string>
    [ managed: <boolean> ]
    [ required: <boolean> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.interface` | **Required** | string | Required logical interface name from the Router interface index, without Interface/ prefix. It must exist in the declared config. No default. |
| `spec.managed` | *Optional* | boolean | Default true. False selects observation-only RA intent instead of requesting host RA settings. It is independent of the Interface managed flag. |
| `spec.required` | *Optional* | boolean | Default false. The current observation/rendering paths do not read this field; it does not require a completed SLAAC address before apply returns. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `address`
