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

監督執行 routerd-pppoe-client，在 Linux 使用 pppd、FreeBSD 使用 mpd5，並公開協商的 IPv4 工作階段狀態。部分舊欄位僅影響手動 FreeBSD render 輸出，不影響此精靈路徑。


## WireGuardInterface

[WireGuardInterface](resources/WireGuardInterface.md)

建立或設定 WireGuard 裝置，透過 wg setconf 套用完整對等端設定。位址指派與一般路由由其他資源設定。


## WireGuardPeer

[WireGuardPeer](resources/WireGuardPeer.md)

定義已宣告 WireGuardInterface 上的對等端。AllowedIPs 設定 WireGuard 的加密路由；此控制器不會自動建立一般 IP 路由。


## TailscaleNode

[TailscaleNode](resources/TailscaleNode.md)

透過 routerd 產生的 Linux systemd oneshot 或 FreeBSD rc.d 指令稿管理 Tailscale 註冊選項，再讀取 tailscale status --json。Tailscale/Headscale 伺服器端核准仍需另行處理。


## IPsecConnection

[IPsecConnection](resources/IPsecConnection.md)

產生 strongSwan swanctl 連線，並一併同步所有 routerd IPsec 連線。CHILD_SA 設為流量觸發的 trap；載入設定不代表 SA 已建立。


## VRF

[VRF](resources/VRF.md)

宣告Linux VRF裝置、路由表與成員意圖。目前產生計畫/artifact識別並保護路由表，但沒有建立VRF/連接成員的controller；計畫輸出不證明VRF已建立。


## VXLANTunnel

[VXLANTunnel](resources/VXLANTunnel.md)

管理具有靜態泛洪目的地及選用橋接的 Linux VXLAN 裝置，不設定多播探索或動態 MAC 學習。


## IPv4StaticAddress

[IPv4StaticAddress](resources/IPv4StaticAddress.md)

在邏輯介面宣告靜態IPv4位址，由controller協調設定，不是DHCP租約。


## VirtualAddress

[VirtualAddress](resources/VirtualAddress.md)


## BGPRouter

[BGPRouter](resources/BGPRouter.md)

設定本機 GoBGP 執行個體、接納路由及本機起源廣告。核心安裝與遠端可達性不同於 BGP Established 狀態。


## BGPPeer

[BGPPeer](resources/BGPPeer.md)

在已宣告的 BGPRouter 設定一個或多個靜態 BGP 鄰居。


## BGPDynamicPeer

[BGPDynamicPeer](resources/BGPDynamicPeer.md)

定義 GoBGP 傳入動態鄰居接納群組，不是個別對等端或 WireGuard/SAM 授權身分。


## BFD

[BFD](resources/BFD.md)

透過 vtysh 設定及觀察 FRR bfdd 工作階段，供 BGPPeer 故障處理使用。與 GoBGP 為不同後端，BFD Down 可觸發 BGP 對等端重設。


## DHCPv4Client

[DHCPv4Client](resources/DHCPv4Client.md)

執行受管routerd-dhcpv4-client daemon，套用租約位址，以及選用的預設路由與主機DNS設定。


## ClusterNetworkRoute

[ClusterNetworkRoute](resources/ClusterNetworkRoute.md)

透過明確指定的下一跳，設定前往 Kubernetes Pod 與 Service 網路的 IPv4 路由。不會自動探索叢集 CIDR 或節點。


## DHCPv4Server

[DHCPv4Server](resources/DHCPv4Server.md)

透過目前共用 dnsmasq 服務提供 IPv4 DHCP 集區、帶選擇條件的範圍與保留。


## DHCPv4Reservation

[DHCPv4Reservation](resources/DHCPv4Reservation.md)

在已宣告的 DHCPv4Server 為一個 MAC 位址保留 IPv4 位址。


## DHCPv6Address

[DHCPv6Address](resources/DHCPv6Address.md)

宣告主機DHCPv6 client取得的IPv6位址，與DHCPv6PrefixDelegation不同，不分配LAN前綴。


## IPv6RAAddress

[IPv6RAAddress](resources/IPv6RAAddress.md)

宣告主機接收IPv6 RA/SLAAC，屬於RA client，不是向LAN公告的IPv6RouterAdvertisement。


## DHCPv6PrefixDelegation

[DHCPv6PrefixDelegation](resources/DHCPv6PrefixDelegation.md)

在上行介面取得 IPv6 委派前綴。下游或主機位址須以 IPv6DelegatedAddress 衍生並套用，僅取得前綴不會向客戶端公告。


## IPv6DelegatedAddress

[IPv6DelegatedAddress](resources/IPv6DelegatedAddress.md)

從 DHCPv6PrefixDelegation 前綴衍生並安裝本機 IPv6 位址。


## DHCPv6Information

[DHCPv6Information](resources/DHCPv6Information.md)

從使用同介面的DHCPv6-PD daemon取得AFTR、DNS、SNTP與搜尋網域資訊，不啟動獨立Information-request client。


## IPv6RouterAdvertisement

[IPv6RouterAdvertisement](resources/IPv6RouterAdvertisement.md)

設定 LAN 的 IPv6 路由器公告選項，並棄用先前委派的前綴。


## DHCPv6Server

[DHCPv6Server](resources/DHCPv6Server.md)


## DHCPv4ServerLeaseSync

[DHCPv4ServerLeaseSync](resources/DHCPv4ServerLeaseSync.md)

以 SSH 上的 rsync 將 DHCPv4 伺服器租約檔與現有固定保留狀態複製到對等主機，並非 DHCP 容錯移轉協定。


