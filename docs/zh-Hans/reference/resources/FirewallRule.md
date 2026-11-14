# FirewallRule

[各资源的配置](../resources.md#firewallrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

声明区域路径的具名允许、丢弃或拒绝规则，支持地址、协议、端口匹配及 Linux 速率/连接条件。

## 指定条件与引用关系

- fromZone、toZone、action 必填。区域须存在或为 self。虽然 fromZone=self 可通过验证，这些生成器不会为它生成本机源链；本机发出流量按 OS/output 规则处理。
- 规则按 metadata.name 排序，不按 YAML 顺序。已有状态、IPv6 控制流量与生成的服务/入口允许可能先匹配。PF 也先生成广泛隐式允许；转发规则的 toZone 不形成输出接口或目标区域条件，需要目标范围时请指定 destinationCIDRs。
- port 与 destinationPorts 互斥。指定源或目标端口需要 tcp/udp。范围须为列表中唯一条目；接受数值端口与 start-end 字符串。ICMP 类型需要对应协议。
- Linux 的源 CIDR 为 OR；destinationCIDRs 与 destinationSetRefs 取并集，再排除指定集合。源、目标与协议地址族须兼容。PF 当前忽略地址集合引用、rateLimit、connLimit，即使验证接受。
- Linux rateLimit 匹配超过配置速率的流量，connLimit 匹配超过 maxPerSource 的数量，将 action 应用于超额流量。限制时请使用适当 drop/reject。两者同时启用会生成独立替代规则，不是两个阈值的 AND。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # 必填; string; enum=["accept", "drop", "reject"]
    connLimit: # 选填; object
      log: true # 选填; boolean
      maxPerSource: 0 # 选填; integer; minimum=0
    destinationCIDRs: # 选填; array
      - "<string>" # string
    destinationPorts: # 选填; array
      - "<string>" # string
    destinationSetRefs: # 选填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 选填; array
      - "<string>" # string
    fromZone: "<string>" # 必填; string
    icmpType: "<string>" # 选填; string
    icmpv6Type: "<string>" # 选填; string
    log: true # 选填; boolean
    port: 0 # 选填; integer; minimum=0; maximum=65535
    protocol: "" # 选填; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # 选填; object
      burst: 0 # 选填; integer; minimum=0
      log: true # 选填; boolean
      packetsPerSecond: 0 # 选填; integer; minimum=0
      per: "" # 选填; string; enum=["", "second", "minute"]
      rate: 0 # 选填; integer; minimum=0
      unit: "" # 选填; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # 选填; array
      - "<string>" # string
    srcCIDRs: # 选填; array
      - "<string>" # string
    toZone: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.fromZone` | 必填单独 FirewallZone 名称或 self。接受 self，但此处不生成本机发出筛选。 |
| `spec.toZone` | 必填单独 FirewallZone 名称或 self。Linux 匹配区域配对输入/输出接口；self 选择本机输入。PF self 使用路由器地址，其他 toZone 不形成输出区域条件。 |
| `spec.srcCIDRs` | 可选 IPv4/IPv6 源前缀 OR 列表；默认空为任意源。须为有效 CIDR；Linux 生成前会掩码主机位。 |
| `spec.destinationCIDRs` | 可选目标前缀列表，默认空。Linux 与 destinationSetRefs 取并集；两者均无时区域路径内不限制目标。PF 仅使用这些静态目标前缀。 |
| `spec.destinationSetRefs` | 可选已有 IPAddressSet/<name> 引用，默认空。Linux 使用启用的 IPv4/IPv6 nftables 集合；当前 PF 不执行。 |
| `spec.excludeDestinationSetRefs` | 可选已有 IPAddressSet/<name> 引用，从 Linux 目标匹配中排除。默认空；当前 PF 忽略。 |
| `spec.protocol` | 默认空为任意协议。接受 tcp、udp、icmp、icmpv6、ipv6-icmp、ipip。此处 ipip 是 IPv6 内封装 IPv4（next-header 4），不是 IPv4 底层隧道协议规则。 |
| `spec.port` | 可选单一目标端口，默认 0 为未指定。正值 1–65535 需要 tcp/udp，且与 destinationPorts 互斥。 |
| `spec.sourcePorts` | 可选 tcp/udp 源端口 OR 列表，各值为 1–65535 或唯一 start-end 范围。默认空为任意源端口。 |
| `spec.destinationPorts` | 可选 tcp/udp 目标端口 OR 列表，规则与源相同。与非零 port 互斥，默认空为不限制。 |
| `spec.icmpType` | 可选 IPv4 ICMP 名称，须为 icmp。支持 echo-reply、destination-unreachable、source-quench、redirect、echo-request、router-advertisement、router-solicitation、time-exceeded、parameter-problem、timestamp-request、timestamp-reply。空表示全部类型；此键优先于 YAML 别名 icmp-type。 |
| `spec.icmpv6Type` | 可选 IPv6 ICMP 名称，须为 icmpv6/ipv6-icmp。支持 destination-unreachable、packet-too-big、time-exceeded、parameter-problem、echo-request/reply、router-solicit/advert、neighbor-solicit/advert 及其 nd- 别名。空为全部类型，优先于 icmpv6-type 别名。 |
| `spec.action` | 必填 accept、drop 或 reject，无默认值。Linux 搭配速率/连接条件时，动作用于超额流量，不是允许配额。 |
| `spec.log` | 默认 false。true 在动作前加入日志；Linux 使用内核日志或已启用 FirewallEventLog 的 NFLOG，PF 使用 log。 |
| `spec.rateLimit` | 可选 Linux 超速率条件，默认禁用。当前 PF 忽略所有内层速率选项。 |
| `spec.rateLimit.rate` | 非负速率，默认 0 改用 packetsPerSecond。有效值 0 禁用条件，且 unit/per/burst/log 须为空/0/false。正 rate 优先于 packetsPerSecond。 |
| `spec.rateLimit.unit` | 启用时默认 packet，也接受 byte、kilobyte、megabyte。选择 nftables 速率与突发单位，需要正有效速率。 |
| `spec.rateLimit.per` | 启用时默认 second，或 minute，需要正有效速率。 |
| `spec.rateLimit.log` | 默认 false。true 在动作前记录超速率匹配，需要正有效速率。 |
| `spec.rateLimit.packetsPerSecond` | rate=0 时使用的旧式非负速率，默认 0。虽有此名称，明确 unit/per 仍决定有效单位。 |
| `spec.rateLimit.burst` | 所选单位的非负突发量。默认 0 省略 burst 子句，按 nftables 行为。非零需要启用速率。 |
| `spec.connLimit` | 可选 Linux 每源连接数条件，默认禁用。独立于 rateLimit，当前 PF 忽略。 |
| `spec.connLimit.maxPerSource` | 非负上限，默认 0 禁用。正 N 匹配每源地址 ct count 超过 N，再应用 action；IPv4/IPv6 使用独立计量。 |
| `spec.connLimit.log` | 默认 false。true 为超额连接数匹配加入 conn-limit 日志；maxPerSource=0 时无效。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `action`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)
