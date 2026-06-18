---
title: 资源 API v1alpha1
slug: /reference/api-v1alpha1
---

# 资源 API v1alpha1

![Diagram showing the Resource API v1alpha1 shape from apiVersion, kind, metadata, spec, and status through API groups and generated schema validation contracts](/img/diagrams/api-v1alpha1.png)

routerd 的配置由最顶层的 `Router` 以及类型化资源列表组成。
本页依照当前实现列出各资源。
Phase 1.6 起，DHCP 相关的 Kind 依 RFC 表记改为 `DHCPv4*` 与 `DHCPv6*`。
旧名称无兼容别名。

## 通用格式

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: wan
spec:
  ifname: ens18
  adminUp: true
```

| 字段 | 说明 |
| --- | --- |
| `apiVersion` | API 组与版本。 |
| `kind` | 资源种类。 |
| `metadata.name` | 同种类内的名称。 |
| `spec` | 用户声明的意图。 |
| `status` | routerd 或专用守护进程观测到的状态。 |

## API 组

| API 组 | 主要 Kind |
| --- | --- |
| `routerd.net/v1alpha1` | `Router` |
| `net.routerd.net/v1alpha1` | 接口、`ManagementAccess`、可复用的 `IPAddressSet`、DHCP、DNS、路由、隧道、VIP、BGP、事件、流量日志 |
| `firewall.routerd.net/v1alpha1` | `FirewallZone`, `FirewallPolicy`, `FirewallRule`, `FirewallEventLog`, `ClientPolicy`, `PortForward`, `IngressService`, `LocalServiceRedirect` |
| `system.routerd.net/v1alpha1` | `Hostname`, `Sysctl`, `SysctlProfile`, `Package`, `NTPClient`, `NTPServer`, `LogSink`, `ObservabilityPipeline`, `RouterdCluster`, `LogRetention`, `WebConsole` |
| `plugin.routerd.net/v1alpha1` | 插件列表 |

## 系统准备

| Kind | 用途 |
| --- | --- |
| `Package` | 补充无法从其他资源导出的 OS 软件包，是限定范围的 override。一般的运行时依赖软件包会自动导出。 |
| `Sysctl` | 补充当前尚无法从路由器资源导出的 sysctl 值，是限定范围的 escape hatch。可通过 `compare: exact` 与 `compare: atLeast` 选择回读的判断方式。 |
| `SysctlProfile` | 补充路由器推荐的 sysctl 值，是限定范围的 escape hatch。一般的路由器 sysctl 会自动导出。 |
| `Hostname` | 配置主机名称。 |
| `NTPClient` | 启用 OS 的 NTP 客户端。从 DHCPv4 / DHCPv6 的状态导出时间服务器，若为空则回退至公共 NTP 服务器。 |
| `NTPServer` | 运行面向 LAN 的本地 NTP 服务器。除静态的 `allowCIDRs` 外，也可通过 `allowCIDRFrom` 从 `IPv6DelegatedAddress/<name>.address` 或 `DHCPv6PrefixDelegation/<name>.currentPrefix` 等 status 字段导出允许范围。 |
| `LogSink` | 将日志事件转发至 syslog、OTLP、webhook、file、journald。 |
| `ObservabilityPipeline` | 配置 OTLP 的环境，以及将 routerd 事件转发至 stdout / syslog / Loki。 |
| `RouterdCluster` | 通过 file 租约让 leader 负责变更主机配置，standby 则仅进行状态观测。 |
| `LogRetention` | 管理事件、DNS、流量、防火墙事件日志的保存期限。 |
| `WebConsole` | 在管理网络上提供只读的 Web 管理界面。 |

## 接口

| Kind | 用途 |
| --- | --- |
| `Interface` | 将 routerd 使用的稳定名称与 OS 接口名称对应，并提供下游资源所需的 link/address status。 |
| `ManagementAccess` | 声明管理用接口与 apply 前的 lockout 检查。声明后，若检测到管理接口缺失、被 firewall zone 阻断、或启用的 WebConsole 绑定到所有地址，则在未指定 `--allow-mgmt-lockout` 时中止 apply。 |
| `PPPoESession` | 表示 PPPoE 的底层接口配置。 |
| `PPPoESession` | 由 `routerd-pppoe-client` 管理的 PPPoE 会话。 |
| `WireGuardInterface` | 表示 WireGuard 接口。 |
| `WireGuardPeer` | 表示 WireGuard 对端。 |
| `TailscaleNode` | 配置 Tailscale 节点。通过受管理的 systemd 单元管理 Exit node 与 subnet router 的广播。 |
| `IPsecConnection` | 表示 strongSwan 的 cloud VPN 连接定义。 |
| `VRF` | 表示 Linux VRF 设备与路由表。 |
| `VXLANTunnel` | 表示 VXLAN 隧道。 |

将 `PPPoESession.spec.enabled` 设为 `false`，可在保留 PPPoE 定义的同时，停止并禁用受管理的 pppd 单元。
这样在正常运行时不使用 PPPoE 会话，仅在需要时手动用作备用路径。

`TailscaleNode` 可在首次注册时使用 `authKey`。
正式环境建议使用 `authKeyEnv` 或 `authKeyFile`，
以避免将密钥值写入 YAML 与 Git 历史记录。
两者均未指定时，`tailscaled` 视为已登录状态。
routerd 仅重新应用所广播的节点配置。
Tailscale 默认的 UDP/41641 视为保留用途。
WireGuard 的监听端口请使用其他号码。
详细配置步骤请参阅 Tailscale 配置指南。

`WireGuardInterface` 可接受 `privateKeyFile`，以便将私钥存放在路由器 YAML 之外。
`WireGuardPeer` 也可接受可选的 `presharedKeyFile` 作为 PSK。
内嵌的密钥字段主要用于示例与测试。
在 FreeBSD 上，routerd 会生成 rc.d 服务，
该服务负责创建 `wg` 接口、从文件读取私钥，
并应用所声明的 peer 与静态地址。

核心模块，以及 systemd-networkd/resolved 的 adoption drop-in 均从路由器资源自动导出。若已删除的 `KernelModule`、`NetworkAdoption`、`Link` 仍残留在配置中，routerd 不会静默忽略，而是返回错误。

## WAN 地址与委派

| Kind | 用途 |
| --- | --- |
| `IPv4StaticAddress` | 分配静态 IPv4 地址。 |
| `VirtualAddress` | 声明 IPv4 `/32` 或 IPv6 `/128` VIP。`spec.family` 为 `ipv4` 或 `ipv6`。`mode: vrrp` 在 Linux 使用 keepalived，在 FreeBSD 使用 CARP。 |
| `DHCPv4Client` | 由 `routerd-dhcpv4-client` 管理 DHCPv4 租约、IPv4 地址及可选的默认路由。 |
| `DHCPv6Address` | 表示 DHCPv6 IA_NA 的意图。 |
| `DHCPv6PrefixDelegation` | 由 `routerd-dhcpv6-client` 管理的 DHCPv6-PD 租约。 |
| `DHCPv6Information` | DHCPv6 信息请求的结果。观测 DNS、SNTP、域名搜索、AFTR 信息。 |
| `IPv6DelegatedAddress` | 从委派前缀导出 LAN 侧地址。 |
| `IPv6RAAddress` | 表示通过 RA/SLAAC 获取的 IPv6 地址。 |

`DHCPv6PrefixDelegation` 不具备旧式的 OS 客户端选择字段。
DHCPv6-PD 由 `routerd-dhcpv6-client` 负责。

## LAN 侧服务

| Kind | 用途 |
| --- | --- |
| `DHCPv4Server` | 提供 dnsmasq 的 DHCPv4 服务与可选的地址池。 |
| `DHCPv4Reservation` | 表示依 MAC 地址的固定分配。 |
| `DHCPv4Relay` | 表示 dnsmasq 的 DHCPv4 中继。 |
| `IPv6RouterAdvertisement` | 生成 RA、PIO、RDNSS、DNSSL、M/O 标志、MTU、优先级、存活时间。 |
| `RogueRADetector` | 自动导出的资源，以 status 显示在发出 RA 的接口上观测到的、非自身发出的 IPv6 Router Advertisement。 |
| `DHCPv6Server` | dnsmasq 的 DHCPv6/RA 服务。支持 `stateless`、`stateful`、`both`、`ra-only`。 |
| `DNSZone` | 表示本地权威区域。处理手动录入的记录与 DHCP 租约衍生的记录。 |
| `DNSResolver` | 表示 `routerd-dns-resolver` 的守护进程实例、监听、缓存、metrics、查询日志。 |
| `DNSForwarder` | 隶属于某个解析器的 DNS match 规则。可从 `DNSZone` 响应，或转发至指定的 `DNSUpstream`。 |
| `DNSUpstream` | 表示一个上游端点，支持 `udp`、`tcp`、`dot`、`doh`。亦可指定状态衍生地址、bootstrap 解析器、TLS 名称及来源接口。 |

由于 Android 仅靠 DHCPv6 的 DNS 无法完成名称解析，在 IPv6 LAN 环境中需配置 `IPv6RouterAdvertisement.spec.rdnss`。

dnsmasq 仅负责 DHCPv4、DHCPv6、中继、RA。
DNS 的监听与响应由 `DNSResolver` 负责。
LAN 的 DNS suffix 可通过 `DHCPv4Server.spec.domainFrom`、
`IPv6RouterAdvertisement.spec.dnsslFrom`、`DHCPv6Server.spec.domainSearchFrom`
引用 `DNSZone/<name>.zone`，与本地区域保持一致。
`DNSResolver.spec.listen[].sources` 中列出该 listener 使用的 `DNSForwarder` 名称。
省略 listener 时，会使用引用该解析器的所有 `DNSForwarder`。
用户 YAML 的 `DNSResolver.spec.sources` 不予接受。请将旧式的内嵌 source
拆分为 `DNSForwarder` 与 `DNSUpstream`。

`DNSForwarder.spec.match` 可指定 `home.example` 或表示默认上游的 `.`。
`spec.zoneRefs` 从本地 `DNSZone` 响应，`spec.upstreams` 则转发至 `DNSUpstream`。
DNSSEC 验证写在 `DNSForwarder.spec.dnssecValidate`。

`DNSUpstream.spec.protocol` 为 `udp`、`tcp`、`dot`、`doh` 之一。
`addressFrom` 可从 `DHCPv6Information/<name>.dnsServers` 等来源导出 UDP 上游地址。
`sourceInterface` 在 Linux 上绑定发出接口，`bootstrap` 用于解析 DoH/DoT 端点名称的辅助解析器。

## DS-Lite、路由、NAT

| Kind | 用途 |
| --- | --- |
| `DSLiteTunnel` | 向 AFTR 建立 `ip6tnl` 隧道。AFTR 可直接指定 IPv6、FQDN 或从 DHCPv6 信息获取。 |
| `TunnelInterface` | 为 hybrid overlay delivery 创建受信任的 Linux L3 underlay tunnel device。`mode` 支持 `ipip`、`gre` 以及 IPIP-over-UDP `fou`/`gue`。 |
| `OverlayPeer` | 描述 on-prem 或 cloud overlay peer 以及用于到达它的本地 underlay。新 CloudEdge SAM transport 优先使用 `SAMTransportProfile`。 |
| `HybridRoute` | 将非默认远端 IPv4 prefix 经由 `OverlayPeer` 降低为受管理的 `IPv4Route` resource。 |
| `MobilityPool` | CloudEdge mobility 的高层 intent。声明 pool prefix、federation group、node membership、BGP delivery policy、cloud capture profile、local value expansion 与 provider trap placement。 |
| `SAMTransportProfile` | 声明本 router 的 `selfNodeRef`、共享 topology node list、inner tunnel prefix、underlay interface、BGP router 与 SAM transport peer。routerd 通过 `DynamicConfigPart` 生成 per-peer `TunnelInterface`、endpoint `/32` `IPv4Route` 与 `BGPPeer`。 |
| `AddressMobilityDomain` | 低层兼容 SAM resource，用于 hand-authored selective-address config 中的 IPv4 prefix。不是当前 CloudEdge Mobility 的主要 authoring surface。 |
| `CloudProviderProfile` | 描述 provider capabilities 与 external-command auth，用于 declarative address capture planning。 |
| `RemoteAddressClaim` | 低层兼容 SAM resource，声明单个 mobile IPv4 `/32`、capture mechanism 与 legacy `OverlayPeer` route delivery。 |
| `IPAddressSet` | 从直接指定的地址或 FQDN 定义可复用的 IP 地址集。Linux nftables 的生成器将其输出为 named set，可从 redirect、NAT、policy routing 引用。 |
| `IPv4Route` | 添加 IPv4 路由。也可用于 DS-Lite 的默认路由或明确的丢弃路由。 |
| `ClusterNetworkRoute` | 将 Kubernetes 的 Pod / Service CIDR 展开为经由 worker next hop 的静态 IPv4 路由。 |
| `BGPRouter` | 声明本地 BGP 路由器。当前的后端是长寿命的 `routerd-bgp` GoBGP 守护进程，导入策略默认为 deny。 |
| `BGPPeer` | 声明隶属于 `BGPRouter` 的 GoBGP 管理 BGP peer。适用于 Kubernetes BGP speaker 等场景。 |
| `BFD` | 声明 BFD session intent。在 Linux 上，routerd render FRR `bfdd` 配置并记录观测到的 BFD 状态；被引用的 GoBGP peer 不会因 BFD false-down 而被 deconfigure。 |
| `NAT44Rule` | 在 nftables 的 `routerd_nat` 表中执行 IPv4 NAPT。 |
| `PortForward` | 将 WAN 侧的 IPv4 TCP/UDP 端口 DNAT 至单一内部 IPv4 目的地。 |
| `IngressService` | 公开 WAN 侧的 IPv4 TCP/UDP 服务。支持多个 backend、TCP/HTTP 健康检查，以及 `failover` / `sourceHash` / `random` 选择策略。 |
| `LocalServiceRedirect` | 将 LAN 侧客户端向 `IPAddressSet` 发出的 IPv4/IPv6 流量重定向至路由器本地端口。适用于集中纯文本 DNS/NTP，不影响 DoH 或 DoT 端口。 |
| `EgressRoutePolicy` | 表示默认路由选择、基于 mark 的 IPv4 policy routing，以及向多个 target 的 hash 分散。 |

CloudEdge Mobility 的 operator-authored surface 是 `MobilityPool` 与
`SAMTransportProfile`。`MobilityPool` 负责地址 ownership/capture intent，
`SAMTransportProfile` 负责 transport/BGP intent；federation event 是 observed fact，
BGP best path 是 mobility ownership/delivery view。mobility planner 生成 BGP `/32`
advertisement 与 provider trap action plan。operator 不应手写 per-address path 或
capture procedure。`AddressMobilityDomain` 与 `RemoteAddressClaim` 仍作为低层兼容 Kind
保留在 MobilityPool BGP path 之外。

`MobilityPool.spec.deliveryPolicy.mode` 默认为 `bgp`。Provider action plan 是 review
artifact，只有在导入 action journal 并通过 `ProviderActionPolicy`、approval、allowlist
与 executor plugin gate 后才可能执行。

`EgressRoutePolicy` 除 CIDR 指定外，还具有 `destinationSetRefs` 与
`excludeDestinationSetRefs`。这让以 FQDN 为后端的目的地集合无需在 policy
资源中展开地址，即可用于路由控制与排除条件。
`mode: priority` 用于默认路由故障转移，`mode: mark` 用于单一带 mark 的路由
表，`mode: hash` 或 `candidates[].targets` 用于向多个路由表进行
source/destination 的 hash 分散。

routerd 从路由器角色、隧道、防火墙区域、RA/DHCPv6 资源自动导出 reverse path filter sysctl、隧道 MTU、RA MTU、TCP MSS clamp。
配置中只需声明 LAN/WAN 与隧道的意图，无需编写 `IPv4ReversePathFilter` 或
`PathMTUPolicy`。
若外部管理的来源接口（如 `tailscale0`）具有较低的 MTU，可配置 `Interface.spec.mtu`。routerd 仅将其用于该来源路径，不会将较低的 MTU 应用至无关的 LAN 路径。

`EgressRoutePolicy` 具有 `excludeDestinationCIDRs`，可将 LAN 内部、管理网络、HGW LAN、RFC 1918 私有网络等排除在 policy routing 对象之外。

`ClusterNetworkRoute` 是面向 Kubernetes 节点的辅助资源。
在 `spec.pods.cidrs` 与 `spec.services.cidrs` 中列出 Pod / Service CIDR，
并在 `spec.via[]` 中指定 worker 或 VIP 的 next hop，routerd 即会生成
对应的 `IPv4StaticRoute` 意图。相同 weight 视为相同 metric，可用于多 next hop 的 ECMP；不同 weight 会转换为 metric 差值，表示优先路由与备用路由。

`FirewallRule` 除目的地 CIDR 外，还具有 `destinationSetRefs` 与
`excludeDestinationSetRefs`，让可复用的 FQDN 后端集合无需在各规则中展开地址，即可作为允许、拒绝、reject 的条件。
Stateful rule expression 亦支持 `sourcePorts`、`destinationPorts`、ICMP / ICMPv6 的
type matching、`rateLimit`、`connLimit`。`port` 作为单一目的地端口的简写仍可使用，但新示例建议改用 `destinationPorts`。

`NAT44Rule` 支持通过 `outboundInterface`、`sourceCIDRs`、`translation` 进行简单的
source NAT，以及通过 `type`、`egressInterface` 或 `egressPolicyRef`、`sourceRanges`
进行具有 policy 感知的 NAT。此外还具有 `destinationCIDRs`、`destinationSetRefs`、
`excludeDestinationCIDRs`、`excludeDestinationSetRefs`，可将仅互联网流量进行 masquerade，而有静态路由的私有目的地或可复用的地址集合则不进行 NAT。

`PortForward` 与 `IngressService` 在 Linux nftables 与 FreeBSD pf 上生成 DNAT。
指定 `spec.hairpin.enabled: true` 与 `spec.hairpin.interfaces` 后，也会生成让 LAN
客户端通过 WAN 地址连到同一服务的 hairpin NAT。
hairpin 需要 `listen.address` 或 `listen.addressFrom`，routerd 会生成 LAN 侧的
DNAT 与返回路径的 masquerade/NAT reflection。
`listen.addressFrom` 与 backend 的 `addressFrom` 可引用 `IPv4StaticAddress/<name>.address`
或 `VirtualAddress/<name>.address` 等可静态描述的地址资源。
`IngressService` 中，未指定 `spec.hairpin.mode` 视为 `auto`。
当 listen 地址与所选 backend 位于 listen 接口声明的同一前缀上时，routerd 会自动生成
LAN 客户端使用 VIP 所需的同一接口返回 SNAT。即使 YAML 未声明 listen 接口的前缀，
只要私有 IPv4 的 listen/backend 地址位于同一 `/24`，也会判断需要 hairpin。
这是为了涵盖如 Live ISO 等从启动环境继承接口地址的场景。
若要禁用，请使用 `spec.hairpin.mode: off`；明确指定时使用 `manual` 与 `interfaces`。
`VirtualAddress.spec.vrrp.authentication` 在 keepalived 中生成为 `auth_pass`，
在 FreeBSD CARP 中生成为 `pass`。正式环境不建议将共用密钥留在 routerd YAML 中，
请优先使用 `VirtualAddress.spec.vrrp.authenticationFrom`。
`authenticationFrom.file` 读取本地密钥文件，
`authenticationFrom.env` 读取环境变量，`base64: true` 可解码 base64 值。
已生成的 keepalived/CARP 配置与主机接口状态请视为机密。
VRRP authentication 在 VRRPv3（RFC 5798）中已 deprecated。routerd 以 L2 隔离为前提，
authentication 仅在周边网络有要求或作为简单的配置错误防护时使用。
`IngressService` 支持多个 backend、TCP health check、故障转移 policy。
runtime 控制器解析 backend 的 FQDN，DNS 暂时失败时以上次解析的 IPv4 作为 fallback。当健康的 backend 有多个时，Linux nftables 以 `sourceHash` 使用 `jhash ip saddr`、`random` 使用 `numgen random` 进行分配；健康的 backend 只剩一个时则降级为故障转移。
validator 会拒绝 `IngressService`、`LocalServiceRedirect`、routerd 管理的守护进程在同一接口/协议上发生冲突的监听端口配置。

`IPAddressSet` 在应用时将直接指定的 IPv4/IPv6 地址输出至 nftables 的 named set。
FQDN 的 `A`/`AAAA` 记录由 runtime 控制器解析，并在不重新加载整个防火墙、NAT、policy 表的情况下实时更新所引用的 set。下次更新以观测到的最小 DNS TTL 的一半为基准，最短不低于 60 秒。`refreshInterval` 可用作希望更积极更新时的上限。

`IPAddressSet.spec.names` 仅处理完全匹配的 DNS 名称。`microsoft.com` 仅表示
`microsoft.com` 本身的 `A`/`AAAA` 记录，不包含 `www.microsoft.com`、
`login.microsoft.com`、`*.microsoft.com` 及更深层的子域名。
通配符或以 suffix 形式判断服务的场景，需要能处理 DNS 查询观测或 provider 端点 feed 的其他资源，而非简单的 FQDN 解析。

`BGPRouter` 与 `BGPPeer` 使用长寿命的 `routerd-bgp` 守护进程。
routerd 通过本地 gRPC Unix socket 将资源 spec 直接映射为类型化的 GoBGP API 对象，
并以 `ListPeer` 与 `ListPath` 观测状态。不使用 FRR 的文本配置、
`frr-reload.py`、`vtysh` 解析、GoBGP 的文件配置。
`apply` 仅生成主机 artifact，
BGP 作为 `routerd serve` 的管理对象显示于 status。`routerctl show bgp` 显示从存储的
GoBGP 观测数据中，路由器、peer、消息计数器、路由选择状态及最近的错误。
前缀 status 包含 `best`、`valid`、`installed`、`stale`、`nextHop`、
observed community。符合 `spec.importPolicy.allowedPrefixes` 的已学习 IPv4 best path，
以 routerd 拥有的 protocol/metric 写入内核 FIB。
默认情况下，GoBGP import policy 接受的 eBGP next-hop 会改写为学习来源的 peer 地址
（`spec.importPolicy.nextHopRewrite: peer-address`）。这与旧版 FRR 的
`set ip next-hop peer-address` 含义相同，即使广播的 next-hop 指向 downstream speaker，
也可以 peer 地址的 ECMP 形式写入。仅在希望将广播的 next-hop 原样写入内核时，才指定 `nextHopRewrite: unchanged`。
相同前缀的 equal best path 作为 ECMP 的 next-hop 写入。

`BGPRouter.spec.convergenceProfile: fast` 适用于 Kubernetes/edge 路由器，优先快速收敛而非 graceful restart 的 stale-path 保留。fast profile 缩短 peer timer，并在未明确配置 `spec.gracefulRestart.enabled` 时禁用 graceful restart。导入策略默认为 deny。请在 `spec.importPolicy.allowedPrefixes` 中列出希望接受的前缀，例如 Kubernetes LoadBalancer pool。
`BGPPeer.spec.ebgpMultihop` 适用于 loopback peering 或 lab 至正式路由器验证等非直连的 eBGP session。未指定、`0`、`1` 为直连 eBGP 的默认行为；指定 `2` 至 `255` 时，作为该 peer group 的 GoBGP multihop TTL。
router ID 不必与 TCP 的来源地址相同，但 peer 侧需配置主机实际使用的 BGP 来源地址。LAN 有多个地址时，在 Linux 可用 `ip route get <peer-address>` 确认来源地址，除非有明确理由，建议将 router ID 也与该运行中的来源地址对齐，以避免混乱。

`BGPRouter` 可广播 connected/static IPv4 路由，并附上各自的 `allowedPrefixes`。
仅在 `BGPRouter.spec.exportPolicy.allowedPrefixes` 或 redistribute 的 allow-list 中明确列出的前缀，才会作为 GoBGP 的本地路径添加。BGP community policy 可在路由器或
peer 上以 `communities.send`、`communities.accept`、`communities.set.in/out` 声明，
GoBGP 报告的 observed route community 存储于 status。watcher 默认每 15 秒轮询，前缀 status 上限为 4096 个条目。可通过 `BGPRouter.spec.watcher` 调整
`pollInterval`、`maxPrefixes`、`peerStateChangeThrottle`。验证会拒绝小于 3 秒的 interval 与 1,000,000 以上的前缀上限。GoBGP MVP 每个路由器支持一个 `BGPRouter`，`spec.vrf` 尚未支持。
multi-router、VRF、BFD 资源不会静默忽略，而是报告为 Pending。
`spec.listen.address` 与 `spec.listen.port` 用于绑定 `routerd-bgp` 的 GoBGP listener。

`VirtualAddress` 的 `mode: vrrp` 在 Linux 使用 keepalived，在 FreeBSD 使用 CARP。
`spec.family: ipv4` 需要 IPv4 `/32`，`spec.family: ipv6` 需要 IPv6 `/128`。
IPv6 VIP 在 keepalived 中生成为 VRRPv3 的 `family inet6`，在 FreeBSD 中成为 `inet6` 的 CARP alias。
Linux VRRP 使用明确的 unicast peer，默认为 `nopreempt`。
FreeBSD CARP 使用父接口上的 multicast advertisement，因此 `spec.vrrp.peers` 在 FreeBSD 上会被忽略。`preempt: true` 仅在需要自动 failback 时使用。advertisement 与
failback 的低级 timing 不通过各资源字段，而是通过 routerd 的 profile 默认值处理。可通过 `track` 根据 `BGPRouter`、`BGPPeer`、`IngressService` 等的状态降低优先级。默认在连续 3 次 unhealthy 时应用惩罚，连续 2 次 healthy 时解除。`spec.hostname` 可让 DNSResolver 自动将 VIP 发布至对应的 `DNSZone`。IPv4 VIP 成为 A 记录，IPv6 VIP 成为 AAAA
记录。若由外部 AD DNS 等管理名称，请配置 `spec.externalDNS: true`。routerd 仅验证 hostname 语法，不发出 DNSZone coverage 警告也不自动发布。`routerctl show vrrp` 显示角色、
优先级、peer，以及自上次切换以来的经过时间。

### VRRP 正式环境调整

仅在需要自动 failback 的控制平面 VIP 等场景下使用 `preempt: true`。
家庭 LAN 或 DS-Lite 周边的 VIP，稳定性优先于回复至优先 owner，建议使用默认的非抢占行为。backup 取得 VIP 后，在该节点停机或明确移动之前会持续持有。完整的资源片段请参阅
`examples/vrrp-tuning-presets.yaml`。

`BGPPeer.spec.password` 作为 GoBGP peer 的 TCP MD5 authentication 密码传递。
正式环境不建议将共用密钥留在 routerd YAML 中，请优先使用 `BGPPeer.spec.passwordFrom`。
`passwordFrom.file` 读取本地 root 拥有的密钥文件，
`passwordFrom.env` 读取环境变量，`base64: true` 可解码 base64 值。


`IngressService` 支持多个 backend、TCP health check、故障转移 policy。
runtime 控制器解析 backend 的 FQDN，DNS 失败时以上次解析的 IPv4 作为 fallback。Linux nftables 在下次 NAT 调和（reconcile）时以 status 中的 active backend 作为转发目的地。不清除现有的 conntrack，因此现有流量保留在旧 backend，新流量则导向所选的 backend。`spec.hostname` 可自动反映至 DNSResolver 作为 listen 地址的 A 记录。若由外部 DNS 管理名称，请配置 `spec.externalDNS: true`。
`routerctl show ingress` 显示 active backend 及各 backend 的健康状态。
`routerctl show ingress --verbose` 亦显示 live dataplane 的 `ip_forward`、nftables 的
DNAT/SNAT 规则数、对应的 conntrack 流量数。`DETAIL` 栏显示
`hairpinMode`、是否需要 hairpin，以及预期的 nftables SNAT 规则是 present 还是 missing。从 Ingress、NAT 系、DS-Lite、IPv6 PD/RA、路由资源导出转发、redirect 抑制、reverse path filter 例外、各接口的 RA 接收等所需的 runtime sysctl。`routerctl apply` 会 plan / render 衍生配置，但主机变更仅限于明确的 `Sysctl` / `SysctlProfile` escape hatch。
衍生 runtime 配置的应用由 `routerd serve` 的控制器调和（reconcile）负责。
维护期间可用 `routerctl drain
ingress/<service> backend=<name> --duration 10m` 将 backend 设为 drain 状态。控制器在 duration 结束或执行 `routerctl undrain
ingress/<service> backend=<name>` 解除前，将该 backend 视为以 `Drained` 为原因的 unhealthy。

`LocalServiceRedirect` 在 Linux nftables 的 `prerouting` 生成 `redirect` 规则。
仅针对从指定接口进入的数据包，以及指向 `IPAddressSet` 目的地的流量。
路由器自身发出的通信与健康检查不经过此 hook。

示例：

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: PortForward
metadata:
  name: web-admin
spec:
  listen:
    interface: wan
    addressFrom:
      resource: IPv4StaticAddress/wan-ip
      field: address
    protocol: tcp
    port: 8443
  target:
    address: 172.18.1.88
    port: 443
  hairpin:
    enabled: true
    mode: manual
    interfaces:
      - lan
```

