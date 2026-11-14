# DNSResolver

[Resource configuration reference](../resources.md#dnsresolver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

A local DNS service with UDP and TCP listeners, connected to DNSForwarder rules.

## Conditions and references

- At least one DNSForwarder must refer to this resolver. Configure forwarding through separate resources; the old inline sources field is not a supported YAML input.
- Omitting listen or using an empty list normalizes to 127.0.0.1:5053. Each endpoint serves both UDP and TCP. Do not expose an unrestricted recursive resolver to untrusted networks.
- Static and resolved listen addresses are merged and deduplicated. Missing required dynamic addresses mark dependencies as waiting but do not stop other usable addresses; with no resolved listeners the resolver stays Pending.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    [ cache:
        [ enabled: <boolean> ]
        [ maxEntries: <integer> ]
        [ maxTTL: <string> ]
        [ minTTL: <string> ]
        [ negativeTTL: <string> ]
    ]
    listen:
      -
        [ addressFrom:
            -
              field: <string>
              [ optional: <boolean> ]
              resource: <string>
        ]
        [ addresses:
            - <string>
        ]
        [ name: <string> ]
        [ port: <integer> ]
        [ sources:
            - <string>
        ]
    [ metrics:
        [ perUpstream: <boolean> ]
    ]
    [ queryLog:
        [ enabled: <boolean> ]
        [ path: <string> ]
    ]
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
| `spec.listen` | **Required** | array | Listener list; normalization supplies a loopback endpoint when empty. |
| `spec.listen[].name` | *Optional* | string | Optional label; default listen-0, listen-1, … in list order. |
| `spec.listen[].addresses` | *Optional* | array | Literal IPv4/IPv6 addresses without ports or CIDR. If both addresses and addressFrom are empty, defaults to [127.0.0.1]. |
| `spec.listen[].addressFrom` | *Optional* | array | Optional list of common resource/field/optional value sources. Existing resource and explicit provided field required. Declared IPv4StaticAddress.address can be read before status exists; CIDR values are reduced to addresses. optional defaults false; missing optional values are skipped. |
| `spec.listen[].port` | *Optional* | integer; minimum=1; maximum=65535 | Optional port 1–65535; omitted/0 normalizes to 5053. |
| `spec.listen[].sources` | *Optional* | array | Optional DNSForwarder names attached to this resolver. Empty selects all its forwarders. Use bare metadata.name: validation accepts qualified names, but the daemon matches bare names literally and does not strip a prefix. List order does not reorder the forwarders. |
| `spec.cache` | *Optional* | object | Optional response-cache settings. Caches upstream replies, not local-zone responses; disabled by default. |
| `spec.cache.enabled` | *Optional* | boolean | Default false. true enables the in-memory response cache. |
| `spec.cache.maxEntries` | *Optional* | integer; minimum=0 | Default 10000 when omitted/0; nonnegative schema value. Bounds cached entries, not bytes. |
| `spec.cache.minTTL` | *Optional* | string | Optional Go duration lower bound on cache lifetime. Empty imposes no lower bound; invalid duration is ignored by this path rather than rejected. Does not rewrite record TTLs in the response. |
| `spec.cache.maxTTL` | *Optional* | string | Optional Go duration upper bound, applied after minTTL; empty imposes none and invalid duration is ignored. |
| `spec.cache.negativeTTL` | *Optional* | string | Optional Go duration replacing lifetime for successful empty answers or NXDOMAIN, after min/max bounds. Default empty uses 60s before those bounds. SERVFAIL and other transient failures are not cached. |
| `spec.metrics` | *Optional* | object | Metrics options; no current runtime consumer for perUpstream. |
| `spec.metrics.perUpstream` | *Optional* | boolean | Decoded boolean default false; does not currently enable or disable per-upstream metrics. |
| `spec.queryLog` | *Optional* | object | Optional query logging in a SQLite database; disabled by default. |
| `spec.queryLog.enabled` | *Optional* | boolean | Default false; true records queries using queryLog.path. |
| `spec.queryLog.path` | *Optional* | string | When enabled, omitted/blank defaults to /var/lib/routerd/dns-queries.db. This is a database path, not a text log. |
| `spec.when` | *Optional* | object | Optional common state condition, default unconditional. See common fields for state/all/any and schema limitations. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `listeners`, `listenAddresses`, `sources`, `updatedAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
