# BGPPeer

[各资源的配置](../resources.md#bgppeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在已声明的 BGPRouter 配置一个或多个静态 BGP 邻居。

## 指定条件与引用关系

- 对等端导入策略被视为已配置时，完整替换路由器策略，不逐字段合并。导出前缀则与路由器及运行时派生通告前缀求并集。
- 目前控制器同时启用 IPv4/IPv6 unicast。Established 会话不会自动授权所有路由安装至 FIB，也不证明数据转发可达。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPPeer
  metadata:
    name: "<name>"
  spec:
    bfd: "<string>" # 选填; string
    communities: # 选填; object
      accept: # 选填; array
        - "<string>" # string
      send: "" # 选填; string; enum=["", "standard", "extended", "both"]
      set: # 选填; object
        in: # 选填; array
          - "<string>" # string
        out: # 选填; array
          - "<string>" # string
    convergenceProfile: "" # 选填; string; enum=["", "default", "fast", "stable"]
    ebgpMultihop: 0 # 选填; integer; minimum=0; maximum=255
    exportPolicy: # 选填; object
      allowedPrefixes: # 选填; array
        - "<string>" # string
    importPolicy: # 选填; object
      allowedPrefixLengthMax: 0 # 选填; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 选填; integer; minimum=0; maximum=128
      allowedPrefixes: # 选填; array
        - "<string>" # string
      forbiddenCommunities: # 选填; array
        - "<string>" # string
      localPreference: 0 # 选填; integer
      nextHopRewrite: "" # 选填; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 选填; array
        - "<string>" # string
    passiveMode: true # 选填; boolean
    password: "<string>" # 选填; string
    passwordFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
    peerASN: 1 # 必填; integer; minimum=1
    peers: # 选填; array
      - "<string>" # string
    routeReflectorClient: true # 选填; boolean
    routeReflectorClusterID: "<string>" # 选填; string
    routerRef: "<string>" # 必填; string
    timers: # 选填; object
      profile: "" # 选填; string; enum=["", "default", "fast", "slow"]
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
| `spec.importPolicy` | 可选导入策略。对等端若无非空前缀、community 条件、下一跳改写或 localPreference，会完整继承路由器策略；仅前缀长度限制不算覆盖。 |
| `spec.importPolicy.allowedPrefixes` | 可选 IPv4/IPv6 CIDR 允许列表，按前缀及允许的更具体前缀长度匹配。拒绝掩码后重复的前缀。为空时，不在其他已配置导入条件上加入前缀条件。 |
| `spec.importPolicy.allowedPrefixLengthMin` | 整数 0–128。省略或 0 使用每个允许前缀自身长度。正值须大于等于各前缀长度、小于等于地址族上限及正数最大长度。 |
| `spec.importPolicy.allowedPrefixLengthMax` | 整数 0–128。省略或 0 时 IPv4 为 32、IPv6 为 128。正值须涵盖所有列出前缀长度且不超过地址族上限。动态 leaf 接纳要求 min=max=32。 |
| `spec.importPolicy.requiredCommunities` | 可选 community 列表；接受的路径必须包含全部列出值。空列表不增加必需 community 条件。 |
| `spec.importPolicy.forbiddenCommunities` | 可选禁止 community 列表；任一匹配就先于接受规则拒绝路径。空列表不增加禁止条件。 |
| `spec.importPolicy.nextHopRewrite` | 默认 peer-address；unchanged 保留通告的下一跳。在接受路径的选择及 FIB 投影之前应用。对等端只配置此字段也会建立自己的导入策略，不继承路由器允许列表。 |
| `spec.importPolicy.localPreference` | 可选 uint32 LOCAL_PREF，在最佳路径选择之前设置于接受的路径。默认 0 保留 BGP 实现默认值。 |
| `spec.exportPolicy` | 可选导出前缀过滤。对等端前缀会与路由器静态及动态通告合并；RR 客户端另加入路由器导入前缀，不是替换并限制所有路由器派生导出前缀。 |
| `spec.exportPolicy.allowedPrefixes` | 可选 CIDR 列表，默认为空。合并后非空列表允许列出前缀及更具体路由（至 /32 或 /128），拒绝不符路由。有效列表为空时不安装导出过滤。 |
| `spec.timers` | 可选计时器配置。目前控制器不将路由器级 timers 继承给对等端，需要时请在各对等端明确配置。 |
| `spec.timers.profile` | default 或省略：keepalive 30 秒、hold 90 秒、connectRetry 10 秒；fast 为 3/9/1 秒，slow 为 60/180/30 秒。不支持直接指定 keepalive/holdTime/connectRetry YAML。 |
| `spec.when` | 可选的通用启用条件，省略表示无条件。state/all/any、引用验证与模式限制请见通用字段。 |
| `spec.routerRef` | 必填 BGPRouter/name，引用同一 Router 配置中声明的路由器，不接受单独名称。 |
| `spec.peerASN` | 必填非零 uint32 远端 ASN，等于本机 ASN 时为 iBGP；routeReflectorClient 要求两者相等。 |
| `spec.peers` | 必填且不可为空的地址/主机名列表，移除首尾空白后不可重复或包含空白。配置验证不解析主机名，直接交给 GoBGP，应用时才确认接受性。不同 BGPPeer 的重复地址未在此拒绝，运行时共用同一地址键。 |
| `spec.passiveMode` | 默认 false 正常主动连接，true 配置 GoBGP passive transport 等待传入连接，不是动态来源前缀接纳规则。 |
| `spec.password` | 可选直接指定的 BGP TCP 认证密钥，默认无，与非空 passwordFrom 来源互斥，不是 SAM 注册令牌。 |
| `spec.passwordFrom` | 可选密钥来源，使用时 file/env 必须且只能指定一项。省略表示无外部密钥。不是状态值或 Kind/name 引用，运行时文件或环境变量缺失会使对等端协调失败。 |
| `spec.passwordFrom.file` | 路由器本机原始密码文本文件，与 env 及直接 password 互斥。移除末尾 CR/LF，其余空白保留。 |
| `spec.passwordFrom.env` | 存放密钥的 routerd 进程环境变量名称，不是密钥本身。与 file 及直接 password 互斥，不自动加载环境文件。 |
| `spec.passwordFrom.base64` | 默认 false。true 对选定 file/env 值执行标准 Base64 解码，再移除末尾 CR/LF。这是编码，不是加密。 |
| `spec.ebgpMultihop` | 整数 0–255，默认 0。仅大于 1 时启用 GoBGP eBGP multihop 并配置 TTL，0/1 保持禁用。 |
| `spec.routeReflectorClient` | 默认 false。true 标记 iBGP 路由反射器客户端，要求 peerASN 等于路由器 ASN，并将路由器导入前缀加入有效导出前缀。 |
| `spec.routeReflectorClusterID` | 可选 IPv4 格式集群 ID，仅 routeReflectorClient=true 时传入。空值由 GoBGP 决定，单独配置不启用反射。 |
| `spec.convergenceProfile` | 可选 default/fast/stable，省略继承 BGPRouter convergenceProfile。选择 graceful restart 默认值，不是 timers.profile；路由器 gracefulRestart.enabled 仍优先覆盖。 |
| `spec.bfd` | 可选的已声明 BFD/name 引用。其 peer 必须标识此 BGPPeer 或其中一个对等端地址；Up/Established 后 BFD Down 会触发 BGP 重置。省略不关联 BFD。 |
| `spec.communities` | 目前 BGPPeer 仅解码及验证，不在运行时使用。GoBGP SendCommunity 固定 both，实际导入匹配请配置 importPolicy。对等端 community 配置不继承或更改路由器本机通告 community。 |
| `spec.communities.send` | 目前 BGPPeer 仅解码及验证，不在运行时使用。GoBGP SendCommunity 固定 both，实际导入匹配请配置 importPolicy。对等端 community 配置不继承或更改路由器本机通告 community。 |
| `spec.communities.accept` | 目前 BGPPeer 仅解码及验证，不在运行时使用。GoBGP SendCommunity 固定 both，实际导入匹配请配置 importPolicy。对等端 community 配置不继承或更改路由器本机通告 community。 |
| `spec.communities.set` | 目前 BGPPeer 仅解码及验证，不在运行时使用。GoBGP SendCommunity 固定 both，实际导入匹配请配置 importPolicy。对等端 community 配置不继承或更改路由器本机通告 community。 |
| `spec.communities.set.in` | 目前 BGPPeer 仅解码及验证，不在运行时使用。GoBGP SendCommunity 固定 both，实际导入匹配请配置 importPolicy。对等端 community 配置不继承或更改路由器本机通告 community。 |
| `spec.communities.set.out` | 目前 BGPPeer 仅解码及验证，不在运行时使用。GoBGP SendCommunity 固定 both，实际导入匹配请配置 importPolicy。对等端 community 配置不继承或更改路由器本机通告 community。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
