# DHCPv4Reservation

[各资源的配置](../resources.md#dhcpv4reservation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在已声明的 DHCPv4Server 为一个 MAC 地址保留 IPv4 地址。

## 指定条件与引用关系

- 虽然模式将 server 标为可选，server、macAddress、ipAddress 在语义上必填。server 须为 interface 非空的 DHCPv4Server 裸名；常规验证即使仅有一台也不允许省略。scope 会被拒绝。
- 当前在共用 dnsmasq 主机附属文件写入带资源标签的保留。server 关联用于验证、状态及自定义选项生成，不代表独立进程或主机文件命名空间。Applied 不证明客户端已取得租约。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 选填; string
    ipAddress: "<string>" # 必填; string
    leaseTime: "<string>" # 选填; string
    macAddress: "<string>" # 必填; string
    options: # 选填; array
      - # object
        code: 1 # 选填; integer; minimum=1; maximum=65535
        name: "<string>" # 选填; string
        value: "<string>" # 选填; string
    scope: "<string>" # 选填; string
    server: "<string>" # 选填; string
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
| `spec.scope` | 已移除字段，非空值会被拒绝，请用 server。 |
| `spec.server` | 必填已设置 interface 的现有直接 DHCPv4Server metadata.name，此处不移除 Kind/name 前缀。 |
| `spec.macAddress` | 必填可由 net.ParseMAC 解析的 MAC 地址，以小写输出，无默认值。 |
| `spec.hostname` | 可选且不含空白或逗号的主机名，默认省略。可供已启用 DNSZone 的 DHCP 派生主机快照使用。 |
| `spec.ipAddress` | 必填纯 IPv4 保留地址。当前验证地址族，但此处不要求位于服务器地址池内，也不拒绝重复保留 IP/MAC。 |
| `spec.leaseTime` | 可选后端时长字符串，默认空值且拒绝逗号。当前运行时保留主机行不输出此字段，因此不会在此路径覆盖租约时间。 |
| `spec.options` | 以此保留标签输出的可选 DHCP 选项列表，每项须有 code/name 其中之一及非空 value，默认空。 |
| `spec.options[].code` | 数字选项标识符，与 name 互斥，模式范围 1–65535，无默认标识符。 |
| `spec.options[].name` | dnsmasq 选项名称，与 code 互斥，不可含空白或逗号。 |
| `spec.options[].value` | 必填非空后端格式值，不可含 CR/LF；原样传入，逗号可分隔多个选项值。 |
| `spec.when` | 可选通用状态条件，默认无条件。不成立时排除保留并报告 Pending/WhenFalse，服务器不可用时为 ServerPending。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `address`, `hostname`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)
