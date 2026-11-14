# PortForward

[各资源的配置](../resources.md#portforward) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

将 TCP/UDP 端口转发至单一 IPv4 端点，可选内部回送 NAT。Linux 生成 DNAT 与转发允许，FreeBSD 使用 rdr pass。

## 指定条件与引用关系

- listen/target 必填。target 需要且只能提供 address/addressFrom 其中之一与 port；固定目标必须是纯 IPv4，不接受主机名或 CIDR。listen 两者均省略时匹配接口所有目标。
- 引用须存在并提供对应字段，但 NAT 生成器仅读声明的 IPv4StaticAddress.address/VirtualAddress.address。optional=true 可在支持来源未解析时省略规则，不会启用任意动态来源。PortForward 没有后端健康检查。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    [ hairpin:
        [ enabled: <boolean> ]
        [ interfaces:
            - <string>
        ]
        [ mode: ("" | "auto" | "manual" | "off") ]
    ]
    listen:
      [ address: <string> ]
      [ addressFrom:
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
      ]
      interface: <string>
      port: <integer>
      protocol: ("tcp" | "udp")
    target:
      [ address: <string> ]
      [ addressFrom:
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
      ]
      port: <integer>
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
| `spec.listen` | **必填** | object | 必填 IPv4 入口端点，创建 NAT/防火墙规则，不创建应用程序监听或分配地址。 |
| `spec.listen.interface` | **必填** | string | 必填单独已声明逻辑接口名称，不是 Kind/name。须存在配置索引并可解析 OS ifname。 |
| `spec.listen.address` | *选填* | string | 可选纯 IPv4 目标地址，与 addressFrom 互斥。省略时匹配监听接口上任意目标地址。 |
| `spec.listen.addressFrom` | *选填* | object | 可选通用 resource/field/optional 引用，与 address 互斥。NAT/静态生成支持声明的 IPv4StaticAddress.address 或 VirtualAddress.address，不读任意运行时状态。普通引用验证通过不保证生成器支持。 |
| `spec.listen.protocol` | **必填** | string; enum=["tcp", "udp"] | 必填 tcp/udp，无默认值。选择转发流量，不决定健康检查协议。 |
| `spec.listen.port` | **必填** | integer; minimum=1; maximum=65535 | 必填目标端口 1–65535，无范围、列表或默认值。 |
| `spec.hairpin` | *选填* | object | 可选经公开地址返回内部的 NAT 配置，启用时需可解析 listen 地址。PortForward 默认 off，IngressService 默认 auto。 |
| `spec.hairpin.enabled` | *选填* | boolean | 默认 false。mode 省略时 true 让 PortForward 使用 manual，IngressService 仍默认 auto。明确 off 即使 enabled=true 也禁用。 |
| `spec.hairpin.mode` | *选填* | string; enum=["", "auto", "manual", "off"] | auto/manual/off。auto 在 listen 与后端同属声明 IPv4 子网或相同私有 /24 时选 listen 接口，不发现任意 LAN 路由。manual 需 enabled=true 与 interfaces 才生成规则。 |
| `spec.hairpin.interfaces` | *选填* | array | 可选单独声明逻辑接口名，不可重复，启用 manual 时需非空。auto 可另加入 listen 接口。IPv4 DNAT 加 masquerade/PF SNAT 让回程经路由器。 |
| `spec.when` | *选填* | object | 可选通用启用条件，默认无条件。不成立时从有效生成排除；条件与模式限制请见通用字段。 |
| `spec.target` | **必填** | object | 必填单一内部端点，不分配 IP 或启动服务。 |
| `spec.target.address` | *选填* | string | 纯 IPv4 目标，与 target.addressFrom 必须择一，无默认。 |
| `spec.target.addressFrom` | *选填* | object | 通用 resource/field/optional 引用，与 listen.addressFrom 相同声明字段限制，须解析为 IPv4。 |
| `spec.target.port` | **必填** | integer; minimum=1; maximum=65535 | 必填内部 TCP/UDP 端口 1–65535，独立于 listen.port。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `listenAddress`, `target`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)
