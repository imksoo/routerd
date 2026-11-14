# FirewallPolicy

[各资源的配置](../resources.md#firewallpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

配置与区域及明确规则搭配的共享隐式防火墙选项。多个资源会汇总，不是独立应用。

## 指定条件与引用关系

- 没有必填 spec 字段。logDeny 在多个资源间取 OR。生成器的 sameRoleAccept 从 true 开始且没有设为 false 的路径，因此当前填 false 也无法禁用相同角色间转发。
- Linux nftables 与 FreeBSD PF 的隐式/明确规则顺序不同。PF 在已允许路径的明确拒绝前生成广泛隐式允许；Linux 在该区域配对的隐式动作前生成明确规则。不可假定明确拒绝能覆盖所有隐式、控制或状态例外。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    [ logDeny: <boolean> ]
    [ sameRoleAccept: <boolean> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.logDeny` | *选填* | boolean | 默认 false，任一资源为 true 即启用 Linux 隐式 input/forward 拒绝日志。明确 drop 仍需其 log 标志。当前 PF 不读取此选项。 |
| `spec.sameRoleAccept` | *选填* | boolean | 解码默认 false，但生成器有效默认固定 true，false 无效。Linux 包含 untrust 对 untrust；PF 不生成其广泛隐式转发允许。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
