# FirewallEventLog

[Resource configuration reference](../resources.md#firewalleventlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # optional; boolean
    events: # optional; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # optional; array
      - "<string>" # string
    log: # optional; object
      acceptSampleRate: 0 # optional; integer; minimum=0
      copyRange: 0 # optional; integer; minimum=0
      dropSampleRate: 0 # optional; integer; minimum=0
      reject: true # optional; boolean
    nflogGroup: 0 # optional; integer; minimum=0; maximum=65535
    path: "<string>" # optional; string
    retention: "<string>" # optional; string
    rules: # optional; array
      - "<string>" # string
    sampleRate: 0 # optional; integer; minimum=0
    sinks: # optional; array
      - "<string>" # string
    toZones: # optional; array
      - "<string>" # string
```