## DHCPv6ServerLeaseSync

[DHCPv6ServerLeaseSync](resources/DHCPv6ServerLeaseSync.md)


## DHCPv6PrefixDelegationLeaseSync

[DHCPv6PrefixDelegationLeaseSync](resources/DHCPv6PrefixDelegationLeaseSync.md)


## DHCPv4Relay

[DHCPv4Relay](resources/DHCPv4Relay.md)

透過 dnsmasq 將 DHCPv4 請求轉送至明確指定的一個 IPv4 伺服器。


## SelfAddressPolicy

[SelfAddressPolicy](resources/SelfAddressPolicy.md)


## DNSZone

[DNSZone](resources/DNSZone.md)

透過 DNSForwarder.zoneRefs 提供本機 A/AAAA 記錄及自動 PTR 回應；不是支援任意記錄類型、區域傳送或簽署的通用權威 DNS。


## DNSResolver

[DNSResolver](resources/DNSResolver.md)

具有 UDP 與 TCP 接聽端點、連接 DNSForwarder 規則的本機 DNS 服務。


## DNSForwarder

[DNSForwarder](resources/DNSForwarder.md)

依查詢名稱替一個 DNSResolver 選擇本機區域或上游 DNS 的規則。


## DNSUpstream

[DNSUpstream](resources/DNSUpstream.md)

供 DNSForwarder 參照的 DNS 伺服器端點；僅宣告不會啟動接聽或轉送查詢。


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

宣告共用的聯邦事件群組，以及此路由器的識別、對等節點來源、接收與本機保留設定。


## EventPeer

[EventPeer](resources/EventPeer.md)

宣告 EventGroup 的一個遠端傳送目標。傳輸驗證與本機識別由群組提供。


## EventSubscription

[EventSubscription](resources/EventSubscription.md)

從持久化資料庫比對尚未到期的聯邦事件，呼叫本機 Plugin，將結果儲存為 DynamicConfigPart。


## FederationSLO

[FederationSLO](resources/FederationSLO.md)

設定 routerctl doctor 診斷聯邦配送與訂閱時使用的每群組門檻，不改變配送重試或 Plugin 執行策略。


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

安裝 IPv4 單播或黑洞路由，可從資源狀態取得裝置與閘道。


## HealthCheck

[HealthCheck](resources/HealthCheck.md)

探測 IP 目標並發布 Healthy/Unhealthy 狀態供路由選擇。成功僅代表設定的協定與路徑通過。


## EgressRoutePolicy

[EgressRoutePolicy](resources/EgressRoutePolicy.md)


## EventRule

[EventRule](resources/EventRule.md)

從持久化的本機事件紀錄比對模式並產生事件，可依關聯鍵分開維護狀態。


## DerivedEvent

[DerivedEvent](resources/DerivedEvent.md)

資源狀態條件改變時，發送成立或撤回事件。與 EventRule 不同，此處評估目前狀態而非事件序列。


## NAT44Rule

[NAT44Rule](resources/NAT44Rule.md)


## NAT44FlowDNATPinhole

[NAT44FlowDNATPinhole](resources/NAT44FlowDNATPinhole.md)


## NAT44SessionSync

[NAT44SessionSync](resources/NAT44SessionSync.md)


## ManagementAccess

[ManagementAccess](resources/ManagementAccess.md)

宣告套用前管理連線中斷檢查所檢查的介面。此資源不建立管理接聽端或防火牆允許規則。


## PortForward

[PortForward](resources/PortForward.md)


## IngressService

[IngressService](resources/IngressService.md)


## IPAddressSet

[IPAddressSet](resources/IPAddressSet.md)

在支援的 nftables NAT、路由及防火牆規則間共用固定與 DNS 解析的 IP 位址。不建立路由或啟動 DNS 服務。


## LocalServiceRedirect

[LocalServiceRedirect](resources/LocalServiceRedirect.md)

以 nftables prerouting NAT 將指定介面收到的符合流量重新導向路由器本機服務連接埠。


## FirewallFlowPinhole

[FirewallFlowPinhole](resources/FirewallFlowPinhole.md)


## TrafficFlowLog

[TrafficFlowLog](resources/TrafficFlowLog.md)

將連線快照記錄至 SQLite 流量資料庫，並可從 DPI 快取補充資訊。不是封包擷取，也不記錄每個封包。


## FirewallZone

[FirewallZone](resources/FirewallZone.md)

將已設定介面歸入具名信任角色，用於產生防火牆規則。不建立 OS 介面或獨立路由、橋接網域。


## FirewallPolicy

[FirewallPolicy](resources/FirewallPolicy.md)

設定與區域及明確規則搭配的共用隱含防火牆選項。多個資源會彙總，不是獨立套用。


## ClientPolicy

[ClientPolicy](resources/ClientPolicy.md)

對選定或非例外用戶端套用訪客服務與路由目的限制。Linux 以設定的 MAC 識別，FreeBSD PF 需要明確 IP 身分。


## FirewallRule

[FirewallRule](resources/FirewallRule.md)

宣告區域路徑的具名允許、丟棄或拒絕規則，支援位址、協定、連接埠比對及 Linux 速率/連線條件。


## FirewallEventLog

[FirewallEventLog](resources/FirewallEventLog.md)

啟用防火牆封包紀錄收集器，設定資料庫、Linux NFLOG 群組與規則產生時的紀錄選項。部分接受的篩選與封存欄位目前沒有執行期使用者。


## Hostname

[Hostname](resources/Hostname.md)

管理OS主機名稱，與Router.metadata.name及資源名稱不同。
