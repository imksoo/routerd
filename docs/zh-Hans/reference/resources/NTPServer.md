# NTPServer

[各资源的配置](../resources.md#ntpserver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

使用chrony或FreeBSD ntpd配置时间服务器与上游。

## 指定条件与引用关系

- managed默认false；true须声明上游来源，static另须servers。监听与允许来源列表会合并，上游替代来源则按source顺序选择。
- ntpd生成器不输出allowCIDRs限制，不能假设声明此列表就会对ntpd应用客户端ACL。
- 动态来源模式下，上游值未解析本身不会停止生成服务器配置，可能没有上游。Applied表示配置/服务处理，不证明时钟已同步。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    allowCIDRFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    allowCIDRs: # 选填; array
      - "<string>" # string
    fallbackServers: # 选填; array
      - "<string>" # string
    listenAddressFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    listenAddresses: # 选填; array
      - "<string>" # string
    managed: true # 选填; boolean
    provider: "chrony" # 选填; string; enum=["chrony", "ntpd"]
    serverFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    servers: # 选填; array
      - "<string>" # string
    source: "static" # 选填; string; enum=["static", "auto", "dhcp", "dhcpv6"]
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
| `spec.managed` | 默认false，仅记录Observed，不配置或启动时间服务；管理文件与服务时设true。 |
| `spec.source` | 默认static，使用servers。auto/dhcp/dhcpv6均依serverFrom解析值、fallbackServers、servers的顺序选择，不会自动查找DHCP来源。 |
| `spec.servers` | 上游主机名或IP，默认空；managed=true且source省略或static时必填。各项须为非空白单一值，不是资源引用。 |
| `spec.serverFrom` | 显式值来源列表，默认空。例如DHCPv4Client/wan的ntpServers或DHCPv6PrefixDelegation/wan-pd的sntpServers。仅用于auto/dhcp/dhcpv6，每个resource与field须存在。 此解析器不使用optional标记，缺值即选下一种替代来源。 |
| `spec.when` | 启用条件，省略不设限制；state/all/any、unknown及时间条件请见共用条件参考。 |
| `spec.provider` | 服务器实现，通常默认chrony，FreeBSD默认ntpd；ntpd仅支持FreeBSD。 |
| `spec.fallbackServers` | 备用上游列表，默认空；auto/dhcp/dhcpv6的serverFrom无值时，优先于servers采用，不生成独立备用指令。 |
| `spec.listenAddresses` | 静态监听IP，默认空；CIDR取其主机IP。解析结果为空时不生成bindaddress/interface listen指令，使用后端默认行为。 |
| `spec.listenAddressFrom` | 额外监听地址来源，默认空，与listenAddresses合并。每项须有resource与field。已声明监听配置但合并结果为空时，为Pending/NoListenAddresses。 |
| `spec.allowCIDRs` | 允许客户端CIDR列表，默认空。chrony生成各allow，ntpd生成配置不使用此列表。静态值在验证时仅检查非空白，不检查CIDR语法。 |
| `spec.allowCIDRFrom` | 与allowCIDRs合并的CIDR来源，默认空。非optional来源缺值时为Pending/AllowCIDRFromPending；派生CIDR错误时为Pending/AllowCIDRFromInvalid。不接受没有前缀长度的裸IP。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go)
