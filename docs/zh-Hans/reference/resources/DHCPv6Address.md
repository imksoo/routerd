# DHCPv6Address

[各资源的配置](../resources.md#dhcpv6address) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

声明主机DHCPv6 client获取的IPv6地址，与DHCPv6PrefixDelegation不同，不分配LAN前缀。

## 指定条件与引用关系

- 当前用于声明验证/索引、观察与主机网络接管判断，不生成专属DHCPv6Address daemon。计划成功不代表主机client已配置完成。
- 同接口外部PD client须与DHCPv6Address的client选择匹配。配置索引将省略视为networkd，当前observe/plan却显示dhcpcd；省略行为不一致，请显式指定client。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    [ client: <string> ]
    interface: <string>
    [ required: <boolean> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.interface` | **必填** | string | 必填Router接口索引中的逻辑名称，不加Interface/，必须在声明配置存在，无默认值。 |
| `spec.client` | *选填* | string | 主机DHCPv6 client/程序选择，解码默认空。接管判断识别networkd，其他显式名称会禁用该接口的networkd DHCPv6。验证器不限制已知枚举，不是资源名引用。 |
| `spec.required` | *选填* | boolean | 默认false，可解码但当前地址观察/接管处理不使用，不增加等待地址要求。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interface`, `address`
