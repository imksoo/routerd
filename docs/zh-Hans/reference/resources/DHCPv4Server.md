# DHCPv4Server

[各资源的配置](../resources.md#dhcpv4server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

通过当前共用 dnsmasq 服务提供 IPv4 DHCP 地址池、带选择条件的范围与保留。

## 指定条件与引用关系

- 当前实时控制器请用 interface/addressPool 与 gateway/gatewayFrom、dnsServers/dnsServerFrom、ntpServers/ntpServerFrom。静态渲染与实时写入行为不同，旧 routerSource/dnsSource、内嵌 dns、authoritative 不应视为可用实时控制。
- 有效地址池需有现有逻辑接口及有效 IPv4 起止地址。会检查 start <= end，但验证不确立子网成员关系、地址池重叠或所提供网关的可达性。
- 通用值来源须有现有资源与明确提供字段。gateway/DNS/NTP/domain 必要来源 optional false 缺值时，即使有固定替代值仍暂停地址池；optional true 跳过缺值。动态地址读取器不对所有状态值强制 IPv4。
- 范围使用 MAC/OUI 替代条件及本地 profileRef 或内嵌配置，不能并用。配置档与范围需有效起止值。动态租约符合选择器但不在任何匹配地址池时，应用会移除并重启 dnsmasq，保留明确保留；不强制客户端立即更新。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    [ addressPool:
        [ end: <string> ]
        [ leaseTime: <string> ]
        [ start: <string> ]
    ]
    [ authoritative: <boolean> ]
    [ dns:
        [ cacheSize: <integer> ]
        [ enabled: <boolean> ]
        [ upstreamInterface: <string> ]
        [ upstreamServers:
            - <string>
        ]
        [ upstreamSource: ("dhcpv4" | "static" | "system" | "none") ]
    ]
    [ dnsInterface: <string> ]
    [ dnsServerFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ dnsServers:
        - <string>
    ]
    [ dnsSource: ("dhcpv4" | "static" | "self" | "none") ]
    [ domain: <string> ]
    [ domainFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ gateway: <string> ]
    [ gatewayFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ interface: <string> ]
    [ leaseFile: <string> ]
    [ leaseTime: <string> ]
    [ listenInterfaces:
        - <string>
    ]
    [ logDHCP: <boolean> ]
    [ managed: <boolean> ]
    [ ntpServerFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ ntpServers:
        - <string>
    ]
    [ options:
        -
          [ code: <integer> ]
          [ name: <string> ]
          [ value: <string> ]
    ]
    [ profiles:
        -
          addressPool:
            [ end: <string> ]
            [ leaseTime: <string> ]
            [ start: <string> ]
          [ dnsServers:
              - <string>
          ]
          [ gateway: <string> ]
          name: <string>
          [ netmask: <string> ]
          [ ntpServers:
              - <string>
          ]
          [ options:
              -
                [ code: <integer> ]
                [ name: <string> ]
                [ value: <string> ]
          ]
    ]
    [ rangeEnd: <string> ]
    [ rangeStart: <string> ]
    [ role: ("server" | "transit") ]
    [ router: <string> ]
    [ routerSource: ("interfaceAddress" | "static" | "none") ]
    [ scopes:
        -
          addressPool:
            [ end: <string> ]
            [ leaseTime: <string> ]
            [ start: <string> ]
          [ dnsServers:
              - <string>
          ]
          [ gateway: <string> ]
          match:
            [ macAddresses:
                - <string>
            ]
            [ ouiPrefixes:
                - <string>
            ]
          name: <string>
          [ netmask: <string> ]
          [ ntpServers:
              - <string>
          ]
          [ options:
              -
                [ code: <integer> ]
                [ name: <string> ]
                [ value: <string> ]
          ]
          [ profileRef: <string> ]
    ]
    [ server: ("dnsmasq" | "kea" | "dhcpd") ]
    [ stickyHoldDays: <integer> ]
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
| `spec.server` | *选填* | string; enum=["dnsmasq", "kea", "dhcpd"] | 模式接受 dnsmasq/kea/dhcpd，规划与依赖选择默认 dnsmasq。当前实时控制器仍一律启动 dnsmasq，不能切换到 kea/dhcpd。固定保留天数仅计入 dnsmasq 服务器。 |
| `spec.managed` | *选填* | boolean | 布尔值默认 false，当前实时 DHCP 控制器不检查；false 不表示仅观察或禁用。 |
| `spec.role` | *选填* | string; enum=["server", "transit"] | 验证 server/transit，默认 server。静态渲染器区分 transit，但当前实时 LAN 生成不遵循此切换，不可用 transit 抑制分配。 |
| `spec.listenInterfaces` | *选填* | array | 可选已声明逻辑接口列表，所有名称须存在。用于验证与防火墙规划，但当前实时地址池仅使用 interface，此列表不能替代 interface。 |
| `spec.logDHCP` | *选填* | boolean | 默认 false，静态渲染可输出 log-dhcp，但当前实时写入不使用此标志，租约事件回调生成与此独立。 |
| `spec.stickyHoldDays` | *选填* | integer; minimum=0 | 非负保留天数，默认 0 禁用。正值将释放或到期的分配保存在 dhcp-sticky.db 以便重用。当前对全部 IPv4 租约事件采用 dnsmasq DHCPv4Server 中的最大值，不按接口区分；明确保留优先。 |
| `spec.interface` | **条件必填** | string | 主地址池的已声明逻辑接口。虽然模式与验证允许省略，当前实时地址池生成仍需要它，且须存在于 Router 接口索引。 |
| `spec.addressPool` | **条件必填** | object | 主 IPv4 分配池；设置 interface 时 start/end 必填，各字段优先于旧 rangeStart/rangeEnd/leaseTime。 |
| `spec.addressPool.start` | **条件必填** | string | 纯 IPv4 起始地址，包含端点；除非 rangeStart 补足，否则必填，须 <= end，不自动推导子网或地址池。 |
| `spec.addressPool.end` | **条件必填** | string | 纯 IPv4 结束地址，包含端点；除非 rangeEnd 补足，否则必填，须 >= start。 |
| `spec.addressPool.leaseTime` | *选填* | string | 后端租约时长字符串，优先级 addressPool.leaseTime → leaseTime → 12h，此资源验证不按 Go duration 解析。 |
| `spec.rangeStart` | *选填* | string | addressPool.start 的旧式替代值，默认空值；也会切换静态渲染器至旧式范围路径，建议用 addressPool。 |
| `spec.rangeEnd` | *选填* | string | addressPool.end 的旧式替代值，静态渲染器切换同 rangeStart。 |
| `spec.leaseTime` | *选填* | string | addressPool.leaseTime 为空时使用的后端租约时长，默认 12h。 |
| `spec.routerSource` | *选填* | string; enum=["interfaceAddress", "static", "none"] | 验证 interfaceAddress/static/none，static 需要 router。静态渲染默认 interfaceAddress 并读 IPv4StaticAddress，但当前实时写入忽略 routerSource/router，改用 gateway/gatewayFrom。 |
| `spec.router` | **条件必填** | string | routerSource: static 时必填 IPv4 值，仅旧静态渲染使用，当前实时网关选项路径不使用。 |
| `spec.gateway` | *选填* | string | 可选纯 IPv4 DHCP 路由器选项，默认不明确输出；有效 gatewayFrom 值优先，可并用。 |
| `spec.gatewayFrom` | *选填* | object | 可选含 resource/field 的状态来源，optional 默认 false；来源未解析时即使有 gateway 仍为 Pending。optional true 缺值时可用 gateway。 |
| `spec.dnsSource` | *选填* | string; enum=["dhcpv4", "static", "self", "none"] | 验证 dhcpv4/static/self/none，static 需要 dnsServers，dhcpv4 需要 dnsInterface。静态默认 self；当前实时写入忽略选择器并使用 dnsServers 与 dnsServerFrom。 |
| `spec.dnsInterface` | **条件必填** | string | dnsSource: dhcpv4 所需的已声明逻辑接口。即使用其他选择器，提供此值仍验证存在；当前实时写入不以此发现 DNS。 |
| `spec.dnsServers` | *选填* | array | 可选纯 IPv4 DNS 服务器列表，默认空值；与 dnsServerFrom 合并形成 DHCP 选项 6，不会启动 DNSResolver 或 DNS 监听。 |
| `spec.dnsServerFrom` | *选填* | array | 可选通用 resource/field 来源列表，optional 默认 false。必要值缺失即使有固定 DNS 仍阻挡整个服务器。运行时读状态并去除 CIDR，请用提供 IPv4 的字段。 |
| `spec.ntpServers` | *选填* | array | 可选纯 IPv4 NTP 列表，默认空；与 ntpServerFrom 合并为 DHCP 选项 42，不启动 NTPServer。 |
| `spec.ntpServerFrom` | *选填* | array | 可选通用 resource/field 来源列表，必要与可选等待条件及仅读状态同 dnsServerFrom，默认无引用。 |
| `spec.domain` | *选填* | string | 可选单一域名，不可含空白、斜线或逗号，与 domainFrom 互斥。输出 domain-name，若未明确提供选项 119/domain-search 亦输出 domain-search。 |
| `spec.domainFrom` | *选填* | object | 可选来源限 DNSZone/<name>、field: zone，资源须存在，可在状态建立前读声明 zone。optional false 缺值时等待，true 可省略；与 domain 互斥。 |
| `spec.authoritative` | *选填* | boolean | 默认 false，影响旧静态渲染及范围路径选择；当前实时写入不据此输出 dhcp-authoritative。 |
| `spec.leaseFile` | *选填* | string | 可选路径，默认 <StateDir>/dnsmasq/dnsmasq.leases，通常为 /var/lib/routerd/dnsmasq/dnsmasq.leases。共用 dnsmasq，所有 DHCPv4/6 服务器明确 leaseFile 须一致，否则运行失败。 |
| `spec.when` | *选填* | object | 可选通用状态条件，默认无条件。不成立则排除地址池并为 Pending/WhenFalse；必要动态值未解析也可另使其 Pending。 |
| `spec.dns` | *选填* | object | 旧式 dnsmasq 内嵌 DNS 配置。当前实时写入固定 port=0 与 no-resolv，无法以此启用 DNS 转发，请改用 DNSResolver/DNSForwarder/DNSUpstream；部分值仍影响验证。 |
| `spec.dns.enabled` | *选填* | boolean | 默认 false；true 在监听冲突验证中保留 TCP/UDP 53，但不实际开启 DNS 监听。 |
| `spec.dns.upstreamSource` | *选填* | string; enum=["dhcpv4", "static", "system", "none"] | 接受空值/dhcpv4/static/system/none；dhcpv4 须有 upstreamInterface，static 须有 upstreamServers，当前不影响实时 DNS 转发。 |
| `spec.dns.upstreamInterface` | **条件必填** | string | dns.upstreamSource: dhcpv4 时必须非空，当前不参与实时 DNS 解析。不同于 dnsInterface，此旧字段不对照接口索引。 |
| `spec.dns.upstreamServers` | **条件必填** | array | upstreamSource 为 static 时必填 IPv4/IPv6 固定值，默认空；会验证但当前实时 DNS 转发不使用。 |
| `spec.dns.cacheSize` | *选填* | integer; minimum=0 | 模式要求非负整数，默认 0，当前实时配置写入不使用，与 DNSResolver.cache 分开。 |
| `spec.profiles` | *选填* | array | 此服务器内可选的可重用配置，未由 scopes[].profileRef 选取即无作用，不是独立资源，默认空。 |
| `spec.profiles[].name` | **条件必填** | string | 服务器内必填且唯一的配置档名称，不可含空白或逗号，不加 Kind/name 前缀。 |
| `spec.scopes` | *选填* | array | 可选的条件专用地址池，在主通用地址池前生成；未符合选择器的客户端使用主地址池。默认空，验证不拒绝同时符合多个范围。 |
| `spec.scopes[].name` | **条件必填** | string | 服务器内必填且唯一的范围名称，不可含空白或逗号，用于生成 dnsmasq 标签，不是外部资源引用。 |
| `spec.scopes[].match` | **条件必填** | object | 必填选择器，macAddresses 或 ouiPrefixes 至少有一项。各项为替代匹配，不要求同时符合两个列表。 |
| `spec.scopes[].match.macAddresses` | *选填* | array | 可选完整 MAC 列表，由 net.ParseMAC 解析，默认空。dnsmasq 输出请用标准冒号分隔六字节格式。 |
| `spec.scopes[].match.ouiPrefixes` | *选填* | array | 可选三字节 OUI 前缀，请用 aa:bb:cc，运行时原样附加 :*:*:*。验证虽规范化其他写法，实时写入不使用该规范化结果。 |
| `spec.scopes[].profileRef` | *选填* | string | 同一服务器现有 profiles[] 的裸名，与内嵌 addressPool 字段、netmask、gateway、dnsServers、ntpServers、options 互斥；空值使用内嵌配置。 |
| `spec.profiles[].addressPool` | **条件必填** | object | 配置档与内嵌范围所需 IPv4 地址池，端点无默认值。 |
| `spec.profiles[].addressPool.start` | *选填* | string | 必填且包含端点的纯 IPv4 起始值，须 <= end，不验证地址池间重叠。 |
| `spec.profiles[].addressPool.end` | *选填* | string | 必填且包含端点的纯 IPv4 结束值，须 >= start。 |
| `spec.profiles[].addressPool.leaseTime` | *选填* | string | 可选后端租约时长，空值继承服务器有效时长（addressPool.leaseTime → leaseTime → 12h）。 |
| `spec.profiles[].netmask` | *选填* | string | 可选连续 1–32 位的点分 IPv4 掩码，空值不输出并由 dnsmasq 推导，不从其他配置档继承。 |
| `spec.profiles[].gateway` | *选填* | string | 此标签地址池的可选纯 IPv4 路由器选项，空值不复制 server.gateway。 |
| `spec.profiles[].dnsServers` | *选填* | array | 此标签地址池的可选 IPv4 DNS 列表，默认空，无动态来源字段或自动继承服务器列表。 |
| `spec.profiles[].ntpServers` | *选填* | array | 此标签地址池的可选 IPv4 NTP 列表，默认空，无动态来源字段。 |
| `spec.profiles[].options` | *选填* | array | 此地址池的可选额外选项，每项须有 code/name 其中之一及非空 value，默认空。 |
| `spec.profiles[].options[].code` | **条件必填** | integer; minimum=1; maximum=65535 | 数字选项，与 name 互斥，模式 1–65535。 |
| `spec.profiles[].options[].name` | **条件必填** | string | dnsmasq 选项名称，与 code 互斥，不可含空白或逗号。 |
| `spec.profiles[].options[].value` | *选填* | string | 必填后端格式值，不可含 CR/LF，逗号可分隔多个值。 |
| `spec.scopes[].addressPool` | **条件必填** | object | 配置档与内嵌范围所需 IPv4 地址池，端点无默认值。 |
| `spec.scopes[].addressPool.start` | *选填* | string | 必填且包含端点的纯 IPv4 起始值，须 <= end，不验证地址池间重叠。 |
| `spec.scopes[].addressPool.end` | *选填* | string | 必填且包含端点的纯 IPv4 结束值，须 >= start。 |
| `spec.scopes[].addressPool.leaseTime` | *选填* | string | 可选后端租约时长，空值继承服务器有效时长（addressPool.leaseTime → leaseTime → 12h）。 |
| `spec.scopes[].netmask` | *选填* | string | 可选连续 1–32 位的点分 IPv4 掩码，空值不输出并由 dnsmasq 推导，不从其他配置档继承。 |
| `spec.scopes[].gateway` | *选填* | string | 此标签地址池的可选纯 IPv4 路由器选项，空值不复制 server.gateway。 |
| `spec.scopes[].dnsServers` | *选填* | array | 此标签地址池的可选 IPv4 DNS 列表，默认空，无动态来源字段或自动继承服务器列表。 |
| `spec.scopes[].ntpServers` | *选填* | array | 此标签地址池的可选 IPv4 NTP 列表，默认空，无动态来源字段。 |
| `spec.scopes[].options` | *选填* | array | 此地址池的可选额外选项，每项须有 code/name 其中之一及非空 value，默认空。 |
| `spec.scopes[].options[].code` | **条件必填** | integer; minimum=1; maximum=65535 | 数字选项，与 name 互斥，模式 1–65535。 |
| `spec.scopes[].options[].name` | **条件必填** | string | dnsmasq 选项名称，与 code 互斥，不可含空白或逗号。 |
| `spec.scopes[].options[].value` | *选填* | string | 必填后端格式值，不可含 CR/LF，逗号可分隔多个值。 |
| `spec.options` | *选填* | array | 此地址池的可选额外选项，每项须有 code/name 其中之一及非空 value，默认空。 |
| `spec.options[].code` | **条件必填** | integer; minimum=1; maximum=65535 | 数字选项，与 name 互斥，模式 1–65535。 |
| `spec.options[].name` | **条件必填** | string | dnsmasq 选项名称，与 code 互斥，不可含空白或逗号。 |
| `spec.options[].value` | *选填* | string | 必填后端格式值，不可含 CR/LF，逗号可分隔多个值。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/dhcp_profile_leases.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/dhcp_profile_leases.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [cmd/routerd/runtime_services.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/runtime_services.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interface`, `dnsServers`, `ntpServers`, `domain`, `configPath`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