DS-Lite、IPv4 默认路由、NAT44 均已在实际 lab 中验证。

## 状态联动

| Kind | 用途 |
| --- | --- |
| `HealthCheck` | 从 target、protocol、cadence、threshold 声明连接探测的意图。被 `EgressRoutePolicy` 的 candidate/target 引用时，routerd 自动导出 health-check 守护进程、来源绑定及 socket mark。 |
| `EgressRoutePolicy` | 从准备就绪的候选中选出 weight 最高的 egress 路由。具有 `destinationCIDRs` 以及 candidate 的 `gatewaySource`、`gateway`。 |
| `EventRule` | 对事件序列评估 all_of、any_of、sequence、window、absence、throttle、debounce、count。 |
| `DerivedEvent` | 从多个资源状态发出虚拟事件。 |
| `SelfAddressPolicy` | 表示自身主机地址的选择策略。 |

将 `HealthCheck.spec.enabled` 设为 `false` 时，守护进程单元仍会生成，但会停止并禁用。
`EgressRoutePolicy` 的候选也可指定 `enabled: false`。
禁用的候选即使最后观测状态为 Healthy，也不会被选中。
`mode: priority` 中，candidate 的 `weight` 仍是选择的第一排序键，`priority` 用于平局决胜与 policy 规则的优先级。删除候选时，ledger 拥有的 policy-route 规则/路由表也会一并删除。

