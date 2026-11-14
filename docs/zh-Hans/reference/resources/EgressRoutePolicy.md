# EgressRoutePolicy

[各资源的配置](../resources.md#egressroutepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # 必填; array
      - # object
        dependsOn: # 选填; array
          - # object
            equals: "<string>" # 选填; string
            field: "<string>" # 选填; string
            notEmpty: true # 选填; boolean
            optional: true # 选填; boolean
            phase: "<string>" # 选填; string
            resource: "<string>" # 必填; string
        device: "<string>" # 选填; string
        deviceFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        enabled: true # 选填; boolean
        gateway: "<string>" # 选填; string
        gatewayFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        gatewaySource: "" # 选填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # 选填; string
        interface: "<string>" # 选填; string
        mark: 0 # 选填; integer; minimum=0; maximum=4294967295
        metric: 0 # 选填; integer; minimum=0
        name: "<string>" # 选填; string
        preferredSource: "" # 选填; string; enum=["", "interface"]
        priority: 0 # 选填; integer; minimum=0; maximum=32765
        routeMetric: 0 # 选填; integer; minimum=0
        routeTable: 0 # 选填; integer; minimum=0; maximum=4294967295
        source: "<string>" # 选填; string
        table: 0 # 选填; integer; minimum=0; maximum=4294967295
        targets: # 选填; array
          - # object
            gateway: "<string>" # 选填; string
            gatewayFrom: # 选填; object
              field: "<string>" # 选填; string
              optional: true # 选填; boolean
              resource: "<string>" # 必填; string
            gatewaySource: "" # 选填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # 选填; string
            interface: "<string>" # 选填; string
            mark: 0 # 选填; integer; minimum=0; maximum=4294967295
            metric: 0 # 选填; integer; minimum=0
            name: "<string>" # 选填; string
            outboundInterface: "<string>" # 选填; string
            priority: 0 # 选填; integer; minimum=0; maximum=32765
            routeMetric: 0 # 选填; integer; minimum=0
            routeTable: 0 # 选填; integer; minimum=0; maximum=4294967295
            table: 0 # 选填; integer; minimum=0; maximum=4294967295
        weight: 0 # 选填; integer; minimum=0
        when: # 选填; object
          state: # 选填; object
            "<key>": # 选填; object
              contains: "<string>" # 选填; string
              equals: "<string>" # 选填; string
              exists: true # 选填; boolean
              for: "<string>" # 选填; string
              in: # 选填; array
                - "<string>" # string
              maxAge: "<string>" # 选填; string
              status: "set" # 选填; string; enum=["set", "unset", "unknown"]
    destinationCIDRs: # 选填; array
      - "<string>" # string
    destinationSetRefs: # 选填; array
      - "<string>" # string
    excludeDestinationCIDRs: # 选填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 选填; array
      - "<string>" # string
    family: "ipv4" # 选填; string; enum=["ipv4", "ipv6"]
    hashFields: # 选填; array
      - "<string>" # string
    hostTraffic: true # 选填; boolean
    hysteresis: "<string>" # 选填; string
    mode: "priority" # 选填; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # 选填; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # 选填; array
      - "<string>" # string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `family`, `selectedCandidate`, `selectedSource`, `selectedDevice`, `selectedGateway`, `selectedGatewaySource`, `selectedRouteTable`, `selectedMetric`, `selectedWeight`, `selectedTargets`, `selectedInterface`, `candidates`, `role`, `advisory`, `lastTransitionAt`, `datapathRoleResource`, `datapathRole`, `datapathRoleTransitionAt`, `datapathReady`, `datapathState`, `datapathReadyAt`, `datapathReadyDuration`, `datapathReadyDurationMillis`, `datapathFallbackBehavior`, `newFlowBehavior`, `updatedAt`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
