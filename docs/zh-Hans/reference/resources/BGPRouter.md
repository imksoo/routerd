# BGPRouter

[各资源的配置](../resources.md#bgprouter) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

配置本机 GoBGP 实例、接纳路由及本机起源通告。内核安装与远端可达性不同于 BGP Established 状态。

## 指定条件与引用关系

- 实际操作目前只支持一个 BGPRouter 且不支持 VRF。验证接受不同 ASN/VRF/监听实例不代表运行时支持。
- 路由器导入、connected 与 static 重分发三组前缀不可跨组重叠；列表内掩码后重复也会拒绝。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # 必填; integer; minimum=1
    backend: "" # 选填; string; enum=["", "gobgp"]
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
    exportPolicy: # 选填; object
      allowedPrefixes: # 选填; array
        - "<string>" # string
    gracefulRestart: # 选填; object
      enabled: true # 选填; boolean
      restartTime: "<string>" # 选填; string
      stalePathTime: "<string>" # 选填; string
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
    listen: # 选填; object
      address: "<string>" # 选填; string
      port: 1 # 选填; integer; minimum=1; maximum=65535
    redistribute: # 选填; object
      connected: # 选填; object
        allowedPrefixes: # 选填; array
          - "<string>" # string
      static: # 选填; object
        allowedPrefixes: # 选填; array
          - "<string>" # string
    routerID: "<string>" # 必填; string
    timers: # 选填; object
      profile: "" # 选填; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # 选填; string
    watcher: # 选填; object
      maxPrefixes: 1 # 选填; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # 选填; string
      pollInterval: "<string>" # 选填; string
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
| `spec.importPolicy` | 无覆盖对等端继承的可选路由器导入策略。空配置不安装路由器导入过滤，但不会自动授权所有收到路由安装至内核 FIB。 |
| `spec.importPolicy.allowedPrefixes` | 可选 IPv4/IPv6 CIDR 允许列表，按前缀及允许的更具体前缀长度匹配。拒绝掩码后重复的前缀。为空时，不在其他已配置导入条件上加入前缀条件。 |
| `spec.importPolicy.allowedPrefixLengthMin` | 整数 0–128。省略或 0 使用每个允许前缀自身长度。正值须大于等于各前缀长度、小于等于地址族上限及正数最大长度。 |
| `spec.importPolicy.allowedPrefixLengthMax` | 整数 0–128。省略或 0 时 IPv4 为 32、IPv6 为 128。正值须涵盖所有列出前缀长度且不超过地址族上限。动态 leaf 接纳要求 min=max=32。 |
| `spec.importPolicy.requiredCommunities` | 可选 community 列表；接受的路径必须包含全部列出值。空列表不增加必需 community 条件。 |
| `spec.importPolicy.forbiddenCommunities` | 可选禁止 community 列表；任一匹配就先于接受规则拒绝路径。空列表不增加禁止条件。 |
| `spec.importPolicy.nextHopRewrite` | 默认 peer-address；unchanged 保留通告的下一跳。在接受路径的选择及 FIB 投影之前应用。对等端只配置此字段也会建立自己的导入策略，不继承路由器允许列表。 |
| `spec.importPolicy.localPreference` | 可选 uint32 LOCAL_PREF，在最佳路径选择之前设置于接受的路径。默认 0 保留 BGP 实现默认值。 |
| `spec.exportPolicy` | 可选导出前缀过滤。对等端前缀会与路由器静态及动态通告合并；RR 客户端另加入路由器导入前缀，不是替换并限制所有路由器派生导出前缀。 |
| `spec.exportPolicy.allowedPrefixes` | 明确的本机起源通告 CIDR，默认为空，与 redistribute.connected/static 及动态通告合并，也用于对等端导出过滤。不检查 OS 路由存在就会起源通告。 |
| `spec.timers` | 可选计时器配置。目前控制器不将路由器级 timers 继承给对等端，需要时请在各对等端明确配置。 |
| `spec.timers.profile` | 接受 default/fast/slow，但目前运行时不以路由器级值建立对等端计时器，请配置 BGPPeer/BGPDynamicPeer 的 timers.profile。 |
| `spec.when` | 可选的通用启用条件，省略表示无条件。state/all/any、引用验证与模式限制请见通用字段。 |
| `spec.asn` | 必填非零 uint32 本机 ASN。配置拒绝 BGPRouter 间重复使用；目前运行时仅支持一个 BGPRouter。 |
| `spec.routerID` | 必填 IPv4 格式 BGP 路由器标识符，与传输地址不同。未指定接口的 BFD 也将其用作本机多跳地址。 |
| `spec.vrf` | 可选 VRF/name 或普通 VRF 名称，须引用已声明 VRF。验证接受，但实际 GoBGP 控制器拒绝任何非空 vrf，报告 GoBGPVRFUnsupported。实际使用请省略。 |
| `spec.listen` | 可选本机 TCP 监听配置，默认所有地址、端口 179，不是选择远端 TCP 端口。 |
| `spec.listen.address` | 可选本机 IPv4/IPv6 绑定地址，空值让 GoBGP 监听所有地址。必须为 IP，不接受资源引用或主机名。 |
| `spec.listen.port` | TCP 监听端口 1–65535，省略或 0 为 179。拒绝配置中的地址/端口冲突。 |
| `spec.redistribute` | 可选的明确通告前缀组。目前运行时将 connected/static 与 exportPolicy.allowedPrefixes 求并集，不从 OS 发现匹配路由。 |
| `spec.redistribute.connected` | 可选的 connected 标记通告组，默认为空；不以内核直连路由存在与否作为通告条件。 |
| `spec.redistribute.connected.allowedPrefixes` | 明确起源的 IPv4/IPv6 CIDR，默认为空，不可与路由器导入或 static 重分发前缀重叠。不要求对应直连路由存在。 |
| `spec.redistribute.static` | 可选的 static 标记通告组，默认为空，不会自动列举 IPv4StaticRoute/IPv6StaticRoute 资源。 |
| `spec.redistribute.static.allowedPrefixes` | 明确起源的 IPv4/IPv6 CIDR，默认为空，不可与路由器导入或 connected 前缀重叠，不检查内核静态路由存在。 |
| `spec.communities` | 可选 community 配置。目前仅路由器 set.out 影响本机起源通告；send、accept、set.in 会解码及验证但不使用。 |
| `spec.communities.send` | 接受 standard/extended/both 或空，但不影响操作；GoBGP 对等端固定使用 SendCommunity=3。 |
| `spec.communities.accept` | 可选且受验证的标准 community 列表，默认为空，目前不过滤导入。实际匹配请用 importPolicy.requiredCommunities/forbiddenCommunities。 |
| `spec.communities.set` | 可选的传入/传出 community 列表，目前控制器仅使用路由器 out。 |
| `spec.communities.set.in` | 可选且受验证的列表，默认为空，目前不应用至导入路径。 |
| `spec.communities.set.out` | 附加于 routerd 本机起源通告的标准 community，默认为空，不改写所有转发路径。使用标准 16 位组成的 asn:value 或支持的已知名称，此处不编码 extended community。 |
| `spec.convergenceProfile` | default、省略或 stable 默认启用 graceful restart；fast 默认禁用，可由 gracefulRestart.enabled 明确覆盖。不会自动选择 timers.profile。 |
| `spec.gracefulRestart` | 可选 graceful restart 配置，选择收敛配置后也用于对等端。明确 enabled 优先于配置默认值。 |
| `spec.gracefulRestart.enabled` | 可选布尔值，fast 收敛以外默认 true。false 即使指定时间也会禁用 GR。 |
| `spec.gracefulRestart.restartTime` | 正数 Go duration，默认 120 秒，转为整秒时截断小数，低于一秒可能变成 0，请使用整秒。 |
| `spec.gracefulRestart.stalePathTime` | 保留陈旧路由的正数 Go duration，默认 360 秒，转为整秒。GR 禁用时不使用。 |
| `spec.watcher` | 可选状态观察配置，不是 BGP keepalive/hold 计时器或对等端接纳限制。 |
| `spec.watcher.pollInterval` | Go duration，至少 3 秒。有效轮询从 15 秒起仅采用较短值，因此大于 15 秒不会延长间隔。 |
| `spec.watcher.maxPrefixes` | 观察前缀上限，默认 4096，明确正值须小于 1000000。不配置远端会话的 maximum-prefix 关闭策略。 |
| `spec.watcher.peerStateChangeThrottle` | 限制重复对等端状态事件发布的非负 Go duration，省略或 0 不节流，不延迟 BGP 协议状态机。 |
| `spec.backend` | 默认 gobgp，也是唯一接受的后端，使用 routerd-bgp 守护进程及控制客户端，不是 FRR BGP 配置文件。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go) · [pkg/bgp/watcher.go](https://github.com/imksoo/routerd/blob/main/pkg/bgp/watcher.go) · [pkg/controller/bgp/fib_linux.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_linux.go) · [pkg/controller/bgp/fib_bsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_bsd.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