## `spec.when`

具有 `spec.when` 的资源，只在对 routerd 本地状态存储的 predicate 相符时才生效。传统的单一 predicate 语法仍可使用。

```yaml
when:
  state:
    wan.ipv6.mode:
      equals: pd-ready
```

AND 以 `all`、OR 以 `any` 表示，可任意深度嵌套。

```yaml
when:
  any:
    - all:
        - state:
            dslite.a.health:
              status: set
        - state:
            wan.ipv6.mode:
              in: [pd-ready, address-only]
    - state:
        pppoe.health:
          equals: healthy
```

每个 `when` 节点只能包含 `state`、`all`、`any` 之一。
`state` 以状态变量名称为键，通过 `exists`、`equals`、`in`、`contains`、
`status`、`for` 进行匹配。只有一个元素的 `all` 等同于单一 predicate 语法。
不公开专门用于状态管理的资源 Kind。条件式 activation 直接写在相依资源的 `spec.when` 中。

`HealthCheck.spec.sourceInterface` 在执行时解析为 OS 的接口名称。
Linux 使用 `SO_BINDTODEVICE`。若指定 `fwmark`，也会配置 `SO_MARK`。`HealthCheck` 被 `EgressRoutePolicy` 的 candidate 或 target 引用时，routerd 自动从该路由 target 的 mark 导出 `SO_MARK`。
直接指定 `fwmark` 适用于不与路由 target 绑定的低级探测。
FreeBSD 因没有与 Linux 相同的 socket option，改从指定接口选择来源地址。

