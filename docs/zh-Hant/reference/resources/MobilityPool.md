# MobilityPool

[各資源的設定](../resources.md#mobilitypool) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    members: # 選填; array
      - # object
        capture: # 選填; object
          activeWhen: # 選填; object
            type: "" # 選填; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # 選填; string
          captureStrategy: "" # 選填; string; enum=["", "route-table"]
          excludeAddresses: # 選填; array
            - "<string>" # string
          gratuitousARP: true # 選填; boolean
          interface: "<string>" # 選填; string
          nicRef: "<string>" # 選填; string
          providerRef: "<string>" # 選填; string
          sourceAddress: "<string>" # 選填; string
          sourceAddressFrom: # 選填; object
            field: "<string>" # 選填; string
            optional: true # 選填; boolean
            resource: "<string>" # 必填; string
          target: # 選填; object
            "<key>": "<string>" # 選填; string
          targetFrom: # 選填; object
            "<key>": "<string>" # 選填; string
          type: "provider-secondary-ip" # 選填; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # 必填; string
        ownershipDiscovery: # 選填; object
          allowEmptyAfter: "<string>" # 選填; string
          leaseTTL: "<string>" # 選填; string
          mode: "" # 選填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # 選填; string
          providerRef: "<string>" # 選填; string
          scanInterval: "<string>" # 選填; string
          scope: # 選填; object
            excludeAddresses: # 選填; array
              - "<string>" # string
            includeAddresses: # 選填; array
              - "<string>" # string
          selector: # 選填; object
            tags: # 選填; object
              "<key>": "<string>" # 選填; string
          sources: # 選填; array
            - # object
              bridge: "<string>" # 選填; string
              interface: "<string>" # 選填; string
              leaseTTL: "<string>" # 選填; string
              network: "<string>" # 選填; string
              probeRetries: 0 # 選填; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # 選填; string
              resource: "<string>" # 選填; string
              scanInterval: "<string>" # 選填; string
              sourceAddressFrom: # 選填; object
                field: "<string>" # 選填; string
                optional: true # 選填; boolean
                resource: "<string>" # 必填; string
              type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # 選填; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # 選填; string
          subnetRefFrom: "<string>" # 選填; string
        profileRef: "<string>" # 選填; string
        staticOwnedAddresses: # 選填; array
          - "<string>" # string
    membersFrom: # 必填; array; minItems=1
      - # object
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    prefix: "<string>" # 必填; string
    profiles: # 選填; object
      cloudCaptures: # 選填; object
        "<key>": # 選填; object
          capture: # 選填; object
            activeWhen: # 選填; object
              type: "" # 選填; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # 選填; string
            captureStrategy: "" # 選填; string; enum=["", "route-table"]
            excludeAddresses: # 選填; array
              - "<string>" # string
            gratuitousARP: true # 選填; boolean
            interface: "<string>" # 選填; string
            nicRef: "<string>" # 選填; string
            providerRef: "<string>" # 選填; string
            sourceAddress: "<string>" # 選填; string
            sourceAddressFrom: # 選填; object
              field: "<string>" # 選填; string
              optional: true # 選填; boolean
              resource: "<string>" # 必填; string
            target: # 選填; object
              "<key>": "<string>" # 選填; string
            targetFrom: # 選填; object
              "<key>": "<string>" # 選填; string
            type: "provider-secondary-ip" # 選填; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # 選填; object
            allowEmptyAfter: "<string>" # 選填; string
            leaseTTL: "<string>" # 選填; string
            mode: "" # 選填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # 選填; string
            providerRef: "<string>" # 選填; string
            scanInterval: "<string>" # 選填; string
            scope: # 選填; object
              excludeAddresses: # 選填; array
                - "<string>" # string
              includeAddresses: # 選填; array
                - "<string>" # string
            selector: # 選填; object
              tags: # 選填; object
                "<key>": "<string>" # 選填; string
            sources: # 選填; array
              - # object
                bridge: "<string>" # 選填; string
                interface: "<string>" # 選填; string
                leaseTTL: "<string>" # 選填; string
                network: "<string>" # 選填; string
                probeRetries: 0 # 選填; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # 選填; string
                resource: "<string>" # 選填; string
                scanInterval: "<string>" # 選填; string
                sourceAddressFrom: # 選填; object
                  field: "<string>" # 選填; string
                  optional: true # 選填; boolean
                  resource: "<string>" # 必填; string
                type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # 選填; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # 選填; string
            subnetRefFrom: "<string>" # 選填; string
    staticHandovers: # 選填; array
      - # object
        address: "<string>" # 必填; string
        fromNodeRef: "<string>" # 必填; string
        toNodeRef: "<string>" # 必填; string
    values: # 選填; object
      "<key>": "<string>" # 選填; string
```

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `groupRef`, `prefix`, `reason`, `generatedBGPPaths`, `resolvedMemberCount`, `pendingSources`, `placementGroup`, `placementActive`, `placementActiveNode`, `providerActionPhase`, `providerActionError`, `providerActionFailedAddresses`, `providerObservationPendingAddresses`, `ownershipResolverControlPlaneOwnerTable`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
