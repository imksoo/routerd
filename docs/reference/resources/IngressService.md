# IngressService

[Resource configuration reference](../resources.md#ingressservice) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends: # Required; array
      - # object
        address: "<string>" # optional; string
        addressFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        name: "<string>" # optional; string
        port: 1 # Required; integer; minimum=1; maximum=65535
        weight: 0 # optional; integer; minimum=0
    externalDNS: true # optional; boolean
    hairpin: # optional; object
      enabled: true # optional; boolean
      interfaces: # optional; array
        - "<string>" # string
      mode: "" # optional; string; enum=["", "auto", "manual", "off"]
    healthCheck: # optional; object
      expectedBody: "<string>" # optional; string
      expectedStatus: # optional; array
        - 0 # integer
      healthyThreshold: 1 # optional; integer; minimum=1
      host: "<string>" # optional; string
      interval: "<string>" # optional; string
      path: "<string>" # optional; string
      protocol: "" # optional; string; enum=["", "tcp", "http", "https"]
      timeout: "<string>" # optional; string
      tlsSkipVerify: true # optional; boolean
      unhealthyThreshold: 1 # optional; integer; minimum=1
    hostname: "<string>" # optional; string
    listen: # Required; object
      address: "<string>" # optional; string
      addressFrom: # optional; object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
      interface: "<string>" # Required; string
      port: 1 # Required; integer; minimum=1; maximum=65535
      protocol: "tcp" # Required; string; enum=["tcp", "udp"]
    policy: # optional; object
      onNoHealthyBackends: "" # optional; string; enum=["", "drop", "reject"]
      selection: "" # optional; string; enum=["", "failover", "sourceHash", "random"]
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

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `hostname`, `listenAddress`, `activeBackend`, `activeBackends`, `healthyBackends`, `totalBackends`, `backends`, `observedAt`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)