## 系统

| Kind | 用途 |
| --- | --- |
| `Hostname` | 管理主机名称。 |
| `Sysctl` | 管理 sysctl 值。 |
| `NTPClient` | 管理 NTP 客户端配置。可通过 `serverFrom` 引用 `DHCPv4Client.status.ntpServers` 或 `DHCPv6Information.status.sntpServers`。 |
| `LogSink` | 表示日志的发送目的地。 |
| `WebConsole` | 显示状态、事件、IPv4/IPv6 连接观测的只读界面。 |

`Telemetry` 是将 routerd 自身及受管理守护进程的 metrics / traces / logs 送至
OpenTelemetry 端点的资源。`LogSink` 表示运行事件与观测日志的转发路径。若要将日志转发至 OTLP，请勿重复填写 collector 端点，而是通过 `LogSink.spec.otlp.telemetryRef` 引用 `Telemetry`。

`WebConsole.spec.listenAddressFrom` 从其他资源的状态导出 HTTP 的监听地址。
例如可引用 `Interface/mgmt.status.ipv4Addresses`。
若管理地址由 DHCP、IPAM 或其他声明资源提供，请使用此方式而非固定的 `listenAddress`。

## Status Provides Contract

`addressFrom`、`gatewayFrom`、`dnsServerFrom`、`dependsOn[].field`
等引用字段，只能引用来源 Kind 在此 contract 中声明的字段。引用不存在的资源，或 `provides` 中未声明的字段，验证器会返回错误。

