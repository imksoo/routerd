# ClientPolicy

[Resource configuration reference](../resources.md#clientpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Apply guest-style local-service and routed-destination restrictions to selected or non-exempt clients. Linux identifies clients by configured MACs; FreeBSD PF requires explicit IP identities.

## Conditions and references

- mode must be include or exclude. include restricts the selected set; exclude restricts clients outside the selected trusted set. Linux include with no selected MACs produces no restriction; exclude with no selected MACs matches all clients on the selected interfaces.
- Linux classification only contributes match.macs: guest/isolated classes enter include sets, trusted classes enter exclude sets. OUI/hostname/fingerprint selectors are validated but not used by the firewall renderer. ipv4Reservation/ipv6Addresses do not become Linux match identities.
- FreeBSD rejects top-level MACs and every match selector. Each class instead needs ipv4Reservation and/or explicit ipv6Addresses. PF uses the reservation’s ipAddress, not its MAC. Interfaces must be explicit for PF; its renderer does not inherit trust-zone interfaces when omitted.
- These are routed/input firewall restrictions, not L2 switch-port isolation. Existing-state/control/service allowances may run earlier. PF’s implicit trust/mgmt-to-self pass precedes its client self restrictions, and PF’s client local-service restrictions currently use IPv4 identities only. Do not treat the policy as uniform enforcement across both OSes or all address families.
- Linux guestEgressAllow is evaluated before discovery/deny blocks, so it can override them; remaining traffic continues into zone policy. PF IPv4 allow precedes deny, but PF IPv6 deny rules are emitted earlier globally and cannot be overridden by its later IPv6 allow rules.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    [ classification:
        -
          [ ipv4Reservation: <string> ]
          [ ipv6Addresses:
              - <string>
          ]
          match:
            [ dhcpFingerprints:
                - <string>
            ]
            [ hostnamePatterns:
                - <string>
            ]
            [ macs:
                - <string>
            ]
            [ ouiPrefixes:
                - <string>
            ]
          mode: ("trusted" | "guest" | "isolated")
          [ name: <string> ]
    ]
    [ guestEgressAllow:
        - <string>
    ]
    [ guestEgressDeny:
        - <string>
    ]
    [ guestServices:
        - <string>
    ]
    [ interfaces:
        - <string>
    ]
    [ isolation:
        [ lanInternet: ("" | "allow" | "deny") ]
        [ lanLAN: ("" | "allow" | "deny") ]
        [ lanMgmt: ("" | "allow" | "deny") ]
        [ mDNSBroadcast: ("" | "allow" | "deny") ]
    ]
    [ macs:
        - <string>
    ]
    mode: ("include" | "exclude")
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.mode` | **Required** | string; enum=["include", "exclude"] | Required include or exclude, no default. Determines whether the configured identity set is restricted or exempt. |
| `spec.interfaces` | *Optional* | array | Optional Interface/name or bare logical-interface names; references must exist and may not repeat. Linux omitted list uses all trust-zone interfaces and errors if none exist. PF omitted list produces no applicable interface restrictions. |
| `spec.macs` | *Optional* | array | Optional Linux static identity list, default empty. Normalized MAC duplicates across this list and classification.match.macs are rejected. Forbidden on FreeBSD. |
| `spec.classification` | *Optional* | array | Optional classes, default empty. Linux requires at least one selector per class, but only MAC selectors enforce firewall membership. FreeBSD requires stable IP identities and forbids selectors. |
| `spec.classification[].name` | *Optional* | string | Optional descriptive class name, default empty. Does not change matching precedence or create a resource identity. |
| `spec.classification[].mode` | **Conditionally required** | string; enum=["trusted", "guest", "isolated"] | Required trusted, guest or isolated. guest and isolated are equivalent for membership in an include policy; trusted contributes to an exclude policy’s exemption set. |
| `spec.classification[].match` | **Conditionally required** | object | Selector object; Linux needs at least one nonempty selector list. FreeBSD requires it empty even when stable IP identities are supplied. Different selectors are not dynamically resolved into client MACs by the current firewall. |
| `spec.classification[].match.macs` | *Optional* | array | Optional exact MAC list; effective Linux classifier. MACs must parse and cannot repeat across classes or top-level macs. |
| `spec.classification[].match.ouiPrefixes` | *Optional* | array | Accepted three hex octets such as 18:ec:e7; duplicates within a class are rejected. Currently no firewall enforcement from this selector. FreeBSD rejects it. |
| `spec.classification[].match.hostnamePatterns` | *Optional* | array | Accepted nonblank Go path.Match glob patterns, not regexes. Syntax is validated but the current firewall does not resolve them into identities; FreeBSD rejects them. |
| `spec.classification[].match.dhcpFingerprints` | *Optional* | array | Accepted nonblank, whitespace-free tokens with no within-class duplicates. Currently not used for firewall membership; FreeBSD rejects them. |
| `spec.classification[].ipv4Reservation` | *Optional* | string | Optional existing DHCPv4Reservation bare metadata.name; Kind/name is rejected. PF takes its nonempty ipAddress. Linux validates the reference but does not use its address for client matching. |
| `spec.classification[].ipv6Addresses` | *Optional* | array | Optional bare IPv6 addresses for PF, no CIDRs or IPv4-mapped forms. Normalized duplicates across all classes are rejected. Never inferred from MAC or IPv4 reservations; not used by Linux client matching. |
| `spec.isolation` | *Optional* | object | Optional allow/deny hints. Only the Linux renderer reads these four fields; PF uses explicit guest egress lists/defaults instead. This is not independently applied per class. |
| `spec.isolation.lanInternet` | *Optional* | string; enum=["", "allow", "deny"] | Empty by default. Linux deny with no explicit guestEgressDeny replaces the default denied CIDRs with 0.0.0.0/0 and ::/0. allow/empty does not add an unconditional Internet accept. PF ignores this field. |
| `spec.isolation.lanLAN` | *Optional* | string; enum=["", "allow", "deny"] | Empty by default. Linux removes the default private-prefix deny list only when both lanLAN and lanMgmt are allow; this does not distinguish individual LAN networks. Explicit guestEgressDeny overrides the default. PF ignores it. |
| `spec.isolation.lanMgmt` | *Optional* | string; enum=["", "allow", "deny"] | Empty by default; same combined-default rule as lanLAN. It does not independently resolve management-zone destinations. PF ignores it. |
| `spec.isolation.mDNSBroadcast` | *Optional* | string; enum=["", "allow", "deny"] | Empty by default. Linux deny adds forwarded mDNS/SSDP multicast and UDP 137/138 drop rules after guestEgressAllow. It does not control L2 bridging or independently block local services. PF ignores it. |
| `spec.guestServices` | *Optional* | array | Allowed local-service names: dns, dhcp, ntp, mdns, ssdp. Omitted or empty defaults to dhcp/dns/ntp; an empty list cannot disable all exceptions. Services open firewall access but do not start their daemons. |
| `spec.guestEgressDeny` | *Optional* | array | Optional IPv4/IPv6 CIDRs. Omitted/empty defaults to 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16 and fc00::/7, subject to the Linux isolation adjustments above. Explicit values replace, not append to, defaults. PF always uses the private-prefix defaults when empty. |
| `spec.guestEgressAllow` | *Optional* | array | Optional IPv4/IPv6 CIDR exceptions, default empty. Linux and PF IPv4 evaluate them before deny; PF IPv6 deny precedes the allow rules. These are routing destination prefixes, not resource references. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/webconsole/clients.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/clients.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`

[Example](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
