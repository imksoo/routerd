# Interface

[各资源的配置](../resources.md#interface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

为现有OS接口提供metadata.name逻辑名称，供其他资源引用，不创建物理接口。

## 指定条件与引用关系

- 依赖资源的interface使用metadata.name逻辑名称，ifname填写实际设备名。managed=false不会抑制已要求的LinkController启用/MTU变更。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    adminUp: true # 选填; boolean
    ifname: "<string>" # 必填; string
    managed: true # 必填; boolean
    mtu: 576 # 选填; integer; minimum=576; maximum=9216
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

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.ifname` | 必填现有内核设备名，例如ens18，无默认值；不存在时不创建，而记为Down/InterfaceNotFound。 |
| `spec.managed` | 管理声明，解码默认false，但JSON Schema要求显式填写。影响管理/观察信息及周边处理，不保证adminUp/MTU仅观察不变更。 |
| `spec.adminUp` | 默认false，不要求启用；true会启用已存在但关闭的link。false不表示强制关闭。 |
| `spec.mtu` | 默认0保留当前MTU；非零值须为576–9216，不同时应用。 |
| `spec.when` | 启用条件，省略不设限制；state/all/any、unknown及时间条件请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `ifname`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `primaryIPv4`, `primaryIPv6`, `macAddress`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
