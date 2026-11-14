# DNSResolver

[各资源的配置](../resources.md#dnsresolver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

具有 UDP 与 TCP 监听端点、连接 DNSForwarder 规则的本机 DNS 服务。

## 指定条件与引用关系

- 至少一个 DNSForwarder 必须引用此解析器。请以独立资源配置转发规则，旧式内嵌 sources 并非支持的 YAML 输入。
- 省略 listen 或使用空列表会规范化为 127.0.0.1:5053。每个端点均提供 UDP 与 TCP。允许递归查询的端点应限于可信任网络。
- 固定与动态监听地址会合并去重。必要动态值未解析时会记录等待依赖，但仍使用其他有效地址；若无任何可用端点则维持 Pending。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    cache: # 选填; object
      enabled: true # 选填; boolean
      maxEntries: 0 # 选填; integer; minimum=0
      maxTTL: "<string>" # 选填; string
      minTTL: "<string>" # 选填; string
      negativeTTL: "<string>" # 选填; string
    listen: # 必填; array
      - # object
        addressFrom: # 选填; array
          - # object
            field: "<string>" # 选填; string
            optional: true # 选填; boolean
            resource: "<string>" # 必填; string
        addresses: # 选填; array
          - "<string>" # string
        name: "<string>" # 选填; string
        port: 1 # 选填; integer; minimum=1; maximum=65535
        sources: # 选填; array
          - "<string>" # string
    metrics: # 选填; object
      perUpstream: true # 选填; boolean
    queryLog: # 选填; object
      enabled: true # 选填; boolean
      path: "<string>" # 选填; string
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
| `spec.listen` | 监听端点列表；为空时补入回环端点。 |
| `spec.listen[].name` | 可选标签，按列表顺序默认 listen-0、listen-1……。 |
| `spec.listen[].addresses` | 不含端口或 CIDR 的 IPv4/IPv6 地址；与 addressFrom 同时为空时默认 [127.0.0.1]。 |
| `spec.listen[].addressFrom` | 可选的通用 resource/field/optional 来源列表。须有已声明资源与明确的提供字段。IPv4StaticAddress.address 可在状态建立前读取声明值，CIDR 取地址部分。optional 默认 false；可选值未解析则跳过。 |
| `spec.listen[].port` | 可选端口 1–65535，省略或 0 会规范化为 5053。 |
| `spec.listen[].sources` | 此解析器所属 DNSForwarder 名称列表；空值选取全部。请用裸 metadata.name：验证虽接受带类型前缀的名称，但守护进程不移除前缀。列表顺序不会重排转发规则。 |
| `spec.cache` | 可选的响应缓存设置。仅缓存上游响应，不缓存本地区域响应；默认禁用。 |
| `spec.cache.enabled` | 默认 false；true 启用内存响应缓存。 |
| `spec.cache.maxEntries` | 省略或 0 时为 10000；模式要求非负值。限制条目数而非字节数。 |
| `spec.cache.minTTL` | 可选的 Go duration 缓存时间下限；空值不限制。无效时长在此路径会被忽略，不会改写响应中的 TTL。 |
| `spec.cache.maxTTL` | 可选的 Go duration 上限，在 minTTL 之后应用；空值不限制，无效时长忽略。 |
| `spec.cache.negativeTTL` | 可选的 Go duration，在 min/max 之后覆盖空的成功响应或 NXDOMAIN 保留时间。默认空值使用应用上下限前的 60 秒；不缓存 SERVFAIL 等暂时失败。 |
| `spec.metrics` | 指标选项；当前运行代码未使用 perUpstream。 |
| `spec.metrics.perUpstream` | 解码为布尔值，默认 false；当前不切换各上游指标。 |
| `spec.queryLog` | 可选的 SQLite 查询记录，默认禁用。 |
| `spec.queryLog.enabled` | 默认 false；true 按 queryLog.path 记录查询。 |
| `spec.queryLog.path` | 启用时省略或空白默认 /var/lib/routerd/dns-queries.db，为数据库路径而非文本日志。 |
| `spec.when` | 可选通用状态条件，默认无条件。state/all/any 与模式限制请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
