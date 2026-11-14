# MobilityPool

[Resource configuration reference](../resources.md#mobilitypool) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # Required; string
    members: # optional; array
      - # object
        capture: # optional; object
          activeWhen: # optional; object
            type: "" # optional; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # optional; string
          captureStrategy: "" # optional; string; enum=["", "route-table"]
          excludeAddresses: # optional; array
            - "<string>" # string
          gratuitousARP: true # optional; boolean
          interface: "<string>" # optional; string
          nicRef: "<string>" # optional; string
          providerRef: "<string>" # optional; string
          sourceAddress: "<string>" # optional; string
          sourceAddressFrom: # optional; object
            field: "<string>" # optional; string
            optional: true # optional; boolean
            resource: "<string>" # Required; string
          target: # optional; object
            "<key>": "<string>" # optional; string
          targetFrom: # optional; object
            "<key>": "<string>" # optional; string
          type: "provider-secondary-ip" # optional; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # Required; string
        ownershipDiscovery: # optional; object
          allowEmptyAfter: "<string>" # optional; string
          leaseTTL: "<string>" # optional; string
          mode: "" # optional; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # optional; string
          providerRef: "<string>" # optional; string
          scanInterval: "<string>" # optional; string
          scope: # optional; object
            excludeAddresses: # optional; array
              - "<string>" # string
            includeAddresses: # optional; array
              - "<string>" # string
          selector: # optional; object
            tags: # optional; object
              "<key>": "<string>" # optional; string
          sources: # optional; array
            - # object
              bridge: "<string>" # optional; string
              interface: "<string>" # optional; string
              leaseTTL: "<string>" # optional; string
              network: "<string>" # optional; string
              probeRetries: 0 # optional; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # optional; string
              resource: "<string>" # optional; string
              scanInterval: "<string>" # optional; string
              sourceAddressFrom: # optional; object
                field: "<string>" # optional; string
                optional: true # optional; boolean
                resource: "<string>" # Required; string
              type: "dhcpv4-lease" # Required; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # optional; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # optional; string
          subnetRefFrom: "<string>" # optional; string
        profileRef: "<string>" # optional; string
        staticOwnedAddresses: # optional; array
          - "<string>" # string
    membersFrom: # Required; array; minItems=1
      - # object
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    prefix: "<string>" # Required; string
    profiles: # optional; object
      cloudCaptures: # optional; object
        "<key>": # optional; object
          capture: # optional; object
            activeWhen: # optional; object
              type: "" # optional; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # optional; string
            captureStrategy: "" # optional; string; enum=["", "route-table"]
            excludeAddresses: # optional; array
              - "<string>" # string
            gratuitousARP: true # optional; boolean
            interface: "<string>" # optional; string
            nicRef: "<string>" # optional; string
            providerRef: "<string>" # optional; string
            sourceAddress: "<string>" # optional; string
            sourceAddressFrom: # optional; object
              field: "<string>" # optional; string
              optional: true # optional; boolean
              resource: "<string>" # Required; string
            target: # optional; object
              "<key>": "<string>" # optional; string
            targetFrom: # optional; object
              "<key>": "<string>" # optional; string
            type: "provider-secondary-ip" # optional; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # optional; object
            allowEmptyAfter: "<string>" # optional; string
            leaseTTL: "<string>" # optional; string
            mode: "" # optional; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # optional; string
            providerRef: "<string>" # optional; string
            scanInterval: "<string>" # optional; string
            scope: # optional; object
              excludeAddresses: # optional; array
                - "<string>" # string
              includeAddresses: # optional; array
                - "<string>" # string
            selector: # optional; object
              tags: # optional; object
                "<key>": "<string>" # optional; string
            sources: # optional; array
              - # object
                bridge: "<string>" # optional; string
                interface: "<string>" # optional; string
                leaseTTL: "<string>" # optional; string
                network: "<string>" # optional; string
                probeRetries: 0 # optional; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # optional; string
                resource: "<string>" # optional; string
                scanInterval: "<string>" # optional; string
                sourceAddressFrom: # optional; object
                  field: "<string>" # optional; string
                  optional: true # optional; boolean
                  resource: "<string>" # Required; string
                type: "dhcpv4-lease" # Required; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # optional; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # optional; string
            subnetRefFrom: "<string>" # optional; string
    staticHandovers: # optional; array
      - # object
        address: "<string>" # Required; string
        fromNodeRef: "<string>" # Required; string
        toNodeRef: "<string>" # Required; string
    values: # optional; object
      "<key>": "<string>" # optional; string
```

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `groupRef`, `prefix`, `reason`, `generatedBGPPaths`, `resolvedMemberCount`, `pendingSources`, `placementGroup`, `placementActive`, `placementActiveNode`, `providerActionPhase`, `providerActionError`, `providerActionFailedAddresses`, `providerObservationPendingAddresses`, `ownershipResolverControlPlaneOwnerTable`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
