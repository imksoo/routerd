# TunnelInterface

[各资源的配置](../resources.md#tunnelinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

创建并协调未加密的 IPv4 外层隧道及可选内层 IPv4 地址，metadata.name 为实际 OS 隧道名称。

## 指定条件与引用关系

- 两端点各须固定值或通用状态来源其中之一，仅限 IPv4。动态端点缺值即使 optional true 仍暂停协调，不自动撤回现有转发；无 when/dependsOn。
- Linux 以 ip link/address 及 FOU 监听支持四模式。FreeBSD 仅 IPIP/gifN、GRE/greN，拒绝明确 TTL，address 非空时须明确内层 peerAddress。控制器强制链路 up，无 managed/enabled 禁用选项。
- 配置 address 替换隧道其他 IPv4，空值全移除。Linux 清理也移除未配置 IPIP/GRE（内置及其他配置保留者除外）及不用的 FOU 监听；FreeBSD 撤除依记录状态及类型检查。变更按整体配置，不以所有权限制。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 选填; string
    encapDport: 1 # 选填; integer; minimum=1; maximum=65535
    encapSport: 1 # 选填; integer; minimum=1; maximum=65535
    key: 0 # 选填; integer; minimum=0; maximum=4294967295
    local: "<string>" # 选填; string
    localFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # 选填; integer; minimum=576; maximum=9216
    pathMTU: # 选填; object
      forceFragmentIPv4: true # 选填; boolean
    peerAddress: "<string>" # 选填; string
    remote: "<string>" # 选填; string
    remoteFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    trustedUnderlay: true # 必填; boolean
    ttl: 1 # 选填; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # 选填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.mode` | 必填 ipip/gre/fou/gue，无默认值。Linux FOU/GUE 使用 UDP 封装 IPIP 链路；FreeBSD 仅支持 ipip（gifN）及 gre（greN），拒绝 FOU/GUE。 |
| `spec.local` | 本机外层纯 IPv4 端点，local/localFrom 恰需一个。拒绝固定 CIDR、主机名与 IPv6，不自动选择接口地址。 |
| `spec.localFrom` | 可选通用状态来源，须明确指定现有 Kind/name 与提供字段，与 local 互斥。运行时仅读状态，接受单一 IPv4 或 CIDR 并规范化；即使 optional true，缺值仍为 Pending/EndpointSourcePending，无端点替代值。 |
| `spec.remote` | 远端外层纯 IPv4 端点，remote/remoteFrom 恰需一个，拒绝固定主机名、IPv6 与 CIDR。 |
| `spec.remoteFrom` | 可选通用状态来源，与 remote 互斥；必要 field、IPv4/CIDR 规范化及忽略 optional 行为同 localFrom。状态缺值暂停协调，但不自动拆除现有隧道。 |
| `spec.address` | 可选内层 IPv4 主机/CIDR。控制器移除隧道其他 IPv4，省略或空值因此也移除现有 IPv4；无自动内层地址。FreeBSD 设置后需 peerAddress。 |
| `spec.peerAddress` | 仅 FreeBSD 的明确内层纯 IPv4 对端，与 address 必须成对，不从 CIDR 推导，Linux 拒绝。 |
| `spec.underlayInterface` | 可选 MTU 估算资源名称（Interface、WireGuardInterface、嵌套 TunnelInterface）。此处非必填且未全局验证存在，未知使用 1500 替代。当前隧道命令不绑定此设备，外层出口由主机选路，请避免递归引用。 |
| `spec.mtu` | 可选明确 576–9216，原样使用。省略或 0 为配置底层 MTU（替代 1500）减 ipip 20、gre 24（有 key 加 4）、fou 28、gue 32。替代默认 1480/1476/1472/1468，有 key GRE 1472，非实时 PMTU 探测。 |
| `spec.ttl` | Linux 外层 TTL 1–255，省略或 0 为 64。FreeBSD 拒绝非零明确 TTL，ifconfig 路径也不应用内部报告默认 64。 |
| `spec.key` | 仅 GRE 的整数 0–4294967295，默认 0 表示无 key；其他模式非零拒绝。FreeBSD 使用 ifconfig grekey，Linux key/nokey。它是标识符，不是加密或认证秘密。 |
| `spec.encapSport` | fou/gue 必填 UDP 源及本机接收端口 1–65535，无默认值，控制器也在此建立 FOU/GUE 监听；ipip/gre 非零拒绝。 |
| `spec.encapDport` | fou/gue 必填远端 UDP 目标端口 1–65535，须符合对端接收端口，无默认值；ipip/gre 非零拒绝。 |
| `spec.trustedUnderlay` | 必须 true，false 或省略会拒绝。IPIP/GRE/FOU/GUE 不提供加密或对端认证，此确认也不增加这些功能。 |
| `spec.pathMTU` | 可选转发路径策略选项，默认不强制 IPv4 分片，独立于明确隧道 MTU。 |
| `spec.pathMTU.forceFragmentIPv4` | 默认 false，Linux 路径 MTU 策略可清除符合派生转发路径之过大 IPv4 的 DF，不是隧道 ignore-df 选项或 IPv6 功能。FreeBSD 跳过此 nftables 控制器，此路径无同等动作。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/tunnel.go) · [internal/ipiptunnel/ipip.go](https://github.com/imksoo/routerd/blob/main/internal/ipiptunnel/ipip.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/render/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/render/mtu.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/mtu.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
