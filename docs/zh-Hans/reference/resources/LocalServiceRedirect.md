# LocalServiceRedirect

[各资源的配置](../resources.md#localserviceredirect) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

以 nftables prerouting NAT 将指定接口收到的匹配流量重定向至路由器本机服务端口。

## 指定条件与引用关系

- interface 与非空 rules 必填。每条规则都需要 protocols、destinationSetRef 及两个端口。规则内条件为 AND，协议选项为 OR。
- 匹配接收接口、目标地址集合及目标端口。影响接收数据包，不处理本机发起的 OUTPUT 流量。目标是本机服务，不是远端后端；其他用途请用 PortForward/IngressService。
- 本机监听服务与防火墙许可须另行配置。重定向不启动 DNSResolver 或其他程序，也不新增通用允许规则。按引用集合的地址族生成 IPv4/IPv6 规则；DNS 集合由 IPAddressSet 更新。此实现生成 nftables 规则，不生成 FreeBSD PF 重定向。
- 规则按资源 ID 再按名称排序，不按 YAML 列表顺序。省略名称时使用从 0 开始的十进制索引字符串，字典序可能使 10 在 2 前面。条件重叠时请明确命名以控制顺序。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: "<name>"
  spec:
    interface: <string>
    rules:
      -
        destinationPort: <integer>
        destinationSetRef: <string>
        [ name: <string> ]
        protocols:
          - <string>
        redirectPort: <integer>
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.interface` | **必填** | string | 必填，同一 Router 声明的逻辑接口，解析为 OS 接收接口名称。无默认值或状态来源。 |
| `spec.rules` | **必填** | array | 必填且非空的重定向列表。协议、地址集合及端口不从其他规则继承。 |
| `spec.rules[].name` | *选填* | string | 可选的排序／注释标签，默认为从 0 开始的十进制索引。不是资源引用，此处不拒绝重复名称。 |
| `spec.rules[].protocols` | **必填** | array | 必填且非空的 tcp/udp 子集，拒绝重复或其他协议。不会默认同时启用两者。 |
| `spec.rules[].destinationSetRef` | **必填** | string | 必填，同一 Router 声明的 IPAddressSet 裸名称或 IPAddressSet/name。不支持字段选择或内嵌地址列表。 |
| `spec.rules[].destinationPort` | **必填** | integer; minimum=1; maximum=65535 | 必填的原目标端口 1–65535，无范围语法或默认值。 |
| `spec.rules[].redirectPort` | **必填** | integer; minimum=1; maximum=65535 | 必填的本机服务端口 1–65535。重定向后需有可连接的监听服务，YAML 验证不检查该服务。 |
| `spec.when` | *选填* | object | 可选的通用启用条件，默认无条件。state/all/any 与引用限制请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
