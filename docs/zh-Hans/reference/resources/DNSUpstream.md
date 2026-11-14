# DNSUpstream

[各资源的配置](../resources.md#dnsupstream) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

供 DNSForwarder 引用的 DNS 服务器端点；仅声明不会启动监听或转发查询。

## 指定条件与引用关系

- protocol 必填，即使内部 URL 生成器有 udp 默认值。address 或 addressFrom 至少提供一个，可同时使用并合并端点。动态 addressFrom 当前仅支持 UDP。
- 必要 addressFrom 未解析时，即使有固定端点，仍暂停包含它的整个转发来源；可选值则跳过。bootstrap 不同：固定列表可补足必要动态 bootstrap 的缺值。
- sourceInterface 原样传入套接字绑定，不解析逻辑名称或 Kind/name。Linux 请声明 metadata.name 与 OS ifname 相同的接口。FreeBSD 套接字路径拒绝直接 ifname 绑定，而常规配置验证也未提供可用的 fib:<n> 替代方式。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 选填; string
    addressFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    bootstrap: # 选填; array
      - "<string>" # string
    bootstrapFrom: # 选填; array
      - # object
        field: "<string>" # 必填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    path: "<string>" # 选填; string
    port: 1 # 选填; integer; minimum=1; maximum=65535
    protocol: "udp" # 必填; string; enum=["udp", "tcp", "dot", "doh"]
    sourceInterface: "<string>" # 选填; string
    tlsName: "<string>" # 选填; string
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
| `spec.protocol` | 必填 udp、tcp、dot 或 doh，请用标准小写。UDP/TCP 为明文 DNS，DoT 使用 TLS，DoH 使用 HTTPS。 |
| `spec.address` | 不含协议、端口或路径的服务器 IP 或主机名；addressFrom 为空时必填。无隐含服务器，若提供 bootstrap 则用于名称解析。 |
| `spec.addressFrom` | 可选且仅限 UDP 的状态值来源列表，每项须有已声明 resource 与明确提供字段；optional 默认 false。动态纯地址会独立于此资源 port 规范化为 UDP 53，可返回多个服务器。 |
| `spec.port` | 可选 1–65535；省略或 0 时 udp/tcp 为 53、dot 为 853、doh 为 443。用于固定 address，不应用于 addressFrom 返回的纯地址。 |
| `spec.path` | DoH URL 路径，默认 /dns-query；doh 提供此值时须以 / 开头。其他协议不使用。 |
| `spec.tlsName` | DoT 证书验证与 SNI 服务器名称，省略使用端点主机名。DoH 明确设置会被验证拒绝；UDP/TCP 虽可通过验证但会忽略。 |
| `spec.bootstrap` | 可选的上游主机名解析 DNS 列表。与 bootstrapFrom 均空或省略且无必要依赖等待时，使用 OS 解析器。同一转发规则的上游共用合并后设置。 |
| `spec.bootstrapFrom` | 可选且 field 必填的状态来源列表；resource 与 field 须存在，optional 默认 false。动态与固定服务器合并；固定 bootstrap 可补足必要动态值的缺值。 |
| `spec.sourceInterface` | 可选的已声明 Interface 或 WireGuardInterface 名称；默认空值由 OS 选路。绑定由转发池共用，须注意上述原样名称及 FreeBSD 限制。 |
| `spec.when` | 可选通用状态条件，默认无条件。筛除此上游可能使转发规则没有可用端点。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go) · [cmd/routerd-dns-resolver/upstream_bind_linux.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_linux.go) · [cmd/routerd-dns-resolver/upstream_bind_freebsd.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_freebsd.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
