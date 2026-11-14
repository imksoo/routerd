# TailscaleNode

[Resource configuration reference](../resources.md#tailscalenode) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    acceptDNS: true # optional; boolean
    acceptRoutes: true # optional; boolean
    advertiseExitNode: true # optional; boolean
    advertiseRoutes: # optional; array
      - "<string>" # string
    advertiseTags: # optional; array
      - "<string>" # string
    authKey: "<string>" # optional; string
    authKeyEnv: "<string>" # optional; string
    authKeyFile: "<string>" # optional; string
    hostname: "<string>" # optional; string
    loginServer: "<string>" # optional; string
    shieldsUp: true # optional; boolean
    ssh: true # optional; boolean
    state: "" # optional; string; enum=["", "present", "absent"]
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

[Example](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)
