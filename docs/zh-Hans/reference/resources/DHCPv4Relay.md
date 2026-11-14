# DHCPv4Relay

[各资源的配置](../resources.md#dhcpv4relay) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

通过 dnsmasq 将 DHCPv4 请求转发至明确指定的一个 IPv4 服务器。

## 指定条件与引用关系

- 至少需要一个已声明接口与纯 IPv4 上游地址，不提供自动发现、上游列表、各接口中继源地址或条件字段。
- Linux 与 FreeBSD 均在共用 dnsmasq 进程为各接口生成 dhcp-relay=0.0.0.0,<upstream>,<OS名称>。主机路由须能到达上游。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces:
      - <string>
    upstream: <string>
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.interfaces` | **必填** | array | 必填且不可为空的逻辑接口列表，各名称须存在于同一 Router 的接口索引。运行时解析 Interface.ifname 与支持的生成接口别名，无默认值。 |
| `spec.upstream` | **必填** | string | 必填的 DHCP 服务器纯 IPv4 地址，不含端口、CIDR 或资源引用，无默认值。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`
