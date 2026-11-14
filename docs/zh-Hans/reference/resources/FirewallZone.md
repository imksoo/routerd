# FirewallZone

[各资源的配置](../resources.md#firewallzone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

将已配置接口归入具名信任角色，用于生成防火墙规则。不创建 OS 接口或独立路由、桥接域。

## 指定条件与引用关系

- role 与至少一个接口必填，同一区域内重复的接口字符串会被拒绝。FirewallRule 使用区域 metadata.name；self 是路由器本机的特殊端点，不是普通接口区域。
- 隐式规则允许 mgmt 前往所有角色与 self，trust 前往 self 及 mgmt 以外的角色。untrust 原则拒绝，但当前共享规则允许相同角色间转发。已有连接、相关流量与控制协议例外另行评估。OS 顺序限制请见 FirewallPolicy/FirewallRule。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces:
      - <string>
    role: ("untrust" | "trust" | "mgmt")
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.role` | **必填** | string; enum=["untrust", "trust", "mgmt"] | 必填 untrust、trust 或 mgmt，无默认值。选择隐式防火墙行为，不是接口所有权或认证配置。 |
| `spec.interfaces` | **必填** | array | 必填且非空的逻辑接口引用。单独名称默认为 Interface；接受 Interface/name、PPPoESession/name、WireGuardInterface/name、DSLiteTunnel/name。必须可在配置接口/隧道索引中解析，且生成规则时有非空内核接口名称。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interfaces`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
