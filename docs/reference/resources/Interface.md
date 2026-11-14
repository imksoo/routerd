# Interface

[Resource configuration reference](../resources.md#interface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Names an existing OS interface so other resources can refer to its logical metadata.name. It does not create a physical interface.

## Conditions and references

- Use the logical metadata.name in interface fields of dependent resources and put the real device name in ifname. managed=false does not suppress LinkController admin-up/MTU operations when those settings are requested.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    adminUp: true # optional; boolean
    ifname: "<string>" # Required; string
    managed: true # Required; boolean
    mtu: 576 # optional; integer; minimum=576; maximum=9216
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
| `spec.ifname` | Required existing kernel device name, e.g. ens18; no default. Missing devices produce a Down/InterfaceNotFound status rather than being created. |
| `spec.managed` | Management declaration, decoded default false; JSON Schema asks for explicit presence. It affects management/observation metadata and surrounding logic, but is not an observe-only guarantee for adminUp or MTU. |
| `spec.adminUp` | Default false: no request to bring the link up. True brings an existing down link up. False does not mean administratively force the link down. |
| `spec.mtu` | Default zero means preserve the current MTU. Explicit nonzero values must be 576–9216 and are applied when different. |
| `spec.when` | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `ifname`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `primaryIPv4`, `primaryIPv6`, `macAddress`

[Example](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
