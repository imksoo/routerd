# 各资源的配置

资源写在Router.spec.resources内。示例中的名称、接口、地址、凭据及对端配置必须按环境调整。必填表示在所属对象内必须指定。跨资源引用与语义限制还须通过routerd validate确认。

[Kind/name · when · dependsOn](common-fields.md)

```sh
routerd validate --config router.yaml
```

[JSON Schema](https://github.com/imksoo/routerd/blob/main/schemas/routerd-config-v1alpha1.schema.json) · [YAML](https://github.com/imksoo/routerd/tree/main/examples)

### Router

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: "<router-name>"
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: "<name>"
      spec:
        ifname: "<interface-name>"
```

- [Plugin](resources/Plugin.md)
- [DynamicConfigSource](resources/DynamicConfigSource.md)
- [DynamicOverridePolicy](resources/DynamicOverridePolicy.md)
- [LogSink](resources/LogSink.md)
- [Telemetry](resources/Telemetry.md)
- [ObservabilityPipeline](resources/ObservabilityPipeline.md)
- [LogRetention](resources/LogRetention.md)
- [Sysctl](resources/Sysctl.md)
- [SysctlProfile](resources/SysctlProfile.md)
- [Package](resources/Package.md)
- [NTPClient](resources/NTPClient.md)
- [NTPServer](resources/NTPServer.md)
- [WebConsole](resources/WebConsole.md)
- [ControlAPI](resources/ControlAPI.md)
- [RouterdCluster](resources/RouterdCluster.md)
- [Interface](resources/Interface.md)
- [PPPoESession](resources/PPPoESession.md)
- [WireGuardInterface](resources/WireGuardInterface.md)
- [WireGuardPeer](resources/WireGuardPeer.md)
- [TailscaleNode](resources/TailscaleNode.md)
- [IPsecConnection](resources/IPsecConnection.md)
- [VRF](resources/VRF.md)
- [VXLANTunnel](resources/VXLANTunnel.md)
- [IPv4StaticAddress](resources/IPv4StaticAddress.md)
- [VirtualAddress](resources/VirtualAddress.md)
- [BGPRouter](resources/BGPRouter.md)
- [BGPPeer](resources/BGPPeer.md)
- [BGPDynamicPeer](resources/BGPDynamicPeer.md)
- [BFD](resources/BFD.md)
- [DHCPv4Client](resources/DHCPv4Client.md)
- [ClusterNetworkRoute](resources/ClusterNetworkRoute.md)
- [DHCPv4Server](resources/DHCPv4Server.md)
- [DHCPv4Reservation](resources/DHCPv4Reservation.md)
- [DHCPv6Address](resources/DHCPv6Address.md)
- [IPv6RAAddress](resources/IPv6RAAddress.md)
- [DHCPv6PrefixDelegation](resources/DHCPv6PrefixDelegation.md)
- [IPv6DelegatedAddress](resources/IPv6DelegatedAddress.md)
- [DHCPv6Information](resources/DHCPv6Information.md)
- [IPv6RouterAdvertisement](resources/IPv6RouterAdvertisement.md)
- [DHCPv6Server](resources/DHCPv6Server.md)
- [DHCPv4ServerLeaseSync](resources/DHCPv4ServerLeaseSync.md)
- [DHCPv6ServerLeaseSync](resources/DHCPv6ServerLeaseSync.md)
- [DHCPv6PrefixDelegationLeaseSync](resources/DHCPv6PrefixDelegationLeaseSync.md)
- [DHCPv4Relay](resources/DHCPv4Relay.md)
- [SelfAddressPolicy](resources/SelfAddressPolicy.md)
- [DNSZone](resources/DNSZone.md)
- [DNSResolver](resources/DNSResolver.md)
- [DNSForwarder](resources/DNSForwarder.md)
- [DNSUpstream](resources/DNSUpstream.md)
- [DSLiteTunnel](resources/DSLiteTunnel.md)
- [TunnelInterface](resources/TunnelInterface.md)
- [OverlayPeer](resources/OverlayPeer.md)
- [HybridRoute](resources/HybridRoute.md)
- [CloudProviderProfile](resources/CloudProviderProfile.md)
- [ProviderActionPolicy](resources/ProviderActionPolicy.md)
- [EventGroup](resources/EventGroup.md)
- [EventPeer](resources/EventPeer.md)
- [EventSubscription](resources/EventSubscription.md)
- [FederationSLO](resources/FederationSLO.md)
- [MobilityPool](resources/MobilityPool.md)
- [SAMNodeSet](resources/SAMNodeSet.md)
- [SAMEnrollmentPolicy](resources/SAMEnrollmentPolicy.md)
- [SAMEnrollmentClaim](resources/SAMEnrollmentClaim.md)
- [SAMEnrollmentClient](resources/SAMEnrollmentClient.md)
- [SAMTransportProfile](resources/SAMTransportProfile.md)
- [SAMSubnetPolicy](resources/SAMSubnetPolicy.md)
- [IPv4Route](resources/IPv4Route.md)
- [HealthCheck](resources/HealthCheck.md)
- [EgressRoutePolicy](resources/EgressRoutePolicy.md)
- [EventRule](resources/EventRule.md)
- [DerivedEvent](resources/DerivedEvent.md)
- [NAT44Rule](resources/NAT44Rule.md)
- [NAT44FlowDNATPinhole](resources/NAT44FlowDNATPinhole.md)
- [NAT44SessionSync](resources/NAT44SessionSync.md)
- [ManagementAccess](resources/ManagementAccess.md)
- [PortForward](resources/PortForward.md)
- [IngressService](resources/IngressService.md)
- [IPAddressSet](resources/IPAddressSet.md)
- [LocalServiceRedirect](resources/LocalServiceRedirect.md)
- [FirewallFlowPinhole](resources/FirewallFlowPinhole.md)
- [TrafficFlowLog](resources/TrafficFlowLog.md)
- [FirewallZone](resources/FirewallZone.md)
- [FirewallPolicy](resources/FirewallPolicy.md)
- [ClientPolicy](resources/ClientPolicy.md)
- [FirewallRule](resources/FirewallRule.md)
- [FirewallEventLog](resources/FirewallEventLog.md)
- [Hostname](resources/Hostname.md)

## Plugin

[Plugin](resources/Plugin.md)

注册实现routerd插件协议的可执行文件，由显式操作或EventSubscription等使用方调用。


## DynamicConfigSource

[DynamicConfigSource](resources/DynamicConfigSource.md)

声明通过插件获取的动态配置来源。当前有类型解码与验证，但没有读取DynamicConfigSourceSpec的runtime处理，声明本身不会执行插件。


## DynamicOverridePolicy

[DynamicOverridePolicy](resources/DynamicOverridePolicy.md)

授权指定动态来源在动态部分有效期间屏蔽特定启动配置资源，不改写已保存的启动配置。


## LogSink

[LogSink](resources/LogSink.md)

按type选择运维事件日志的输出目的地，与ObservabilityPipeline内的日志输出配置不同。


## Telemetry

[Telemetry](resources/Telemetry.md)

为生成的受管服务设置OpenTelemetry环境变量，不定义LogSink或独立collector进程。


## ObservabilityPipeline

[ObservabilityPipeline](resources/ObservabilityPipeline.md)

结合受管服务的OpenTelemetry环境声明与独立内置运维事件日志consumer。


## LogRetention

[LogRetention](resources/LogRetention.md)

清理运维日志的SQLite记录，并可在删除前转发保存；不管理任意OS日志文件。


## Sysctl

[Sysctl](resources/Sysctl.md)

协调一项内核sysctl，并可写入持久sysctl.d文件；没有指向其他routerd资源的引用字段。


## SysctlProfile

[SysctlProfile](resources/SysctlProfile.md)

按名称应用路由器用sysctl集合，并可逐键覆盖；不是按名称引用Sysctl资源。


## Package

[Package](resources/Package.md)

确保所选OS软件包已安装，选择第一个os与运行主机完全匹配的软件包集合。


## NTPClient

[NTPClient](resources/NTPClient.md)

管理或观察向上游同步时间的客户端。


## NTPServer

[NTPServer](resources/NTPServer.md)

使用chrony或FreeBSD ntpd配置时间服务器与上游。


## WebConsole

[WebConsole](resources/WebConsole.md)

由routerd提供只读Web控制台，HTTP监听与可变更配置的ControlAPI不同。


## ControlAPI

[ControlAPI](resources/ControlAPI.md)

启用可读状态及变更配置的HTTP Control API，与Unix socket控制及WebConsole分开。


## RouterdCluster

[RouterdCluster](resources/RouterdCluster.md)

通过文件租约与OS文件锁协调哪个routerd可以变更状态，不是VRRP选举或对端网络发现。


## Interface

[Interface](resources/Interface.md)

为现有OS接口提供metadata.name逻辑名称，供其他资源引用，不创建物理接口。


## PPPoESession

[PPPoESession](resources/PPPoESession.md)

监督运行 routerd-pppoe-client，在 Linux 使用 pppd、FreeBSD 使用 mpd5，并公开协商的 IPv4 会话状态。部分旧字段仅影响手动 FreeBSD render 输出，不影响此守护进程路径。


## WireGuardInterface

[WireGuardInterface](resources/WireGuardInterface.md)

创建或配置 WireGuard 设备，通过 wg setconf 应用完整对等端配置。地址分配与普通路由由其他资源配置。


## WireGuardPeer

[WireGuardPeer](resources/WireGuardPeer.md)

定义已声明 WireGuardInterface 上的对等端。AllowedIPs 配置 WireGuard 的加密路由；此控制器不会自动创建普通 IP 路由。


## TailscaleNode

[TailscaleNode](resources/TailscaleNode.md)

通过 routerd 生成的 Linux systemd oneshot 或 FreeBSD rc.d 脚本管理 Tailscale 注册选项，再读取 tailscale status --json。Tailscale/Headscale 服务端批准仍需另行处理。


## IPsecConnection

[IPsecConnection](resources/IPsecConnection.md)

生成 strongSwan swanctl 连接，并统一同步所有 routerd IPsec 连接。CHILD_SA 设为流量触发的 trap；加载配置不代表 SA 已建立。


## VRF

[VRF](resources/VRF.md)

声明Linux VRF设备、路由表与成员意图。当前生成计划/artifact标识并保护路由表，但没有创建VRF/连接成员的controller；计划输出不证明VRF已创建。


## VXLANTunnel

[VXLANTunnel](resources/VXLANTunnel.md)

管理具有静态泛洪目标及可选桥接的 Linux VXLAN 设备，不配置组播发现或动态 MAC 学习。


## IPv4StaticAddress

[IPv4StaticAddress](resources/IPv4StaticAddress.md)

在逻辑接口声明静态IPv4地址，由controller协调配置，不是DHCP租约。


## VirtualAddress

[VirtualAddress](resources/VirtualAddress.md)


## BGPRouter

[BGPRouter](resources/BGPRouter.md)


## BGPPeer

[BGPPeer](resources/BGPPeer.md)


## BGPDynamicPeer

[BGPDynamicPeer](resources/BGPDynamicPeer.md)


## BFD

[BFD](resources/BFD.md)


## DHCPv4Client

[DHCPv4Client](resources/DHCPv4Client.md)

运行受管routerd-dhcpv4-client daemon，应用租约地址，以及可选的默认路由与主机DNS配置。


## ClusterNetworkRoute

[ClusterNetworkRoute](resources/ClusterNetworkRoute.md)

通过明确指定的下一跳，配置前往 Kubernetes Pod 与 Service 网络的 IPv4 路由。不会自动发现集群 CIDR 或节点。


## DHCPv4Server

[DHCPv4Server](resources/DHCPv4Server.md)


## DHCPv4Reservation

[DHCPv4Reservation](resources/DHCPv4Reservation.md)


## DHCPv6Address

[DHCPv6Address](resources/DHCPv6Address.md)

声明主机DHCPv6 client获取的IPv6地址，与DHCPv6PrefixDelegation不同，不分配LAN前缀。


## IPv6RAAddress

[IPv6RAAddress](resources/IPv6RAAddress.md)

声明主机接收IPv6 RA/SLAAC，属于RA client，不是向LAN通告的IPv6RouterAdvertisement。


## DHCPv6PrefixDelegation

[DHCPv6PrefixDelegation](resources/DHCPv6PrefixDelegation.md)


## IPv6DelegatedAddress

[IPv6DelegatedAddress](resources/IPv6DelegatedAddress.md)


## DHCPv6Information

[DHCPv6Information](resources/DHCPv6Information.md)

从使用同接口的DHCPv6-PD daemon获取AFTR、DNS、SNTP与搜索域信息，不启动独立Information-request client。


## IPv6RouterAdvertisement

[IPv6RouterAdvertisement](resources/IPv6RouterAdvertisement.md)


## DHCPv6Server

[DHCPv6Server](resources/DHCPv6Server.md)


## DHCPv4ServerLeaseSync

[DHCPv4ServerLeaseSync](resources/DHCPv4ServerLeaseSync.md)


## DHCPv6ServerLeaseSync

[DHCPv6ServerLeaseSync](resources/DHCPv6ServerLeaseSync.md)


## DHCPv6PrefixDelegationLeaseSync

[DHCPv6PrefixDelegationLeaseSync](resources/DHCPv6PrefixDelegationLeaseSync.md)


## DHCPv4Relay

[DHCPv4Relay](resources/DHCPv4Relay.md)


## SelfAddressPolicy

[SelfAddressPolicy](resources/SelfAddressPolicy.md)


## DNSZone

[DNSZone](resources/DNSZone.md)

通过 DNSForwarder.zoneRefs 提供本地 A/AAAA 记录及自动 PTR 响应；不是支持任意记录类型、区域传送或签名的通用权威 DNS。


## DNSResolver

[DNSResolver](resources/DNSResolver.md)

具有 UDP 与 TCP 监听端点、连接 DNSForwarder 规则的本机 DNS 服务。


## DNSForwarder

[DNSForwarder](resources/DNSForwarder.md)

按查询名称为一个 DNSResolver 选择本地区域或上游 DNS 的规则。


## DNSUpstream

[DNSUpstream](resources/DNSUpstream.md)

供 DNSForwarder 引用的 DNS 服务器端点；仅声明不会启动监听或转发查询。


## DSLiteTunnel

[DSLiteTunnel](resources/DSLiteTunnel.md)


## TunnelInterface

[TunnelInterface](resources/TunnelInterface.md)


## OverlayPeer

[OverlayPeer](resources/OverlayPeer.md)


## HybridRoute

[HybridRoute](resources/HybridRoute.md)


## CloudProviderProfile

[CloudProviderProfile](resources/CloudProviderProfile.md)


## ProviderActionPolicy

[ProviderActionPolicy](resources/ProviderActionPolicy.md)


## EventGroup

[EventGroup](resources/EventGroup.md)

声明共享的联合事件组，以及此路由器的标识、对等节点来源、接收与本机保留设置。


## EventPeer

[EventPeer](resources/EventPeer.md)

声明 EventGroup 的一个远端发送目标。传输认证与本机标识由组提供。


## EventSubscription

[EventSubscription](resources/EventSubscription.md)

从持久化数据库匹配尚未过期的联合事件，调用本机 Plugin，将结果保存为 DynamicConfigPart。


## FederationSLO

[FederationSLO](resources/FederationSLO.md)

配置 routerctl doctor 诊断联合发送与订阅时使用的每组阈值，不改变发送重试或 Plugin 执行策略。


## MobilityPool

[MobilityPool](resources/MobilityPool.md)


## SAMNodeSet

[SAMNodeSet](resources/SAMNodeSet.md)


## SAMEnrollmentPolicy

[SAMEnrollmentPolicy](resources/SAMEnrollmentPolicy.md)


## SAMEnrollmentClaim

[SAMEnrollmentClaim](resources/SAMEnrollmentClaim.md)


## SAMEnrollmentClient

[SAMEnrollmentClient](resources/SAMEnrollmentClient.md)


## SAMTransportProfile

[SAMTransportProfile](resources/SAMTransportProfile.md)


## SAMSubnetPolicy

[SAMSubnetPolicy](resources/SAMSubnetPolicy.md)


## IPv4Route

[IPv4Route](resources/IPv4Route.md)

安装 IPv4 单播或黑洞路由，可从资源状态取得设备与网关。


## HealthCheck

[HealthCheck](resources/HealthCheck.md)

探测 IP 目标并发布 Healthy/Unhealthy 状态供路由选择。成功仅代表配置的协议与路径通过。


## EgressRoutePolicy

[EgressRoutePolicy](resources/EgressRoutePolicy.md)


## EventRule

[EventRule](resources/EventRule.md)

从持久化的本机事件记录匹配模式并生成事件，可按关联键分别维护状态。


## DerivedEvent

[DerivedEvent](resources/DerivedEvent.md)

资源状态条件改变时，发送成立或撤回事件。与 EventRule 不同，此处评估当前状态而非事件序列。


## NAT44Rule

[NAT44Rule](resources/NAT44Rule.md)


## NAT44FlowDNATPinhole

[NAT44FlowDNATPinhole](resources/NAT44FlowDNATPinhole.md)


## NAT44SessionSync

[NAT44SessionSync](resources/NAT44SessionSync.md)


## ManagementAccess

[ManagementAccess](resources/ManagementAccess.md)

声明应用前管理连接中断检查所检查的接口。此资源不创建管理监听端或防火墙允许规则。


## PortForward

[PortForward](resources/PortForward.md)


## IngressService

[IngressService](resources/IngressService.md)


## IPAddressSet

[IPAddressSet](resources/IPAddressSet.md)


## LocalServiceRedirect

[LocalServiceRedirect](resources/LocalServiceRedirect.md)


## FirewallFlowPinhole

[FirewallFlowPinhole](resources/FirewallFlowPinhole.md)


## TrafficFlowLog

[TrafficFlowLog](resources/TrafficFlowLog.md)


## FirewallZone

[FirewallZone](resources/FirewallZone.md)


## FirewallPolicy

[FirewallPolicy](resources/FirewallPolicy.md)


## ClientPolicy

[ClientPolicy](resources/ClientPolicy.md)


## FirewallRule

[FirewallRule](resources/FirewallRule.md)


## FirewallEventLog

[FirewallEventLog](resources/FirewallEventLog.md)

启用防火墙数据包日志收集器，配置数据库、Linux NFLOG 组与规则生成时的日志选项。部分接受的筛选与归档字段当前没有运行时使用者。


## Hostname

[Hostname](resources/Hostname.md)

管理OS主机名，与Router.metadata.name及资源名不同。
