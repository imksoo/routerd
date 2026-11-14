# ManagementAccess

[各资源的配置](../resources.md#managementaccess) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

声明应用前管理连接中断检查所检查的接口。此资源不创建管理监听端或防火墙允许规则。

## 指定条件与引用关系

- 应用前与 routerctl doctor 会执行检查。fail 结果会阻止实际应用，除非明确指定 --allow-mgmt-lockout；dry-run 仅报告。检查的是配置，不是实际管理连接。
- 每个接口必须声明为 Interface。只要存在任何 FirewallZone，每个管理接口就必须属于 role 为 trust 或 mgmt 的区域。仅有同名 Bridge/WireGuard 资源无法满足此检查。
- 未声明 ManagementAccess 时，此检查不产生结果。requireWebConsoleBound 检查明确的通配监听地址，不要求 WebConsole 存在，也不证明其地址属于所列管理接口。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ManagementAccess
  metadata:
    name: "<name>"
  spec:
    allowSourceCIDRs: # 选填; array
      - "<string>" # string
    interfaces: # 必填; array
      - "<string>" # string
    requireWebConsoleBound: true # 选填; boolean
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.interfaces` | 必填且不可为空的 Interface 名称或 Interface/name 列表。规范化后重复会被拒绝。指的是已声明资源，不是任意 OS 名称。 |
| `spec.allowSourceCIDRs` | 可选的 IPv4/IPv6 CIDR 列表，默认为空。会验证语法，但目前管理检查与防火墙生成器不使用此字段，不会强制源地址访问限制。 |
| `spec.requireWebConsoleBound` | 默认 true。启用的 WebConsole 明确监听 0.0.0.0 或 :: 时产生 fail；false 则降为 warn。多个 ManagementAccess 中任一省略或 true 都会启用要求。动态监听地址仅警告；未配置来源且省略 listenAddress 时，此处不产生结果。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/management.go](https://github.com/imksoo/routerd/blob/main/pkg/config/management.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go) · [cmd/routerctl/doctor.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interfaces`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
