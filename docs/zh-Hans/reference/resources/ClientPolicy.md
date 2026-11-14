# ClientPolicy

[各资源的配置](../resources.md#clientpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

对选定或非例外客户端应用访客服务与路由目标限制。Linux 以配置的 MAC 标识，FreeBSD PF 需要明确 IP 身份。

## 指定条件与引用关系

- mode 必须为 include/exclude。include 限制所选集合，exclude 限制所选可信集合以外的客户端。Linux include 没有所选 MAC 时不生成限制；exclude 集合空时匹配所选接口全部客户端。
- Linux 分类仅使用 match.macs：guest/isolated 加入 include 集合，trusted 加入 exclude 集合。OUI、主机名、指纹选择器会验证，但防火墙生成器不使用。ipv4Reservation/ipv6Addresses 不形成 Linux 匹配身份。
- FreeBSD 拒绝顶层 MAC 与所有 match 选择器；每类改需 ipv4Reservation 或明确 ipv6Addresses。PF 使用预约的 ipAddress，不是 MAC。PF 请明确提供 interfaces；省略时不继承 trust 区域接口。
- 这是路由/本机输入限制，不是 L2 交换机端口隔离。已有状态、控制与服务允许可能先执行。PF 的 trust/mgmt-to-self 隐式允许在客户端本机限制之前，且当前本机服务限制仅使用 IPv4 身份。不可视为两个 OS 或所有地址族均一致强制。
- Linux guestEgressAllow 先于发现/拒绝规则，可覆盖它们；其余流量继续区域规则。PF IPv4 允许先于拒绝，但 IPv6 拒绝规则在全局前段，无法由之后的 IPv6 允许覆盖。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    [ classification:
        -
          [ ipv4Reservation: <string> ]
          [ ipv6Addresses:
              - <string>
          ]
          match:
            [ dhcpFingerprints:
                - <string>
            ]
            [ hostnamePatterns:
                - <string>
            ]
            [ macs:
                - <string>
            ]
            [ ouiPrefixes:
                - <string>
            ]
          mode: ("trusted" | "guest" | "isolated")
          [ name: <string> ]
    ]
    [ guestEgressAllow:
        - <string>
    ]
    [ guestEgressDeny:
        - <string>
    ]
    [ guestServices:
        - <string>
    ]
    [ interfaces:
        - <string>
    ]
    [ isolation:
        [ lanInternet: ("" | "allow" | "deny") ]
        [ lanLAN: ("" | "allow" | "deny") ]
        [ lanMgmt: ("" | "allow" | "deny") ]
        [ mDNSBroadcast: ("" | "allow" | "deny") ]
    ]
    [ macs:
        - <string>
    ]
    mode: ("include" | "exclude")
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.mode` | **必填** | string; enum=["include", "exclude"] | 必填 include/exclude，无默认值。决定身份集合为限制对象或例外。 |
| `spec.interfaces` | *选填* | array | 可选 Interface/name 或单独逻辑接口名，引用须存在且不可重复。Linux 省略时使用所有 trust 区域接口，均无则失败。PF 省略时没有适用接口限制。 |
| `spec.macs` | *选填* | array | 可选 Linux 固定 MAC 列表，默认空。此列表与 classification.match.macs 间规范化后的重复会被拒绝。FreeBSD 禁用。 |
| `spec.classification` | *选填* | array | 可选分类列表，默认空。Linux 每类需至少一个选择器，但仅 MAC 形成防火墙集合。FreeBSD 需要稳定 IP 身份并禁止选择器。 |
| `spec.classification[].name` | *选填* | string | 可选描述名称，默认空。不改变匹配优先顺序，也不创建资源标识。 |
| `spec.classification[].mode` | **条件必填** | string; enum=["trusted", "guest", "isolated"] | 必填 trusted、guest 或 isolated。include 将 guest/isolated 同样加入限制集合；exclude 将 trusted 加入例外集合。 |
| `spec.classification[].match` | **条件必填** | object | Linux 至少需要一个非空选择器列表。FreeBSD 即使提供稳定 IP 也须留空。当前防火墙不将各选择器动态解析成客户端 MAC。 |
| `spec.classification[].match.macs` | *选填* | array | 可选完整 MAC 列表，是 Linux 的有效分类器。MAC 须有效，且不可跨类别或顶层 macs 重复。 |
| `spec.classification[].match.ouiPrefixes` | *选填* | array | 接受如 18:ec:e7 的三个十六进制八位组，同类重复会被拒绝。当前不形成防火墙强制，FreeBSD 拒绝。 |
| `spec.classification[].match.hostnamePatterns` | *选填* | array | 接受非空 Go path.Match glob，不是正则表达式。当前仅验证语法，不解析为防火墙身份；FreeBSD 拒绝。 |
| `spec.classification[].match.dhcpFingerprints` | *选填* | array | 接受非空且不含空白的标记，同类不可重复。当前不形成防火墙集合；FreeBSD 拒绝。 |
| `spec.classification[].ipv4Reservation` | *选填* | string | 可选已有 DHCPv4Reservation 的单独 metadata.name，拒绝 Kind/name。PF 使用其非空 ipAddress；Linux 验证引用但不以地址匹配客户端。 |
| `spec.classification[].ipv6Addresses` | *选填* | array | 可选 PF 的纯 IPv6 地址，不接受 CIDR 或 IPv4 映射格式。所有类别间规范化后不可重复。不从 MAC/IPv4 预约推导，Linux 客户端匹配不使用。 |
| `spec.isolation` | *选填* | object | 可选 allow/deny 提示，仅 Linux 生成器读取这四个字段；PF 使用明确 guest egress 列表或默认值。不会逐类独立应用。 |
| `spec.isolation.lanInternet` | *选填* | string; enum=["", "allow", "deny"] | 默认空。Linux deny 且未明确设 guestEgressDeny 时，将拒绝集合换成 0.0.0.0/0 与 ::/0。allow/空不增加无条件互联网允许。PF 忽略。 |
| `spec.isolation.lanLAN` | *选填* | string; enum=["", "allow", "deny"] | 默认空。Linux 仅在 lanLAN 与 lanMgmt 均 allow 时移除默认私有前缀拒绝，不区分个别 LAN。明确 guestEgressDeny 覆盖默认。PF 忽略。 |
| `spec.isolation.lanMgmt` | *选填* | string; enum=["", "allow", "deny"] | 默认空，与 lanLAN 共同决定默认集合。不会独立解析管理区域目标。PF 忽略。 |
| `spec.isolation.mDNSBroadcast` | *选填* | string; enum=["", "allow", "deny"] | 默认空。Linux deny 在 guestEgressAllow 后加入转发 mDNS/SSDP 多播与 UDP 137/138 丢弃。不控制 L2 桥接或独立封锁本机服务。PF 忽略。 |
| `spec.guestServices` | *选填* | array | 允许的本机服务名称：dns、dhcp、ntp、mdns、ssdp。省略或空默认 dhcp/dns/ntp，空列表无法禁用全部例外。仅开放防火墙，不启动服务。 |
| `spec.guestEgressDeny` | *选填* | array | 可选 IPv4/IPv6 CIDR。省略或空默认 10.0.0.0/8、172.16.0.0/12、192.168.0.0/16、fc00::/7，Linux 按上述 isolation 调整。明确值替换而非追加默认；PF 空时固定使用私有前缀默认。 |
| `spec.guestEgressAllow` | *选填* | array | 可选 IPv4/IPv6 CIDR 例外，默认空。Linux 与 PF IPv4 先于拒绝评估，PF IPv6 则在拒绝之后。这些是路由目标前缀，不是资源引用。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/webconsole/clients.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/clients.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
