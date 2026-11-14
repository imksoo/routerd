# FederationSLO

[Resource configuration reference](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

Configure per-group thresholds used by routerctl doctor for federation delivery and subscription diagnostics. This does not change delivery retry or plugin execution policy.

## Conditions and references

- groupRef is required and must name an existing EventGroup. At most one FederationSLO may reference each group. All numeric fields must be nonnegative; effective lagWarnSeconds must be strictly less than effective lagFailSeconds after zero-value defaults are applied.
- Lag comparisons are inclusive (>= warn/fail threshold). Subscription counts warn/fail only when greater than their allowed maximum. Expiring-soon uses remaining TTL strictly below the threshold; pending/failed delivery can raise that diagnostic from WARN to FAIL.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # optional; object
      expiresSoonSeconds: 0 # optional; integer; minimum=0
      lagFailSeconds: 0 # optional; integer; minimum=0
      lagWarnSeconds: 0 # optional; integer; minimum=0
    groupRef: "<string>" # Required; string
    subscription: # optional; object
      maxFailedRuns: 0 # optional; integer; minimum=0
      maxPendingRuns: 0 # optional; integer; minimum=0
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.groupRef` | Required bare EventGroup metadata.name, not EventGroup/name; existence and uniqueness are validated. |
| `spec.delivery` | Optional delivery-diagnostic thresholds; omitted fields use defaults below. Not a transport timeout or retry configuration. |
| `spec.delivery.lagWarnSeconds` | Nonnegative seconds; omitted or 0 means 60. Maximum measured delivery lag >= this value produces WARN unless the fail threshold is met. |
| `spec.delivery.lagFailSeconds` | Nonnegative seconds; omitted or 0 means 180. Maximum measured delivery lag >= this value produces FAIL. Must exceed the effective warn threshold. |
| `spec.delivery.expiresSoonSeconds` | Nonnegative seconds; omitted or 0 means 120. Remaining event TTL below this value is expiring soon. Zero cannot disable the check; events with no expiry are handled separately. |
| `spec.subscription` | Optional subscription-run diagnostic allowances, default zero pending/failed tolerated. This does not cap the controller’s queue or retry count. |
| `spec.subscription.maxPendingRuns` | Nonnegative count, default 0. Pending run count greater than this value produces WARN when the failed-run allowance has not already produced FAIL. |
| `spec.subscription.maxFailedRuns` | Nonnegative count, default 0. Failed run count greater than this value produces FAIL. It does not change the three-attempt subscription retry policy. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [cmd/routerctl/doctor_federation.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor_federation.go)
