# IPv6RouterAdvertisement

[各资源的配置](../resources.md#ipv6routeradvertisement) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

配置 LAN 的 IPv6 路由器通告选项，并弃用先前委派的前缀。

## 指定条件与引用关系

- interface 及 prefix 或 prefixFrom 必填。静态渲染与实时写入不同：当前实时生成 interface/enable-ra/选项，但不由 prefix/prefixFrom 派生常规 dhcp-range。须搭配可用 DHCPv6Server/地址配置并确认实际线上 PIO，模式或 Applied 无法证明当前前缀通告。
- dnssl 与 dnsslFrom 互斥，prefix/prefixFrom、rdnss/rdnssFrom 可并用。即使 optional true，通用引用仍须有现有资源与明确提供字段。已移除 prefixSource/ready_when 会被拒绝。
- prefixFrom 指向 IPv6DelegatedAddress 时，仍有效的旧 /64 以前缀首选期 0、剩余有效期的 PIO 直接发送，使用实际链路本地源地址与跳数限制 255。常规当前前缀服务仍走共用 dnsmasq。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: "<name>"
  spec:
    configPath: "<string>" # 选填; string
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    dnssl: # 选填; array
      - "<string>" # string
    dnsslFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    mFlag: true # 选填; boolean
    mtu: 1280 # 选填; integer; minimum=1280; maximum=65535
    oFlag: true # 选填; boolean
    pidFile: "<string>" # 选填; string
    preferredLifetime: "<string>" # 选填; string
    prefix: "<string>" # 选填; string
    prefixFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    prfPreference: "" # 选填; string; enum=["", "low", "medium", "high"]
    rdnss: # 选填; array
      - "<string>" # string
    rdnssFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    validLifetime: "<string>" # 选填; string
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
| `spec.interface` | 必填逻辑接口。不同于许多地址资源，RA 此字段未由全局引用验证确认存在；运行时先解析别名，失败则原样作 OS 名称。请声明 Interface 并使用其名称。 |
| `spec.prefix` | 固定 IPv6 前缀，未提供 prefixFrom.resource 时必填，可作 prefixFrom 状态替代值。当前实时写入不以此生成常规前缀范围，不能假设仅声明即可通告。 |
| `spec.prefixFrom` | 可选通用 resource/field/optional 来源，须有现有资源与明确提供字段。用于报告前缀，引用 IPv6DelegatedAddress 时也找出待弃用的旧 /64；单纯缺值不阻挡常规配置生成。 |
| `spec.rdnss` | 可选固定 IPv6 DNS 列表，默认空，与 rdnssFrom 合并。在共用 dnsmasq 输出无标签 option6:dns-server，此写入器不按 RA 接口隔离。 |
| `spec.rdnssFrom` | 可选通用状态来源列表，无自动来源。当前 RA 路径忽略未解析来源的等待原因，仅使用已有值；optional 在此不提供服务就绪条件。 |
| `spec.dnssl` | 可选搜索域列表，默认空；每值为不含空白、斜线或逗号的单一域名，与 dnsslFrom 互斥，输出共用无标签 option6:domain-search。 |
| `spec.dnsslFrom` | 可选来源列表，限 DNSZone/<name>、field: zone，资源须存在；可先读声明值，与 dnssl 互斥。当前常规 RA 生成忽略来源缺值的等待原因。 |
| `spec.mFlag` | 布尔值默认 false，供直接弃用旧委派前缀的 RA 使用；当前常规 dnsmasq RA 生成不映射至 M。 |
| `spec.oFlag` | 布尔值默认 false，使用路径同 mFlag 的直接弃用通告，不是常规 dnsmasq O 位开关。 |
| `spec.mtu` | 可选 1280–65535，省略或 0 不输出明确 MTU，除非有优先的 PathMTU 派生值。经 dnsmasq ra-param mtu:<值> 传入。 |
| `spec.prfPreference` | 空值或 medium 使用常规偏好，low/high 输出 dnsmasq 路由器偏好并影响旧前缀直接通告。 |
| `spec.preferredLifetime` | 可选字符串，默认空，当前常规实时写入不使用。旧前缀直接 PIO 一律使用 preferred lifetime 0，不受此值影响。 |
| `spec.validLifetime` | 常规实时 dnsmasq 渲染将此值作 ra-param 路由器有效期，不是当前前缀 PIO 有效期。请用十进制秒，省略保留 dnsmasq 默认值。旧前缀直接 RA 空值或无效值为 1800、上限 65535，旧 PIO 则用租约剩余时长。 |
| `spec.configPath` | 可选解码字符串，默认空；当前实时控制器使用共用 dnsmasq ConfigPath/PIDFile 选项，不使用此资源覆盖。 |
| `spec.pidFile` | 可选解码字符串，默认空；当前实时控制器使用共用 dnsmasq ConfigPath/PIDFile 选项，不使用此资源覆盖。 |
| `spec.when` | 可选通用状态条件，默认无条件；为假时从有效配置排除并为 Pending/WhenFalse。 |
| `spec.dependsOn` | 可选通用 AND 依赖列表，默认空。不成立报告 Pending/DependsOnFalse 并跳过旧前缀直接通告，但常规共用 dnsmasq 行写入器不检查此条件，不能完全抑制常规服务。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/routeradvertisement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/routeradvertisement.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interface`, `prefix`, `rdnss`, `configPath`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
