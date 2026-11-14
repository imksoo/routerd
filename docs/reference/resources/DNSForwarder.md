# DNSForwarder

[Resource configuration reference](../resources.md#dnsforwarder) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Select local zones or upstream DNS servers for matching query names on one DNSResolver.

## Conditions and references

- Exactly one nonempty list of zoneRefs or upstreams is required, along with resolver and nonempty match. Every reference must exist in the same Router; use a bare name or the corresponding Kind/name.
- Rules are tried in Router resource order after listener filtering, not longest-suffix order. Put private/specific rules before a catch-all. Once a matching upstream source is selected, failure returns SERVFAIL; it does not fall through to another source and leak private queries.
- Upstream references are combined into one source/pool. Bootstrap settings are merged, and the first nonempty sourceInterface is shared by the entire pool; conflicting per-upstream bindings are not independently honored.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    dnssecValidate: true # optional; boolean
    healthcheck: # optional; object
      failThreshold: 1 # optional; integer; minimum=1
      interval: "<string>" # optional; string
      passThreshold: 1 # optional; integer; minimum=1
      timeout: "<string>" # optional; string
    match: # Required; array
      - "<string>" # string
    queryTimeout: "<string>" # optional; string
    resolver: "<string>" # Required; string
    upstreams: # optional; array
      - "<string>" # string
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
    zoneRefs: # optional; array
      - "<string>" # string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.resolver` | Required existing DNSResolver name or DNSResolver/name. No default. |
| `spec.match` | Required nonempty domain-suffix list; comparisons ignore case and a trailing dot. example.net matches the name and its subdomains; "." matches all queries. No implicit catch-all. |
| `spec.zoneRefs` | Existing DNSZone names or DNSZone/name references for local answers. Mutually exclusive with upstreams; required when upstreams is empty. |
| `spec.upstreams` | Existing DNSUpstream names or DNSUpstream/name, not raw server IPs/URLs. Mutually exclusive with zoneRefs. Servers from these resources form one failover pool. |
| `spec.dnssecValidate` | Default false. true sets the DNSSEC DO bit on upstream queries and requires AD in the reply; it does not perform local cryptographic DNSSEC validation. Zone sources do not use this field. |
| `spec.queryTimeout` | Positive Go duration for each upstream query attempt. Omitted inherits a positive healthcheck.timeout, otherwise 3s. Distinct from background probe timing; zone answers do not use it. |
| `spec.healthcheck` | Background upstream probe options; defaults interval 15s, timeout 3s, failThreshold 3, passThreshold 2. Not used for local-zone sources. |
| `spec.healthcheck.interval` | Go duration; omitted/empty or nonpositive at runtime uses 15s. Duration syntax is validated but positivity is not enforced at configuration validation. |
| `spec.healthcheck.timeout` | Probe Go duration; omitted/empty or nonpositive uses 3s. Also supplies the default queryTimeout when positive. |
| `spec.healthcheck.failThreshold` | Consecutive failures to mark an upstream unhealthy; default 3 (0/nonpositive runtime normalizes to 3; schema minimum 1). |
| `spec.healthcheck.passThreshold` | Consecutive successes for recovery; default 2 (0/nonpositive runtime normalizes to 2; schema minimum 1). |
| `spec.when` | Optional common state predicate; omitted means unconditional. A condition can remove this rule from the effective resolver configuration. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
