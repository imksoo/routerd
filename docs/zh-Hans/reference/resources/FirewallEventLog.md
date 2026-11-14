# FirewallEventLog

[各资源的配置](../resources.md#firewalleventlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

启用防火墙数据包日志收集器，配置数据库、Linux NFLOG 组与规则生成时的日志选项。部分接受的筛选与归档字段当前没有运行时使用者。

## 指定条件与引用关系

- enabled 默认 false。Linux 从 NFLOG 收集，FreeBSD 使用 pflog0。此配置不会让所有允许或拒绝规则都记录；FirewallRule.log 与 FirewallPolicy.logDeny 也决定哪些数据包被记录。
- 请仅启用一个资源：规则生成使用第一个启用资源的选项，服务生成则共用 routerd-firewall-logger 名称。多个资源名称不会创建独立收集器。
- fromZones/toZones 须为已有 FirewallZone 名称或 self；rules 接受 FirewallRule/name 或单独规则名称；sinks 接受 LogSink/name 或单独输出名称；retention 接受 LogRetention/name 或单独名称。虽然当前不会用这些字段筛选、导出或选择保留规则，仍会验证引用。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    [ enabled: <boolean> ]
    [ events:
        - ("deny" | "allow" | "rateLimit" | "connLimit")
    ]
    [ fromZones:
        - <string>
    ]
    [ log:
        [ acceptSampleRate: <integer> ]
        [ copyRange: <integer> ]
        [ dropSampleRate: <integer> ]
        [ reject: <boolean> ]
    ]
    [ nflogGroup: <integer> ]
    [ path: <string> ]
    [ retention: <string> ]
    [ rules:
        - <string>
    ]
    [ sampleRate: <integer> ]
    [ sinks:
        - <string>
    ]
    [ toZones:
        - <string>
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.enabled` | *选填* | boolean | 默认 false。true 请求收集服务并启用组日志选项；当前服务生成也会请求 DPI 分类辅助程序。 |
| `spec.path` | **条件必填** | string | 可选 SQLite 数据库路径。Linux 默认 /var/lib/routerd/firewall-logs.db，FreeBSD 为 /var/db/routerd/firewall-logs.db。这是数据库，不是逐行 JSON 文件。 |
| `spec.events` | *选填* | array | 接受 deny、allow、rateLimit、connLimit，默认空。当前仅验证，不筛选收集输入或生成的防火墙日志。 |
| `spec.fromZones` | *选填* | array | 默认空。源区域须为 FirewallZone 名称或 self，但运行时不作日志筛选。 |
| `spec.fromZones[]` | **条件必填** | string | 已有 FirewallZone 的 metadata.name 或特殊 self 区域，不是 Kind/name。当前不影响筛选。 |
| `spec.toZones` | *选填* | array | 默认空。目标区域须为 FirewallZone 名称或 self，但运行时不作日志筛选。 |
| `spec.rules` | *选填* | array | 默认空。指定时须引用已有 FirewallRule（单独名称或 FirewallRule/name），当前不选择记录规则。请设置实际规则的 FirewallRule.log。 |
| `spec.sampleRate` | *选填* | integer; minimum=0 | 非负整数，默认 0；收集器与规则生成器均未使用。支持的生成控制请用 log.acceptSampleRate/log.dropSampleRate。 |
| `spec.sinks` | *选填* | array | 默认空。会验证已有 LogSink 引用，但收集器不会导出至这些目标，输出为 path 数据库。 |
| `spec.retention` | *选填* | string | 可选的已有 LogRetention 引用，默认空；当前不读取以选择保留规则。请在 LogRetention 配置 firewallEvents 信号以处理此数据库。 |
| `spec.nflogGroup` | *选填* | integer; minimum=0; maximum=65535 | 整数 0–65535；Linux 省略或 0 时，nftables 与收集器都使用组 1。FreeBSD 使用 pflog0，忽略此组。 |
| `spec.log` | *选填* | object | 可选的规则生成日志选项，省略字段为 0/false。这些选项不会实现未使用的顶层事件、区域或规则筛选。 |
| `spec.log.acceptSampleRate` | *选填* | integer; minimum=0 | 默认 0 禁用额外的隐式允许日志。nftables 正 N 约记录合格数据包的 1/N（1 全部记录）。PF 仅把正值当启用开关，不做 1/N 采样。明确的 FirewallRule.log 另行控制。 |
| `spec.log.dropSampleRate` | *选填* | integer; minimum=0 | 默认 0。对已启用的隐式拒绝日志，nftables 0/1 全部记录，N>1 约采样 1/N。此值不会自行启用 FirewallPolicy.logDeny。PF 不使用此采样值。 |
| `spec.log.copyRange` | *选填* | integer; minimum=0 | 非负的 NFLOG snaplen 字节数。默认 0 省略 nftables snaplen 参数；正值限制复制的数据包长度。PF 不应用。 |
| `spec.log.reject` | *选填* | boolean | 默认 false；当前规则生成器与收集器不读取。不改变规则裁决，也不启用 reject 日志。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/render/firewall_logger.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_logger.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `path`, `sinks`