| Kind | Provides |
| --- | --- |
| `BFD` | `peer` (string), `phase` (string) |
| `BGPPeer` | `acceptedPrefixes` (int), `address` (string), `observedAt` (timestamp), `phase` (string), `state` (string) |
| `BGPRouter` | `acceptedPrefixes` (int), `establishedPeers` (int), `observedAt` (timestamp), `peers` (objectList), `phase` (string), `prefixes` (int) |
| `Bridge` | `ifname` (string), `members` (stringList), `phase` (string) |
| `ClientPolicy` | `phase` (string) |
| `ClusterNetworkRoute` | `phase` (string), `pods` (stringList), `services` (stringList) |
| `DHCPv4Client` | `currentAddress` (string), `defaultGateway` (string), `device` (string), `dnsServers` (stringList), `domain` (string), `expiresAt` (timestamp), `gateway` (string), `interface` (string), `leaseTime` (int), `ntpServers` (stringList), `phase` (string), `rebindAt` (timestamp), `renewAt` (timestamp) |
| `DHCPv4Relay` | `phase` (string) |
| `DHCPv4Reservation` | `address` (string), `hostname` (string), `phase` (string) |
| `DHCPv4Server` | `configPath` (string), `dnsServers` (stringList), `domain` (string), `dryRun` (bool), `interface` (string), `ntpServers` (stringList), `phase` (string) |
| `DHCPv6Address` | `address` (string), `interface` (string), `phase` (string) |
| `DHCPv6Information` | `aftrName` (string), `dnsServers` (stringList), `domainSearch` (stringList), `phase` (string), `sntpServers` (stringList), `source` (string) |
| `DHCPv6PrefixDelegation` | `aftrName` (string), `currentPrefix` (string), `dnsServers` (stringList), `domainSearch` (stringList), `interface` (string), `phase` (string), `sntpServers` (stringList) |
| `DHCPv6Server` | `configPath` (string), `dnsServers` (stringList), `dryRun` (bool), `interface` (string), `phase` (string), `sntpServers` (stringList) |
| `DNSForwarder` | `phase` (string), `resolver` (string), `upstreams` (stringList) |
| `DNSResolver` | `listenAddresses` (stringList), `listeners` (int), `phase` (string), `sources` (int), `updatedAt` (timestamp) |
| `DNSUpstream` | `address` (string), `phase` (string), `url` (string) |
| `DNSZone` | `pendingRecords` (objectList), `phase` (string), `records` (int), `updatedAt` (timestamp), `zone` (string) |
| `DSLiteTunnel` | `aftrIPv6` (string), `aftrName` (string), `device` (string), `dryRun` (bool), `innerLocalIPv4` (string), `innerRemoteIPv4` (string), `interface` (string), `localIPv6` (string), `localInterface` (string), `mtu` (int), `phase` (string), `tunnelName` (string) |
| `DerivedEvent` | `phase` (string), `topic` (string) |
| `EgressRoutePolicy` | `advisory` (bool), `candidates` (objectList), `dryRun` (bool), `family` (string), `lastTransitionAt` (timestamp), `phase` (string), `role` (string), `selectedCandidate` (string), `selectedDevice` (string), `selectedGateway` (string), `selectedGatewaySource` (string), `selectedInterface` (string), `selectedMetric` (int), `selectedRouteTable` (int), `selectedSource` (string), `selectedTargets` (int), `selectedWeight` (int), `updatedAt` (timestamp) |
| `EventRule` | `phase` (string), `topic` (string) |
| `FirewallEventLog` | `path` (string), `phase` (string), `sinks` (stringList) |
| `FirewallPolicy` | `phase` (string) |
| `FirewallRule` | `action` (string), `phase` (string) |
| `FirewallZone` | `interfaces` (stringList), `phase` (string) |
| `HealthCheck` | `consecutiveFailed` (int), `lastCheckedAt` (timestamp), `phase` (string), `protocol` (string), `role` (string), `sourceAddress` (string), `sourceInterface` (string), `target` (string) |
| `Hostname` | `hostname` (string), `phase` (string) |
| `IPAddressSet` | `addresses` (stringList), `ipv4Addresses` (stringList), `ipv6Addresses` (stringList), `phase` (string), `updatedAt` (timestamp) |
| `IPsecConnection` | `phase` (string) |
| `IPv4Route` | `destination` (string), `device` (string), `dryRun` (bool), `gateway` (string), `metric` (int), `phase` (string), `type` (string) |
| `IPv4StaticAddress` | `address` (string), `dryRun` (bool), `ifname` (string), `interface` (string), `phase` (string) |
| `IPv4StaticRoute` | `destination` (string), `gateway` (string), `interface` (string), `phase` (string) |
| `IPv6DelegatedAddress` | `address` (string), `dryRun` (bool), `interface` (string), `phase` (string), `prefixSource` (string) |
| `IPv6RAAddress` | `address` (string), `interface` (string), `phase` (string) |
| `IPv6RouterAdvertisement` | `configPath` (string), `dryRun` (bool), `interface` (string), `phase` (string), `prefix` (string), `rdnss` (stringList) |
| `RogueRADetector` | `interface` (string), `observedRouters` (string), `packetsSeen` (string), `phase` (string), `rogueCount` (string), `selfMAC` (string) |
| `IPv6StaticRoute` | `destination` (string), `gateway` (string), `interface` (string), `phase` (string) |
| `IngressService` | `activeBackend` (object), `activeBackends` (objectList), `backends` (objectList), `dryRun` (bool), `healthyBackends` (int), `hostname` (string), `listenAddress` (string), `observedAt` (timestamp), `phase` (string), `totalBackends` (int) |
| `Interface` | `addresses` (stringList), `ifname` (string), `ipv4Addresses` (stringList), `ipv6Addresses` (stringList), `macAddress` (string), `phase` (string) |
| `Inventory` | `host` (object), `phase` (string) |
| `LocalServiceRedirect` | `phase` (string) |
| `LogRetention` | `phase` (string), `targets` (objectList), `updatedAt` (timestamp) |
| `LogSink` | `phase` (string), `type` (string) |
| `ManagementAccess` | `interfaces` (stringList), `phase` (string) |
| `NAT44Rule` | `dryRun` (bool), `egressInterface` (string), `phase` (string), `snatAddress` (string) |
| `NTPClient` | `phase` (string), `servers` (stringList), `source` (string), `updatedAt` (timestamp) |
| `NTPServer` | `allowCIDRs` (stringList), `listenAddresses` (stringList), `phase` (string), `servers` (stringList), `source` (string), `updatedAt` (timestamp) |
| `ObservabilityPipeline` | `phase` (string), `signals` (stringList) |
| `PPPoESession` | `connectedAt` (timestamp), `currentAddress` (string), `device` (string), `dnsServers` (stringList), `dryRun` (bool), `gateway` (string), `interface` (string), `peerAddress` (string), `phase` (string) |
| `Package` | `dryRun` (bool), `packages` (stringList), `phase` (string) |
| `PortForward` | `dryRun` (bool), `listenAddress` (string), `phase` (string), `target` (object) |
| `RouterdCluster` | `leader` (string), `leaseExpiresAt` (timestamp), `phase` (string) |
| `SelfAddressPolicy` | `address` (string), `phase` (string), `source` (string) |
| `Sysctl` | `dryRun` (bool), `key` (string), `phase` (string), `value` (string) |
| `SysctlProfile` | `dryRun` (bool), `phase` (string), `profile` (string) |
| `TailscaleNode` | `advertiseRoutes` (stringList), `peerCount` (int), `phase` (string), `tailnetName` (string) |
| `Telemetry` | `phase` (string), `signals` (stringList) |
| `TrafficFlowLog` | `path` (string), `phase` (string), `sinks` (stringList) |
| `VRF` | `ifname` (string), `members` (stringList), `phase` (string), `routeTable` (int) |
| `VXLANSegment` | `ifname` (string), `phase` (string), `vni` (int) |
| `VXLANTunnel` | `ifname` (string), `phase` (string), `vni` (int) |
| `VirtualAddress` | `address` (string), `dryRun` (bool), `hostname` (string), `ifname` (string), `phase` (string), `priority` (int), `role` (string), `virtualRouterID` (int) |
| `WebConsole` | `listenAddress` (string), `phase` (string), `port` (int) |
| `WireGuardInterface` | `fwmark` (int), `listenPort` (int), `peerCount` (int), `phase` (string), `publicKey` (string) |
| `WireGuardPeer` | `handshakeAgeSeconds` (int), `latestEndpoint` (string), `latestHandshake` (timestamp), `phase` (string), `transferRxBytes` (int), `transferTxBytes` (int) |

