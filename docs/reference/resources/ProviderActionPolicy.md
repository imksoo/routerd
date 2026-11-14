# ProviderActionPolicy

[Resource configuration reference](../resources.md#provideractionpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

Gate execution of proposed provider actions by executor Plugins. Declaring a policy is not itself an action or a cloud login.

## Conditions and references

- Only the first ProviderActionPolicy is used. Omission is locked down: enabled false, dryRunOnly true, requireApproval true, no provider/action allowlist and maxActionsPerRun 0. All gates apply together, including for dry-run previews where applicable.
- Automatic execution requires enabled true, dryRunOnly false, requireApproval false and a positive cap, plus matching allowlists and a suitable executor Plugin with execute.providerAction capability. External credentials and real provider authorization belong to that executor.
- executionWindow currently accepts every string and enforces no time restriction. allowUndo enables only best-effort rollback when suitable undo data exists; it is not an atomic transaction guarantee.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    [ allowUndo: <boolean> ]
    [ allowedActions:
        - <string>
    ]
    [ allowedCIDRs:
        - <string>
    ]
    [ allowedProviderRefs:
        - <string>
    ]
    [ allowedProviders:
        - <string>
    ]
    [ dryRunOnly: <boolean> ]
    [ enabled: <boolean> ]
    [ executionWindow: <string> ]
    [ maxActionsPerRun: <integer> ]
    [ requireApproval: <boolean> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.enabled` | *Optional* | boolean | Default false. Must be true to permit action execution or preview. |
| `spec.dryRunOnly` | *Optional* | boolean | Default true. False permits live mutation only if all other gates pass; true still requires enabled and allowlists for previews. |
| `spec.requireApproval` | *Optional* | boolean | Default true. False allows auto-approval only with enabled true and dryRunOnly false. This does not bypass allowlists or executor selection. |
| `spec.allowedProviders` | *Optional* | array | Exact aws/azure/oci/gcp allowlist; default empty denies every provider. Use canonical spelling without surrounding whitespace. |
| `spec.allowedProviderRefs` | *Optional* | array | Optional exact action providerRef allowlist; empty imposes no reference restriction. Strings are not resolved as status sources or checked for profile existence by this validator. Match the actual action providerRef spelling. |
| `spec.allowedActions` | *Optional* | array | Exact canonical verb allowlist: assign-secondary-ip, unassign-secondary-ip, assign-route-table-route, unassign-route-table-route, ensure-forwarding-enabled, ensure-forwarding-disabled. Empty denies all. |
| `spec.allowedCIDRs` | *Optional* | array | Optional IPv4/IPv6 CIDRs. Empty adds no CIDR gate; nonempty requires target.address to exist and its IP (including the host part of CIDR input) to fall in any allowed prefix. Not a check of every address an executor might change. |
| `spec.maxActionsPerRun` | *Optional* | integer | Nonnegative integer, default 0 denies execution. Positive value caps candidate selection per automatic controller run; single-action engine calls check only that it is positive. Not a lifetime or billing budget. |
| `spec.allowUndo` | *Optional* | boolean | Default false; true permits best-effort rollback subject to available undo data and policy checks. |
| `spec.executionWindow` | *Optional* | string | Optional free-form string, default empty. Currently neither parsed as a schedule nor enforced at runtime; does not restrict execution time. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go) · [pkg/controller/provideraction/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/provideraction/controller.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.
