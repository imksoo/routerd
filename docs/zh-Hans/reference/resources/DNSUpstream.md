# DNSUpstream

[各资源的配置](../resources.md#dnsupstream) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

供 DNSForwarder 引用的 DNS 服务器端点；仅声明不会启动监听或转发查询。

## 指定条件与引用关系

- protocol 必填，即使内部 URL 生成器有 udp 默认值。address 或 addressFrom 至少提供一个，可同时使用并合并端点。动态 addressFrom 当前仅支持 UDP。
- 必要 addressFrom 未解析时，即使有固定端点，仍暂停包含它的整个转发来源；可选值则跳过。bootstrap 不同：固定列表可补足必要动态 bootstrap 的缺值。
- sourceInterface 原样传入套接字绑定，不解析逻辑名称或 Kind/name。Linux 请声明 metadata.name 与 OS ifname 相同的接口。FreeBSD 套接字路径拒绝直接 ifname 绑定，而常规配置验证也未提供可用的 fib:<n> 替代方式。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    [ address: <string> ]
    [ addressFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ bootstrap:
        - <string>
    ]
    [ bootstrapFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ path: <string> ]
    [ port: <integer> ]
    protocol: ("udp" | "tcp" | "dot" | "doh")
    [ sourceInterface: <string> ]
    [ tlsName: <string> ]
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
| `spec.protocol` | **必填** | string; enum=["udp", "tcp", "dot", "doh"] | 必填 udp、tcp、dot 或 doh，请用标准小写。UDP/TCP 为明文 DNS，DoT 使用 TLS，DoH 使用 HTTPS。 |
| `spec.address` | **条件必填** | string | 不含协议、端口或路径的服务器 IP 或主机名；addressFrom 为空时必填。无隐含服务器，若提供 bootstrap 则用于名称解析。 |
| `spec.addressFrom` | **条件必填** | array | 可选且仅限 UDP 的状态值来源列表，每项须有已声明 resource 与明确提供字段；optional 默认 false。动态纯地址会独立于此资源 port 规范化为 UDP 53，可返回多个服务器。 |
| `spec.port` | *选填* | integer; minimum=1; maximum=65535 | 可选 1–65535；省略或 0 时 udp/tcp 为 53、dot 为 853、doh 为 443。用于固定 address，不应用于 addressFrom 返回的纯地址。 |
| `spec.path` | *选填* | string | DoH URL 路径，默认 /dns-query；doh 提供此值时须以 / 开头。其他协议不使用。 |
| `spec.tlsName` | *选填* | string | DoT 证书验证与 SNI 服务器名称，省略使用端点主机名。DoH 明确设置会被验证拒绝；UDP/TCP 虽可通过验证但会忽略。 |
| `spec.bootstrap` | *选填* | array | 可选的上游主机名解析 DNS 列表。与 bootstrapFrom 均空或省略且无必要依赖等待时，使用 OS 解析器。同一转发规则的上游共用合并后设置。 |
| `spec.bootstrapFrom` | *选填* | array | 可选且 field 必填的来源列表；resource/field 须非空，optional 默认 false。请使用现有 Kind/name 及其提供的状态字段；但此类型不同于常规 StatusValueSourceSpec，当前验证不检查目标存在或提供字段成员资格。动态与固定 bootstrap 合并，固定列表可补足必要动态值的缺值。 |
| `spec.sourceInterface` | *选填* | string | 可选的已声明 Interface 或 WireGuardInterface 名称；默认空值由 OS 选路。绑定由转发池共用，须注意上述原样名称及 FreeBSD 限制。 |
| `spec.when` | *选填* | object | 可选通用状态条件，默认无条件。筛除此上游可能使转发规则没有可用端点。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go) · [cmd/routerd-dns-resolver/upstream_bind_linux.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_linux.go) · [cmd/routerd-dns-resolver/upstream_bind_freebsd.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_freebsd.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `address`, `url`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
