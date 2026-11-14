# MobilityPool

[各资源的配置](../resources.md#mobilitypool) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

结合共享 SAM 拓扑、本机所有权观察及 BGP/联邦状态，规划地址移动与本机捕获。YAML 是本机意图，共享身份位于 SAMNodeSet。

## 指定条件与引用关系

- prefix 须为 IPv4，groupRef 须解析至有 nodeName 的本机 EventGroup，该节点须存在于必需 membersFrom 拓扑。各成员需非空 site 及 onprem/cloud 角色，仅本机可带捕获/发现/静态所有权覆盖。
- onprem proxy-ARP 需 capture.interface 及明确 single-router/vrrp-master activeWhen。提供商捕获需 providerRef 及 nicRef（发现 NIC 时例外），route-table 另需 target.routeTableRef；提供商捕获禁用 activeWhen。
- Linux 使用每接口 proxy-ARP 控制，FreeBSD 使用按路由判断的全局 proxyall，而非 Linux sysctl 名称。捕获启用仍依实际所有权/就绪事实，有效 YAML 或计划意图不证明扩展网段可运行。
- 配置以非零语义合并，空字符串/列表及 false 不清除继承字段。源地址固定值优先状态来源；map 以本机值优先合并，target 固定值再优先于 targetFrom。配置引用仅限 cloud。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    members: # 选填; array
      - # object
        capture: # 选填; object
          activeWhen: # 选填; object
            type: "" # 选填; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # 选填; string
          captureStrategy: "" # 选填; string; enum=["", "route-table"]
          excludeAddresses: # 选填; array
            - "<string>" # string
          gratuitousARP: true # 选填; boolean
          interface: "<string>" # 选填; string
          nicRef: "<string>" # 选填; string
          providerRef: "<string>" # 选填; string
          sourceAddress: "<string>" # 选填; string
          sourceAddressFrom: # 选填; object
            field: "<string>" # 选填; string
            optional: true # 选填; boolean
            resource: "<string>" # 必填; string
          target: # 选填; object
            "<key>": "<string>" # 选填; string
          targetFrom: # 选填; object
            "<key>": "<string>" # 选填; string
          type: "provider-secondary-ip" # 选填; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # 必填; string
        ownershipDiscovery: # 选填; object
          allowEmptyAfter: "<string>" # 选填; string
          leaseTTL: "<string>" # 选填; string
          mode: "" # 选填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # 选填; string
          providerRef: "<string>" # 选填; string
          scanInterval: "<string>" # 选填; string
          scope: # 选填; object
            excludeAddresses: # 选填; array
              - "<string>" # string
            includeAddresses: # 选填; array
              - "<string>" # string
          selector: # 选填; object
            tags: # 选填; object
              "<key>": "<string>" # 选填; string
          sources: # 选填; array
            - # object
              bridge: "<string>" # 选填; string
              interface: "<string>" # 选填; string
              leaseTTL: "<string>" # 选填; string
              network: "<string>" # 选填; string
              probeRetries: 0 # 选填; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # 选填; string
              resource: "<string>" # 选填; string
              scanInterval: "<string>" # 选填; string
              sourceAddressFrom: # 选填; object
                field: "<string>" # 选填; string
                optional: true # 选填; boolean
                resource: "<string>" # 必填; string
              type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # 选填; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # 选填; string
          subnetRefFrom: "<string>" # 选填; string
        profileRef: "<string>" # 选填; string
        staticOwnedAddresses: # 选填; array
          - "<string>" # string
    membersFrom: # 必填; array; minItems=1
      - # object
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    prefix: "<string>" # 必填; string
    profiles: # 选填; object
      cloudCaptures: # 选填; object
        "<key>": # 选填; object
          capture: # 选填; object
            activeWhen: # 选填; object
              type: "" # 选填; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # 选填; string
            captureStrategy: "" # 选填; string; enum=["", "route-table"]
            excludeAddresses: # 选填; array
              - "<string>" # string
            gratuitousARP: true # 选填; boolean
            interface: "<string>" # 选填; string
            nicRef: "<string>" # 选填; string
            providerRef: "<string>" # 选填; string
            sourceAddress: "<string>" # 选填; string
            sourceAddressFrom: # 选填; object
              field: "<string>" # 选填; string
              optional: true # 选填; boolean
              resource: "<string>" # 必填; string
            target: # 选填; object
              "<key>": "<string>" # 选填; string
            targetFrom: # 选填; object
              "<key>": "<string>" # 选填; string
            type: "provider-secondary-ip" # 选填; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # 选填; object
            allowEmptyAfter: "<string>" # 选填; string
            leaseTTL: "<string>" # 选填; string
            mode: "" # 选填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # 选填; string
            providerRef: "<string>" # 选填; string
            scanInterval: "<string>" # 选填; string
            scope: # 选填; object
              excludeAddresses: # 选填; array
                - "<string>" # string
              includeAddresses: # 选填; array
                - "<string>" # string
            selector: # 选填; object
              tags: # 选填; object
                "<key>": "<string>" # 选填; string
            sources: # 选填; array
              - # object
                bridge: "<string>" # 选填; string
                interface: "<string>" # 选填; string
                leaseTTL: "<string>" # 选填; string
                network: "<string>" # 选填; string
                probeRetries: 0 # 选填; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # 选填; string
                resource: "<string>" # 选填; string
                scanInterval: "<string>" # 选填; string
                sourceAddressFrom: # 选填; object
                  field: "<string>" # 选填; string
                  optional: true # 选填; boolean
                  resource: "<string>" # 必填; string
                type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # 选填; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # 选填; string
            subnetRefFrom: "<string>" # 选填; string
    staticHandovers: # 选填; array
      - # object
        address: "<string>" # 必填; string
        fromNodeRef: "<string>" # 必填; string
        toNodeRef: "<string>" # 必填; string
    values: # 选填; object
      "<key>": "<string>" # 选填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.prefix` | 必填池管理的 IPv4 CIDR，所有权/捕获地址须在其内，无默认前缀或自动地址分配。 |
| `spec.groupRef` | 必填具有 nodeName 的本机 EventGroup 引用，其本机节点标识须存在于 membersFrom 拓扑，用于所有权及到期事件。 |
| `spec.values` | 可选提供商标识字符串常量，供 targetFrom/subnetRefFrom 引用，不自动展开变量或保存密钥。 |
| `spec.profiles` | 可选可重用本机捕获/发现默认值，不包含共享拓扑身份。 |
| `spec.profiles.cloudCaptures` | 以配置名称为键的 map，仅供本机 cloud 成员 profileRef 选择，不支持 onprem 简写。包含未选用配置在内，所有 target/value 引用均会规范化。 |
| `spec.membersFrom` | 必填且非空 SAMNodeSet 来源列表，是成员身份、site、role、placement、maintenance 及容量唯一来源，不由 members 覆盖补入拓扑。 |
| `spec.membersFrom[].resource` | 必填 SAMNodeSet/name。导入 nodeRef 不可重复，每个解析成员需明确 site 及 onprem/cloud 角色。 |
| `spec.membersFrom[].optional` | 默认 false。非 optional 运行时来源缺失为 Pending，true 可容忍，但不能免除 EventGroup 本机节点必须在拓扑解析的要求。 |
| `spec.members` | 可选本机专属覆盖，nodeRef 须已由 membersFrom 导入。不可配置共享 site/role/placement/maintenance/容量，也不可配置远端捕获。 |
| `spec.members[].nodeRef` | 必填已导入 nodeRef，选择 EventGroup 本机成员，不能以覆盖新增成员。 |
| `spec.members[].profileRef` | 可选 profiles.cloudCaptures 键，仅支持 cloud，缺少配置使规范化失败。非空字符串/列表覆盖，map 合并，true 布尔为累加，空列表/false 无法清除继承值。 |
| `spec.members[].staticOwnedAddresses` | 可选 prefix 内明确 IPv4 /32 CIDR，仅 onprem。此处拒绝纯 IPv4，静态所有权不可重复，这些声明不依赖学习客户端事件。 |
| `spec.staticHandovers` | 可选计划静态所有权转移，观察到源成员释放后才投影目标，不是同时双重拥有。 |
| `spec.staticHandovers[].address` | 必填池内 IPv4 /32 CIDR，转移间须唯一。有静态拥有声明时，所有者须等于 fromNodeRef。 |
| `spec.staticHandovers[].fromNodeRef` | 必填已有已导入 onprem 成员 nodeRef，须不同于 toNodeRef。 |
| `spec.staticHandovers[].toNodeRef` | 必填已有已导入目标 nodeRef，须不同于来源，不隐含创建目标成员。 |
| `spec.members[].capture` | 本机捕获配置，type 须明确指定，不从 role 推定。仅本机成员可带捕获/发现覆盖。 |
| `spec.members[].capture.type` | onprem 使用 proxy-arp，cloud 使用 provider-secondary-ip。空值表示无捕获机制，但本机 cloud 成员需解析的捕获配置。 |
| `spec.members[].capture.providerRef` | provider-secondary-ip 必填的 CloudProviderProfile 名称，不是状态来源，亦为所有权发现 providerRef 默认值。 |
| `spec.members[].capture.captureStrategy` | 仅 provider-secondary-ip 可选用 route-table，需 target.routeTableRef。普通 secondary-IP/proxy-ARP 请省略，明确 secondary-ip/proxy-arp 策略值会被拒绝。 |
| `spec.members[].capture.nicRef` | 提供商 NIC 标识，除可由 provider-private-ip 发现解析外，提供商捕获必填。不可放在 target/targetFrom 下，该写法会被拒绝。 |
| `spec.members[].capture.interface` | proxy-arp 必填的逻辑/OS 捕获接口，由使用端解析别名，不自动选择 LAN。 |
| `spec.members[].capture.sourceAddress` | proxy-ARP/BGP 传送使用的可选池内本机 IPv4 源地址。固定值优先 sourceAddressFrom，此处不检查互斥；有效时派生 /32 地址及首选源地址，不是远端所有者地址。 |
| `spec.members[].capture.sourceAddressFrom` | 可选源地址状态引用，接受有效池内 IPv4，缺失或无效值仍不可用。不同于固定值，不生成 IPv4StaticAddress，因来源资源管理地址生命周期。 |
| `spec.members[].capture.excludeAddresses` | 可选排除于 proxy-ARP 捕获的池内 IPv4 地址/CIDR，默认无。供仅本机网关等使用，不是所有 BGP 路由过滤。 |
| `spec.members[].capture.gratuitousARP` | 默认 false。true 请求捕获更改时发送非请求 ARP 通告。配置继承为累加，本机 false 不清除 profile 的 true。 |
| `spec.members[].capture.activeWhen` | onprem proxy-arp 必填的明确权限条件，provider-secondary-ip 禁用，不是普通 state/all/any 条件。 |
| `spec.members[].capture.activeWhen.type` | single-router 无条件授权本机 proxy-ARP；vrrp-master 需引用 VirtualAddress 的 MASTER 状态。proxy-ARP 无默认值。 |
| `spec.members[].capture.activeWhen.virtualAddressRef` | vrrp-master 必填已有本机 VirtualAddress 名称（也接受 VirtualAddress/name），single-router 禁用，仅对本机成员检查。 |
| `spec.members[].capture.target` | 可选非秘密提供商目标 map。键值不可为空，拒绝 secret/token/password/credential/private-key/access-key 类键。明确值优先于 targetFrom。 |
| `spec.members[].capture.targetFrom` | 可选从提供商目标键对应 spec.values 键的 map，引用常数缺失或空白会使规范化失败。禁用 nicRef，请用 capture.nicRef。 |
| `spec.members[].capture.sourceAddressFrom.resource` | 来源 Kind/name，通用验证要求资源存在。 |
| `spec.members[].capture.sourceAddressFrom.field` | resource 配置时必填来源提供字段。 |
| `spec.members[].capture.sourceAddressFrom.optional` | 默认 false，不免除引用验证或补入缺失源地址。 |
| `spec.members[].ownershipDiscovery` | 本机所有权事实发现配置，默认禁用，仅本机成员可配置；接收的远端事件为另一输入。 |
| `spec.members[].ownershipDiscovery.mode` | 空/disabled 不执行。provider-private-ip 需 cloud/provider-secondary-ip；onprem-l2 需 onprem/proxy-arp、capture.interface 及非空 sources。 |
| `spec.members[].ownershipDiscovery.providerRef` | 提供商清单配置名称，默认 capture.providerRef，由 provider-private-ip 使用，不是事件资源引用。 |
| `spec.members[].ownershipDiscovery.pluginRef` | 可选清单 Plugin 名称，空值按提供商名称或唯一具 observe.providerPrivateIPs 能力的插件解析；歧义或能力缺失使发现失败。 |
| `spec.members[].ownershipDiscovery.subnetRef` | 可选提供商子网标识，明确值优先于 subnetRefFrom，空值可由支持的插件从自身 NIC 推定。 |
| `spec.members[].ownershipDiscovery.subnetRefFrom` | 仅 subnetRef 为空时使用的可选 spec.values 键。常数缺失或为空使规范化失败，不是 Kind/name/field。 |
| `spec.members[].ownershipDiscovery.scanInterval` | 提供商扫描间隔，默认 60 秒，明确值 >=30 秒。onprem-l2 也会验证，但事件来源不需提供商扫描，观察轮询使用各来源 scanInterval。 |
| `spec.members[].ownershipDiscovery.leaseTTL` | 所有权事件的正 Go duration，默认 5 分钟，onprem 观察由各来源 leaseTTL 覆盖。是所有权租约，不是重启期限。 |
| `spec.members[].ownershipDiscovery.stoppedInstancePolicy` | 仅 provider-private-ip：hold/省略保留停止实例的所有权事实，release 使其过期。其他模式禁止非空值。 |
| `spec.members[].ownershipDiscovery.allowEmptyAfter` | 仅 onprem-l2，来源启动后正数等待期间，之后零客户端可视为完成；省略则零客户端被动发现仍待处理。使用 profileRef 时目前合并不复制本机此字段覆盖，保留 profile 值。 |
| `spec.members[].ownershipDiscovery.sources` | onprem-l2 必填非空列表，provider-private-ip 禁用。拒绝重复的 type/resource/有效接口/network/bridge 组合。 |
| `spec.members[].ownershipDiscovery.sources[].type` | 必填 dhcpv4-lease/arp-observer/on-demand-arp/pve-svnet。ARP/PVE 来源派生本机观察守护进程意图，DHCP 事实来自实际租约事件。 |
| `spec.members[].ownershipDiscovery.sources[].resource` | 可选来源标识字符串，默认为空，用于去重及观察身份指纹，不是观察进程名称或验证存在的 Kind/name 引用。事件主要按 type/interface/network/bridge 匹配。 |
| `spec.members[].ownershipDiscovery.sources[].interface` | 可选观察接口，默认 capture.interface。on-demand-arp 在替代解析后必填，使用端将声明别名解析为 OS 名称。 |
| `spec.members[].ownershipDiscovery.sources[].bridge` | 可选本机网桥标签，供 PVE/svnet 观察匹配。默认空值不限制 bridge，不要求创建 Bridge。 |
| `spec.members[].ownershipDiscovery.sources[].network` | 可选 PVE/svnet 逻辑网络标签，例如 svnet1。空值不限制 network 选择，不是 IP CIDR。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom` | 可选主动 ARP 发送源状态。缺失或无法使用时改用已验证捕获源地址；两者均无法解析时不生成 on-demand probe 意图。 |
| `spec.members[].ownershipDiscovery.sources[].scanInterval` | 可选观察轮询间隔，明确值 >=1 秒；生成 ARP/PVE 守护进程默认 10 秒，事件驱动 DHCP 忽略轮询。 |
| `spec.members[].ownershipDiscovery.sources[].probeTimeout` | 主动 ARP 重试之间的正 Go duration，观察守护进程默认 1 秒，仅供 on-demand probing。 |
| `spec.members[].ownershipDiscovery.sources[].probeRetries` | 整数 0–20 额外 ARP 重试，默认 0，不是总尝试次数，初次尝试另计。 |
| `spec.members[].ownershipDiscovery.sources[].leaseTTL` | 可选正数事件租约期间，覆盖此来源 ownershipDiscovery.leaseTTL，省略继承池发现租约（默认 5 分钟）。 |
| `spec.members[].ownershipDiscovery.scope` | 可选池内所有权观察地址过滤，不授权任意外部前缀通告。 |
| `spec.members[].ownershipDiscovery.scope.includeAddresses` | 可选池内 IPv4 纯地址/CIDR 允许列表，空值允许所有候选池地址。有效分片分配可替换此提供商发现列表。 |
| `spec.members[].ownershipDiscovery.scope.excludeAddresses` | 可选池内 IPv4 纯地址/CIDR 排除列表，默认无，在包含匹配后排除。 |
| `spec.members[].ownershipDiscovery.selector` | 可选提供商清单标签选择器，不更改云端标签。 |
| `spec.members[].ownershipDiscovery.selector.tags` | 非空标签键与预期字符串值，所有键的修剪值均须符合，空 map 不限制。供提供商清单使用，不用于 onprem 事件来源匹配。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.resource` | 使用状态来源时必填已有 Kind/name。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.field` | resource 配置时必填来源提供字段。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.optional` | 默认 false，不免除引用验证，也不补入未解析发送源地址。 |
| `spec.profiles.cloudCaptures.<name>.capture` | 本机捕获配置，type 须明确指定，不从 role 推定。仅本机成员可带捕获/发现覆盖。 |
| `spec.profiles.cloudCaptures.<name>.capture.type` | onprem 使用 proxy-arp，cloud 使用 provider-secondary-ip。空值表示无捕获机制，但本机 cloud 成员需解析的捕获配置。 |
| `spec.profiles.cloudCaptures.<name>.capture.providerRef` | provider-secondary-ip 必填的 CloudProviderProfile 名称，不是状态来源，亦为所有权发现 providerRef 默认值。 |
| `spec.profiles.cloudCaptures.<name>.capture.captureStrategy` | 仅 provider-secondary-ip 可选用 route-table，需 target.routeTableRef。普通 secondary-IP/proxy-ARP 请省略，明确 secondary-ip/proxy-arp 策略值会被拒绝。 |
| `spec.profiles.cloudCaptures.<name>.capture.nicRef` | 提供商 NIC 标识，除可由 provider-private-ip 发现解析外，提供商捕获必填。不可放在 target/targetFrom 下，该写法会被拒绝。 |
| `spec.profiles.cloudCaptures.<name>.capture.interface` | proxy-arp 必填的逻辑/OS 捕获接口，由使用端解析别名，不自动选择 LAN。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddress` | proxy-ARP/BGP 传送使用的可选池内本机 IPv4 源地址。固定值优先 sourceAddressFrom，此处不检查互斥；有效时派生 /32 地址及首选源地址，不是远端所有者地址。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom` | 可选源地址状态引用，接受有效池内 IPv4，缺失或无效值仍不可用。不同于固定值，不生成 IPv4StaticAddress，因来源资源管理地址生命周期。 |
| `spec.profiles.cloudCaptures.<name>.capture.excludeAddresses` | 可选排除于 proxy-ARP 捕获的池内 IPv4 地址/CIDR，默认无。供仅本机网关等使用，不是所有 BGP 路由过滤。 |
| `spec.profiles.cloudCaptures.<name>.capture.gratuitousARP` | 默认 false。true 请求捕获更改时发送非请求 ARP 通告。配置继承为累加，本机 false 不清除 profile 的 true。 |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen` | onprem proxy-arp 必填的明确权限条件，provider-secondary-ip 禁用，不是普通 state/all/any 条件。 |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.type` | single-router 无条件授权本机 proxy-ARP；vrrp-master 需引用 VirtualAddress 的 MASTER 状态。proxy-ARP 无默认值。 |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.virtualAddressRef` | vrrp-master 必填已有本机 VirtualAddress 名称（也接受 VirtualAddress/name），single-router 禁用，仅对本机成员检查。 |
| `spec.profiles.cloudCaptures.<name>.capture.target` | 可选非秘密提供商目标 map。键值不可为空，拒绝 secret/token/password/credential/private-key/access-key 类键。明确值优先于 targetFrom。 |
| `spec.profiles.cloudCaptures.<name>.capture.targetFrom` | 可选从提供商目标键对应 spec.values 键的 map，引用常数缺失或空白会使规范化失败。禁用 nicRef，请用 capture.nicRef。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.resource` | 来源 Kind/name，通用验证要求资源存在。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.field` | resource 配置时必填来源提供字段。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.optional` | 默认 false，不免除引用验证或补入缺失源地址。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery` | 本机所有权事实发现配置，默认禁用，仅本机成员可配置；接收的远端事件为另一输入。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.mode` | 空/disabled 不执行。provider-private-ip 需 cloud/provider-secondary-ip；onprem-l2 需 onprem/proxy-arp、capture.interface 及非空 sources。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.providerRef` | 提供商清单配置名称，默认 capture.providerRef，由 provider-private-ip 使用，不是事件资源引用。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.pluginRef` | 可选清单 Plugin 名称，空值按提供商名称或唯一具 observe.providerPrivateIPs 能力的插件解析；歧义或能力缺失使发现失败。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRef` | 可选提供商子网标识，明确值优先于 subnetRefFrom，空值可由支持的插件从自身 NIC 推定。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRefFrom` | 仅 subnetRef 为空时使用的可选 spec.values 键。常数缺失或为空使规范化失败，不是 Kind/name/field。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scanInterval` | 提供商扫描间隔，默认 60 秒，明确值 >=30 秒。onprem-l2 也会验证，但事件来源不需提供商扫描，观察轮询使用各来源 scanInterval。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.leaseTTL` | 所有权事件的正 Go duration，默认 5 分钟，onprem 观察由各来源 leaseTTL 覆盖。是所有权租约，不是重启期限。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.stoppedInstancePolicy` | 仅 provider-private-ip：hold/省略保留停止实例的所有权事实，release 使其过期。其他模式禁止非空值。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.allowEmptyAfter` | 仅 onprem-l2，来源启动后正数等待期间，之后零客户端可视为完成；省略则零客户端被动发现仍待处理。使用 profileRef 时目前合并不复制本机此字段覆盖，保留 profile 值。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources` | onprem-l2 必填非空列表，provider-private-ip 禁用。拒绝重复的 type/resource/有效接口/network/bridge 组合。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].type` | 必填 dhcpv4-lease/arp-observer/on-demand-arp/pve-svnet。ARP/PVE 来源派生本机观察守护进程意图，DHCP 事实来自实际租约事件。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].resource` | 可选来源标识字符串，默认为空，用于去重及观察身份指纹，不是观察进程名称或验证存在的 Kind/name 引用。事件主要按 type/interface/network/bridge 匹配。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].interface` | 可选观察接口，默认 capture.interface。on-demand-arp 在替代解析后必填，使用端将声明别名解析为 OS 名称。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].bridge` | 可选本机网桥标签，供 PVE/svnet 观察匹配。默认空值不限制 bridge，不要求创建 Bridge。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].network` | 可选 PVE/svnet 逻辑网络标签，例如 svnet1。空值不限制 network 选择，不是 IP CIDR。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom` | 可选主动 ARP 发送源状态。缺失或无法使用时改用已验证捕获源地址；两者均无法解析时不生成 on-demand probe 意图。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].scanInterval` | 可选观察轮询间隔，明确值 >=1 秒；生成 ARP/PVE 守护进程默认 10 秒，事件驱动 DHCP 忽略轮询。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeTimeout` | 主动 ARP 重试之间的正 Go duration，观察守护进程默认 1 秒，仅供 on-demand probing。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeRetries` | 整数 0–20 额外 ARP 重试，默认 0，不是总尝试次数，初次尝试另计。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].leaseTTL` | 可选正数事件租约期间，覆盖此来源 ownershipDiscovery.leaseTTL，省略继承池发现租约（默认 5 分钟）。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope` | 可选池内所有权观察地址过滤，不授权任意外部前缀通告。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.includeAddresses` | 可选池内 IPv4 纯地址/CIDR 允许列表，空值允许所有候选池地址。有效分片分配可替换此提供商发现列表。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.excludeAddresses` | 可选池内 IPv4 纯地址/CIDR 排除列表，默认无，在包含匹配后排除。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector` | 可选提供商清单标签选择器，不更改云端标签。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector.tags` | 非空标签键与预期字符串值，所有键的修剪值均须符合，空 map 不限制。供提供商清单使用，不用于 onprem 事件来源匹配。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.resource` | 使用状态来源时必填已有 Kind/name。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.field` | resource 配置时必填来源提供字段。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.optional` | 默认 false，不免除引用验证，也不补入未解析发送源地址。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller.go) · [pkg/controller/mobility/controller_effects.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller_effects.go) · [pkg/controller/mobility/bgp_delivery_planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/bgp_delivery_planner.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go) · [pkg/controller/mobility/discovery_support.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_support.go) · [pkg/controller/mobility/discovery_facts.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_facts.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/pool_plan.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/pool_plan.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/chain/sam.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam.go) · [pkg/controller/chain/sam_freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam_freebsd.go) · [cmd/routerd-arp-observer/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-arp-observer/main.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `groupRef`, `prefix`, `reason`, `generatedBGPPaths`, `resolvedMemberCount`, `pendingSources`, `placementGroup`, `placementActive`, `placementActiveNode`, `providerActionPhase`, `providerActionError`, `providerActionFailedAddresses`, `providerObservationPendingAddresses`, `ownershipResolverControlPlaneOwnerTable`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
