# DHCPv6PrefixDelegation

[各资源的配置](../resources.md#dhcpv6prefixdelegation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在上行接口取得 IPv6 委派前缀。下游或主机地址须以 IPv6DelegatedAddress 派生并应用，仅取得前缀不会向客户端通告。

## 指定条件与引用关系

- interface 须为现有逻辑接口名称，默认 client 为 routerd-dhcpv6-client。也识别 networkd、dhcp6c、dhcpcd 并生成平台配置，但实时监督与状态套接字以 routerd 守护进程为主，生命周期支持不应视为等同。
- profile/prefixLength 影响外部客户端配置及还原快照比对。受监督的 routerd-dhcpv6-client 不接收 profile 或 prefix-length 标志，获取时不以此提示长度；clientDUID 会传入。
- 不支持 iaid/duidType YAML 控制。routerd 守护进程 IA_PD IAID 默认 1，未指定 clientDUID 时由上行 MAC 派生 DUID-LL。required 不会实现阻挡启动的要求。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    [ client: <string> ]
    [ clientDUID: <string> ]
    interface: <string>
    [ prefixLength: <integer> ]
    [ profile: ("default" | "ntt-ngn-direct-hikari-denwa" | "ntt-hgw-lan-pd") ]
    [ required: <boolean> ]
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
| `spec.interface` | **必填** | string | 必填同一 Router 的逻辑上行接口。若 DHCPv6Address 也使用同一接口，外部 dhcp6c/dhcpcd PD client 须与该资源 client 相符。 |
| `spec.client` | *选填* | string | 默认 routerd-dhcpv6-client，接受 networkd/dhcp6c/dhcpcd。当前监督也可能合成 routerd DHCPv6 systemd 单元，选外部客户端不普遍保证禁用内置守护进程。 |
| `spec.profile` | *选填* | string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"] | 空值/default、ntt-ngn-direct-hikari-denwa 或 ntt-hgw-lan-pd。NTT 在支持的生成路径提供 /60 与 link-layer DUID 默认值，不配置运营商凭据或变更内置命令。 |
| `spec.prefixLength` | *选填* | integer; minimum=1; maximum=128 | 可选 1–128，省略或 0 时 NTT 为 /60，default 无长度提示，不保证分配长度，也不传入内置获取。HA 预置快照重用却要求有效长度完全相同：default 省略时为 0，不符合常规委派前缀，此路径须明确指定实际长度。 |
| `spec.clientDUID` | *选填* | string | 可选无分隔符且偶数长度的十六进制 DUID，拒绝冒号与空白。空值由内置客户端从 MAC 派生 DUID-LL，身份也影响恢复租约是否可复用。 |
| `spec.required` | *选填* | boolean | 布尔值默认 false，会解码但当前不作为必要性或启动等待策略。依赖方应使用实际 Bound 状态及依赖条件。 |
| `spec.when` | *选填* | object | 可选通用状态条件，默认无条件。监督客户端按已解析条件筛选，条件为假可在保留声明时停止获取。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/profiles.go](https://github.com/imksoo/routerd/blob/main/pkg/api/profiles.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-dhcpv6-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv6-client/main.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interface`, `currentPrefix`, `dnsServers`, `sntpServers`, `domainSearch`, `aftrName`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