## 防火墙

| Kind | 用途 |
| --- | --- |
| `FirewallZone` | 将接口分配至区域，配置 `untrust`、`trust`、`mgmt` 角色。 |
| `FirewallPolicy` | 表示拒绝日志等全局配置。 |
| `FirewallRule` | 表示无法以角色组合表达的例外。可通过来源 CIDR、目的地 CIDR、`IPAddressSet` 目的地引用缩小范围。 |
| `ClientPolicy` | 依 MAC 地址分类客户端，通过 Linux nftables 实现访客隔离。 |
| `PortForward` | 添加单一目的地的 ingress DNAT 规则。routerd 同时管理防火墙表时，也会生成内部的 forward accept。可选的 hairpin 模式下，也会生成 LAN 侧的 DNAT 与返回路径的 SNAT。 |
| `IngressService` | 添加与 `PortForward` 相同的 ingress DNAT。接受多个 backend、选择策略及健康检查的意图，runtime 的故障转移状态由控制器路径处理。可选的 hairpin 模式与 `PortForward` 相同。 |
| `LocalServiceRedirect` | 将明确指向 `IPAddressSet` 的通信重定向至本地服务。防火墙的生成器也会生成从来源区域到对应本地输入端口的开口。 |

有状态的过滤规则生成于 nftables 的 `inet routerd_filter` 表。
已建立的通信、loopback、必要的 ICMPv6 始终允许。
DHCP、DNS、DS-Lite 等所需的开口由 routerd 内部生成。

