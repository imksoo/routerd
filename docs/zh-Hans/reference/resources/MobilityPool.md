# MobilityPool

[各资源的配置](../resources.md#mobilitypool) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    members: # 选填; array
      - # object
        capture: # 选填; object
          activeWhen: # 选填; object
            type: "" # 选填; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # 选填; string
          captureStrategy: "" # 选填; string; enum=["", "route-table"]
          excludeAddresses: # 选填; array
            - "<string>" # string
          gratuitousARP: true # 选填; boolean
          interface: "<string>" # 选填; string
          nicRef: "<string>" # 选填; string
          providerRef: "<string>" # 选填; string
          sourceAddress: "<string>" # 选填; string
          sourceAddressFrom: # 选填; object
            field: "<string>" # 选填; string
            optional: true # 选填; boolean
            resource: "<string>" # 必填; string
          target: # 选填; object
            "<key>": "<string>" # 选填; string
          targetFrom: # 选填; object
            "<key>": "<string>" # 选填; string
          type: "provider-secondary-ip" # 选填; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # 必填; string
        ownershipDiscovery: # 选填; object
          allowEmptyAfter: "<string>" # 选填; string
          leaseTTL: "<string>" # 选填; string
          mode: "" # 选填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # 选填; string
          providerRef: "<string>" # 选填; string
          scanInterval: "<string>" # 选填; string
          scope: # 选填; object
            excludeAddresses: # 选填; array
              - "<string>" # string
            includeAddresses: # 选填; array
              - "<string>" # string
          selector: # 选填; object
            tags: # 选填; object
              "<key>": "<string>" # 选填; string
          sources: # 选填; array
            - # object
              bridge: "<string>" # 选填; string
              interface: "<string>" # 选填; string
              leaseTTL: "<string>" # 选填; string
              network: "<string>" # 选填; string
              probeRetries: 0 # 选填; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # 选填; string
              resource: "<string>" # 选填; string
              scanInterval: "<string>" # 选填; string
              sourceAddressFrom: # 选填; object
                field: "<string>" # 选填; string
                optional: true # 选填; boolean
                resource: "<string>" # 必填; string
              type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # 选填; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # 选填; string
          subnetRefFrom: "<string>" # 选填; string
        profileRef: "<string>" # 选填; string
        staticOwnedAddresses: # 选填; array
          - "<string>" # string
    membersFrom: # 必填; array; minItems=1
      - # object
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    prefix: "<string>" # 必填; string
    profiles: # 选填; object
      cloudCaptures: # 选填; object
        "<key>": # 选填; object
          capture: # 选填; object
            activeWhen: # 选填; object
              type: "" # 选填; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # 选填; string
            captureStrategy: "" # 选填; string; enum=["", "route-table"]
            excludeAddresses: # 选填; array
              - "<string>" # string
            gratuitousARP: true # 选填; boolean
            interface: "<string>" # 选填; string
            nicRef: "<string>" # 选填; string
            providerRef: "<string>" # 选填; string
            sourceAddress: "<string>" # 选填; string
            sourceAddressFrom: # 选填; object
              field: "<string>" # 选填; string
              optional: true # 选填; boolean
              resource: "<string>" # 必填; string
            target: # 选填; object
              "<key>": "<string>" # 选填; string
            targetFrom: # 选填; object
              "<key>": "<string>" # 选填; string
            type: "provider-secondary-ip" # 选填; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # 选填; object
            allowEmptyAfter: "<string>" # 选填; string
            leaseTTL: "<string>" # 选填; string
            mode: "" # 选填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # 选填; string
            providerRef: "<string>" # 选填; string
            scanInterval: "<string>" # 选填; string
            scope: # 选填; object
              excludeAddresses: # 选填; array
                - "<string>" # string
              includeAddresses: # 选填; array
                - "<string>" # string
            selector: # 选填; object
              tags: # 选填; object
                "<key>": "<string>" # 选填; string
            sources: # 选填; array
              - # object
                bridge: "<string>" # 选填; string
                interface: "<string>" # 选填; string
                leaseTTL: "<string>" # 选填; string
                network: "<string>" # 选填; string
                probeRetries: 0 # 选填; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # 选填; string
                resource: "<string>" # 选填; string
                scanInterval: "<string>" # 选填; string
                sourceAddressFrom: # 选填; object
                  field: "<string>" # 选填; string
                  optional: true # 选填; boolean
                  resource: "<string>" # 必填; string
                type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # 选填; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # 选填; string
            subnetRefFrom: "<string>" # 选填; string
    staticHandovers: # 选填; array
      - # object
        address: "<string>" # 必填; string
        fromNodeRef: "<string>" # 必填; string
        toNodeRef: "<string>" # 必填; string
    values: # 选填; object
      "<key>": "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
