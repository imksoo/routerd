# IngressService

[各资源的配置](../resources.md#ingressservice) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

以后端选择与主动健康检查公开 IPv4 TCP/UDP 端点，可搭配本机 DNS 与内部回送 NAT。属于第 4 层 NAT，不是 HTTP 反向代理或 TLS 终止。

## 指定条件与引用关系

- listen 与至少一个后端必填。每个后端需且仅提供 address/addressFrom 之一与 port；固定值可为 IPv4 或主机名。数据转发仍为 IPv4。健康历史按名称索引，请使用稳定唯一名称；省略使用 backend-0、backend-1 等。
- 不论 healthCheck.interval，通常每 5 秒依次检查后端。未指定协议时默认 TCP，即使 listen 是 UDP。主机名使用字符串排序第一个 IPv4；DNS 失败可复用先前地址，但仍进行健康检查。
- NoHealthyBackends 与 policy.onNoHealthyBackends 是状态输出，不保证生成 drop/reject。没有有效后端时 NAT 控制器保留原后端定义，可能回到第一个配置的静态后端。不可依赖此配置在全部异常时可靠封锁。
- failover 按声明顺序选第一个健康后端。Linux sourceHash/random 可分散至所有健康且已解析后端，不使用权重。PF 仅生成第一个所选目标，不实现分散算法。静态生成不按健康数据库选择，可能使用第一个声明后端。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends:
      -
        [ address: <string> ]
        [ addressFrom:
            field: <string>
            [ optional: <boolean> ]
            resource: <string>
        ]
        [ name: <string> ]
        port: <integer>
        [ weight: <integer> ]
    [ externalDNS: <boolean> ]
    [ hairpin:
        [ enabled: <boolean> ]
        [ interfaces:
            - <string>
        ]
        [ mode: ("" | "auto" | "manual" | "off") ]
    ]
    [ healthCheck:
        [ expectedBody: <string> ]
        [ expectedStatus:
            - <integer>
        ]
        [ healthyThreshold: <integer> ]
        [ host: <string> ]
        [ interval: <string> ]
        [ path: <string> ]
        [ protocol: ("" | "tcp" | "http" | "https") ]
        [ timeout: <string> ]
        [ tlsSkipVerify: <boolean> ]
        [ unhealthyThreshold: <integer> ]
    ]
    [ hostname: <string> ]
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
    [ policy:
        [ onNoHealthyBackends: ("" | "drop" | "reject") ]
        [ selection: ("" | "failover" | "sourceHash" | "random") ]
    ]
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
| `spec.hostname` | *选填* | string | 可选 FQDN，用于本机 DNS 发布与状态，默认空。不作 HTTP Host 路由或健康检查 Host/SNI。发布需有 DNSResolver 服务的适当 DNSZone。 |
| `spec.externalDNS` | *选填* | boolean | 默认 false。true 跳过 routerd 自动本机名称记录与 DNS 范围警告，不更新外部 DNS 提供商。 |
| `spec.backends` | **必填** | array | 必填非空后端列表，顺序为 failover 优先顺。配置接受主机名，但静态 NAT 生成需已解析 IPv4，不自行查 DNS。 |
| `spec.backends[].name` | *选填* | string | 可选稳定健康历史键，默认 backend-<从零起算索引>。此验证不拒绝重复名称，可能混合历史，请用唯一名称。 |
| `spec.backends[].address` | *选填* | string | 纯 IPv4 或主机名，与 addressFrom 必须择一。主机名仅解析 IPv4，DNS 失败时有先前地址则保留。 |
| `spec.backends[].addressFrom` | *选填* | object | 与 address 互斥的通用来源。主动检查读运行状态，optional 不会让未解析后端变健康。静态生成仅支持上述声明地址字段。 |
| `spec.backends[].port` | **必填** | integer; minimum=1; maximum=65535 | 必填后端端口 1–65535，用于数据转换与健康检查连接。 |
| `spec.backends[].weight` | *选填* | integer; minimum=0 | 接受非负整数，默认 0；当前健康选择与 Linux/PF NAT 生成器不读取。不提供加权分散。 |
| `spec.healthCheck` | *选填* | object | 可选主动健康检查配置，省略仍启用 TCP 检查，不是禁用。 |
| `spec.healthCheck.protocol` | *选填* | string; enum=["", "tcp", "http", "https"] | tcp（默认）、http 或 https，独立于 listen.protocol。HTTP(S) 直接向已解析后端地址与端口发 GET。 |
| `spec.healthCheck.interval` | *选填* | string | 验证接受 Go 时长，但控制器不读取。实际使用链的 5 秒周期与协调事件。 |
| `spec.healthCheck.timeout` | *选填* | string | 正 Go 时长，有效默认 1s。省略/零/负值即使通过语法验证也使用 1s。适用每后端探测，不是整轮或 DNS 查询期限。 |
| `spec.healthCheck.path` | *选填* | string | HTTP(S) URL 路径，默认 /；非空须以 / 开头，TCP 不使用。 |
| `spec.healthCheck.host` | *选填* | string | 可选 HTTP Host 与 HTTPS TLS ServerName/SNI 覆盖，默认空，URL 使用解析 IP。不可含空白或控制字符。 |
| `spec.healthCheck.expectedStatus` | *选填* | array | 可选完整 HTTP 状态允许列表 100–599。省略/空接受 200–399；HTTP 客户端默认先跟随重定向再评估响应。 |
| `spec.healthCheck.tlsSkipVerify` | *选填* | boolean | 默认 false。true 仅禁用 HTTPS 健康探测的证书/主机名验证，不是数据 TLS 处理。 |
| `spec.healthCheck.expectedBody` | *选填* | string | HTTP 响应正文前 1 MiB 中区分大小写的子字符串。默认空禁用正文匹配，不是正则表达式。 |
| `spec.healthCheck.healthyThreshold` | *选填* | integer; minimum=1 | 升为健康的连续成功次数，省略/0 为 1。健康与计数从后端状态恢复。模式最小 1 与接受零默认不同。 |
| `spec.healthCheck.unhealthyThreshold` | *选填* | integer; minimum=1 | 移除已健康后端的连续失败次数，省略/0 为 1。初始未解析后端为不健康。 |
| `spec.policy` | *选填* | object | 可选选择配置，默认 failover/drop，但当前无健康动作仅报告。 |
| `spec.policy.selection` | *选填* | string; enum=["", "failover", "sourceHash", "random"] | failover（默认）、sourceHash 或 random。Linux sourceHash 哈希客户端 IPv4 源，random 以等权后端数取模。单一健康后端回退 failover；PF 一律单一目标。 |
| `spec.policy.onNoHealthyBackends` | *选填* | string; enum=["", "drop", "reject"] | 接受 drop（默认）/reject，但仅写入 status.conditions.reason，不生成明确 drop/reject；请见上述无健康后端限制。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/ingressservice/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/ingressservice/controller.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `hostname`, `listenAddress`, `activeBackend`, `activeBackends`, `healthyBackends`, `totalBackends`, `backends`, `observedAt`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)
