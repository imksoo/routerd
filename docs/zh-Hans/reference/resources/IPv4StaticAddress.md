# IPv4StaticAddress

[各资源的配置](../resources.md#ipv4staticaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在逻辑接口声明静态IPv4地址，由controller协调配置，不是DHCP租约。

## 指定条件与引用关系

- interface引用与address/prefix必填。同逻辑接口及掩码后网络前缀的两个静态资源，即使主机地址不同也拒绝；与相同组合的VirtualAddress重叠也拒绝。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必填; string
    allowOverlap: true # 选填; boolean
    allowOverlapReason: "<string>" # 选填; string
    exclusive: true # 选填; boolean
    interface: "<string>" # 必填; string
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
| `spec.interface` | 必填Router接口索引中的逻辑名称，不加Interface/，必须在声明配置存在，无默认值。 |
| `spec.address` | 必填含前缀长度的IPv4主机地址，例如192.0.2.1/24，无默认值或隐含/32。解析器检查前缀语法，运行时使用IPv4操作。 |
| `spec.exclusive` | 默认false。当前观察器警告exclusive清理未实现，运行时地址controller也不读取此标记。额外地址的整体协调独立，false不保证保留手动地址。 |
| `spec.allowOverlap` | 默认false，保留供解码但当前验证与地址协调不使用，不会跳过重复/冲突检查。 |
| `spec.allowOverlapReason` | 默认空，仅保留声明，不授权重叠或改变allowOverlap效果。 |
| `spec.when` | 启用条件，省略不设限制；state/all/any、unknown及时间条件请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)
