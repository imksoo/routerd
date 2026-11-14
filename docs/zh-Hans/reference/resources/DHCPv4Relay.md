# DHCPv4Relay

[各资源的配置](../resources.md#dhcpv4relay) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

通过 dnsmasq 将 DHCPv4 请求转发至明确指定的一个 IPv4 服务器。

## 指定条件与引用关系

- 至少需要一个已声明接口与纯 IPv4 上游地址，不提供自动发现、上游列表、各接口中继源地址或条件字段。
- Linux 与 FreeBSD 均在共用 dnsmasq 进程为各接口生成 dhcp-relay=0.0.0.0,<upstream>,<OS名称>。主机路由须能到达上游。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces: # 必填; array
      - "<string>" # string
    upstream: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.interfaces` | 必填且不可为空的逻辑接口列表，各名称须存在于同一 Router 的接口索引。运行时解析 Interface.ifname 与支持的生成接口别名，无默认值。 |
| `spec.upstream` | 必填的 DHCP 服务器纯 IPv4 地址，不含端口、CIDR 或资源引用，无默认值。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`
