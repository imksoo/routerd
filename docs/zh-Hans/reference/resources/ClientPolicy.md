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

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # 选填; array
      - # object
        ipv4Reservation: "<string>" # 选填; string
        ipv6Addresses: # 选填; array
          - "<string>" # string
        match: # 必填; object
          dhcpFingerprints: # 选填; array
            - "<string>" # string
          hostnamePatterns: # 选填; array
            - "<string>" # string
          macs: # 选填; array
            - "<string>" # string
          ouiPrefixes: # 选填; array
            - "<string>" # string
        mode: "trusted" # 必填; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # 选填; string
    guestEgressAllow: # 选填; array
      - "<string>" # string
    guestEgressDeny: # 选填; array
      - "<string>" # string
    guestServices: # 选填; array
      - "<string>" # string
    interfaces: # 选填; array
      - "<string>" # string
    isolation: # 选填; object
      lanInternet: "" # 选填; string; enum=["", "allow", "deny"]
      lanLAN: "" # 选填; string; enum=["", "allow", "deny"]
      lanMgmt: "" # 选填; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # 选填; string; enum=["", "allow", "deny"]
    macs: # 选填; array
      - "<string>" # string
    mode: "include" # 必填; string; enum=["include", "exclude"]
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.mode` | 必填 include/exclude，无默认值。决定身份集合为限制对象或例外。 |
| `spec.interfaces` | 可选 Interface/name 或单独逻辑接口名，引用须存在且不可重复。Linux 省略时使用所有 trust 区域接口，均无则失败。PF 省略时没有适用接口限制。 |
| `spec.macs` | 可选 Linux 固定 MAC 列表，默认空。此列表与 classification.match.macs 间规范化后的重复会被拒绝。FreeBSD 禁用。 |
| `spec.classification` | 可选分类列表，默认空。Linux 每类需至少一个选择器，但仅 MAC 形成防火墙集合。FreeBSD 需要稳定 IP 身份并禁止选择器。 |
| `spec.classification[].name` | 可选描述名称，默认空。不改变匹配优先顺序，也不创建资源标识。 |
| `spec.classification[].mode` | 必填 trusted、guest 或 isolated。include 将 guest/isolated 同样加入限制集合；exclude 将 trusted 加入例外集合。 |
| `spec.classification[].match` | Linux 至少需要一个非空选择器列表。FreeBSD 即使提供稳定 IP 也须留空。当前防火墙不将各选择器动态解析成客户端 MAC。 |
| `spec.classification[].match.macs` | 可选完整 MAC 列表，是 Linux 的有效分类器。MAC 须有效，且不可跨类别或顶层 macs 重复。 |
| `spec.classification[].match.ouiPrefixes` | 接受如 18:ec:e7 的三个十六进制八位组，同类重复会被拒绝。当前不形成防火墙强制，FreeBSD 拒绝。 |
| `spec.classification[].match.hostnamePatterns` | 接受非空 Go path.Match glob，不是正则表达式。当前仅验证语法，不解析为防火墙身份；FreeBSD 拒绝。 |
| `spec.classification[].match.dhcpFingerprints` | 接受非空且不含空白的标记，同类不可重复。当前不形成防火墙集合；FreeBSD 拒绝。 |
| `spec.classification[].ipv4Reservation` | 可选已有 DHCPv4Reservation 的单独 metadata.name，拒绝 Kind/name。PF 使用其非空 ipAddress；Linux 验证引用但不以地址匹配客户端。 |
| `spec.classification[].ipv6Addresses` | 可选 PF 的纯 IPv6 地址，不接受 CIDR 或 IPv4 映射格式。所有类别间规范化后不可重复。不从 MAC/IPv4 预约推导，Linux 客户端匹配不使用。 |
| `spec.isolation` | 可选 allow/deny 提示，仅 Linux 生成器读取这四个字段；PF 使用明确 guest egress 列表或默认值。不会逐类独立应用。 |
| `spec.isolation.lanInternet` | 默认空。Linux deny 且未明确设 guestEgressDeny 时，将拒绝集合换成 0.0.0.0/0 与 ::/0。allow/空不增加无条件互联网允许。PF 忽略。 |
| `spec.isolation.lanLAN` | 默认空。Linux 仅在 lanLAN 与 lanMgmt 均 allow 时移除默认私有前缀拒绝，不区分个别 LAN。明确 guestEgressDeny 覆盖默认。PF 忽略。 |
| `spec.isolation.lanMgmt` | 默认空，与 lanLAN 共同决定默认集合。不会独立解析管理区域目标。PF 忽略。 |
| `spec.isolation.mDNSBroadcast` | 默认空。Linux deny 在 guestEgressAllow 后加入转发 mDNS/SSDP 多播与 UDP 137/138 丢弃。不控制 L2 桥接或独立封锁本机服务。PF 忽略。 |
| `spec.guestServices` | 允许的本机服务名称：dns、dhcp、ntp、mdns、ssdp。省略或空默认 dhcp/dns/ntp，空列表无法禁用全部例外。仅开放防火墙，不启动服务。 |
| `spec.guestEgressDeny` | 可选 IPv4/IPv6 CIDR。省略或空默认 10.0.0.0/8、172.16.0.0/12、192.168.0.0/16、fc00::/7，Linux 按上述 isolation 调整。明确值替换而非追加默认；PF 空时固定使用私有前缀默认。 |
| `spec.guestEgressAllow` | 可选 IPv4/IPv6 CIDR 例外，默认空。Linux 与 PF IPv4 先于拒绝评估，PF IPv6 则在拒绝之后。这些是路由目标前缀，不是资源引用。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/webconsole/clients.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/clients.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
