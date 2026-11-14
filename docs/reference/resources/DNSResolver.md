# DNSResolver

[Resource configuration reference](../resources.md#dnsresolver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

A local DNS service with UDP and TCP listeners, connected to DNSForwarder rules.

## Conditions and references

- At least one DNSForwarder must refer to this resolver. Configure forwarding through separate resources; the old inline sources field is not a supported YAML input.
- Omitting listen or using an empty list normalizes to 127.0.0.1:5053. Each endpoint serves both UDP and TCP. Do not expose an unrestricted recursive resolver to untrusted networks.
- Static and resolved listen addresses are merged and deduplicated. Missing required dynamic addresses mark dependencies as waiting but do not stop other usable addresses; with no resolved listeners the resolver stays Pending.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    cache: # optional; object
      enabled: true # optional; boolean
      maxEntries: 0 # optional; integer; minimum=0
      maxTTL: "<string>" # optional; string
      minTTL: "<string>" # optional; string
      negativeTTL: "<string>" # optional; string
    listen: # Required; array
      - # object
        addressFrom: # optional; array
          - # object
            field: "<string>" # optional; string
            optional: true # optional; boolean
            resource: "<string>" # Required; string
        addresses: # optional; array
          - "<string>" # string
        name: "<string>" # optional; string
        port: 1 # optional; integer; minimum=1; maximum=65535
        sources: # optional; array
          - "<string>" # string
    metrics: # optional; object
      perUpstream: true # optional; boolean
    queryLog: # optional; object
      enabled: true # optional; boolean
      path: "<string>" # optional; string
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
| `spec.listen` | Listener list; normalization supplies a loopback endpoint when empty. |
| `spec.listen[].name` | Optional label; default listen-0, listen-1, … in list order. |
| `spec.listen[].addresses` | Literal IPv4/IPv6 addresses without ports or CIDR. If both addresses and addressFrom are empty, defaults to [127.0.0.1]. |
| `spec.listen[].addressFrom` | Optional list of common resource/field/optional value sources. Existing resource and explicit provided field required. Declared IPv4StaticAddress.address can be read before status exists; CIDR values are reduced to addresses. optional defaults false; missing optional values are skipped. |
| `spec.listen[].port` | Optional port 1–65535; omitted/0 normalizes to 5053. |
| `spec.listen[].sources` | Optional DNSForwarder names attached to this resolver. Empty selects all its forwarders. Use bare metadata.name: validation accepts qualified names, but the daemon matches bare names literally and does not strip a prefix. List order does not reorder the forwarders. |
| `spec.cache` | Optional response-cache settings. Caches upstream replies, not local-zone responses; disabled by default. |
| `spec.cache.enabled` | Default false. true enables the in-memory response cache. |
| `spec.cache.maxEntries` | Default 10000 when omitted/0; nonnegative schema value. Bounds cached entries, not bytes. |
| `spec.cache.minTTL` | Optional Go duration lower bound on cache lifetime. Empty imposes no lower bound; invalid duration is ignored by this path rather than rejected. Does not rewrite record TTLs in the response. |
| `spec.cache.maxTTL` | Optional Go duration upper bound, applied after minTTL; empty imposes none and invalid duration is ignored. |
| `spec.cache.negativeTTL` | Optional Go duration replacing lifetime for successful empty answers or NXDOMAIN, after min/max bounds. Default empty uses 60s before those bounds. SERVFAIL and other transient failures are not cached. |
| `spec.metrics` | Metrics options; no current runtime consumer for perUpstream. |
| `spec.metrics.perUpstream` | Decoded boolean default false; does not currently enable or disable per-upstream metrics. |
| `spec.queryLog` | Optional query logging in a SQLite database; disabled by default. |
| `spec.queryLog.enabled` | Default false; true records queries using queryLog.path. |
| `spec.queryLog.path` | When enabled, omitted/blank defaults to /var/lib/routerd/dns-queries.db. This is a database path, not a text log. |
| `spec.when` | Optional common state condition, default unconditional. See common fields for state/all/any and schema limitations. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `listeners`, `listenAddresses`, `sources`, `updatedAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
