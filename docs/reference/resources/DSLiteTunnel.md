# DSLiteTunnel

[Resource configuration reference](../resources.md#dslitetunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Encapsulate IPv4 in IPv6 toward an AFTR. Tunnel setup, an IPv4 default route, NAT and reachability checks are separate concerns.

## Conditions and references

- interface and at least one of aftrFrom/aftrFQDN/aftrIPv6/remoteAddress are required even when disabled. AFTR sources may coexist; runtime tries them in that order until one resolves.
- localAddress is the outer IPv6 source, but localAddressFrom currently selects the INNER IPv4 source. They are not interchangeable. The inner source defaults 192.0.0.2 and inner remote is 192.0.0.1. No IPv4-from reference means the default inner source.
- Missing AFTR/local/dependency values produce Pending. Up records configured tunnel/link state, not AFTR reachability or working Internet access. A false activation condition or disabled resource removes an unused tunnel; dependency failure alone skips reconciliation rather than promising immediate teardown.
- Linux uses ip6tnl mode ipip6. FreeBSD uses a gif device; a requested non-gif name maps deterministically to gif100–gif999 and the actual name is published in status. Removed aftrSource and ready_when are rejected; use aftrFrom and dependsOn.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # optional; integer; minimum=1
    aftrAddressSelection: "ordinal" # optional; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # optional; array
      - "<string>" # string
    aftrFQDN: "<string>" # optional; string
    aftrFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    aftrIPv6: "<string>" # optional; string
    defaultRoute: true # optional; boolean
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    enabled: true # optional; boolean
    encapsulationLimit: "<string>" # optional; string
    interface: "<string>" # Required; string
    localAddress: "<string>" # optional; string
    localAddressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    localAddressSource: "interface" # optional; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # optional; string
    localDelegatedAddress: "<string>" # optional; string
    mtu: 1280 # optional; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # optional; string
    routeMetric: 0 # optional; integer; minimum=0
    tunnelName: "<string>" # optional; string
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
| `spec.interface` | Required declared logical underlay interface; normal reference validation requires it to exist. |
| `spec.tunnelName` | Optional desired device name, default metadata.name. FreeBSD may publish a different gif name as described above. |
| `spec.enabled` | Default true. False removes an unused configured tunnel and reports Disabled; validation still applies. |
| `spec.aftrFQDN` | Optional AFTR hostname; no whitespace or slash. Tried after aftrFrom, before literal alternatives. |
| `spec.aftrIPv6` | Optional bare AFTR IPv6 address, tried after aftrFQDN. No default. |
| `spec.remoteAddress` | Optional IPv6 AFTR alternative, last in the resolution order. Not an inner IPv4 destination. |
| `spec.aftrFrom` | Optional common resource/field source, usually a DHCPv6-provided AFTR name. It must name an existing source and provided field. Runtime also reads dnsServers from that same resource and merges them with aftrDNSServers. |
| `spec.aftrDNSServers` | Optional bare IPv4/IPv6 DNS servers, queried on port 53. If no explicit or source-derived servers exist, runtime uses 127.0.0.1:1053 by default. Supply reachable DNS servers; declaring this does not start a resolver. |
| `spec.aftrAddressOrdinal` | One-based AAAA selection, default 1. Explicit selection sorts normalized IPv6 strings lexically before indexing; omission with no selection mode retains resolver order. |
| `spec.aftrAddressSelection` | ordinal by default; out-of-range ordinal fails. ordinalModulo wraps (ordinal−1) by record count. Does not distribute traffic across AFTRs. |
| `spec.localAddress` | Optional literal OUTER IPv6 source; required with localAddressSource: static. Not the inner IPv4 source. |
| `spec.localAddressFrom` | Optional common status source for INNER IPv4. Runtime first reads status, then declared IPv4StaticAddress.address. Unresolved required source yields Pending; optional true falls back to 192.0.0.2. Resolved value must be usable unicast IPv4. |
| `spec.localAddressSource` | interface default discovers a usable global IPv6 unless localAddress supplies one. static requires localAddress. delegatedAddress derives from localDelegatedAddress and may stage a /128 until DAD completes. |
| `spec.localDelegatedAddress` | Bare existing IPv6DelegatedAddress name, required for delegatedAddress mode. Validation checks any supplied reference even in other modes. |
| `spec.localAddressSuffix` | Optional IPv6 suffix. Delegated mode precedence: this value → referenced addressSuffix → ::1. Interface mode can also derive a source using this suffix. |
| `spec.defaultRoute` | Default false. FreeBSD runtime can change/add the default via 192.0.0.1. The Linux tunnel controller does not install an IPv4 default from this flag; use IPv4Route/EgressRoutePolicy. |
| `spec.routeMetric` | Optional nonnegative integer, decoded default 0. Current DS-Lite runtime does not pass it when configuring the default route. |
| `spec.mtu` | Optional 1280–65535, runtime default 1460. Applied to the tunnel link; it does not prove the underlay path supports that size. |
| `spec.encapsulationLimit` | Linux encaplimit argument, default none. No detailed value validation here; invalid values can fail the ip command. FreeBSD runtime does not use this argument. |
| `spec.when` | Optional common activation predicate, default unconditional; false removes the unused configured tunnel. |
| `spec.dependsOn` | Optional AND-list of common dependency predicates, default empty. Unmet conditions yield Pending/DependsOnFalse and skip tunnel updates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `device`, `tunnelName`, `localIPv6`, `innerLocalIPv4`, `innerRemoteIPv4`, `localInterface`, `aftrName`, `aftrIPv6`, `mtu`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
