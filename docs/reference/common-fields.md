# Shared reference and condition fields

## Resource identity and value sources

`metadata.name` identifies a resource within its kind. A value-source `resource` uses `Kind/name`, not an OS interface name or a URL. Ordinary status-reference validation requires the referenced resource to exist in the same Router and the requested field to appear in that kind's provided-field list. `optional: true` does not bypass these checks.

```yaml
dnsServerFrom:
  - resource: IPv4StaticAddress/lan-address
    field: address
```

| Field | Meaning, default and condition |
| --- | --- |
| `resource` | Source as `Kind/name`. Required when using the source, including when `field` is set. No wildcard. |
| `field` | Field to read, such as `address`, `phase` or `zone`, limited by the source kind. Although the low-level reader can default to `phase`, ordinary configuration validation requires an explicit field for a supplied resource. Specify it. |
| `optional` | Default `false`. Lets a supporting consumer tolerate an unresolved runtime value; it does not make a missing configuration resource valid. The individual resource documents how it handles missing values. |

Readers that use `ValuesFromStoreOrRouter` first read `DNSZone.zone` or `IPv4StaticAddress.address` from the declared config and then try status. Other readers consume status directly. A declared reference does not guarantee the dynamic value is ready, and it does not itself impose an execution order.

## Dependencies

`dependsOn` is a list of dependency predicates. All entries must pass; an empty list has no dependency gate.

```yaml
dependsOn:
  - resource: DHCPv6PrefixDelegation/wan-pd
    phase: Bound
```

| Field | Meaning, default and condition |
| --- | --- |
| `resource` | Dependency as `Kind/name`. Referenced configuration resource and field must exist. |
| `field` | Default `phase`. Used unless `phase` is specified. |
| `phase` | Shorthand for matching the source `phase`; when nonempty it takes precedence over both `field` and `equals`. |
| `equals` | Exact string required from any returned value. Default empty: require a nonempty first value. |
| `notEmpty` | Default `false`. Tests that the first normalized value is nonblank. The current reader already removes blank values, so setting this flag normally adds no further restriction. |
| `optional` | Default `false`. At runtime, absent source values pass only when true. It does not turn a resolved mismatching value into a match and does not relax configuration-reference validation. |

## Conditional activation

Omitting `when` imposes no condition. A nonempty condition must use exactly one of `state`, `all` and `any` at each level. `all` and `any` may contain nested conditions, but their child entries cannot be empty. Both are accepted by the implementation even though the JSON Schema omits these recursive properties.

```yaml
when:
  all:
    - state:
        DHCPv6PrefixDelegation/wan-pd.phase:
          equals: Bound
    - state:
        HealthCheck/upstream.phase:
          equals: Healthy
```

`state` combines all its entries with AND. `all` combines child conditions with AND; `any` combines them with OR. Resource status keys can use `Kind/name.field`, `Kind/name.status.field`, or the equivalent `${...}` spelling. Resource-style keys are checked against declared resources and provided fields; other state-key forms are handled as state-store names.

| State predicate | Meaning and omitted behavior |
| --- | --- |
| `exists` | Omitted: no presence constraint. `true` requires status `set`; `false` requires `unset`, which is different from `unknown`. |
| `equals` | Nonempty value requires exact, case-sensitive equality and status `set`. Empty disables this comparison; use `exists` or `status` to express absence. |
| `in` | Nonempty list requires an exact match to any member and status `set`. Empty disables the membership constraint. |
| `contains` | Nonempty string requires a case-sensitive substring and status `set`. Empty disables this constraint. |
| `status` | Optional state-store status: `set`, `unset` or `unknown`. This is not the resource's lifecycle `phase`. |
| `for` | Optional Go duration: require the observed state's age to be at least this duration. It tracks state age, not a separate timer for the entire predicate. Parsing is validated; positive duration is not enforced here. |
| `maxAge` | Optional strictly positive Go duration: state age must not exceed it. With `for`, both age constraints apply. |

Within one state entry, all supplied predicates apply together. Unresolved `unknown` values can be indeterminate rather than simply false; some effective-config paths keep indeterminate resources while resolved-only filtering does not. VXLANTunnel rejects conditions that explicitly accept unknown. Consult the individual resource for its pending/withdrawal behavior.

Implementation: [reference validation](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go), [condition validation](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go), [value/dependency readers](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go), [condition evaluation](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go), [provided fields](https://github.com/imksoo/routerd/blob/main/pkg/api/provides.go).
