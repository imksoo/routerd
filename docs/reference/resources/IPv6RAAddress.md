# IPv6RAAddress

[Resource configuration reference](../resources.md#ipv6raaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declares host-side receipt of IPv6 router advertisements/SLAAC. This is an RA client, not an IPv6RouterAdvertisement server for the LAN.

## Conditions and references

- managed controls host RA setup. FreeBSD renders accept_rtadv/rtsold. Linux networkd adoption preserves/enables RA; when DHCPv6 is moved away from networkd, the drop-in also suppresses RA-triggered DHCPv6 startup. This declaration does not guarantee usable upstream RAs.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RAAddress
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # Required; string
    managed: true # optional; boolean
    required: true # optional; boolean
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.interface` | Required logical interface name from the Router interface index, without Interface/ prefix. It must exist in the declared config. No default. |
| `spec.managed` | Default true. False selects observation-only RA intent instead of requesting host RA settings. It is independent of the Interface managed flag. |
| `spec.required` | Default false. The current observation/rendering paths do not read this field; it does not require a completed SLAAC address before apply returns. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)
