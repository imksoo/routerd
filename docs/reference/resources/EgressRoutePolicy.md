# EgressRoutePolicy

[Resource configuration reference](../resources.md#egressroutepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # Required; array
      - # object
        dependsOn: # optional; array
          - # object
            equals: "<string>" # optional; string
            field: "<string>" # optional; string
            notEmpty: true # optional; boolean
            optional: true # optional; boolean
            phase: "<string>" # optional; string
            resource: "<string>" # Required; string
        device: "<string>" # optional; string
        deviceFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        enabled: true # optional; boolean
        gateway: "<string>" # optional; string
        gatewayFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        gatewaySource: "" # optional; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # optional; string
        interface: "<string>" # optional; string
        mark: 0 # optional; integer; minimum=0; maximum=4294967295
        metric: 0 # optional; integer; minimum=0
        name: "<string>" # optional; string
        preferredSource: "" # optional; string; enum=["", "interface"]
        priority: 0 # optional; integer; minimum=0; maximum=32765
        routeMetric: 0 # optional; integer; minimum=0
        routeTable: 0 # optional; integer; minimum=0; maximum=4294967295
        source: "<string>" # optional; string
        table: 0 # optional; integer; minimum=0; maximum=4294967295
        targets: # optional; array
          - # object
            gateway: "<string>" # optional; string
            gatewayFrom: # optional; object
              field: "<string>" # optional; string
              optional: true # optional; boolean
              resource: "<string>" # Required; string
            gatewaySource: "" # optional; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # optional; string
            interface: "<string>" # optional; string
            mark: 0 # optional; integer; minimum=0; maximum=4294967295
            metric: 0 # optional; integer; minimum=0
            name: "<string>" # optional; string
            outboundInterface: "<string>" # optional; string
            priority: 0 # optional; integer; minimum=0; maximum=32765
            routeMetric: 0 # optional; integer; minimum=0
            routeTable: 0 # optional; integer; minimum=0; maximum=4294967295
            table: 0 # optional; integer; minimum=0; maximum=4294967295
        weight: 0 # optional; integer; minimum=0
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
    destinationCIDRs: # optional; array
      - "<string>" # string
    destinationSetRefs: # optional; array
      - "<string>" # string
    excludeDestinationCIDRs: # optional; array
      - "<string>" # string
    excludeDestinationSetRefs: # optional; array
      - "<string>" # string
    family: "ipv4" # optional; string; enum=["ipv4", "ipv6"]
    hashFields: # optional; array
      - "<string>" # string
    hostTraffic: true # optional; boolean
    hysteresis: "<string>" # optional; string
    mode: "priority" # optional; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # optional; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # optional; array
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

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `family`, `selectedCandidate`, `selectedSource`, `selectedDevice`, `selectedGateway`, `selectedGatewaySource`, `selectedRouteTable`, `selectedMetric`, `selectedWeight`, `selectedTargets`, `selectedInterface`, `candidates`, `role`, `advisory`, `lastTransitionAt`, `datapathRoleResource`, `datapathRole`, `datapathRoleTransitionAt`, `datapathReady`, `datapathState`, `datapathReadyAt`, `datapathReadyDuration`, `datapathReadyDurationMillis`, `datapathFallbackBehavior`, `newFlowBehavior`, `updatedAt`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
