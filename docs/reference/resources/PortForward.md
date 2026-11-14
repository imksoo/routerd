# PortForward

[Resource configuration reference](../resources.md#portforward) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Forward a TCP/UDP port to one IPv4 endpoint with optional hairpin NAT. Linux generates DNAT plus matching forward allowances; FreeBSD uses rdr pass.

## Conditions and references

- listen and target are required. target needs exactly one address/addressFrom and a port; a literal target must be bare IPv4, not hostname or CIDR. listen may omit both address forms, matching every destination on its interface.
- Address references must exist and declare a supported provided field, but this NAT renderer reads only declared IPv4StaticAddress.address/VirtualAddress.address. optional=true may omit a rule for an unresolved supported source, not enable arbitrary dynamic sources. There is no backend health check in PortForward.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    [ hairpin:
        [ enabled: <boolean> ]
        [ interfaces:
            - <string>
        ]
        [ mode: ("" | "auto" | "manual" | "off") ]
    ]
    listen:
      [ address: <string> ]
      [ addressFrom:
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
      ]
      interface: <string>
      port: <integer>
      protocol: ("tcp" | "udp")
    target:
      [ address: <string> ]
      [ addressFrom:
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
      ]
      port: <integer>
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
| `spec.listen` | **Required** | object | Required IPv4 ingress endpoint. This creates NAT/firewall rules, not an application listener or address assignment. |
| `spec.listen.interface` | **Required** | string | Required bare declared logical interface name, not Kind/name. Must exist in the configuration interface index and resolve to an OS ifname. |
| `spec.listen.address` | *Optional* | string | Optional bare IPv4 destination address. Mutually exclusive with addressFrom; omission matches any destination address on the listen interface. |
| `spec.listen.addressFrom` | *Optional* | object | Optional common resource/field/optional reference, mutually exclusive with address. NAT/static rendering supports declared IPv4StaticAddress.address or VirtualAddress.address, not arbitrary runtime status. Generic reference validation does not guarantee renderer support. |
| `spec.listen.protocol` | **Required** | string; enum=["tcp", "udp"] | Required tcp or udp, no default. This selects forwarded traffic; it does not select the health-check protocol. |
| `spec.listen.port` | **Required** | integer; minimum=1; maximum=65535 | Required destination port 1–65535. No range/list and no default. |
| `spec.hairpin` | *Optional* | object | Optional loopback-through-public-address NAT settings. Requires a resolved listen address when active. PortForward defaults off; IngressService defaults auto. |
| `spec.hairpin.enabled` | *Optional* | boolean | Default false. With omitted mode, true selects manual on PortForward; IngressService still defaults auto. Explicit off disables hairpin even when enabled is true. |
| `spec.hairpin.mode` | *Optional* | string; enum=["", "auto", "manual", "off"] | auto, manual or off. auto detects the listen interface when listen and backend share a declared IPv4 subnet or the same private /24; it does not discover arbitrary LAN routes. manual needs enabled=true and interfaces to produce rules. |
| `spec.hairpin.interfaces` | *Optional* | array | Optional bare declared logical interface names, no duplicates; enabled/manual requires a nonempty list. auto may additionally use the listen interface. IPv4 DNAT plus masquerade/PF SNAT ensures a return through the router. |
| `spec.when` | *Optional* | object | Optional common activation condition, default unconditional. False conditions exclude the resource from effective generation; see common fields for predicates/schema limits. |
| `spec.target` | **Required** | object | Required single internal endpoint. Setting a target does not assign its IP or start its service. |
| `spec.target.address` | *Optional* | string | Bare IPv4 target, mutually exclusive with target.addressFrom; one is required. No default. |
| `spec.target.addressFrom` | *Optional* | object | Common resource/field/optional reference; same declared-field rendering limits as listen.addressFrom. Must resolve to IPv4. |
| `spec.target.port` | **Required** | integer; minimum=1; maximum=65535 | Required internal TCP/UDP port 1–65535, independent of listen.port. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `listenAddress`, `target`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)
