# EgressRoutePolicy

[リソース別の設定](../resources.md#egressroutepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # 必須; array
      - # object
        dependsOn: # 任意; array
          - # object
            equals: "<string>" # 任意; string
            field: "<string>" # 任意; string
            notEmpty: true # 任意; boolean
            optional: true # 任意; boolean
            phase: "<string>" # 任意; string
            resource: "<string>" # 必須; string
        device: "<string>" # 任意; string
        deviceFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        enabled: true # 任意; boolean
        gateway: "<string>" # 任意; string
        gatewayFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        gatewaySource: "" # 任意; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # 任意; string
        interface: "<string>" # 任意; string
        mark: 0 # 任意; integer; minimum=0; maximum=4294967295
        metric: 0 # 任意; integer; minimum=0
        name: "<string>" # 任意; string
        preferredSource: "" # 任意; string; enum=["", "interface"]
        priority: 0 # 任意; integer; minimum=0; maximum=32765
        routeMetric: 0 # 任意; integer; minimum=0
        routeTable: 0 # 任意; integer; minimum=0; maximum=4294967295
        source: "<string>" # 任意; string
        table: 0 # 任意; integer; minimum=0; maximum=4294967295
        targets: # 任意; array
          - # object
            gateway: "<string>" # 任意; string
            gatewayFrom: # 任意; object
              field: "<string>" # 任意; string
              optional: true # 任意; boolean
              resource: "<string>" # 必須; string
            gatewaySource: "" # 任意; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # 任意; string
            interface: "<string>" # 任意; string
            mark: 0 # 任意; integer; minimum=0; maximum=4294967295
            metric: 0 # 任意; integer; minimum=0
            name: "<string>" # 任意; string
            outboundInterface: "<string>" # 任意; string
            priority: 0 # 任意; integer; minimum=0; maximum=32765
            routeMetric: 0 # 任意; integer; minimum=0
            routeTable: 0 # 任意; integer; minimum=0; maximum=4294967295
            table: 0 # 任意; integer; minimum=0; maximum=4294967295
        weight: 0 # 任意; integer; minimum=0
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
    destinationCIDRs: # 任意; array
      - "<string>" # string
    destinationSetRefs: # 任意; array
      - "<string>" # string
    excludeDestinationCIDRs: # 任意; array
      - "<string>" # string
    excludeDestinationSetRefs: # 任意; array
      - "<string>" # string
    family: "ipv4" # 任意; string; enum=["ipv4", "ipv6"]
    hashFields: # 任意; array
      - "<string>" # string
    hostTraffic: true # 任意; boolean
    hysteresis: "<string>" # 任意; string
    mode: "priority" # 任意; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # 任意; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # 任意; array
      - "<string>" # string
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

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `family`, `selectedCandidate`, `selectedSource`, `selectedDevice`, `selectedGateway`, `selectedGatewaySource`, `selectedRouteTable`, `selectedMetric`, `selectedWeight`, `selectedTargets`, `selectedInterface`, `candidates`, `role`, `advisory`, `lastTransitionAt`, `datapathRoleResource`, `datapathRole`, `datapathRoleTransitionAt`, `datapathReady`, `datapathState`, `datapathReadyAt`, `datapathReadyDuration`, `datapathReadyDurationMillis`, `datapathFallbackBehavior`, `newFlowBehavior`, `updatedAt`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
