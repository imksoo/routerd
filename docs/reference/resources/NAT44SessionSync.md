# NAT44SessionSync

[Resource configuration reference](../resources.md#nat44sessionsync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    conntrackCommand: "<string>" # optional; string
    conntrackd: # optional; object
      ignoreIPv4: # optional; array
        - "<string>" # string
      interface: "<string>" # Required; string
      localAddress: "<string>" # Required; string
      peerAddress: "<string>" # Required; string
      port: 1 # optional; integer; minimum=1; maximum=65535
    excludeNatRules: # optional; array
      - "<string>" # string
    interval: "<string>" # optional; string
    mode: "event-stream" # optional; string; enum=["event-stream", "conntrackd"]
    natRules: # optional; array
      - "<string>" # string
    snatAddresses: # optional; array
      - "<string>" # string
    targets: # optional; array
      - # object
        host: "<string>" # Required; string
        name: "<string>" # optional; string
        restoreCommand: # optional; array
          - "<string>" # string
        sshOptions: # optional; array
          - "<string>" # string
        user: "<string>" # optional; string
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
