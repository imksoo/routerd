# EgressRoutePolicy

[各资源的配置](../resources.md#egressroutepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

选择或配置出口路由，实际行为取决于明确 mode 与 hostTraffic。

## 指定条件与引用关系

- 请明确选择路径：省略 mode 仅公布建议选择，Linux priority/mark/hash 安装 IPv4 路由表与 nftables，hostTraffic true 为独立本机 IPv6 路径。即使模式接受，这些 IPv4 写入器未实现常规 IPv6 转发策略。
- 有效的直接 Linux 导向须有 interface/device/deviceFrom 输出，以及正 table/priority/mark 组合。验证允许三者皆零，但实时直接表安装会跳过，Applied 或选择状态不能证明数据包导向。配置 metric 零时 Linux 路由用 50。
- priority 选就绪候选最大 weight，以较小 priority、名称、输入顺序打破同分。检查新鲜健康、实际设备、直接候选依赖与 DS-Lite Up。目标池须至少一个健康且存在的目标，但此 priority 分支不评估候选 dependsOn。无可用者为 Pending/NoReadyCandidates 并移除新流导向，不保证所有探测路由或现有状态撤除。
- mark 按资源与候选顺序写入各合格直接候选标记，重叠匹配可被后规则覆盖，不选单一最大 weight。hash/priority 目标池用地址 jhash、固定 256 桶 rendezvous 映射及 conntrack 标记，无目标权重可设，变更后现有流仍可保留标记。
- 通用 device/gateway 来源须有现有资源与明确提供字段。DHCP 网关不自动发现，须给 gatewayFrom。table/routeTable、routeMetric/metric 不可均非零。targets 与直接 device/gateway/table/mark/metric 互斥，仅明确 hash/priority 可用；虽验证仅在 hash 要求，生成仍需 hashFields。
- Linux 目标表配置检查实际链路并跳过缺失目标，但后续 hash 映射就绪副本对常规非 DS-Lite 目标只检查健康，不重复检查链路，因此缺失设备仍可能收到新 hash 桶。依赖分配前须确认目标链路与健康。
- FreeBSD 仅支持 mode hash、family ipv4、hashFields: [sourceAddress]、非空 sourceCIDRs、单一启用候选及至少两个不同固定 IPv4 接口/网关目标。省略 selection/hysteresis 与候选直接、动态、健康、依赖字段；目标 table/priority/mark/metric 须零，禁止健康与动态网关。禁用目标集合引用，最多一个排除 CIDR，包含与排除不可并用。PF 用 route-to round-robin sticky-address，不是 Linux 确定性 hash。
- FreeBSD 渲染器拒绝此 route-to 与明确 FirewallRule、生成的防火墙放行、FirewallEventLog 或 ClientPolicy 混用。若有 FirewallZone 过滤，须有广泛允许转发路径；语法验证不证明可与其他资源共同生成最终 PF 规则。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # 必填; array
      - # object
        dependsOn: # 选填; array
          - # object
            equals: "<string>" # 选填; string
            field: "<string>" # 选填; string
            notEmpty: true # 选填; boolean
            optional: true # 选填; boolean
            phase: "<string>" # 选填; string
            resource: "<string>" # 必填; string
        device: "<string>" # 选填; string
        deviceFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        enabled: true # 选填; boolean
        gateway: "<string>" # 选填; string
        gatewayFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        gatewaySource: "" # 选填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # 选填; string
        interface: "<string>" # 选填; string
        mark: 0 # 选填; integer; minimum=0; maximum=4294967295
        metric: 0 # 选填; integer; minimum=0
        name: "<string>" # 选填; string
        preferredSource: "" # 选填; string; enum=["", "interface"]
        priority: 0 # 选填; integer; minimum=0; maximum=32765
        routeMetric: 0 # 选填; integer; minimum=0
        routeTable: 0 # 选填; integer; minimum=0; maximum=4294967295
        source: "<string>" # 选填; string
        table: 0 # 选填; integer; minimum=0; maximum=4294967295
        targets: # 选填; array
          - # object
            gateway: "<string>" # 选填; string
            gatewayFrom: # 选填; object
              field: "<string>" # 选填; string
              optional: true # 选填; boolean
              resource: "<string>" # 必填; string
            gatewaySource: "" # 选填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # 选填; string
            interface: "<string>" # 选填; string
            mark: 0 # 选填; integer; minimum=0; maximum=4294967295
            metric: 0 # 选填; integer; minimum=0
            name: "<string>" # 选填; string
            outboundInterface: "<string>" # 选填; string
            priority: 0 # 选填; integer; minimum=0; maximum=32765
            routeMetric: 0 # 选填; integer; minimum=0
            routeTable: 0 # 选填; integer; minimum=0; maximum=4294967295
            table: 0 # 选填; integer; minimum=0; maximum=4294967295
        weight: 0 # 选填; integer; minimum=0
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
    destinationCIDRs: # 选填; array
      - "<string>" # string
    destinationSetRefs: # 选填; array
      - "<string>" # string
    excludeDestinationCIDRs: # 选填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 选填; array
      - "<string>" # string
    family: "ipv4" # 选填; string; enum=["ipv4", "ipv6"]
    hashFields: # 选填; array
      - "<string>" # string
    hostTraffic: true # 选填; boolean
    hysteresis: "<string>" # 选填; string
    mode: "priority" # 选填; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # 选填; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # 选填; array
      - "<string>" # string
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
| `spec.family` | 默认 ipv4。模式与验证接受 ipv6，但当前 Linux 转发 mark/priority/hash 实现限 IPv4；IPv6 另有 hostTraffic 路径，不代表常规 IPv6 转发同等支持。FreeBSD 子集也须 ipv4。 |
| `spec.hostTraffic` | 默认 false。Linux true 须 family ipv6、单一候选、preferredSource: interface、固定 static gateway、mark 0 且无 targets。安装所有本机流量的 iif lo 策略，不应用来源或目标 CIDR；设备或地址未就绪保留先前有效规则，首次则 Pending。此独立构建器不评估候选健康、依赖或选择字段。 |
| `spec.mode` | 省略或空值仅做建议性状态选择，不安装路由表。priority 选一个就绪候选供新流标记，mark 直接标记数据包，hash 将流量映射到候选 targets。Linux 转发请明确设置 mode；FreeBSD 仅限受限 hash 形状。 |
| `spec.sourceCIDRs` | 可选同地址族源前缀，默认任意来源，列表为 OR。FreeBSD hash 需非空 IPv4 列表；hostTraffic 不使用此选择器。 |
| `spec.destinationCIDRs` | 可选同地址族目标前缀，与 destinationSetRefs 均空时为全部目标。mark/hash 中与集合引用 OR 结合；priority 默认标记生成仅使用 CIDR，不使用地址集合。 |
| `spec.destinationSetRefs` | 可选现有 IPAddressSet/<name> 引用，拒绝重复。用于 Linux mark/hash 匹配，priority 默认标记不应用，FreeBSD 禁用；空值不增加集合替代项。 |
| `spec.excludeDestinationCIDRs` | 可选同地址族目标排除，默认空，须避开所有排除。IPv4 /0 排除不产生导向匹配。FreeBSD 最多一项，且不可与非空 destinationCIDRs 并用。 |
| `spec.excludeDestinationSetRefs` | 可选现有 IPAddressSet/<name> 排除，拒绝重复，用于 Linux mark/hash，priority 默认标记忽略，FreeBSD 禁用。 |
| `spec.hashFields` | sourceAddress/destinationAddress 的有序列表，hash 必填；含 priority 目标池在内，任何有 targets 的生成也需要。Linux 对地址而非端口做 jhash，使用资源派生固定种子与 256 个 rendezvous 映射桶。FreeBSD 必须 [sourceAddress]，实现却是 round-robin sticky-address 而非 jhash。 |
| `spec.selection` | 默认 highest-weight-ready，就绪候选按 weight 降序、priority 升序、有效名称字典序、输入顺序选择。weighted-ecmp 虽可通过模式，建议或 priority 选择未实现，为 Pending/UnsupportedSelection。与 hash 目标算法不同，mark/hash 不用此选项；FreeBSD 须空。 |
| `spec.hysteresis` | Go duration，仅省略 mode 的建议选择默认 30s。更改后此期间可保留仍就绪的旧胜者，不保留未就绪者；零或负值无延迟。明确 priority/mark/hash 不使用，FreeBSD 须空。 |
| `spec.candidates` | 必填非空列表，禁用条目仍做结构验证。Linux priority 可混合直接候选与目标池；未验证候选名称唯一，请明确使用唯一名称以免状态或 hash 身份混淆。 |
| `spec.when` | 可选通用状态条件，默认无条件。为假时从有效配置移除，候选条件另行筛选；未解析可维持不确定，不保证停止。 |
| `spec.candidates[].name` | 可选明确身份。建议模式按 source、candidate-N（从零开始）补名；明确 priority 状态按 interface/device、targets，未命名 hash 池用输入索引。稳定身份请用唯一名称。 |
| `spec.candidates[].enabled` | 默认 true，false 排除选择与渲染，但不跳过配置或引用验证，也不单独保证全部现有连接或路由表项消失。 |
| `spec.candidates[].source` | 可选 Kind/name 建议就绪来源，不自动提供 device/gateway；此字符串不受通用类型引用存在验证。建议模式接受 Applied/Bound/Healthy/Installed/Ready/Running/Up，新鲜通过的健康检查可替代未就绪来源。明确路由特别要求所引用 DS-Lite 为 Up，但不常规检查其他来源。 |
| `spec.candidates[].interface` | 明确 Linux 路由的可选已声明逻辑接口，优先于 device，提供时验证 Router 接口或 DS-Lite 索引。省略 mode 的建议输出不使用此别名，该处请用 device/deviceFrom。targets 非空时禁用。 |
| `spec.candidates[].device` | 可选固定设备替代值，明确路由先用 interface，建议模式直接用 device。此字符串无通用存在检查，但实时安装与选择须有实际链路。已移除 ${...}，请用 deviceFrom；与 targets 禁止并用。 |
| `spec.candidates[].deviceFrom` | 可选通用 resource/field 状态来源，验证现有资源与提供字段。第一个规范化值优先于 interface/device，缺值退回固定值；optional 不创建设备，不可与 targets 并用。 |
| `spec.candidates[].gateway` | 固定纯地址须符合 family，需要 gatewaySource: static 且与 gatewayFrom 互斥；DHCP 模式禁止固定值，默认空，目标池候选也禁用。 |
| `spec.candidates[].gatewayFrom` | 可选现有 resource/提供字段来源。static 恰需 gateway/gatewayFrom 之一；DHCP 模式可用此来源，但不自动从接口发现网关。建议模式任何未解析来源即使 optional true 仍未就绪；明确安装在 static/DHCP 网关缺值时报错，optional 不是常规跳过机制。与 targets 禁止并用。 |
| `spec.candidates[].gatewaySource` | 默认 none，为设备默认路由并禁止 gateway 字段。static 需固定或来源恰一个；dhcpv4 需 IPv4、无固定值且明确 gatewayFrom 可用。dhcpv6 虽仅在 IPv6 验证接受，常规 IPv4 安装器不支持，独立 hostTraffic 也须 static。池候选禁用。 |
| `spec.candidates[].preferredSource` | 空值或 interface，hostTraffic 须 interface，选候选接口第一个非 tentative/deprecated 的全局 IPv6。常规 IPv4 转发及目标池不使用。 |
| `spec.candidates[].table` | Linux 路由表编号，模式 0–4294967295，默认 0 未配置。与非零 routeTable 互斥；有效直接导向需正 table/priority/mark（hostTraffic mark 为 0）。targets 候选禁用，FreeBSD 须 0。 |
| `spec.candidates[].routeTable` | table 为 0 时使用的别名，两者非零拒绝；范围及要求同 table。 |
| `spec.candidates[].priority` | 设置直接 mark/table/priority 任一时，Linux ip rule 优先级须 1–32765；默认 0 不是有效编程规则。也为候选选择较小优先的同分判定，池候选可不设直接 table/mark 而使用；FreeBSD 须 0。 |
| `spec.candidates[].mark` | Linux 数据包/fwmark 标识符，模式 0–4294967295。直接转发需正 mark/table/priority，默认 0 会跳过直接路由表安装。hostTraffic 须 0，池候选及 FreeBSD 禁止非零直接 mark。 |
| `spec.candidates[].routeMetric` | 非负优先路由 metric，非零时与非零 metric 互斥；有效 0 在 Linux 安装含 hostTraffic 时转为 50，建议状态仍可能报告 0。targets 禁用，FreeBSD 须 0。 |
| `spec.candidates[].metric` | routeMetric 为 0 时的替代别名，两者非零不可并用，Linux 编程路由有效默认也为 50。 |
| `spec.candidates[].weight` | 非负整数默认 0，建议或 priority 选择较大优先，不是百分比、ECMP 或目标 hash 权重。mark/hash 不按此比例分配，FreeBSD 须 0。 |
| `spec.candidates[].healthCheck` | 可选现有 HealthCheck 裸名，空值无健康条件。选择须新鲜 Healthy 或未达 unhealthyThreshold 的 Failing，lastCheckedAt 不超过 max(2m,3×interval+timeout)。同 mark 探测的检查可在未就绪时仍安装探测表，与流量资格不同。hostTraffic 忽略，FreeBSD 禁用。 |
| `spec.candidates[].targets` | 可选非空目标池，仅限 hash/priority。与直接 interface/device/deviceFrom/gateway/gatewayFrom/gatewaySource/table/routeTable/mark/routeMetric/metric 互斥。Linux 需各目标正 table/priority/mark 及 hashFields；FreeBSD 需单一候选、至少两个不同固定 routehost 且无 Linux 路由数字。 |
| `spec.candidates[].dependsOn` | 可选通用 AND 依赖，默认空，应用常规引用检查。建议、明确直接路由及 mark/hash 候选筛选会评估；当前 priority 目标池资格不评估候选 dependsOn，hostTraffic 也忽略。候选整体停止请用 when，FreeBSD 禁用。 |
| `spec.candidates[].when` | 可选通用状态条件，默认无条件；为假时在控制器选择前移除，未解析可因不确定而保留，enabled false 也移除。FreeBSD 固定子集禁止候选条件。 |
| `spec.candidates[].targets[].name` | 可选目标身份，默认空，与有效 interface、mark、table 共同组成稳定 hash 身份，更改可重新分配新流。Linux 通常不验证名称/mark/priority/table 唯一。 |
| `spec.candidates[].targets[].interface` | 未提供 outboundInterface 时必填目标逻辑接口，验证已声明接口或 DS-Lite 索引。Linux 同时提供时 outboundInterface 优先；FreeBSD 拒绝两者非空。 |
| `spec.candidates[].targets[].outboundInterface` | 优先于 interface 的别名，应用相同引用检查；实际投递仍须可用 OS 接口，不创建隧道。 |
| `spec.candidates[].targets[].gateway` | 可选符合 family 的固定值，须 gatewaySource static 且与 gatewayFrom 互斥；DHCP 模式禁止固定值。FreeBSD 目标必填此纯固定 IPv4。 |
| `spec.candidates[].targets[].gatewayFrom` | 可选通用 resource/field 来源，即使 optional true 仍验证存在及提供字段。static 恰需固定值或来源之一，Linux 使用第一个值，optional 不跳过网关缺值；FreeBSD 完全禁止动态网关字段。 |
| `spec.candidates[].targets[].gatewaySource` | 默认 none，禁止 gateway/gatewayFrom 并安装设备路由。static 恰需 gateway/gatewayFrom 之一；dhcpv4/6 需对应地址族且无固定值，但不自动发现，请用 gatewayFrom。常规实时安装器仅支持 none/static/dhcpv4，FreeBSD 须明确 static。 |
| `spec.candidates[].targets[].table` | Linux 每目标必须正 table（模式上限 4294967295），与非零 routeTable 互斥；无自动分配、保留表或唯一检查。FreeBSD 须省略或 0，改用 PF route-to。 |
| `spec.candidates[].targets[].routeTable` | table 为 0 时使用的别名，两者非零拒绝，Linux 正值或 FreeBSD 零值要求相同。 |
| `spec.candidates[].targets[].priority` | Linux 目标 ip rule 优先级必填 1–32765，无自动分配，与候选选择 priority 不同。FreeBSD 须省略或 0。 |
| `spec.candidates[].targets[].mark` | Linux 目标 fwmark 必填正值（模式上限 4294967295），用于 hash 桶与 ip rule，无自动分配或冲突检测。FreeBSD 须省略或 0。 |
| `spec.candidates[].targets[].routeMetric` | 非负 Linux 路由 metric，优先于 metric，两者非零拒绝，有效省略或 0 安装时为 50。FreeBSD 须省略或 0。 |
| `spec.candidates[].targets[].metric` | routeMetric 为 0 时的替代别名，要求相同，Linux 有效默认值 50。 |
| `spec.candidates[].targets[].healthCheck` | 可选现有 HealthCheck 裸名，默认无健康条件，同候选新鲜度与阈值检查。失败目标从新 hash 映射排除，但现有 conntrack 标记会恢复而非全面终止连接，同 mark 探测表也可保留；FreeBSD 禁用。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/egressroute/selection.go](https://github.com/imksoo/routerd/blob/main/pkg/egressroute/selection.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go) · [pkg/controller/chain/phase_status.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase_status.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `family`, `selectedCandidate`, `selectedSource`, `selectedDevice`, `selectedGateway`, `selectedGatewaySource`, `selectedRouteTable`, `selectedMetric`, `selectedWeight`, `selectedTargets`, `selectedInterface`, `candidates`, `role`, `advisory`, `lastTransitionAt`, `datapathRoleResource`, `datapathRole`, `datapathRoleTransitionAt`, `datapathReady`, `datapathState`, `datapathReadyAt`, `datapathReadyDuration`, `datapathReadyDurationMillis`, `datapathFallbackBehavior`, `newFlowBehavior`, `updatedAt`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
