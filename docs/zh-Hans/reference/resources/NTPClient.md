# NTPClient

[各资源的配置](../resources.md#ntpclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

管理或观察向上游同步时间的客户端。

## 指定条件与引用关系

- managed=true须声明servers/serverFrom/fallbackServers至少一项，static还要求servers本身。无法解析上游时客户端为Pending/NoServers；管理chrony时禁用系统systemd-timesyncd。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    [ fallbackServers:
        - <string>
    ]
    [ interface: <string> ]
    [ managed: <boolean> ]
    [ provider: ("systemd-timesyncd" | "chrony" | "ntpd") ]
    [ serverFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ servers:
        - <string>
    ]
    [ source: ("static" | "auto" | "dhcp" | "dhcpv6") ]
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
| `spec.provider` | *选填* | string; enum=["systemd-timesyncd", "chrony", "ntpd"] | 客户端实现，通常默认systemd-timesyncd，FreeBSD默认ntpd，也接受chrony。ntpd需要FreeBSD rc.d，systemd-timesyncd需要平台支持。 |
| `spec.managed` | *选填* | boolean | 默认false，仅记录Observed，不配置或启动时间服务；管理文件与服务时设true。 |
| `spec.source` | *选填* | string; enum=["static", "auto", "dhcp", "dhcpv6"] | 默认static，使用servers。auto/dhcp/dhcpv6均依serverFrom解析值、fallbackServers、servers的顺序选择，不会自动查找DHCP来源。 |
| `spec.interface` | *选填* | string | 选填逻辑接口名，默认空；指定时Router整体验证要求存在。NTP controller不使用它选择来源或绑定服务。 |
| `spec.servers` | **条件必填** | array | 上游主机名或IP，默认空；managed=true且source省略或static时必填。各项须为非空白单一值，不是资源引用。 |
| `spec.serverFrom` | *选填* | array | 显式值来源列表，默认空。例如DHCPv4Client/wan的ntpServers或DHCPv6PrefixDelegation/wan-pd的sntpServers。仅用于auto/dhcp/dhcpv6，每个resource与field须存在。 此解析器不使用optional标记，缺值即选下一种替代来源。 |
| `spec.fallbackServers` | *选填* | array | 备用上游，默认空；动态来源的serverFrom无值时采用。systemd-timesyncd还会生成FallbackNTP，chrony与ntpd不生成独立备用指令。 |
| `spec.when` | *选填* | object | 启用条件，省略不设限制；state/all/any、unknown及时间条件请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `servers`, `source`, `updatedAt`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)
