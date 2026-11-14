# VirtualAddress

[各资源的配置](../resources.md#virtualaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必填; string
    addressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    externalDNS: true # 选填; boolean
    family: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # 选填; boolean
    hostname: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    mode: "" # 选填; string; enum=["", "static", "vrrp"]
    track: # 选填; array
      - # object
        confirmConsecutiveHealthy: 1 # 选填; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # 选填; integer; minimum=1; maximum=255
        resource: "<string>" # 必填; string
        unhealthyPenalty: 0 # 选填; integer; minimum=0; maximum=254
    vrrp: # 选填; object
      additionalFailoverVMACs: # 选填; array
        - # object
          interface: "<string>" # 必填; string
          linkLocalAddress: "<string>" # 选填; string
          macAddress: "<string>" # 必填; string
          parentInterface: "<string>" # 必填; string
          withdrawRouterAdvertisement: true # 选填; boolean
      authentication: "<string>" # 选填; string
      authenticationFrom: # 选填; object
        base64: true # 选填; boolean
        env: "<string>" # 选填; string
        file: "<string>" # 选填; string
      failoverVMAC: # 选填; object
        interface: "<string>" # 必填; string
        linkLocalAddress: "<string>" # 选填; string
        macAddress: "<string>" # 必填; string
        parentInterface: "<string>" # 必填; string
        withdrawRouterAdvertisement: true # 选填; boolean
      gracefulActivation: # 选填; object
        readyWhen: # 必填; object
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
        timeout: "<string>" # 选填; string
      peers: # 选填; array
        - "<string>" # string
      preempt: true # 选填; boolean
      priority: 1 # 选填; integer; minimum=1; maximum=254
      virtualRouterID: 1 # 必填; integer; minimum=1; maximum=255
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

`phase`, `address`, `ifname`, `role`, `hostname`, `virtualRouterID`, `priority`, `failoverVMACs`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
