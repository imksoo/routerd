# DNSForwarder

[各资源的配置](../resources.md#dnsforwarder) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

按查询名称为一个 DNSResolver 选择本地区域或上游 DNS 的规则。

## 指定条件与引用关系

- resolver 与非空 match 必填，且 zoneRefs 与 upstreams 恰有一个非空列表。所有引用均须存在于同一 Router，可用裸名或对应 Kind/name。
- 监听端点筛选后按 Router 资源顺序评估，并非最长后缀优先。内部或特定规则应置于全局规则之前。已选上游失败时返回 SERVFAIL，不会转往其他来源泄漏内部查询。
- 多个上游引用合并为一个来源池。bootstrap 合并，第一个非空 sourceInterface 由整个池共用；不会分别遵循互相冲突的上游接口设置。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    [ dnssecValidate: <boolean> ]
    [ healthcheck:
        [ failThreshold: <integer> ]
        [ interval: <string> ]
        [ passThreshold: <integer> ]
        [ timeout: <string> ]
    ]
    match:
      - <string>
    [ queryTimeout: <string> ]
    resolver: <string>
    [ upstreams:
        - <string>
    ]
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
    [ zoneRefs:
        - <string>
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.resolver` | **必填** | string | 必填的现有 DNSResolver 名称或 DNSResolver/name，无默认值。 |
| `spec.match` | **必填** | array | 必填非空域名后缀列表；忽略大小写与末尾句点。example.net 匹配本身及子域名，"." 匹配全部。无隐含全局规则。 |
| `spec.zoneRefs` | **条件必填** | array | 供本地应答的现有 DNSZone 名称或 DNSZone/name，与 upstreams 互斥；upstreams 为空时必填。 |
| `spec.upstreams` | **条件必填** | array | 现有 DNSUpstream 名称或 DNSUpstream/name，不是直接 IP 或 URL。与 zoneRefs 互斥；各资源的服务器组成一个故障转移池。 |
| `spec.dnssecValidate` | *选填* | boolean | 默认 false；true 设置上游查询 DO 位并要求响应 AD，不执行本机密码学 DNSSEC 验证。区域来源不使用此字段。 |
| `spec.queryTimeout` | *选填* | string | 每次上游查询尝试的正 Go duration。省略时继承正的 healthcheck.timeout，否则为 3s。不同于后台探测间隔，区域响应不使用。 |
| `spec.healthcheck` | *选填* | object | 上游后台探测选项，默认 interval 15s、timeout 3s、failThreshold 3、passThreshold 2；本地区域来源不使用。 |
| `spec.healthcheck.interval` | *选填* | string | Go duration；省略、空值或运行时非正值使用 15s。配置验证仅检查语法，不强制正值。 |
| `spec.healthcheck.timeout` | *选填* | string | 探测 Go duration；省略、空值或非正值使用 3s。正值亦作为省略 queryTimeout 的默认值。 |
| `spec.healthcheck.failThreshold` | *选填* | integer; minimum=1 | 标记上游异常的连续失败次数，默认 3；运行时非正值亦为 3，模式下限 1。 |
| `spec.healthcheck.passThreshold` | *选填* | integer; minimum=1 | 恢复所需的连续成功次数，默认 2；运行时非正值亦为 2，模式下限 1。 |
| `spec.when` | *选填* | object | 可选通用状态条件；省略表示无条件，可将此规则从有效解析器配置排除。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `resolver`, `upstreams`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
