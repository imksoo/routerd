# リソース別の設定

各リソースはRouter.spec.resourcesに記述します。例の名前・インターフェース・アドレス・認証情報・対向設定は環境に合わせて変更してください。必須は親オブジェクト内での指定要否です。リソース間参照や意味上の制約はスキーマだけでは確定せず、routerd validateで確認します。

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

routerdのプラグインプロトコルで実行する外部プログラムを登録します。明示的な実行、またはEventSubscriptionなどの利用側から呼び出します。


## DynamicConfigSource

[DynamicConfigSource](resources/DynamicConfigSource.md)

プラグインを使う動的設定取得元の宣言です。現行実装には型の読込みと検証はありますが、DynamicConfigSourceSpecを消費するruntime処理はなく、宣言だけではプラグインを実行しません。


## DynamicOverridePolicy

[DynamicOverridePolicy](resources/DynamicOverridePolicy.md)

動的設定の有効期間中、指定した取得元が起動設定の特定リソースをmaskできるよう許可します。保存済みの起動設定自体は書き換えません。


## LogSink

[LogSink](resources/LogSink.md)

typeで選ぶ運用イベントログの出力先です。ObservabilityPipeline内のログ出力先とは別の設定です。


## Telemetry

[Telemetry](resources/Telemetry.md)


## ObservabilityPipeline

[ObservabilityPipeline](resources/ObservabilityPipeline.md)


## LogRetention

[LogRetention](resources/LogRetention.md)

運用ログのSQLiteレコードを削除し、必要なら削除前に保管先へ転送します。任意のOSログファイルを整理する設定ではありません。


## Sysctl

[Sysctl](resources/Sysctl.md)

カーネルのsysctlを1項目反映し、必要ならsysctl.dへ永続設定を書きます。他のrouterdリソースへの参照項目はありません。


## SysctlProfile

[SysctlProfile](resources/SysctlProfile.md)

ルーター向けのsysctlセットを名前で選び、必要ならキー単位で上書きします。Sysctlリソースの名前を参照する設定ではありません。


## Package

[Package](resources/Package.md)


## NTPClient

[NTPClient](resources/NTPClient.md)


## NTPServer

[NTPServer](resources/NTPServer.md)


## WebConsole

[WebConsole](resources/WebConsole.md)


## ControlAPI

[ControlAPI](resources/ControlAPI.md)


## RouterdCluster

[RouterdCluster](resources/RouterdCluster.md)


## Interface

[Interface](resources/Interface.md)


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


## VXLANTunnel

[VXLANTunnel](resources/VXLANTunnel.md)


## IPv4StaticAddress

[IPv4StaticAddress](resources/IPv4StaticAddress.md)


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


## ClusterNetworkRoute

[ClusterNetworkRoute](resources/ClusterNetworkRoute.md)


## DHCPv4Server

[DHCPv4Server](resources/DHCPv4Server.md)


## DHCPv4Reservation

[DHCPv4Reservation](resources/DHCPv4Reservation.md)


## DHCPv6Address

[DHCPv6Address](resources/DHCPv6Address.md)


## IPv6RAAddress

[IPv6RAAddress](resources/IPv6RAAddress.md)


## DHCPv6PrefixDelegation

[DHCPv6PrefixDelegation](resources/DHCPv6PrefixDelegation.md)


## IPv6DelegatedAddress

[IPv6DelegatedAddress](resources/IPv6DelegatedAddress.md)


## DHCPv6Information

[DHCPv6Information](resources/DHCPv6Information.md)


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
