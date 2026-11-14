# ProviderActionPolicy

[Resource configuration reference](../resources.md#provideractionpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

Gate execution of proposed provider actions by executor Plugins. Declaring a policy is not itself an action or a cloud login.

## Conditions and references

- Only the first ProviderActionPolicy is used. Omission is locked down: enabled false, dryRunOnly true, requireApproval true, no provider/action allowlist and maxActionsPerRun 0. All gates apply together, including for dry-run previews where applicable.
- Automatic execution requires enabled true, dryRunOnly false, requireApproval false and a positive cap, plus matching allowlists and a suitable executor Plugin with execute.providerAction capability. External credentials and real provider authorization belong to that executor.
- executionWindow currently accepts every string and enforces no time restriction. allowUndo enables only best-effort rollback when suitable undo data exists; it is not an atomic transaction guarantee.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # optional; boolean
    allowedActions: # optional; array
      - "<string>" # string
    allowedCIDRs: # optional; array
      - "<string>" # string
    allowedProviderRefs: # optional; array
      - "<string>" # string
    allowedProviders: # optional; array
      - "<string>" # string
    dryRunOnly: true # optional; boolean
    enabled: true # optional; boolean
    executionWindow: "<string>" # optional; string
    maxActionsPerRun: 0 # optional; integer
    requireApproval: true # optional; boolean
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.enabled` | Default false. Must be true to permit action execution or preview. |
| `spec.dryRunOnly` | Default true. False permits live mutation only if all other gates pass; true still requires enabled and allowlists for previews. |
| `spec.requireApproval` | Default true. False allows auto-approval only with enabled true and dryRunOnly false. This does not bypass allowlists or executor selection. |
| `spec.allowedProviders` | Exact aws/azure/oci/gcp allowlist; default empty denies every provider. Use canonical spelling without surrounding whitespace. |
| `spec.allowedProviderRefs` | Optional exact action providerRef allowlist; empty imposes no reference restriction. Strings are not resolved as status sources or checked for profile existence by this validator. Match the actual action providerRef spelling. |
| `spec.allowedActions` | Exact canonical verb allowlist: assign-secondary-ip, unassign-secondary-ip, assign-route-table-route, unassign-route-table-route, ensure-forwarding-enabled, ensure-forwarding-disabled. Empty denies all. |
| `spec.allowedCIDRs` | Optional IPv4/IPv6 CIDRs. Empty adds no CIDR gate; nonempty requires target.address to exist and its IP (including the host part of CIDR input) to fall in any allowed prefix. Not a check of every address an executor might change. |
| `spec.maxActionsPerRun` | Nonnegative integer, default 0 denies execution. Positive value caps candidate selection per automatic controller run; single-action engine calls check only that it is positive. Not a lifetime or billing budget. |
| `spec.allowUndo` | Default false; true permits best-effort rollback subject to available undo data and policy checks. |
| `spec.executionWindow` | Optional free-form string, default empty. Currently neither parsed as a schedule nor enforced at runtime; does not restrict execution time. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go) · [pkg/controller/provideraction/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/provideraction/controller.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
