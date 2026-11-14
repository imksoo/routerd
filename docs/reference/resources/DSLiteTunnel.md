# DSLiteTunnel

[Resource configuration reference](../resources.md#dslitetunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Encapsulate IPv4 in IPv6 toward an AFTR. Tunnel setup, an IPv4 default route, NAT and reachability checks are separate concerns.

## Conditions and references

- interface and at least one of aftrFrom/aftrFQDN/aftrIPv6/remoteAddress are required even when disabled. AFTR sources may coexist; runtime tries them in that order until one resolves.
- localAddress is the outer IPv6 source, but localAddressFrom currently selects the INNER IPv4 source. They are not interchangeable. The inner source defaults 192.0.0.2 and inner remote is 192.0.0.1. No IPv4-from reference means the default inner source.
- Missing AFTR/local/dependency values produce Pending. Up records configured tunnel/link state, not AFTR reachability or working Internet access. A false activation condition or disabled resource removes an unused tunnel; dependency failure alone skips reconciliation rather than promising immediate teardown.
- Linux uses ip6tnl mode ipip6. FreeBSD uses a gif device; a requested non-gif name maps deterministically to gif100–gif999 and the actual name is published in status. Removed aftrSource and ready_when are rejected; use aftrFrom and dependsOn.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    [ aftrAddressOrdinal: <integer> ]
    [ aftrAddressSelection: ("ordinal" | "ordinalModulo") ]
    [ aftrDNSServers:
        - <string>
    ]
    [ aftrFQDN: <string> ]
    [ aftrFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ aftrIPv6: <string> ]
    [ defaultRoute: <boolean> ]
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    [ enabled: <boolean> ]
    [ encapsulationLimit: <string> ]
    interface: <string>
    [ localAddress: <string> ]
    [ localAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ localAddressSource: ("interface" | "static" | "delegatedAddress") ]
    [ localAddressSuffix: <string> ]
    [ localDelegatedAddress: <string> ]
    [ mtu: <integer> ]
    [ remoteAddress: <string> ]
    [ routeMetric: <integer> ]
    [ tunnelName: <string> ]
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
| `spec.interface` | **Required** | string | Required declared logical underlay interface; normal reference validation requires it to exist. |
| `spec.tunnelName` | *Optional* | string | Optional desired device name, default metadata.name. FreeBSD may publish a different gif name as described above. |
| `spec.enabled` | *Optional* | boolean | Default true. False removes an unused configured tunnel and reports Disabled; validation still applies. |
| `spec.aftrFQDN` | **Conditionally required** | string | Optional AFTR hostname; no whitespace or slash. Tried after aftrFrom, before literal alternatives. |
| `spec.aftrIPv6` | **Conditionally required** | string | Optional bare AFTR IPv6 address, tried after aftrFQDN. No default. |
| `spec.remoteAddress` | **Conditionally required** | string | Optional IPv6 AFTR alternative, last in the resolution order. Not an inner IPv4 destination. |
| `spec.aftrFrom` | **Conditionally required** | object | Optional common resource/field source, usually a DHCPv6-provided AFTR name. It must name an existing source and provided field. Runtime also reads dnsServers from that same resource and merges them with aftrDNSServers. |
| `spec.aftrDNSServers` | *Optional* | array | Optional bare IPv4/IPv6 DNS servers, queried on port 53. If no explicit or source-derived servers exist, runtime uses 127.0.0.1:1053 by default. Supply reachable DNS servers; declaring this does not start a resolver. |
| `spec.aftrAddressOrdinal` | *Optional* | integer; minimum=1 | One-based AAAA selection, default 1. Explicit selection sorts normalized IPv6 strings lexically before indexing; omission with no selection mode retains resolver order. |
| `spec.aftrAddressSelection` | *Optional* | string; enum=["ordinal", "ordinalModulo"] | ordinal by default; out-of-range ordinal fails. ordinalModulo wraps (ordinal−1) by record count. Does not distribute traffic across AFTRs. |
| `spec.localAddress` | **Conditionally required** | string | Optional literal OUTER IPv6 source; required with localAddressSource: static. Not the inner IPv4 source. |
| `spec.localAddressFrom` | *Optional* | object | Optional common status source for INNER IPv4. Runtime first reads status, then declared IPv4StaticAddress.address. Unresolved required source yields Pending; optional true falls back to 192.0.0.2. Resolved value must be usable unicast IPv4. |
| `spec.localAddressSource` | *Optional* | string; enum=["interface", "static", "delegatedAddress"] | interface default discovers a usable global IPv6 unless localAddress supplies one. static requires localAddress. delegatedAddress derives from localDelegatedAddress and may stage a /128 until DAD completes. |
| `spec.localDelegatedAddress` | **Conditionally required** | string | Bare existing IPv6DelegatedAddress name, required for delegatedAddress mode. Validation checks any supplied reference even in other modes. |
| `spec.localAddressSuffix` | *Optional* | string | Optional IPv6 suffix. Delegated mode precedence: this value → referenced addressSuffix → ::1. Interface mode can also derive a source using this suffix. |
| `spec.defaultRoute` | *Optional* | boolean | Default false. FreeBSD runtime can change/add the default via 192.0.0.1. The Linux tunnel controller does not install an IPv4 default from this flag; use IPv4Route/EgressRoutePolicy. |
| `spec.routeMetric` | *Optional* | integer; minimum=0 | Optional nonnegative integer, decoded default 0. Current DS-Lite runtime does not pass it when configuring the default route. |
| `spec.mtu` | *Optional* | integer; minimum=1280; maximum=65535 | Optional 1280–65535, runtime default 1460. Applied to the tunnel link; it does not prove the underlay path supports that size. |
| `spec.encapsulationLimit` | *Optional* | string | Linux encaplimit argument, default none. No detailed value validation here; invalid values can fail the ip command. FreeBSD runtime does not use this argument. |
| `spec.when` | *Optional* | object | Optional common activation predicate, default unconditional; false removes the unused configured tunnel. |
| `spec.dependsOn` | *Optional* | array | Optional AND-list of common dependency predicates, default empty. Unmet conditions yield Pending/DependsOnFalse and skip tunnel updates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `device`, `tunnelName`, `localIPv6`, `innerLocalIPv4`, `innerRemoteIPv4`, `localInterface`, `aftrName`, `aftrIPv6`, `mtu`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
