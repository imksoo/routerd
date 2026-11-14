# DSLiteTunnel

[各资源的配置](../resources.md#dslitetunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

向 AFTR 以 IPv6 封装 IPv4。隧道配置、IPv4 默认路由、NAT 与连通检查是不同事项。

## 指定条件与引用关系

- 禁用时仍需 interface 及 aftrFrom/aftrFQDN/aftrIPv6/remoteAddress 至少一项。来源可并用，按此顺序尝试至成功。
- localAddress 是外层 IPv6 源地址，但目前 localAddressFrom 选择内层 IPv4 源地址，两者不可互换。内层源地址默认 192.0.0.2，远端 192.0.0.1。无 IPv4 来源引用时使用默认值。
- AFTR／本机／依赖值缺失时为 Pending。Up 仅表示隧道与链路配置，不保证 AFTR 或互联网可达。启用条件为假或禁用时会移除未使用隧道；仅依赖失败会跳过协调，不保证立即删除。
- Linux 使用 ip6tnl/ipip6，FreeBSD 使用 gif。非 gif 名称会确定性映射至 gif100–gif999，实际名称发布于状态。已移除的 aftrSource/ready_when 会被拒绝，请用 aftrFrom/dependsOn。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # 选填; integer; minimum=1
    aftrAddressSelection: "ordinal" # 选填; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # 选填; array
      - "<string>" # string
    aftrFQDN: "<string>" # 选填; string
    aftrFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    aftrIPv6: "<string>" # 选填; string
    defaultRoute: true # 选填; boolean
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    enabled: true # 选填; boolean
    encapsulationLimit: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    localAddress: "<string>" # 选填; string
    localAddressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    localAddressSource: "interface" # 选填; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # 选填; string
    localDelegatedAddress: "<string>" # 选填; string
    mtu: 1280 # 选填; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # 选填; string
    routeMetric: 0 # 选填; integer; minimum=0
    tunnelName: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.interface` | 必填且已声明的逻辑底层接口，正常引用验证检查其存在。 |
| `spec.tunnelName` | 可选设备名称，默认 metadata.name；FreeBSD 可能使用上述不同的 gif 名称。 |
| `spec.enabled` | 默认 true。false 移除未使用配置隧道并报告 Disabled，仍须通过验证。 |
| `spec.aftrFQDN` | 可选 AFTR 主机名，不可有空白或斜线。在 aftrFrom 之后、固定地址之前尝试。 |
| `spec.aftrIPv6` | 可选纯 AFTR IPv6 地址，在 aftrFQDN 之后尝试，无默认值。 |
| `spec.remoteAddress` | 可选 IPv6 AFTR 替代值，最后尝试，不是内层 IPv4 目的地。 |
| `spec.aftrFrom` | 可选通用 resource/field 来源，通常为 DHCPv6 提供的 AFTR 名称。需存在且为提供字段。运行时另读同一资源的 dnsServers 并与 aftrDNSServers 合并。 |
| `spec.aftrDNSServers` | 可选纯 IPv4/IPv6 DNS 服务器，查询端口 53。无明确或来源服务器时，运行时默认 127.0.0.1:1053。此声明不启动解析器，请提供可达服务器。 |
| `spec.aftrAddressOrdinal` | AAAA 选择索引从 1 开始，默认 1。明确指定索引或模式时先按 IPv6 字符串字典序排序；两者省略则保留解析器顺序。 |
| `spec.aftrAddressSelection` | 默认 ordinal，超出范围会失败。ordinalModulo 将索引减 1 后按条数循环，不在 AFTR 间分配流量。 |
| `spec.localAddress` | 可选固定外层 IPv6 源地址；localAddressSource:static 时必填，不是内层 IPv4 源地址。 |
| `spec.localAddressFrom` | 可选的内层 IPv4 通用状态来源，先读状态，再读声明的 IPv4StaticAddress.address。必要来源缺值为 Pending；optional:true 使用 192.0.0.2。结果须为可用单播 IPv4。 |
| `spec.localAddressSource` | 默认 interface 在无 localAddress 时寻找可用全局 IPv6。static 需 localAddress。delegatedAddress 从 localDelegatedAddress 推导，可能先配置 /128 并等待 DAD。 |
| `spec.localDelegatedAddress` | 现有 IPv6DelegatedAddress 裸名称，delegatedAddress 模式必填。其他模式指定时也检查其存在。 |
| `spec.localAddressSuffix` | 可选 IPv6 后缀。委派模式优先顺：此值→引用的 addressSuffix→::1。interface 模式也可用此后缀推导源地址。 |
| `spec.defaultRoute` | 默认 false。FreeBSD 运行时可变更／新增经 192.0.0.1 的默认路由。Linux 隧道控制器不按此安装 IPv4 默认，请用 IPv4Route/EgressRoutePolicy。 |
| `spec.routeMetric` | 可选非负整数，解码默认 0。目前 DS-Lite 执行流程不将它传入默认路由配置。 |
| `spec.mtu` | 可选 1280–65535，运行时默认 1460。应用至隧道链路，不保证底层路径支持此大小。 |
| `spec.encapsulationLimit` | Linux encaplimit 参数，默认 none。此处无详细值验证，不合法值可使 ip 命令失败。FreeBSD 执行流程不使用。 |
| `spec.when` | 可选通用启用条件，默认无条件；为假时移除未使用配置隧道。 |
| `spec.dependsOn` | 可选通用依赖条件 AND 列表，默认空。不满足时为 Pending/DependsOnFalse 并跳过更新。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
