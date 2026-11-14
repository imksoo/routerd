# PortForward

[Resource configuration reference](../resources.md#portforward) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    hairpin: # optional; object
      enabled: true # optional; boolean
      interfaces: # optional; array
        - "<string>" # string
      mode: "" # optional; string; enum=["", "auto", "manual", "off"]
    listen: # Required; object
      address: "<string>" # optional; string
      addressFrom: # optional; object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
      interface: "<string>" # Required; string
      port: 1 # Required; integer; minimum=1; maximum=65535
      protocol: "tcp" # Required; string; enum=["tcp", "udp"]
    target: # Required; object
      address: "<string>" # optional; string
      addressFrom: # optional; object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
      port: 1 # Required; integer; minimum=1; maximum=65535
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

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)
