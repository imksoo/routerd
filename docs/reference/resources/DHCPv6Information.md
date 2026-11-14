# DHCPv6Information

[Resource configuration reference](../resources.md#dhcpv6information) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Projects AFTR, DNS, SNTP and domain-search information from the DHCPv6-PD daemon that uses the same interface. It does not start a standalone information-request client.

## Conditions and references

- A DHCPv6PrefixDelegation resource on the exact same logical interface must reach Bound before this controller reads its daemon socket. Without such a PD resource there is no source to project. Info-request is triggered on bootstrap/transition to Bound, not as a separate lease lifecycle.
- The information controller uses the declared Router rather than the when-filtered effective view and does not evaluate dependsOn. Its Ready status reflects a daemon status read, not proof of a fresh information reply. ready_when is rejected; use supported declarations with the runtime limitations below.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata:
    name: "<name>"
  spec:
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    interface: "<string>" # Required; string
    request: # optional; array
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
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.interface` | Required interface identifier matched exactly against DHCPv6PrefixDelegation.spec.interface. No default. Use the same logical interface name in both declarations. |
| `spec.request` | Requested-option declaration, default empty. The current controller does not use it: the daemon InfoRequest sends its fixed ORO for AFTR name, DNS servers, SNTP servers and domain search. |
| `spec.when` | Conditional declaration, default absent. Validated using the shared when rules, but the current information controller does not filter its own work by this field. Do not use it as an information-request stop switch. |
| `spec.dependsOn` | Dependency declaration, default empty. Reference targets/fields are validated, but this controller does not enforce these predicates before reading or requesting information. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/pdclient/client.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/client.go)
