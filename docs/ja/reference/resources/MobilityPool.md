# MobilityPool

[リソース別の設定](../resources.md#mobilitypool) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必須; string
    members: # 任意; array
      - # object
        capture: # 任意; object
          activeWhen: # 任意; object
            type: "" # 任意; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # 任意; string
          captureStrategy: "" # 任意; string; enum=["", "route-table"]
          excludeAddresses: # 任意; array
            - "<string>" # string
          gratuitousARP: true # 任意; boolean
          interface: "<string>" # 任意; string
          nicRef: "<string>" # 任意; string
          providerRef: "<string>" # 任意; string
          sourceAddress: "<string>" # 任意; string
          sourceAddressFrom: # 任意; object
            field: "<string>" # 任意; string
            optional: true # 任意; boolean
            resource: "<string>" # 必須; string
          target: # 任意; object
            "<key>": "<string>" # 任意; string
          targetFrom: # 任意; object
            "<key>": "<string>" # 任意; string
          type: "provider-secondary-ip" # 任意; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # 必須; string
        ownershipDiscovery: # 任意; object
          allowEmptyAfter: "<string>" # 任意; string
          leaseTTL: "<string>" # 任意; string
          mode: "" # 任意; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # 任意; string
          providerRef: "<string>" # 任意; string
          scanInterval: "<string>" # 任意; string
          scope: # 任意; object
            excludeAddresses: # 任意; array
              - "<string>" # string
            includeAddresses: # 任意; array
              - "<string>" # string
          selector: # 任意; object
            tags: # 任意; object
              "<key>": "<string>" # 任意; string
          sources: # 任意; array
            - # object
              bridge: "<string>" # 任意; string
              interface: "<string>" # 任意; string
              leaseTTL: "<string>" # 任意; string
              network: "<string>" # 任意; string
              probeRetries: 0 # 任意; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # 任意; string
              resource: "<string>" # 任意; string
              scanInterval: "<string>" # 任意; string
              sourceAddressFrom: # 任意; object
                field: "<string>" # 任意; string
                optional: true # 任意; boolean
                resource: "<string>" # 必須; string
              type: "dhcpv4-lease" # 必須; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # 任意; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # 任意; string
          subnetRefFrom: "<string>" # 任意; string
        profileRef: "<string>" # 任意; string
        staticOwnedAddresses: # 任意; array
          - "<string>" # string
    membersFrom: # 必須; array; minItems=1
      - # object
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    prefix: "<string>" # 必須; string
    profiles: # 任意; object
      cloudCaptures: # 任意; object
        "<key>": # 任意; object
          capture: # 任意; object
            activeWhen: # 任意; object
              type: "" # 任意; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # 任意; string
            captureStrategy: "" # 任意; string; enum=["", "route-table"]
            excludeAddresses: # 任意; array
              - "<string>" # string
            gratuitousARP: true # 任意; boolean
            interface: "<string>" # 任意; string
            nicRef: "<string>" # 任意; string
            providerRef: "<string>" # 任意; string
            sourceAddress: "<string>" # 任意; string
            sourceAddressFrom: # 任意; object
              field: "<string>" # 任意; string
              optional: true # 任意; boolean
              resource: "<string>" # 必須; string
            target: # 任意; object
              "<key>": "<string>" # 任意; string
            targetFrom: # 任意; object
              "<key>": "<string>" # 任意; string
            type: "provider-secondary-ip" # 任意; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # 任意; object
            allowEmptyAfter: "<string>" # 任意; string
            leaseTTL: "<string>" # 任意; string
            mode: "" # 任意; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # 任意; string
            providerRef: "<string>" # 任意; string
            scanInterval: "<string>" # 任意; string
            scope: # 任意; object
              excludeAddresses: # 任意; array
                - "<string>" # string
              includeAddresses: # 任意; array
                - "<string>" # string
            selector: # 任意; object
              tags: # 任意; object
                "<key>": "<string>" # 任意; string
            sources: # 任意; array
              - # object
                bridge: "<string>" # 任意; string
                interface: "<string>" # 任意; string
                leaseTTL: "<string>" # 任意; string
                network: "<string>" # 任意; string
                probeRetries: 0 # 任意; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # 任意; string
                resource: "<string>" # 任意; string
                scanInterval: "<string>" # 任意; string
                sourceAddressFrom: # 任意; object
                  field: "<string>" # 任意; string
                  optional: true # 任意; boolean
                  resource: "<string>" # 必須; string
                type: "dhcpv4-lease" # 必須; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # 任意; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # 任意; string
            subnetRefFrom: "<string>" # 任意; string
    staticHandovers: # 任意; array
      - # object
        address: "<string>" # 必須; string
        fromNodeRef: "<string>" # 必須; string
        toNodeRef: "<string>" # 必須; string
    values: # 任意; object
      "<key>": "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