`ClientPolicy` 在 `mode: include` 下的行为是「将列表中的 MAC 地址视为访客」。
`mode: exclude` 下的行为是「将列表中的 MAC 地址视为 trusted，对象接口上的其余设备视为访客」。
`spec.macs` 是简写形式。`classification[]` 是结构化形式，每个条目具有
`mode: trusted|guest|isolated` 以及 `match.macs`、`match.ouiPrefixes`、
`match.hostnamePatterns`、`match.dhcpFingerprints` 选择器。
match 字段以 OR 评估。`ipv4Reservation` 亦可用于在无法直接匹配 Ethernet 来源地址的平台上，稳定以地址为基础的生成。
`spec.isolation` 可表达典型访客的意图，例如允许互联网、拒绝 LAN/mgmt、拒绝 mDNS/SSDP/NetBIOS discovery。
FreeBSD pf 在 routed filter path 上不具备相同的 MAC 匹配模型，因此此资源在 FreeBSD 上视为不支持。

## 管理面（Management plane）

`ManagementAccess` 声明 routerd 必须保持可达的管理接口与管理来源 CIDR，
避免非 dry-run 的 `apply` 把运维人员自己锁出。当存在至少一个 `ManagementAccess` 时，
apply 前 preflight 会执行以下检查，未指定 `--allow-mgmt-lockout` 时**会中止 apply**。
`validate` / `plan` / `show` 不受影响，dry-run apply 仅显示 findings 不中止。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: ManagementAccess
metadata:
  name: home-mgmt
