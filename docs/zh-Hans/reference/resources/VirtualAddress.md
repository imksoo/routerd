# VirtualAddress

[各资源的配置](../resources.md#virtualaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

维护静态或选举后的浮动地址。Linux 静态使用 iproute2、VRRP 使用 keepalived；FreeBSD 使用 ifconfig/CARP。

## 指定条件与引用关系

- family/interface 与 address/addressFrom 择一必填。固定 IPv4 须 /32，IPv6 VRRP/CARP 须 /128；静态 IPv6 可保留其他前缀长度。addressFrom 的纯地址提升为 /32 或 /128。
- interface 须存在配置接口索引。同接口上不可同时由 IPv4StaticAddress 与 VirtualAddress 管理相同索引地址；addressFrom 指向同接口的 IPv4StaticAddress 也会拒绝。VRRP ID 须按 family/interface 唯一。
- addressFrom 经普通来源/字段验证，但地址生成器仅读声明的 IPv4StaticAddress.address/VirtualAddress.address，不读任意运行时状态。optional 不允许最终 VIP 为空，可能出现 Error/Pending。
- mode=vrrp 需要 virtualRouterID，Linux 另需非空单播 peers。FreeBSD 使用原生 CARP，不将 peers 作单播目标。省略 preempt 禁用抢占；CARP 抢占为主机全局，任一实例 true 即启用 sysctl。
- 切换 VMAC 依同一 VIP 的权威选举，不另建 VRRP 选举。Linux 专用辅助程序运行时再验证名称/MAC/链路本地值；additionalFailoverVMACs 的 YAML 验证较主条目弱。生成/应用时需要有效父接口引用与已安装辅助程序。
- gracefulActivation 仅 Linux IPv4 VRRP。选举/VMAC 可先于 VIP 发布，就绪条件允许 VIP 加入。超时仅报失败并持续保留与重试，绝不强制发布。VIP 成功发布后，只要仍为 master，后续就绪条件退化不撤回 VIP。
- VRRP 通告时间由实现决定（通常 1 秒），不通过 advertInterval/preemptDelay 配置；这些旧成员排除于 YAML/JSON 模式。协议优先级/认证仍依 keepalived/CARP。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必填; string
    addressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    externalDNS: true # 选填; boolean
    family: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # 选填; boolean
    hostname: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    mode: "" # 选填; string; enum=["", "static", "vrrp"]
    track: # 选填; array
      - # object
        confirmConsecutiveHealthy: 1 # 选填; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # 选填; integer; minimum=1; maximum=255
        resource: "<string>" # 必填; string
        unhealthyPenalty: 0 # 选填; integer; minimum=0; maximum=254
    vrrp: # 选填; object
      additionalFailoverVMACs: # 选填; array
        - # object
          interface: "<string>" # 必填; string
          linkLocalAddress: "<string>" # 选填; string
          macAddress: "<string>" # 必填; string
          parentInterface: "<string>" # 必填; string
          withdrawRouterAdvertisement: true # 选填; boolean
      authentication: "<string>" # 选填; string
      authenticationFrom: # 选填; object
        base64: true # 选填; boolean
        env: "<string>" # 选填; string
        file: "<string>" # 选填; string
      failoverVMAC: # 选填; object
        interface: "<string>" # 必填; string
        linkLocalAddress: "<string>" # 选填; string
        macAddress: "<string>" # 必填; string
        parentInterface: "<string>" # 必填; string
        withdrawRouterAdvertisement: true # 选填; boolean
      gracefulActivation: # 选填; object
        readyWhen: # 必填; object
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
        timeout: "<string>" # 选填; string
      peers: # 选填; array
        - "<string>" # string
      preempt: true # 选填; boolean
      priority: 1 # 选填; integer; minimum=1; maximum=254
      virtualRouterID: 1 # 必填; integer; minimum=1; maximum=255
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
| `spec.family` | 必填 ipv4/ipv6，无默认，须与地址族一致。 |
| `spec.interface` | 必填单独声明逻辑接口名称，解析为 OS 设备，不是 Kind/name。 |
| `spec.address` | 与 addressFrom 互斥的固定地址前缀。IPv4 /32；IPv6 除静态模式外 /128。语义验证允许 addressFrom 替代，但 JSON Schema 将 address 标为必填。 |
| `spec.addressFrom` | 与 address 互斥的通用来源，生成器支持声明 IPv4StaticAddress.address/VirtualAddress.address，须明确 field。不递归解析嵌套 VirtualAddress 来源。 |
| `spec.hostname` | 可选 FQDN，用于已服务 DNSZone 的自动本机 DNS 记录与状态。不改 OS 主机名或创建外部 DNS 记录。 |
| `spec.externalDNS` | 默认 false，true 跳过自动本机名称记录与 DNS 范围警告，不联系外部 DNS 提供商。 |
| `spec.mode` | static（默认）或 vrrp。static 不选举直接应用本机；vrrp 使用 Linux keepalived/FreeBSD CARP。VRRP/track 对象不限制静态持有。 |
| `spec.gratuitousARP` | 默认 false，仅 Linux 静态 IPv4 可 true。新增/恢复缺失 VIP 后执行 arping -U -c 3，普通协调不反复公告。失败会报告并重试。 |
| `spec.when` | 可选通用资源启用条件，默认无条件。筛选整个资源，与 gracefulActivation.readyWhen 的加入门槛不同。 |
| `spec.vrrp` | 仅 mode=vrrp 使用的选举配置。静态模式忽略，且不验证所有内层值。 |
| `spec.vrrp.virtualRouterID` | vrrp 必填 1–255，映射 CARP vhid。同 Router 的 family/interface 内唯一，同次选举的节点须一致。 |
| `spec.vrrp.priority` | 1–254，省略/0 为 100。跟踪扣分后至少 1；CARP 转换为 advskew=max(1,254-priority)。 |
| `spec.vrrp.preempt` | 省略/false 不仅因较高优先级夺回持有；true 启用抢占。CARP 使用主机全局 net.inet.carp.preempt，任一配置实例 true 即启用。 |
| `spec.vrrp.peers` | Linux vrrp 必填非空对等地址/主机名列表。语法验证，生成时去重排序。FreeBSD CARP 不将此列表作对等配置。 |
| `spec.vrrp.authentication` | 可选明文认证值，默认无，与 authenticationFrom 互斥。Linux 生成 keepalived auth_type PASS，CARP 使用 pass。是协议认证，不是加密 VRRP 流量。 |
| `spec.vrrp.authenticationFrom` | 可选密钥来源，与明文 authentication 互斥。配置时 file/env 必须择一。生成时读取，缺失文件/环境或无效 Base64 会失败；解析空值会省略认证。 |
| `spec.vrrp.authenticationFrom.file` | 路由器上的密钥文件路径，与 env 互斥。生成器移除末尾 CR/LF，不移除全部前后空格。 |
| `spec.vrrp.authenticationFrom.env` | routerd 进程的环境变量名称，与 file 互斥。不是 shell 展开或远端环境变量。 |
| `spec.vrrp.authenticationFrom.base64` | 默认 false。true 在移除编码值前后空白后解码标准 Base64，再移除解码结果末尾 CR/LF。 |
| `spec.vrrp.failoverVMAC` | 可选由同一 VRRP 选举控制的 Linux macvlan L2 身份，不另选举。主条目在 Linux VRRP 模式验证，FreeBSD 不支持。 |
| `spec.vrrp.additionalFailoverVMACs` | 可选依相同角色转换的额外 VMAC，默认空，字段同 failoverVMAC。当前 YAML 验证不完整，Linux 辅助程序/生成仍需有效父接口、名称与 Ethernet MAC。 |
| `spec.vrrp.gracefulActivation` | 可选 Linux IPv4 VRRP 加入门槛，默认无；其他地址族/FreeBSD 拒绝。选举后准备依赖状态并等待发布 VIP。 |
| `spec.vrrp.gracefulActivation.readyWhen` | 提供门槛时必填非空通用 state/all/any 条件。验证引用与条件，仅控制初次发布，不控制成功发布后的持续撤回。 |
| `spec.vrrp.gracefulActivation.timeout` | 严格正 Go 时长，默认 45s。到期仅将启用状态改为 Failed/ReadinessTimeout，不强制发布、不停止重试或重启路由器。 |
| `spec.track` | 可选资源健康跟踪列表，默认空，仅 vrrp。异常扣分累加影响选举优先级，不直接移除静态 VIP。 |
| `spec.track[].resource` | 必填 Kind/name 格式，此处验证格式不验存在。读取 phase，普通就绪 phase 为健康，BGPRouter/BGPPeer 需 Established。Disabled/Standby/NotApplicable 与 when=false 为中立，保留当前扣分决定。 |
| `spec.track[].unhealthyPenalty` | 0–254，省略/0 为 50，零无法禁用扣分。确认异常后应用，确认健康后移除。 |
| `spec.track[].confirmConsecutiveUnhealthy` | 扣分前连续异常评估次数，省略/0 为 3，明确 1–255。是协调观测次数，不是秒数。 |
| `spec.track[].confirmConsecutiveHealthy` | 解除扣分前连续健康次数，省略/0 为 2，明确 1–255。跟踪决定/计数可在重启后由保存状态恢复。 |
| `spec.vrrp.failoverVMAC.parentInterface` | 必填配置的逻辑父接口，生成时解析为 OS 名称，不是状态来源。 |
| `spec.vrrp.additionalFailoverVMACs[].parentInterface` | 必填配置的逻辑父接口，生成时解析为 OS 名称，不是状态来源。 |
| `spec.vrrp.failoverVMAC.interface` | 必填 Linux VMAC 设备名称，最多 15 字符且须有效接口语法，由角色辅助程序创建/启用/禁用。 |
| `spec.vrrp.additionalFailoverVMACs[].interface` | 必填 Linux VMAC 设备名称，最多 15 字符且须有效接口语法，由角色辅助程序创建/启用/禁用。 |
| `spec.vrrp.failoverVMAC.macAddress` | 必填六字节 Ethernet MAC，共享切换身份的节点须一致，不从 VRID 自动生成。 |
| `spec.vrrp.additionalFailoverVMACs[].macAddress` | 必填六字节 Ethernet MAC，共享切换身份的节点须一致，不从 VRID 自动生成。 |
| `spec.vrrp.failoverVMAC.linkLocalAddress` | 可选纯 IPv6 链路本地身份（fe80::/10），不含前缀/区域后缀。Linux 辅助程序调用时验证，省略采用普通链路本地处理。 |
| `spec.vrrp.additionalFailoverVMACs[].linkLocalAddress` | 可选纯 IPv6 链路本地身份（fe80::/10），不含前缀/区域后缀。Linux 辅助程序调用时验证，省略采用普通链路本地处理。 |
| `spec.vrrp.failoverVMAC.withdrawRouterAdvertisement` | 默认 false。多 VMAC 参数形式中 true 会在禁用前以链路本地源发出 router lifetime=0 RA。未明确 linkLocalAddress 的单 VMAC 旧参数形式不传递此标志。 |
| `spec.vrrp.additionalFailoverVMACs[].withdrawRouterAdvertisement` | 默认 false。多 VMAC 参数形式中 true 会在禁用前以链路本地源发出 router lifetime=0 RA。未明确 linkLocalAddress 的单 VMAC 旧参数形式不传递此标志。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/vrrp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/vrrp/controller.go) · [pkg/controller/vrrp/backend.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/vrrp/backend.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/render/vrrp.go](https://github.com/imksoo/routerd/blob/main/pkg/render/vrrp.go) · [pkg/render/carp.go](https://github.com/imksoo/routerd/blob/main/pkg/render/carp.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/secrets.go](https://github.com/imksoo/routerd/blob/main/pkg/render/secrets.go) · [cmd/routerd-vrrp-vmac/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-vrrp-vmac/main.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `address`, `ifname`, `role`, `hostname`, `virtualRouterID`, `priority`, `failoverVMACs`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
