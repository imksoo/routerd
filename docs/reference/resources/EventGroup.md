# EventGroup

[Resource configuration reference](../resources.md#eventgroup) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # optional; object
      mode: "" # optional; string; enum=["", "hmac"]
      secretFile: "<string>" # optional; string
      secretRef: "<string>" # optional; string
    listen: # optional; object
      address: "<string>" # optional; string
      port: 0 # optional; integer; minimum=0; maximum=65535
    nodeName: "<string>" # Required; string
    peersFrom: # optional; array
      - # object
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    replayWindow: "<string>" # optional; string
    retention: # optional; object
      maxAge: "<string>" # optional; string
      maxEvents: 0 # optional; integer; minimum=0
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

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
