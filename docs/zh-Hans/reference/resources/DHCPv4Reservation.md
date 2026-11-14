# DHCPv4Reservation

[各资源的配置](../resources.md#dhcpv4reservation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在已声明的 DHCPv4Server 为一个 MAC 地址保留 IPv4 地址。

## 指定条件与引用关系

- 虽然模式将 server 标为可选，server、macAddress、ipAddress 在语义上必填。server 须为 interface 非空的 DHCPv4Server 裸名；常规验证即使仅有一台也不允许省略。scope 会被拒绝。
- 当前在共用 dnsmasq 主机附属文件写入带资源标签的保留。server 关联用于验证、状态及自定义选项生成，不代表独立进程或主机文件命名空间。Applied 不证明客户端已取得租约。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    [ hostname: <string> ]
    ipAddress: <string>
    [ leaseTime: <string> ]
    macAddress: <string>
    [ options:
        -
          [ code: <integer> ]
          [ name: <string> ]
          [ value: <string> ]
    ]
    [ scope: <string> ]
    server: <string>
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.scope` | *选填* | string | 已移除字段，非空值会被拒绝，请用 server。 |
| `spec.server` | **必填** | string | 必填已设置 interface 的现有直接 DHCPv4Server metadata.name，此处不移除 Kind/name 前缀。 |
| `spec.macAddress` | **必填** | string | 必填可由 net.ParseMAC 解析的 MAC 地址，以小写输出，无默认值。 |
| `spec.hostname` | *选填* | string | 可选且不含空白或逗号的主机名，默认省略。可供已启用 DNSZone 的 DHCP 派生主机快照使用。 |
| `spec.ipAddress` | **必填** | string | 必填纯 IPv4 保留地址。当前验证地址族，但此处不要求位于服务器地址池内，也不拒绝重复保留 IP/MAC。 |
| `spec.leaseTime` | *选填* | string | 可选后端时长字符串，默认空值且拒绝逗号。当前运行时保留主机行不输出此字段，因此不会在此路径覆盖租约时间。 |
| `spec.options` | *选填* | array | 以此保留标签输出的可选 DHCP 选项列表，每项须有 code/name 其中之一及非空 value，默认空。 |
| `spec.options[].code` | **条件必填** | integer; minimum=1; maximum=65535 | 数字选项标识符，与 name 互斥，模式范围 1–65535，无默认标识符。 |
| `spec.options[].name` | **条件必填** | string | dnsmasq 选项名称，与 code 互斥，不可含空白或逗号。 |
| `spec.options[].value` | *选填* | string | 必填非空后端格式值，不可含 CR/LF；原样传入，逗号可分隔多个选项值。 |
| `spec.when` | *选填* | object | 可选通用状态条件，默认无条件。不成立时排除保留并报告 Pending/WhenFalse，服务器不可用时为 ServerPending。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `address`, `hostname`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)
