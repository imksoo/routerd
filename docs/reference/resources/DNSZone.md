# DNSZone

[Resource configuration reference](../resources.md#dnszone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Local A/AAAA records and generated PTR answers served through a DNSForwarder zoneRefs rule. This is not a general authoritative DNS server with arbitrary record types, zone transfers or signing.

## Conditions and references

- A DNSForwarder must refer to this zone, and its match list must include the queried forward or reverse domain. Merely declaring DNSZone does not open a DNS port. The resolver also imports matching VirtualAddress/IngressService hostnames unless those resources specify externalDNS.
- Static ipv4/ipv6 and dynamic ipv4From/ipv6From may coexist. A resolved dynamic value replaces the literal for that family; missing values retain the literal. Missing required sources record pending metadata rather than blocking the entire resolver; optional missing sources do not record pending. Wrong-family or invalid resolved values are errors.
- dnssec.enabled only sets AD on local answers; it does not generate RRSIG/DNSKEY or validate signatures. dnssec.mode, dhcpDerived.hostnameSuffix, dhcpDerived.ddns and reverseZones do not currently change answer behavior. Do not interpret these fields as working signing, DDNS or reverse-zone access controls.
- Only A/AAAA and generated PTR records are answered. An existing name with no requested address family returns NOERROR/NODATA; an absent name within the zone returns NXDOMAIN. Wildcard names are supported. Repeated static hostnames use the last record rather than accumulating addresses across entries.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    dhcpDerived: # optional; object
      ddns: true # optional; boolean
      hostnameSuffix: "<string>" # optional; string
      leaseFile: "<string>" # optional; string
      sources: # optional; array
        - "<string>" # string
      ttl: 0 # optional; integer; minimum=0
    dnssec: # optional; object
      enabled: true # optional; boolean
      mode: "" # optional; string; enum=["", "validate", "sign"]
    records: # optional; array
      - # object
        hostname: "<string>" # Required; string
        ipv4: "<string>" # optional; string
        ipv4From: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        ipv6: "<string>" # optional; string
        ipv6From: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        ttl: 0 # optional; integer; minimum=0
    reverseZones: # optional; array
      - # object
        name: "<string>" # Required; string
    ttl: 0 # optional; integer; minimum=0
    zone: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.zone` | Required zone domain, for example home.example.net. Runtime adds a trailing dot; no default. Use normal lowercase DNS names because the current validator only checks nonempty. |
| `spec.ttl` | Zone default TTL in seconds; omitted/0 becomes 300. Schema permits nonnegative values. Used by static records without TTL and generated PTR replies. |
| `spec.dnssec` | Optional local-answer AD flag settings; no cryptographic signing or validation. |
| `spec.dnssec.enabled` | Default false; true sets AD even on local negative/NODATA replies without producing signatures. |
| `spec.dnssec.mode` | Schema accepts empty, validate or sign; default empty. Decoded but not used by current local-zone answer logic. |
| `spec.records` | Optional static A/AAAA record list, default empty. A hostname with no addresses is accepted but has no A/AAAA answer. |
| `spec.records[].hostname` | Required name without whitespace or comma. Relative names are suffixed with zone; a full name already inside zone stays unchanged. A name outside zone is also suffixed, not served as an external zone. Use *.subdomain for a wildcard. |
| `spec.records[].ipv4` | Optional bare IPv4 address; default none. Can coexist with ipv4From as a fallback retained when the source is unresolved. |
| `spec.records[].ipv4From` | Optional common status-value source; explicit resource/field required and validated. First usable value must be IPv4; CIDR input is reduced to its host address. Runtime reads status, not the declared IPv4StaticAddress fallback used by some other consumers. |
| `spec.records[].ipv6` | Optional bare IPv6 address; default none. Can coexist with ipv6From using the same fallback behavior. |
| `spec.records[].ipv6From` | Optional common resource/field/optional source; first value must be IPv6. Same reference, CIDR and missing-value rules as ipv4From; optional defaults false. |
| `spec.records[].ttl` | Record TTL in seconds; omitted/0 inherits zone TTL. Schema nonnegative; does not affect DHCP-derived TTL. |
| `spec.dhcpDerived` | Optional DHCP lease-to-record settings. Runtime lease events can update enabled zones; this is not RFC 2136 DDNS. |
| `spec.dhcpDerived.sources` | Default empty disables live lease-event import and sticky snapshot import. Any nonempty list enables them; current runtime does not filter by these source strings or validate referenced DHCP resources. All received lease events are offered to each enabled zone. |
| `spec.dhcpDerived.hostnameSuffix` | Decoded optional string, default empty; not read by current lease record generation. Zone suffixing uses zone instead. |
| `spec.dhcpDerived.ddns` | Decoded boolean default false; current daemon neither gates event updates on this flag nor implements DNS UPDATE from it. |
| `spec.dhcpDerived.ttl` | TTL seconds for DHCP-derived forward records; omitted/0 becomes 60, independent of zone TTL. PTR replies use zone TTL. |
| `spec.dhcpDerived.leaseFile` | Optional dnsmasq lease-file path read when constructing/reloading the zone. Empty disables this file import; it is separate from live-event source enablement and not a continuously watched file. |
| `spec.reverseZones` | Optional reverse-zone metadata list, default empty. Current daemon records these names but does not consult them when answering PTR; PTR is generated from records regardless of this list. |
| `spec.reverseZones[].name` | Schema-required string inside each entry. Stored as a fully qualified reverse-zone name but currently has no answer-selection effect. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `zone`, `records`, `pendingRecords`, `updatedAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
