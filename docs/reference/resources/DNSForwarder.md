# DNSForwarder

[Resource configuration reference](../resources.md#dnsforwarder) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Select local zones or upstream DNS servers for matching query names on one DNSResolver.

## Conditions and references

- Exactly one nonempty list of zoneRefs or upstreams is required, along with resolver and nonempty match. Every reference must exist in the same Router; use a bare name or the corresponding Kind/name.
- Rules are tried in Router resource order after listener filtering, not longest-suffix order. Put private/specific rules before a catch-all. Once a matching upstream source is selected, failure returns SERVFAIL; it does not fall through to another source and leak private queries.
- Upstream references are combined into one source/pool. Bootstrap settings are merged, and the first nonempty sourceInterface is shared by the entire pool; conflicting per-upstream bindings are not independently honored.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    [ dnssecValidate: <boolean> ]
    [ healthcheck:
        [ failThreshold: <integer> ]
        [ interval: <string> ]
        [ passThreshold: <integer> ]
        [ timeout: <string> ]
    ]
    match:
      - <string>
    [ queryTimeout: <string> ]
    resolver: <string>
    [ upstreams:
        - <string>
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
    [ zoneRefs:
        - <string>
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.resolver` | **Required** | string | Required existing DNSResolver name or DNSResolver/name. No default. |
| `spec.match` | **Required** | array | Required nonempty domain-suffix list; comparisons ignore case and a trailing dot. example.net matches the name and its subdomains; "." matches all queries. No implicit catch-all. |
| `spec.zoneRefs` | **Conditionally required** | array | Existing DNSZone names or DNSZone/name references for local answers. Mutually exclusive with upstreams; required when upstreams is empty. |
| `spec.upstreams` | **Conditionally required** | array | Existing DNSUpstream names or DNSUpstream/name, not raw server IPs/URLs. Mutually exclusive with zoneRefs. Servers from these resources form one failover pool. |
| `spec.dnssecValidate` | *Optional* | boolean | Default false. true sets the DNSSEC DO bit on upstream queries and requires AD in the reply; it does not perform local cryptographic DNSSEC validation. Zone sources do not use this field. |
| `spec.queryTimeout` | *Optional* | string | Positive Go duration for each upstream query attempt. Omitted inherits a positive healthcheck.timeout, otherwise 3s. Distinct from background probe timing; zone answers do not use it. |
| `spec.healthcheck` | *Optional* | object | Background upstream probe options; defaults interval 15s, timeout 3s, failThreshold 3, passThreshold 2. Not used for local-zone sources. |
| `spec.healthcheck.interval` | *Optional* | string | Go duration; omitted/empty or nonpositive at runtime uses 15s. Duration syntax is validated but positivity is not enforced at configuration validation. |
| `spec.healthcheck.timeout` | *Optional* | string | Probe Go duration; omitted/empty or nonpositive uses 3s. Also supplies the default queryTimeout when positive. |
| `spec.healthcheck.failThreshold` | *Optional* | integer; minimum=1 | Consecutive failures to mark an upstream unhealthy; default 3 (0/nonpositive runtime normalizes to 3; schema minimum 1). |
| `spec.healthcheck.passThreshold` | *Optional* | integer; minimum=1 | Consecutive successes for recovery; default 2 (0/nonpositive runtime normalizes to 2; schema minimum 1). |
| `spec.when` | *Optional* | object | Optional common state predicate; omitted means unconditional. A condition can remove this rule from the effective resolver configuration. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `resolver`, `upstreams`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
