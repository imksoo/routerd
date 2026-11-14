# 各資源的設定

資源寫在Router.spec.resources內。範例的名稱、介面、位址、憑證及對端設定必須依環境調整。必填表示在所屬物件內必須指定。跨資源參照與語意限制還須以routerd validate確認。

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

註冊實作routerd外掛協定的執行檔，由明確操作或EventSubscription等使用端呼叫。


## DynamicConfigSource

[DynamicConfigSource](resources/DynamicConfigSource.md)

宣告以外掛取得的動態設定來源。目前有型別解碼與驗證，但沒有讀取DynamicConfigSourceSpec的runtime處理，宣告本身不會執行外掛。


## DynamicOverridePolicy

[DynamicOverridePolicy](resources/DynamicOverridePolicy.md)

授權指定動態來源在動態部分有效期間遮蔽特定啟動設定資源，不改寫已儲存的啟動設定。


## LogSink

[LogSink](resources/LogSink.md)

依type選擇操作事件記錄的輸出目的地，與ObservabilityPipeline內的記錄輸出設定不同。


## Telemetry

[Telemetry](resources/Telemetry.md)

為產生的受管服務設定OpenTelemetry環境變數，不定義LogSink或獨立collector程序。


## ObservabilityPipeline

[ObservabilityPipeline](resources/ObservabilityPipeline.md)

結合受管服務的OpenTelemetry環境宣告與獨立內建操作事件記錄consumer。


## LogRetention

[LogRetention](resources/LogRetention.md)

清除操作記錄的SQLite資料列，並可在刪除前轉送保存；不管理任意OS記錄檔。


## Sysctl

[Sysctl](resources/Sysctl.md)

協調一項核心sysctl，並可寫入持久sysctl.d檔案；沒有指向其他routerd資源的參照欄位。


## SysctlProfile

[SysctlProfile](resources/SysctlProfile.md)

依名稱套用路由器用sysctl集合，並可逐鍵覆寫；不是依名稱參照Sysctl資源。


## Package

[Package](resources/Package.md)

確保所選OS套件已安裝，選取第一個os與執行主機完全相符的套件集合。


## NTPClient

[NTPClient](resources/NTPClient.md)

管理或觀察向上游同步時間的用戶端。


## NTPServer

[NTPServer](resources/NTPServer.md)

使用chrony或FreeBSD ntpd設定時間伺服器與上游。


## WebConsole

[WebConsole](resources/WebConsole.md)

由routerd提供唯讀Web主控台，HTTP監聽與可變更設定的ControlAPI不同。


## ControlAPI

[ControlAPI](resources/ControlAPI.md)

啟用可讀狀態及變更設定的HTTP Control API，與Unix socket控制及WebConsole分開。


## RouterdCluster

[RouterdCluster](resources/RouterdCluster.md)

透過檔案租約與OS檔案鎖協調哪個routerd可以變更狀態，不是VRRP選舉或對端網路探索。


## Interface

[Interface](resources/Interface.md)

為既有OS介面提供metadata.name邏輯名稱，供其他資源參照，不建立實體介面。


## PPPoESession

[PPPoESession](resources/PPPoESession.md)


## WireGuardInterface

[WireGuardInterface](resources/WireGuardInterface.md)


## WireGuardPeer

[WireGuardPeer](resources/WireGuardPeer.md)


## TailscaleNode

[TailscaleNode](resources/TailscaleNode.md)


## IPsecConnection

[IPsecConnection](resources/IPsecConnection.md)


## VRF

[VRF](resources/VRF.md)

宣告Linux VRF裝置、路由表與成員意圖。目前產生計畫/artifact識別並保護路由表，但沒有建立VRF/連接成員的controller；計畫輸出不證明VRF已建立。


## VXLANTunnel

[VXLANTunnel](resources/VXLANTunnel.md)


## IPv4StaticAddress

[IPv4StaticAddress](resources/IPv4StaticAddress.md)

在邏輯介面宣告靜態IPv4位址，由controller協調設定，不是DHCP租約。


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

執行受管routerd-dhcpv4-client daemon，套用租約位址，以及選用的預設路由與主機DNS設定。


## ClusterNetworkRoute

[ClusterNetworkRoute](resources/ClusterNetworkRoute.md)


## DHCPv4Server

[DHCPv4Server](resources/DHCPv4Server.md)


## DHCPv4Reservation

[DHCPv4Reservation](resources/DHCPv4Reservation.md)


## DHCPv6Address

[DHCPv6Address](resources/DHCPv6Address.md)

宣告主機DHCPv6 client取得的IPv6位址，與DHCPv6PrefixDelegation不同，不分配LAN前綴。


## IPv6RAAddress

[IPv6RAAddress](resources/IPv6RAAddress.md)

宣告主機接收IPv6 RA/SLAAC，屬於RA client，不是向LAN公告的IPv6RouterAdvertisement。


## DHCPv6PrefixDelegation

[DHCPv6PrefixDelegation](resources/DHCPv6PrefixDelegation.md)


## IPv6DelegatedAddress

[IPv6DelegatedAddress](resources/IPv6DelegatedAddress.md)


## DHCPv6Information

[DHCPv6Information](resources/DHCPv6Information.md)

從使用同介面的DHCPv6-PD daemon取得AFTR、DNS、SNTP與搜尋網域資訊，不啟動獨立Information-request client。


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


## DNSResolver

[DNSResolver](resources/DNSResolver.md)


## DNSForwarder

[DNSForwarder](resources/DNSForwarder.md)


## DNSUpstream

[DNSUpstream](resources/DNSUpstream.md)


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


## EventPeer

[EventPeer](resources/EventPeer.md)


## EventSubscription

[EventSubscription](resources/EventSubscription.md)


## FederationSLO

[FederationSLO](resources/FederationSLO.md)


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


## HealthCheck

[HealthCheck](resources/HealthCheck.md)


## EgressRoutePolicy

[EgressRoutePolicy](resources/EgressRoutePolicy.md)


## EventRule

[EventRule](resources/EventRule.md)


## DerivedEvent

[DerivedEvent](resources/DerivedEvent.md)


## NAT44Rule

[NAT44Rule](resources/NAT44Rule.md)


## NAT44FlowDNATPinhole

[NAT44FlowDNATPinhole](resources/NAT44FlowDNATPinhole.md)


## NAT44SessionSync

[NAT44SessionSync](resources/NAT44SessionSync.md)


## ManagementAccess

[ManagementAccess](resources/ManagementAccess.md)


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


## Hostname

[Hostname](resources/Hostname.md)

管理OS主機名稱，與Router.metadata.name及資源名稱不同。