spec:
  interfaces: [mgmt0]
  allowSourceCIDRs:
    - 192.168.100.0/24
    - fd00:100::/64
  requireWebConsoleBound: true  # 默认
```

Preflight 检查内容：

| 检查 | 失败条件 |
| --- | --- |
| 接口存在 | `interfaces[]` 中声明的 IF 在 `Interface` 资源中不存在（管理接口被删除或改名）。 |
| firewall self-access | 存在至少一个 `FirewallZone`（firewall 启用），但声明的管理 IF 未归属于 role `mgmt` / `trust` 的 `FirewallZone` — input 链的 `policy drop` 会切断对路由器自身的 SSH。 |
| WebConsole 绑定 | `WebConsole` 启用且绑定到 `0.0.0.0` / `::`。`requireWebConsoleBound: true`（默认）为 fail，false 为 warn。 |

相同检查也可在 `routerctl doctor mgmt` 中执行（不会 apply）。

`spec.allowSourceCIDRs` 当前是**信息性**字段（用于 status 与 doctor 显示），尚未由 firewall guard 强制执行。

`--allow-mgmt-lockout` 是**紧急覆盖**旗标。例如将管理接口迁移到新 VLAN，需要在准备好 PVE console 等恢复路径的情况下，故意应用会被阻止的配置时使用。日常运维不需要它。

## 名称变更要点

Phase 1.6 中进行了以下名称整理：

| 旧名称 | 现在的名称 |
| --- | --- |
| `IPv4DHCPServer` | `DHCPv4Server` |
| `IPv4DHCPReservation` | `DHCPv4Reservation` |
| `IPv4DHCPScope` | `DHCPv4Server` |
| `IPv6DHCPAddress` | `DHCPv6Address` |
| `IPv6PrefixDelegation` | `DHCPv6PrefixDelegation` |
| `IPv6DHCPServer` / `IPv6DHCPv6Server` | `DHCPv6Server` |
| `IPv6DHCPScope` | `DHCPv6Server` |
| `DHCPRelay` | `DHCPv4Relay` |

二进制文件名称也已更新为 `routerd-dhcpv4-client`、`routerd-dhcpv6-client`。
