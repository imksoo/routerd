# HealthCheck

[Resource configuration reference](../resources.md#healthcheck) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # optional; string; enum=["ipv4", "ipv6"]
    enabled: true # optional; boolean
    healthyThreshold: 1 # optional; integer; minimum=1
    interface: "<string>" # optional; string
    interval: "<string>" # optional; string
    port: 0 # optional; integer; minimum=0; maximum=65535
    protocol: "" # optional; string; enum=["", "icmp", "tcp", "dns", "http"]
    role: "link" # optional; string; enum=["link", "next-hop", "internet", "service", "policy"]
    target: "<string>" # optional; string
    targetSource: "auto" # optional; string; enum=["auto", "static", "defaultGateway", "dsliteRemote"]
    timeout: "<string>" # optional; string
    type: "ping" # optional; string; enum=["ping"]
    unhealthyThreshold: 1 # optional; integer; minimum=1
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

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
