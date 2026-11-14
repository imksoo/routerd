# SAMTransportProfile

[各资源的配置](../resources.md#samtransportprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

从共享拓扑或接纳的 RR/direct 快照派生节点间 SAM 隧道、端点 /32 路由及可选 BGP/BFD。

## 指定条件与引用关系

- direct 要求必需 SAMRRSet 后的最后 direct SAMPeerGroup、pair-stable、生成 BGP、routeReflectorClient:false、IPv4 导入及 min=max=32、低于 direct 的正 RR LOCAL_PREF，以及 BGPRouter.communities.set.out 的自身节点标识 community。direct 不可用时保留 RR。
- 生成 RR 客户端/direct 导入强制 /32 及对等端标识 community，禁止其他已知节点标识。direct 前缀仅为该 leaf 接纳拥有 /32，空值全部拒绝，并强制 nextHopRewrite=peer-address。direct 配置的 RR 备用下一跳也规范化为 peer-address。
- 无本机 MobilityPool 的 RR 客户端需明确 IPv4 导入前缀，长度省略或均为 32。FreeBSD 派生 gifN/greN 与明确点对点 peerAddress；Linux 使用短隧道名称。派生成功不证明隧道/BGP 可达。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # 选填; string; enum=["", "edge-index", "pair-stable"]
    bgp: # 必填; object
      bfd: # 选填; object
        detectMultiplier: 1 # 选填; integer; minimum=1; maximum=50
        enabled: true # 选填; boolean
        interface: "<string>" # 选填; string
        minRx: "<string>" # 选填; string
        minTx: "<string>" # 选填; string
        profile: "" # 选填; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # 选填; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # 选填; integer
      ebgpMultihop: 0 # 选填; integer; minimum=0; maximum=255
      exportPolicy: # 选填; object
        allowedPrefixes: # 选填; array
          - "<string>" # string
      generatePeers: true # 选填; boolean
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
      peerASN: 1 # 必填; integer; minimum=1
      routeReflectorClient: true # 选填; boolean
      routeReflectorClusterID: "<string>" # 选填; string
      routerRef: "<string>" # 必填; string
      timers: # 选填; object
        profile: "" # 选填; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # 选填; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # 选填; integer; minimum=1; maximum=65535
    encapSport: 1 # 选填; integer; minimum=1; maximum=65535
    encryption: "" # 选填; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # 必填; string
    localEndpoint: "<string>" # 选填; string
    localEndpointFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # 选填; array
      - # object
        direct: true # 选填; boolean
        nodeRefs: # 选填; array
          - "<string>" # string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    publishPeerGroup: true # 选填; boolean
    selfNodeRef: "<string>" # 必填; string
    underlayInterface: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.selfNodeRef` | 必填稳定本机 nodeRef，不从主机名/router ID 推定。 |
| `spec.mode` | 必填 ipip/gre/fou/gue。FreeBSD 仅支持 ipip/gif 与 gre，fou/gue 需 Linux。派生隧道本身不加密。 |
| `spec.encryption` | none/省略或 wireguard。wireguard 优先节点 wireGuard.transportEndpoint，不生成私钥或本机 WireGuardInterface，须另配置 WireGuard 底层。 |
| `spec.innerPrefix` | 必填长度 <=31 的 IPv4 CIDR，拆为 /31 边，须有足够拓扑槽位。pair-stable 冲突会报错，不静默移动端点。 |
| `spec.addressingMode` | 默认 edge-index 按排序拓扑边分配 /31，拓扑更改可能改变索引。pair-stable 对 innerPrefix 与无序节点配对哈希至固定槽位，direct 来源必填。 |
| `spec.underlayInterface` | 必填已声明逻辑接口，用于派生隧道及远端端点 /32 设备路由，派生隧道配置 trustedUnderlay:true。 |
| `spec.localEndpoint` | 本机外层 IPv4 固定值，与 localEndpointFrom 必须且只能指定一项，不自动选择接口地址。 |
| `spec.localEndpointFrom` | 替代固定 localEndpoint 的状态来源，适用通用 resource/field 规则，未解析端点使依赖传输待处理。 |
| `spec.localEndpointFrom.resource` | 使用来源时必填已有 Kind/name。 |
| `spec.localEndpointFrom.field` | 必填来源提供字段，例如 currentAddress。 |
| `spec.localEndpointFrom.optional` | 默认 false，不免除引用验证，也不补入未解析端点。 |
| `spec.encapSport` | fou/gue 必填 UDP 源端口 1–65535，ipip/gre 禁用（省略或 0）。 |
| `spec.encapDport` | fou/gue 必填 UDP 目标端口 1–65535，ipip/gre 禁用，无隐含默认端口。 |
| `spec.peersFrom` | 有顺序的拓扑来源，除 publishPeerGroup:true 外至少一个。自身不作为对等端但保留于共享拓扑，后续重复节点可能覆盖前方数据。 |
| `spec.peersFrom[].resource` | 必填 SAMNodeSet/name、SAMPeerGroup/name、SAMEnrollmentPolicy/name 或 SAMRRSet/name。NodeSet 为静态，运行时来源提供接纳/共享快照。 |
| `spec.peersFrom[].optional` | 默认 false，非 optional 来源缺失为待处理。direct 不可用/不兼容不移除可用 RR 备用对等端。 |
| `spec.peersFrom[].direct` | 默认 false，true 仅限必需 SAMRRSet 后的最后 SAMPeerGroup 来源，另适用下述 direct 限制。 |
| `spec.peersFrom[].nodeRefs` | 可选唯一非空 nodeRef 选择值，省略选择所有合格来源节点。仅缩小本机邻接，不替换共享权威拓扑。 |
| `spec.publishPeerGroup` | 默认 false。true 将本机端点发布为运行时 SAMPeerGroup/<配置 metadata.name>，允许 peersFrom 为空，不发布私密凭据。 |
| `spec.bgp` | 必填生成对等端配置，所引用 BGPRouter 须已存在。 |
| `spec.bgp.generatePeers` | 默认 true，false 仅生成传输不生成 BGP/BFD，direct 来源时禁用。 |
| `spec.bgp.timersPreset` | 可选 default/fast/slow 简写，优先级低于 bgp.timers.profile，两者均空时生成对等端继承 BGPRouter.timers。 |
| `spec.bgp.directLocalPreference` | direct 对等端 uint32 LOCAL_PREF，省略或 0 默认 200，direct 时须大于正 RR bgp.importPolicy.localPreference。 |
| `spec.bgp.bfd` | 可选生成 BFD 配置，仅 generatePeers 与 bfd.enabled 均 true 时使用。 |
| `spec.bgp.bfd.enabled` | 默认 false。true 派生关联生成 BGPPeer 的 BFD 资源，需 FRR bfdd/vtysh。 |
| `spec.bgp.routerRef` | 必填 BGPRouter/name，引用同一 Router 配置中声明的路由器，不接受单独名称。 |
| `spec.bgp.peerASN` | 必填非零 uint32 远端 ASN，等于本机 ASN 时为 iBGP；routeReflectorClient 要求两者相等。 |
| `spec.bgp.ebgpMultihop` | 整数 0–255，默认 0。仅大于 1 时启用 GoBGP eBGP multihop 并配置 TTL，0/1 保持禁用。 |
| `spec.bgp.routeReflectorClient` | 默认 false。true 标记 iBGP 路由反射器客户端，要求 peerASN 等于路由器 ASN，并将路由器导入前缀加入有效导出前缀。 |
| `spec.bgp.routeReflectorClusterID` | 可选 IPv4 格式集群 ID，仅 routeReflectorClient=true 时传入。空值由 GoBGP 决定，单独配置不启用反射。 |
| `spec.bgp.convergenceProfile` | 可选 default/fast/stable，省略继承 BGPRouter convergenceProfile。选择 graceful restart 默认值，不是 timers.profile；路由器 gracefulRestart.enabled 仍优先覆盖。 |
| `spec.bgp.importPolicy` | 可选导入策略。对等端若无非空前缀、community 条件、下一跳改写或 localPreference，会完整继承路由器策略；仅前缀长度限制不算覆盖。 |
| `spec.bgp.importPolicy.allowedPrefixes` | 可选 IPv4/IPv6 CIDR 允许列表，按前缀及允许的更具体前缀长度匹配。拒绝掩码后重复的前缀。为空时，不在其他已配置导入条件上加入前缀条件。 |
| `spec.bgp.importPolicy.allowedPrefixLengthMin` | 整数 0–128。省略或 0 使用每个允许前缀自身长度。正值须大于等于各前缀长度、小于等于地址族上限及正数最大长度。 |
| `spec.bgp.importPolicy.allowedPrefixLengthMax` | 整数 0–128。省略或 0 时 IPv4 为 32、IPv6 为 128。正值须涵盖所有列出前缀长度且不超过地址族上限。动态 leaf 接纳要求 min=max=32。 |
| `spec.bgp.importPolicy.requiredCommunities` | 可选 community 列表；接受的路径必须包含全部列出值。空列表不增加必需 community 条件。 |
| `spec.bgp.importPolicy.forbiddenCommunities` | 可选禁止 community 列表；任一匹配就先于接受规则拒绝路径。空列表不增加禁止条件。 |
| `spec.bgp.importPolicy.nextHopRewrite` | 默认 peer-address；unchanged 保留通告的下一跳。在接受路径的选择及 FIB 投影之前应用。对等端只配置此字段也会建立自己的导入策略，不继承路由器允许列表。 |
| `spec.bgp.importPolicy.localPreference` | 可选 uint32 LOCAL_PREF，在最佳路径选择之前设置于接受的路径。默认 0 保留 BGP 实现默认值。 |
| `spec.bgp.exportPolicy` | 可选导出前缀过滤。对等端前缀会与路由器静态及动态通告合并；RR 客户端另加入路由器导入前缀，不是替换并限制所有路由器派生导出前缀。 |
| `spec.bgp.exportPolicy.allowedPrefixes` | 可选 CIDR 列表，默认为空。合并后非空列表允许列出前缀及更具体路由（至 /32 或 /128），拒绝不符路由。有效列表为空时不安装导出过滤。 |
| `spec.bgp.timers` | 生成对等端计时器，优先级为本配置 timers.profile > timersPreset > BGPRouter.timers。 |
| `spec.bgp.timers.profile` | default 或省略：keepalive 30 秒、hold 90 秒、connectRetry 10 秒；fast 为 3/9/1 秒，slow 为 60/180/30 秒。不支持直接指定 keepalive/holdTime/connectRetry YAML。 |
| `spec.bgp.bfd.interface` | 可选单跳接口。建议 Interface/name，控制器会解析声明的 Interface.ifname；单独名称虽验证为逻辑接口，却会原样传给 FRR。省略使用多跳。 |
| `spec.bgp.bfd.profile` | fast 或省略：minRx=minTx=300ms、倍数 3；normal：500ms/500ms/3；slow：1000ms/1000ms/5。明确单独配置优先。 |
| `spec.bgp.bfd.minRx` | 可选 Go duration 50ms–60000ms，覆盖配置的接收间隔并转为整数毫秒。省略使用 profile。这是 BFD 协议计时器，不是路由器重启期限。 |
| `spec.bgp.bfd.minTx` | 可选 Go duration 50ms–60000ms，覆盖 profile 发送间隔，输出为整数毫秒。 |
| `spec.bgp.bfd.detectMultiplier` | 可选整数 1–50；解码为 0 或省略时使用 profile 默认值（3，slow 为 5）。为协商检测间隔倍数，不是 routerd 协调尝试次数。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/transport_support.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport_support.go) · [pkg/mobilityconfig/sam_transport_slots.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/sam_transport_slots.go) · [pkg/mobilityconfig/transport_mesh.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/transport_mesh.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `reason`, `peersFrom`, `pendingSources`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
