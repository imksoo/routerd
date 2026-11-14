# IPv6RouterAdvertisement

[Resource configuration reference](../resources.md#ipv6routeradvertisement) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Configure IPv6 router-advertisement options on a LAN and deprecate previously delegated prefixes.

## Conditions and references

- interface plus prefix or prefixFrom is required. The static renderer and live writer differ: current live generation emits interface/enable-ra/options but does not derive a normal dhcp-range from prefix/prefixFrom. Pair with a working DHCPv6Server/address configuration and confirm actual on-wire PIOs; the schema/Applied status alone cannot prove current-prefix advertisements.
- dnssl and dnsslFrom are mutually exclusive; prefix and prefixFrom, or rdnss and rdnssFrom, are not. All common resource references must exist and specify a provided field even when optional true. Removed prefixSource/ready_when are rejected.
- When prefixFrom points to IPv6DelegatedAddress, still-valid previous /64 prefixes are sent directly as deprecated PIOs (preferred 0, remaining valid lifetime), from a real link-local source with hop limit 255. Current-prefix normal service remains the shared dnsmasq path.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: "<name>"
  spec:
    configPath: "<string>" # optional; string
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    dnssl: # optional; array
      - "<string>" # string
    dnsslFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    interface: "<string>" # Required; string
    mFlag: true # optional; boolean
    mtu: 1280 # optional; integer; minimum=1280; maximum=65535
    oFlag: true # optional; boolean
    pidFile: "<string>" # optional; string
    preferredLifetime: "<string>" # optional; string
    prefix: "<string>" # optional; string
    prefixFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    prfPreference: "" # optional; string; enum=["", "low", "medium", "high"]
    rdnss: # optional; array
      - "<string>" # string
    rdnssFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    validLifetime: "<string>" # optional; string
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
| `spec.interface` | Required logical interface. Current global reference validation does not verify this field for RA, unlike many address resources; runtime attempts alias resolution and then uses the literal as an OS name. Declare an Interface and use its name. |
| `spec.prefix` | Literal IPv6 prefix, required unless prefixFrom.resource is supplied. May coexist with prefixFrom as a status fallback. Current live writer does not generate a normal prefix range from this field; do not assume declaration alone advertises it. |
| `spec.prefixFrom` | Optional common resource/field/optional source, existing resource and explicit provided field required. Used for reported prefix and, when referencing IPv6DelegatedAddress, discovery of previous /64s to deprecate. Missing values do not gate normal config generation by themselves. |
| `spec.rdnss` | Optional literal IPv6 DNS-server list, default empty. Merged with rdnssFrom; emitted as untagged option6:dns-server in shared dnsmasq, not isolated per RA interface by this writer. |
| `spec.rdnssFrom` | Optional common status-value source list. No automatic source. Current RA path discards unresolved-source pending reasons and uses the values available; optional does not provide a service-readiness gate here. |
| `spec.dnssl` | Optional search-domain list, default empty; each value is a single domain without whitespace/slash/comma. Exclusive with dnsslFrom. Emitted as shared untagged option6:domain-search. |
| `spec.dnsslFrom` | Optional source list restricted to DNSZone/<name>, field: zone. Each resource must exist. Reads declared zone values before status; exclusive with dnssl. Current normal RA rendering ignores missing-source pending reasons. |
| `spec.mFlag` | Boolean default false. Used in direct advertisements deprecating previous delegated prefixes; current normal dnsmasq RA option generation does not map it to M. |
| `spec.oFlag` | Boolean default false. Same restricted direct-deprecation path as mFlag; not a current normal dnsmasq O-bit switch. |
| `spec.mtu` | Optional 1280–65535. Zero/omitted emits no explicit MTU unless a PathMTU-derived RA value takes precedence. Passed via dnsmasq ra-param mtu:<value>. |
| `spec.prfPreference` | Empty/medium default uses normal preference; low/high emit dnsmasq router preference and affect direct old-prefix advertisements. |
| `spec.preferredLifetime` | Optional string, default empty. Current live normal writer does not use it. Direct old-prefix PIOs always use preferred lifetime 0 regardless of this setting. |
| `spec.validLifetime` | For normal live dnsmasq rendering this is the ra-param router lifetime, not the current prefix PIO lifetime. Supply decimal seconds; omitted leaves dnsmasq default. Direct old-prefix RA parses decimal seconds, defaults 1800 on empty/invalid and caps at 65535, while old PIO validity is its remaining lease lifetime. |
| `spec.configPath` | Optional decoded string, default empty; current live controller uses its shared dnsmasq ConfigPath/PIDFile options and does not consume this per-resource override. |
| `spec.pidFile` | Optional decoded string, default empty; current live controller uses its shared dnsmasq ConfigPath/PIDFile options and does not consume this per-resource override. |
| `spec.when` | Optional common state predicate, default unconditional. False filters the resource from effective config and reports Pending/WhenFalse. |
| `spec.dependsOn` | Optional common AND-list, default empty. Failed predicates report Pending/DependsOnFalse and skip direct old-prefix advertisements, but the normal shared dnsmasq line writer does not check this gate. It is not a complete normal-service suppression switch here. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/routeradvertisement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/routeradvertisement.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
