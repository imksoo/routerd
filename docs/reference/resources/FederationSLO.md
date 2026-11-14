# FederationSLO

[Resource configuration reference](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

Configure per-group thresholds used by routerctl doctor for federation delivery and subscription diagnostics. This does not change delivery retry or plugin execution policy.

## Conditions and references

- groupRef is required and must name an existing EventGroup. At most one FederationSLO may reference each group. All numeric fields must be nonnegative; effective lagWarnSeconds must be strictly less than effective lagFailSeconds after zero-value defaults are applied.
- Lag comparisons are inclusive (>= warn/fail threshold). Subscription counts warn/fail only when greater than their allowed maximum. Expiring-soon uses remaining TTL strictly below the threshold; pending/failed delivery can raise that diagnostic from WARN to FAIL.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    [ delivery:
        [ expiresSoonSeconds: <integer> ]
        [ lagFailSeconds: <integer> ]
        [ lagWarnSeconds: <integer> ]
    ]
    groupRef: <string>
    [ subscription:
        [ maxFailedRuns: <integer> ]
        [ maxPendingRuns: <integer> ]
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.groupRef` | **Required** | string | Required bare EventGroup metadata.name, not EventGroup/name; existence and uniqueness are validated. |
| `spec.delivery` | *Optional* | object | Optional delivery-diagnostic thresholds; omitted fields use defaults below. Not a transport timeout or retry configuration. |
| `spec.delivery.lagWarnSeconds` | *Optional* | integer; minimum=0 | Nonnegative seconds; omitted or 0 means 60. Maximum measured delivery lag >= this value produces WARN unless the fail threshold is met. |
| `spec.delivery.lagFailSeconds` | *Optional* | integer; minimum=0 | Nonnegative seconds; omitted or 0 means 180. Maximum measured delivery lag >= this value produces FAIL. Must exceed the effective warn threshold. |
| `spec.delivery.expiresSoonSeconds` | *Optional* | integer; minimum=0 | Nonnegative seconds; omitted or 0 means 120. Remaining event TTL below this value is expiring soon. Zero cannot disable the check; events with no expiry are handled separately. |
| `spec.subscription` | *Optional* | object | Optional subscription-run diagnostic allowances, default zero pending/failed tolerated. This does not cap the controller’s queue or retry count. |
| `spec.subscription.maxPendingRuns` | *Optional* | integer; minimum=0 | Nonnegative count, default 0. Pending run count greater than this value produces WARN when the failed-run allowance has not already produced FAIL. |
| `spec.subscription.maxFailedRuns` | *Optional* | integer; minimum=0 | Nonnegative count, default 0. Failed run count greater than this value produces FAIL. It does not change the three-attempt subscription retry policy. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [cmd/routerctl/doctor_federation.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor_federation.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.
