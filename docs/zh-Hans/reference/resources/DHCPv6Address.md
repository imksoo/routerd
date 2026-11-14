# DHCPv6Address

[各资源的配置](../resources.md#dhcpv6address) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

声明主机DHCPv6 client获取的IPv6地址，与DHCPv6PrefixDelegation不同，不分配LAN前缀。

## 指定条件与引用关系

- 当前用于声明验证/索引、观察与主机网络接管判断，不生成专属DHCPv6Address daemon。计划成功不代表主机client已配置完成。
- 同接口外部PD client须与DHCPv6Address的client选择匹配。配置索引将省略视为networkd，当前observe/plan却显示dhcpcd；省略行为不一致，请显式指定client。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    required: true # 选填; boolean
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.interface` | 必填Router接口索引中的逻辑名称，不加Interface/，必须在声明配置存在，无默认值。 |
| `spec.client` | 主机DHCPv6 client/程序选择，解码默认空。接管判断识别networkd，其他显式名称会禁用该接口的networkd DHCPv6。验证器不限制已知枚举，不是资源名引用。 |
| `spec.required` | 默认false，可解码但当前地址观察/接管处理不使用，不增加等待地址要求。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)
