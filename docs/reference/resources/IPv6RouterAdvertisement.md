# IPv6RouterAdvertisement

[Resource configuration reference](../resources.md#ipv6routeradvertisement) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Configure IPv6 router-advertisement options on a LAN and deprecate previously delegated prefixes.

## Conditions and references

- interface plus prefix or prefixFrom is required. The static renderer and live writer differ: current live generation emits interface/enable-ra/options but does not derive a normal dhcp-range from prefix/prefixFrom. Pair with a working DHCPv6Server/address configuration and confirm actual on-wire PIOs; the schema/Applied status alone cannot prove current-prefix advertisements.
- dnssl and dnsslFrom are mutually exclusive; prefix and prefixFrom, or rdnss and rdnssFrom, are not. All common resource references must exist and specify a provided field even when optional true. Removed prefixSource/ready_when are rejected.
- When prefixFrom points to IPv6DelegatedAddress, still-valid previous /64 prefixes are sent directly as deprecated PIOs (preferred 0, remaining valid lifetime), from a real link-local source with hop limit 255. Current-prefix normal service remains the shared dnsmasq path.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: "<name>"
  spec:
    [ configPath: <string> ]
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    [ dnssl:
        - <string>
    ]
    [ dnsslFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    interface: <string>
    [ mFlag: <boolean> ]
    [ mtu: <integer> ]
    [ oFlag: <boolean> ]
    [ pidFile: <string> ]
    [ preferredLifetime: <string> ]
    [ prefix: <string> ]
    [ prefixFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ prfPreference: ("" | "low" | "medium" | "high") ]
    [ rdnss:
        - <string>
    ]
    [ rdnssFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ validLifetime: <string> ]
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
| `spec.interface` | **Required** | string | Required logical interface. Current global reference validation does not verify this field for RA, unlike many address resources; runtime attempts alias resolution and then uses the literal as an OS name. Declare an Interface and use its name. |
| `spec.prefix` | **Conditionally required** | string | Literal IPv6 prefix, required unless prefixFrom.resource is supplied. May coexist with prefixFrom as a status fallback. Current live writer does not generate a normal prefix range from this field; do not assume declaration alone advertises it. |
| `spec.prefixFrom` | **Conditionally required** | object | Optional common resource/field/optional source, existing resource and explicit provided field required. Used for reported prefix and, when referencing IPv6DelegatedAddress, discovery of previous /64s to deprecate. Missing values do not gate normal config generation by themselves. |
| `spec.rdnss` | *Optional* | array | Optional literal IPv6 DNS-server list, default empty. Merged with rdnssFrom; emitted as untagged option6:dns-server in shared dnsmasq, not isolated per RA interface by this writer. |
| `spec.rdnssFrom` | *Optional* | array | Optional common status-value source list. No automatic source. Current RA path discards unresolved-source pending reasons and uses the values available; optional does not provide a service-readiness gate here. |
| `spec.dnssl` | *Optional* | array | Optional search-domain list, default empty; each value is a single domain without whitespace/slash/comma. Exclusive with dnsslFrom. Emitted as shared untagged option6:domain-search. |
| `spec.dnsslFrom` | *Optional* | array | Optional source list restricted to DNSZone/<name>, field: zone. Each resource must exist. Reads declared zone values before status; exclusive with dnssl. Current normal RA rendering ignores missing-source pending reasons. |
| `spec.mFlag` | *Optional* | boolean | Boolean default false. Used in direct advertisements deprecating previous delegated prefixes; current normal dnsmasq RA option generation does not map it to M. |
| `spec.oFlag` | *Optional* | boolean | Boolean default false. Same restricted direct-deprecation path as mFlag; not a current normal dnsmasq O-bit switch. |
| `spec.mtu` | *Optional* | integer; minimum=1280; maximum=65535 | Optional 1280–65535. Zero/omitted emits no explicit MTU unless a PathMTU-derived RA value takes precedence. Passed via dnsmasq ra-param mtu:<value>. |
| `spec.prfPreference` | *Optional* | string; enum=["", "low", "medium", "high"] | Empty/medium default uses normal preference; low/high emit dnsmasq router preference and affect direct old-prefix advertisements. |
| `spec.preferredLifetime` | *Optional* | string | Optional string, default empty. Current live normal writer does not use it. Direct old-prefix PIOs always use preferred lifetime 0 regardless of this setting. |
| `spec.validLifetime` | *Optional* | string | For normal live dnsmasq rendering this is the ra-param router lifetime, not the current prefix PIO lifetime. Supply decimal seconds; omitted leaves dnsmasq default. Direct old-prefix RA parses decimal seconds, defaults 1800 on empty/invalid and caps at 65535, while old PIO validity is its remaining lease lifetime. |
| `spec.configPath` | *Optional* | string | Optional decoded string, default empty; current live controller uses its shared dnsmasq ConfigPath/PIDFile options and does not consume this per-resource override. |
| `spec.pidFile` | *Optional* | string | Optional decoded string, default empty; current live controller uses its shared dnsmasq ConfigPath/PIDFile options and does not consume this per-resource override. |
| `spec.when` | *Optional* | object | Optional common state predicate, default unconditional. False filters the resource from effective config and reports Pending/WhenFalse. |
| `spec.dependsOn` | *Optional* | array | Optional common AND-list, default empty. Failed predicates report Pending/DependsOnFalse and skip direct old-prefix advertisements, but the normal shared dnsmasq line writer does not check this gate. It is not a complete normal-service suppression switch here. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/routeradvertisement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/routeradvertisement.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `prefix`, `rdnss`, `configPath`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
