# DNSResolver

[各资源的配置](../resources.md#dnsresolver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

具有 UDP 与 TCP 监听端点、连接 DNSForwarder 规则的本机 DNS 服务。

## 指定条件与引用关系

- 至少一个 DNSForwarder 必须引用此解析器。请以独立资源配置转发规则，旧式内嵌 sources 并非支持的 YAML 输入。
- 省略 listen 或使用空列表会规范化为 127.0.0.1:5053。每个端点均提供 UDP 与 TCP。允许递归查询的端点应限于可信任网络。
- 固定与动态监听地址会合并去重。必要动态值未解析时会记录等待依赖，但仍使用其他有效地址；若无任何可用端点则维持 Pending。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    [ cache:
        [ enabled: <boolean> ]
        [ maxEntries: <integer> ]
        [ maxTTL: <string> ]
        [ minTTL: <string> ]
        [ negativeTTL: <string> ]
    ]
    listen:
      -
        [ addressFrom:
            -
              field: <string>
              [ optional: <boolean> ]
              resource: <string>
        ]
        [ addresses:
            - <string>
        ]
        [ name: <string> ]
        [ port: <integer> ]
        [ sources:
            - <string>
        ]
    [ metrics:
        [ perUpstream: <boolean> ]
    ]
    [ queryLog:
        [ enabled: <boolean> ]
        [ path: <string> ]
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
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.listen` | **必填** | array | 监听端点列表；为空时补入回环端点。 |
| `spec.listen[].name` | *选填* | string | 可选标签，按列表顺序默认 listen-0、listen-1……。 |
| `spec.listen[].addresses` | *选填* | array | 不含端口或 CIDR 的 IPv4/IPv6 地址；与 addressFrom 同时为空时默认 [127.0.0.1]。 |
| `spec.listen[].addressFrom` | *选填* | array | 可选的通用 resource/field/optional 来源列表。须有已声明资源与明确的提供字段。IPv4StaticAddress.address 可在状态建立前读取声明值，CIDR 取地址部分。optional 默认 false；可选值未解析则跳过。 |
| `spec.listen[].port` | *选填* | integer; minimum=1; maximum=65535 | 可选端口 1–65535，省略或 0 会规范化为 5053。 |
| `spec.listen[].sources` | *选填* | array | 此解析器所属 DNSForwarder 名称列表；空值选取全部。请用裸 metadata.name：验证虽接受带类型前缀的名称，但守护进程不移除前缀。列表顺序不会重排转发规则。 |
| `spec.cache` | *选填* | object | 可选的响应缓存设置。仅缓存上游响应，不缓存本地区域响应；默认禁用。 |
| `spec.cache.enabled` | *选填* | boolean | 默认 false；true 启用内存响应缓存。 |
| `spec.cache.maxEntries` | *选填* | integer; minimum=0 | 省略或 0 时为 10000；模式要求非负值。限制条目数而非字节数。 |
| `spec.cache.minTTL` | *选填* | string | 可选的 Go duration 缓存时间下限；空值不限制。无效时长在此路径会被忽略，不会改写响应中的 TTL。 |
| `spec.cache.maxTTL` | *选填* | string | 可选的 Go duration 上限，在 minTTL 之后应用；空值不限制，无效时长忽略。 |
| `spec.cache.negativeTTL` | *选填* | string | 可选的 Go duration，在 min/max 之后覆盖空的成功响应或 NXDOMAIN 保留时间。默认空值使用应用上下限前的 60 秒；不缓存 SERVFAIL 等暂时失败。 |
| `spec.metrics` | *选填* | object | 指标选项；当前运行代码未使用 perUpstream。 |
| `spec.metrics.perUpstream` | *选填* | boolean | 解码为布尔值，默认 false；当前不切换各上游指标。 |
| `spec.queryLog` | *选填* | object | 可选的 SQLite 查询记录，默认禁用。 |
| `spec.queryLog.enabled` | *选填* | boolean | 默认 false；true 按 queryLog.path 记录查询。 |
| `spec.queryLog.path` | *选填* | string | 启用时省略或空白默认 /var/lib/routerd/dns-queries.db，为数据库路径而非文本日志。 |
| `spec.when` | *选填* | object | 可选通用状态条件，默认无条件。state/all/any 与模式限制请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `listeners`, `listenAddresses`, `sources`, `updatedAt`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
