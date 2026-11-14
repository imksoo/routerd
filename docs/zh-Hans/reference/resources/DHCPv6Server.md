# DHCPv6Server

[各资源的配置](../resources.md#dhcpv6server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

使用共用 dnsmasq 进程提供 LAN DHCPv6 地址或信息服务及相关 RA 行为。

## 指定条件与引用关系

- 当前实时写入与旧静态渲染不同：需要 interface，仅 delegatedAddress 不建立实时范围，dnsSource/selfAddressPolicy 不提供自身 DNS，ra-only 也走 stateless 分支。请用下列明确字段并确认实际 DHCP/RA 流量。
- 常规验证限制 delegatedAddress 的 mode 为 stateless/ra-only。没有 delegatedAddress 时，所有非 stateless 模式（含 ra-only）均须有效 IPv6 起止值。domainSearch/domainSearchFrom 互斥，DNS/SNTP 固定与动态来源可并用。
- 资源共用一个 dnsmasq，constructor 使用实际接口的 /64。Applied 仅记录协调成功，不代表客户端取得租约、收到 RA 或安装默认路由；已移除 ready_when 会被拒绝。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 选填; object
      end: "<string>" # 选填; string
      leaseTime: "<string>" # 选填; string
      start: "<string>" # 选填; string
    configPath: "<string>" # 选填; string
    defaultRoute: true # 选填; boolean
    delegatedAddress: "<string>" # 选填; string
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    dnsServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 选填; array
      - "<string>" # string
    dnsSource: "self" # 选填; string; enum=["self", "static", "none"]
    domainSearch: # 选填; array
      - "<string>" # string
    domainSearchFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 选填; string
    leaseFile: "<string>" # 选填; string
    leaseTime: "<string>" # 选填; string
    listenInterfaces: # 选填; array
      - "<string>" # string
    logDHCP: true # 选填; boolean
    managed: true # 选填; boolean
    mode: "stateless" # 选填; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # 选填; string
    rapidCommit: true # 选填; boolean
    role: "server" # 选填; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # 选填; string
    server: "dnsmasq" # 选填; string; enum=["dnsmasq"]
    sntpServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    sntpServers: # 选填; array
      - "<string>" # string
    stickyHoldDays: 0 # 选填; integer; minimum=0
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
| `spec.server` | 可选 dnsmasq，默认也为 dnsmasq，不接受其他后端；实时服务与 DHCPv4/RA 共用。 |
| `spec.managed` | 布尔值默认 false，当前实时服务写入器与控制器不以此禁用更改。 |
| `spec.role` | server/transit，验证与静态渲染默认 server；当前实时写入不以 transit 抑制服务。 |
| `spec.listenInterfaces` | 可选已声明逻辑接口，均验证存在。提供 delegatedAddress 且列表非空时，须包含该地址资源接口。用于地址族及防火墙规划，但当前实时地址池仍需 interface 本身。 |
| `spec.logDHCP` | 默认 false，静态渲染可输出 log-dhcp，但当前实时写入不使用。 |
| `spec.stickyHoldDays` | 非负天数，默认 0 禁用。dnsmasq 正值将分配保留于 dhcp-sticky.db 供重用；运行时对全部 IPv6 事件采用 IPv6 服务器最大值，不按接口。DHCPv6ServerLeaseSync 不复制此数据库。 |
| `spec.interface` | 已声明的逻辑 LAN 接口，即使验证允许仅有 delegatedAddress，当前实时写入仍需要，省略则跳过地址池生成。 |
| `spec.delegatedAddress` | 可选现有 IPv6DelegatedAddress 裸名。静态渲染可派生接口、前缀与自身 DNS，但当前实时写入直接用 interface/constructor，不以此替代。设置后验证限制 mode 为 stateless 或 ra-only。 |
| `spec.mode` | 默认 stateless；stateful 输出 constructor 范围，both 加 slaac。当前实时写入将 ra-only 同样送入 ra-stateless 默认分支，不能实际禁用 DHCPv6 信息服务。无 delegatedAddress 时验证也要求 ra-only 提供地址池。 |
| `spec.addressPool` | IPv6 有状态分配范围，未有 delegatedAddress 且非 stateless 时必填；stateless 不作分配范围，端点不自动补入。 |
| `spec.addressPool.start` | IPv6 起始地址，包含端点且 <= end；实时 stateful 范围使用 constructor:<OS名称> 与前缀长 64。 |
| `spec.addressPool.end` | IPv6 结束地址，包含端点且 >= start，与 start 配对。 |
| `spec.addressPool.leaseTime` | 后端时长，优先于 leaseTime；空值按 leaseTime、12h 补足，此处不按 Go duration 解析。 |
| `spec.defaultRoute` | 布尔值默认 false，静态渲染可控制默认路由器期限，但当前实时写入不读取。DHCPv6 本身不分发 IPv6 默认路由。 |
| `spec.dnsSource` | 验证 self/static/none，静态默认 self，static 需 dnsServers。当前实时写入忽略选择器，仅用明确 dnsServers/dnsServerFrom，none 也不抑制这些明确选项。 |
| `spec.selfAddressPolicy` | 可选现有 SelfAddressPolicy 裸名，即使当前实时 LAN 生成不使用仍验证存在；供旧静态自身 DNS 选择。 |
| `spec.dnsServers` | 可选固定 IPv6 DNS 列表，默认空，与 dnsServerFrom 合并输出带标签 option6:dns-server，不启动 DNS 监听。 |
| `spec.dnsServerFrom` | 可选通用 resource/field 来源列表，optional 默认 false；必要缺值即使有固定 DNS 仍为 Pending，此读取器不完整验证所有状态值地址族便传入。 |
| `spec.sntpServers` | 可选固定 IPv6 SNTP 列表，默认空，与 sntpServerFrom 合并输出带标签 option6:sntp-server，不启动 NTP 守护进程。 |
| `spec.sntpServerFrom` | 可选通用状态来源列表，必要或可选阻挡行为同 dnsServerFrom，默认无引用。 |
| `spec.domainSearch` | 可选单一域名字符串列表，默认空，不可含空白、斜线或逗号，与 domainSearchFrom 互斥，输出带标签 option6:domain-search。 |
| `spec.domainSearchFrom` | 可选来源限 DNSZone/<name>、field: zone，须存在，可先读声明值。必要缺值阻挡服务器，optional true 跳过缺值，与 domainSearch 互斥。 |
| `spec.leaseTime` | addressPool.leaseTime 之后的后端时长替代值，默认 12h。 |
| `spec.rapidCommit` | 布尔值默认 false，当前输出带标签 option6:rapid-commit，不另输出 dnsmasq dhcp-rapid-commit 指令，不能仅据此证明协商成两消息 Rapid Commit。 |
| `spec.configPath` | 可选解码字符串，默认空，当前控制器使用共用 dnsmasq 选项而非此资源路径。 |
| `spec.pidFile` | 可选解码字符串，默认空，当前控制器使用共用 dnsmasq 选项而非此资源路径。 |
| `spec.leaseFile` | 可选租约文件路径，默认 <StateDir>/dnsmasq/dnsmasq.leases；单一服务共用，所有 DHCPv4/6 明确 leaseFile 须一致，否则实时生成失败。 |
| `spec.dependsOn` | 接受通用依赖列表并验证引用，默认空。当前实时 DHCPv6 服务器写入与状态路径不评估此列表，不能作可靠服务条件；动态值要求及 when 另行评估。 |
| `spec.when` | 可选通用条件，默认无条件；为假时从有效配置排除并为 Pending/WhenFalse。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [cmd/routerd/runtime_services.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/runtime_services.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
