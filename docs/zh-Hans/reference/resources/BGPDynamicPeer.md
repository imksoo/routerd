# BGPDynamicPeer

[各资源的配置](../resources.md#bgpdynamicpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

定义 GoBGP 传入动态邻居接纳组，不是单独对等端或 WireGuard/SAM 授权身份。

## 指定条件与引用关系

- 对等端导入策略被视为已配置时，完整替换路由器策略，不逐字段合并。导出前缀则与路由器及运行时派生通告前缀求并集。
- 目前控制器同时启用 IPv4/IPv6 unicast。Established 会话不会自动授权所有路由安装至 FIB，也不证明数据转发可达。
- Ready 表示已应用对等端组及来源前缀接纳规则，不表示已有动态邻居 Established。动态对等端 GR 默认值来自路由器收敛/GR 配置。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPDynamicPeer
  metadata:
    name: "<name>"
  spec:
    [ ebgpMultihop: <integer> ]
    [ exportPolicy:
        [ allowedPrefixes:
            - <string>
        ]
    ]
    [ importPolicy:
        [ allowedPrefixLengthMax: <integer> ]
        [ allowedPrefixLengthMin: <integer> ]
        [ allowedPrefixes:
            - <string>
        ]
        [ forbiddenCommunities:
            - <string>
        ]
        [ localPreference: <integer> ]
        [ nextHopRewrite: ("" | "peer-address" | "unchanged") ]
        [ requiredCommunities:
            - <string>
        ]
    ]
    listen:
      sourcePrefixes:
        - <string>
    [ password: <string> ]
    [ passwordFrom:
        [ base64: <boolean> ]
        [ env: <string> ]
        [ file: <string> ]
    ]
    peerASN: <integer>
    [ routeReflectorClient: <boolean> ]
    [ routeReflectorClusterID: <string> ]
    routerRef: <string>
    [ timers:
        [ profile: ("" | "default" | "fast" | "slow") ]
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
| `spec.importPolicy` | *选填* | object | 动态 leaf 路由接纳要求有效导入具有非空 IPv4 允许列表及 min=max=32。对等端允许列表为空时，验证完整采用路由器策略；对等端 nextHopRewrite 无自己的允许列表会被拒绝。请在此指定完整策略，避免仅覆盖附加字段。 |
| `spec.importPolicy.allowedPrefixes` | *选填* | array | 可选 IPv4/IPv6 CIDR 允许列表，按前缀及允许的更具体前缀长度匹配。拒绝掩码后重复的前缀。为空时，不在其他已配置导入条件上加入前缀条件。 |
| `spec.importPolicy.allowedPrefixLengthMin` | *选填* | integer; minimum=0; maximum=128 | 整数 0–128。省略或 0 使用每个允许前缀自身长度。正值须大于等于各前缀长度、小于等于地址族上限及正数最大长度。 |
| `spec.importPolicy.allowedPrefixLengthMax` | *选填* | integer; minimum=0; maximum=128 | 整数 0–128。省略或 0 时 IPv4 为 32、IPv6 为 128。正值须涵盖所有列出前缀长度且不超过地址族上限。动态 leaf 接纳要求 min=max=32。 |
| `spec.importPolicy.requiredCommunities` | *选填* | array | 可选 community 列表；接受的路径必须包含全部列出值。空列表不增加必需 community 条件。 |
| `spec.importPolicy.forbiddenCommunities` | *选填* | array | 可选禁止 community 列表；任一匹配就先于接受规则拒绝路径。空列表不增加禁止条件。 |
| `spec.importPolicy.nextHopRewrite` | *选填* | string; enum=["", "peer-address", "unchanged"] | 默认 peer-address；unchanged 保留通告的下一跳。在接受路径的选择及 FIB 投影之前应用。对等端只配置此字段也会建立自己的导入策略，不继承路由器允许列表。 |
| `spec.importPolicy.localPreference` | *选填* | integer | 可选 uint32 LOCAL_PREF，在最佳路径选择之前设置于接受的路径。默认 0 保留 BGP 实现默认值。 |
| `spec.exportPolicy` | *选填* | object | 可选导出前缀过滤。对等端前缀会与路由器静态及动态通告合并；RR 客户端另加入路由器导入前缀，不是替换并限制所有路由器派生导出前缀。 |
| `spec.exportPolicy.allowedPrefixes` | *选填* | array | 可选 CIDR 列表，默认为空。合并后非空列表允许列出前缀及更具体路由（至 /32 或 /128），拒绝不符路由。有效列表为空时不安装导出过滤。 |
| `spec.timers` | *选填* | object | 可选计时器配置。目前控制器不将路由器级 timers 继承给对等端，需要时请在各对等端明确配置。 |
| `spec.timers.profile` | *选填* | string; enum=["", "default", "fast", "slow"] | default 或省略：keepalive 30 秒、hold 90 秒、connectRetry 10 秒；fast 为 3/9/1 秒，slow 为 60/180/30 秒。不支持直接指定 keepalive/holdTime/connectRetry YAML。 |
| `spec.when` | *选填* | object | 可选的通用启用条件，省略表示无条件。state/all/any、引用验证与模式限制请见通用字段。 |
| `spec.routerRef` | **必填** | string | 必填 BGPRouter/name，引用同一 Router 配置中声明的路由器，不接受单独名称。 |
| `spec.peerASN` | **必填** | integer; minimum=1 | 必填非零 uint32 远端 ASN，等于本机 ASN 时为 iBGP；routeReflectorClient 要求两者相等。 |
| `spec.password` | *选填* | string | 可选直接指定的 BGP TCP 认证密钥，默认无，与非空 passwordFrom 来源互斥，不是 SAM 注册令牌。 |
| `spec.passwordFrom` | *选填* | object | 可选密钥来源，使用时 file/env 必须且只能指定一项。省略表示无外部密钥。不是状态值或 Kind/name 引用，运行时文件或环境变量缺失会使对等端协调失败。 |
| `spec.passwordFrom.file` | *选填* | string | 路由器本机原始密码文本文件，与 env 及直接 password 互斥。移除末尾 CR/LF，其余空白保留。 |
| `spec.passwordFrom.env` | *选填* | string | 存放密钥的 routerd 进程环境变量名称，不是密钥本身。与 file 及直接 password 互斥，不自动加载环境文件。 |
| `spec.passwordFrom.base64` | *选填* | boolean | 默认 false。true 对选定 file/env 值执行标准 Base64 解码，再移除末尾 CR/LF。这是编码，不是加密。 |
| `spec.ebgpMultihop` | *选填* | integer; minimum=0; maximum=255 | 整数 0–255，默认 0。仅大于 1 时启用 GoBGP eBGP multihop 并配置 TTL，0/1 保持禁用。 |
| `spec.routeReflectorClient` | *选填* | boolean | 默认 false。true 标记 iBGP 路由反射器客户端，要求 peerASN 等于路由器 ASN，并将路由器导入前缀加入有效导出前缀。 |
| `spec.routeReflectorClusterID` | *选填* | string | 可选 IPv4 格式集群 ID，仅 routeReflectorClient=true 时传入。空值由 GoBGP 决定，单独配置不启用反射。 |
| `spec.listen` | **必填** | object | 必填的传入接纳规则，不是发现的对等端或 SAM 注册对象。 |
| `spec.listen.sourcePrefixes` | **必填** | array | 必填且不可为空的 GoBGP 动态邻居来源 IPv4/IPv6 CIDR 列表，拒绝掩码后重复。限制传入 TCP 对等端地址，不是可通告路由；路由接纳由 importPolicy 另行处理。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `peerGroup`, `sourcePrefixes`, `sourcePrefixCount`, `discoveredPeers`, `discoveredPeerCount`, `acceptedRouteCount`, `rejectedRouteCount`, `rejectedRouteSummary`, `observedAt`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
