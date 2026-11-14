# DNSZone

[各资源的配置](../resources.md#dnszone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

通过 DNSForwarder.zoneRefs 提供本地 A/AAAA 记录及自动 PTR 响应；不是支持任意记录类型、区域传送或签名的通用权威 DNS。

## 指定条件与引用关系

- 须由 DNSForwarder 引用，且 match 须涵盖查询的正向或反向域名。仅声明 DNSZone 不会开启 DNS 端口。符合区域的 VirtualAddress/IngressService hostname 也会导入，除非其指定 externalDNS。
- 固定 ipv4/ipv6 可与动态 ipv4From/ipv6From 并用。已解析动态值替换同地址族固定值，未解析时保留固定值。必要来源缺值记录于各记录的等待信息，不阻挡整个解析器；可选缺值不记录等待。地址族错误或无效值会报错。
- dnssec.enabled 仅在本地响应设置 AD，不生成 RRSIG/DNSKEY 或验证签名。dnssec.mode、dhcpDerived.hostnameSuffix、dhcpDerived.ddns、reverseZones 当前不改变响应行为，不能视为可用的签名、DDNS 或反向区域访问控制。
- 仅应答 A/AAAA 与自动 PTR。名称存在但无所查地址族时为 NOERROR/NODATA，区域内名称不存在时为 NXDOMAIN。支持通配符。重复固定 hostname 采用最后记录，不累加各项地址。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    dhcpDerived: # 选填; object
      ddns: true # 选填; boolean
      hostnameSuffix: "<string>" # 选填; string
      leaseFile: "<string>" # 选填; string
      sources: # 选填; array
        - "<string>" # string
      ttl: 0 # 选填; integer; minimum=0
    dnssec: # 选填; object
      enabled: true # 选填; boolean
      mode: "" # 选填; string; enum=["", "validate", "sign"]
    records: # 选填; array
      - # object
        hostname: "<string>" # 必填; string
        ipv4: "<string>" # 选填; string
        ipv4From: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        ipv6: "<string>" # 选填; string
        ipv6From: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        ttl: 0 # 选填; integer; minimum=0
    reverseZones: # 选填; array
      - # object
        name: "<string>" # 必填; string
    ttl: 0 # 选填; integer; minimum=0
    zone: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.zone` | 必填区域域名，例如 home.example.net，运行时补末尾句点，无默认值。当前仅验证非空，请使用常规小写 DNS 名称。 |
| `spec.ttl` | 区域默认 TTL 秒数；省略或 0 为 300，模式要求非负值。用于未指定 TTL 的固定记录及自动 PTR。 |
| `spec.dnssec` | 可选的本地响应 AD 标志设置，不提供密码学签名或验证。 |
| `spec.dnssec.enabled` | 默认 false；true 不生成签名，连本地否定或 NODATA 响应也设置 AD。 |
| `spec.dnssec.mode` | 模式接受空值、validate 或 sign，默认空值。会解码但当前本地区域响应逻辑不使用。 |
| `spec.records` | 可选固定 A/AAAA 记录列表，默认空。无地址 hostname 也可通过验证，但不提供 A/AAAA 响应。 |
| `spec.records[].hostname` | 必填且不可含空白或逗号。相对名称附加 zone；已在区域内的完整名称保持原样，区域外名称也会附加 zone，不会另行提供外部区域。通配符可用 *.subdomain。 |
| `spec.records[].ipv4` | 可选的纯 IPv4 地址，默认无；可与 ipv4From 并用，来源未解析时保留此值。 |
| `spec.records[].ipv4From` | 可选通用状态来源，须明确指定并验证 resource/field。第一个有效值须为 IPv4，CIDR 取主机地址。运行时仅读状态，不像部分其他使用方会以 IPv4StaticAddress 声明值补足。 |
| `spec.records[].ipv6` | 可选的纯 IPv6 地址，默认无；与 ipv6From 并用及缺值处理方式同 IPv4。 |
| `spec.records[].ipv6From` | 可选通用 resource/field/optional 来源，第一个值须为 IPv6。引用、CIDR 与缺值规则同 ipv4From；optional 默认 false。 |
| `spec.records[].ttl` | 记录 TTL 秒数；省略或 0 继承区域 TTL。模式要求非负，不影响 DHCP 派生 TTL。 |
| `spec.dhcpDerived` | 可选 DHCP 租约派生记录设置，启用区域可由运行时租约事件更新，并非 RFC 2136 DDNS。 |
| `spec.dhcpDerived.sources` | 默认空值禁用实时租约事件与保存主机快照导入。任意非空列表会启用，但当前不按字符串筛选来源或验证 DHCP 资源引用；所有收到的租约事件均提供给各启用区域。 |
| `spec.dhcpDerived.hostnameSuffix` | 解码的可选字符串，默认空值；当前租约记录生成不读取，后缀使用 zone。 |
| `spec.dhcpDerived.ddns` | 解码布尔值，默认 false；当前不以此标志控制事件更新，也不据此实现 DNS UPDATE。 |
| `spec.dhcpDerived.ttl` | DHCP 派生正向记录 TTL 秒数；省略或 0 为 60，与区域 TTL 无关。PTR 响应使用区域 TTL。 |
| `spec.dhcpDerived.leaseFile` | 构建或重新加载区域时读取的可选 dnsmasq 租约文件。空值不导入，与实时事件启用分开，并非持续监视文件。 |
| `spec.reverseZones` | 可选反向区域元数据列表，默认空。守护进程保存名称但不在 PTR 响应时查阅；PTR 按记录生成，不受此列表控制。 |
| `spec.reverseZones[].name` | 每项内模式要求的字符串，保存为完整反向区域名称，但当前不影响响应选择。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
