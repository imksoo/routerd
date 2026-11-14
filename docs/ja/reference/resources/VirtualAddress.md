# VirtualAddress

[リソース別の設定](../resources.md#virtualaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必須; string
    addressFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    externalDNS: true # 任意; boolean
    family: "ipv4" # 必須; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # 任意; boolean
    hostname: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    mode: "" # 任意; string; enum=["", "static", "vrrp"]
    track: # 任意; array
      - # object
        confirmConsecutiveHealthy: 1 # 任意; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # 任意; integer; minimum=1; maximum=255
        resource: "<string>" # 必須; string
        unhealthyPenalty: 0 # 任意; integer; minimum=0; maximum=254
    vrrp: # 任意; object
      additionalFailoverVMACs: # 任意; array
        - # object
          interface: "<string>" # 必須; string
          linkLocalAddress: "<string>" # 任意; string
          macAddress: "<string>" # 必須; string
          parentInterface: "<string>" # 必須; string
          withdrawRouterAdvertisement: true # 任意; boolean
      authentication: "<string>" # 任意; string
      authenticationFrom: # 任意; object
        base64: true # 任意; boolean
        env: "<string>" # 任意; string
        file: "<string>" # 任意; string
      failoverVMAC: # 任意; object
        interface: "<string>" # 必須; string
        linkLocalAddress: "<string>" # 任意; string
        macAddress: "<string>" # 必須; string
        parentInterface: "<string>" # 必須; string
        withdrawRouterAdvertisement: true # 任意; boolean
      gracefulActivation: # 任意; object
        readyWhen: # 必須; object
          state: # 任意; object
            "<key>": # 任意; object
              contains: "<string>" # 任意; string
              equals: "<string>" # 任意; string
              exists: true # 任意; boolean
              for: "<string>" # 任意; string
              in: # 任意; array
                - "<string>" # string
              maxAge: "<string>" # 任意; string
              status: "set" # 任意; string; enum=["set", "unset", "unknown"]
        timeout: "<string>" # 任意; string
      peers: # 任意; array
        - "<string>" # string
      preempt: true # 任意; boolean
      priority: 1 # 任意; integer; minimum=1; maximum=254
      virtualRouterID: 1 # 必須; integer; minimum=1; maximum=255
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
