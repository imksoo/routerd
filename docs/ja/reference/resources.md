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

生成する管理サービスにOpenTelemetryの環境変数を設定します。LogSinkや独立したcollectorプロセスを定義するリソースではありません。


## ObservabilityPipeline

[ObservabilityPipeline](resources/ObservabilityPipeline.md)

管理サービス向けOpenTelemetry環境の宣言と、独立した組込み運用イベントログconsumerをまとめた設定です。


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

指定したOSパッケージの導入を確認します。稼働ホストのOSに完全一致する、最初のパッケージセットを選びます。


## NTPClient

[NTPClient](resources/NTPClient.md)

上流との時刻同期クライアントを管理、または観測します。


## NTPServer

[NTPServer](resources/NTPServer.md)

chronyまたはFreeBSD ntpdを使い、時刻サーバーと上流サーバーを設定します。


## WebConsole

[WebConsole](resources/WebConsole.md)

routerdから読取り用のWebコンソールを公開します。設定変更を行うControlAPIとは別のHTTP待受です。


## ControlAPI

[ControlAPI](resources/ControlAPI.md)

状態の取得と設定変更に使うHTTP Control APIを有効化します。Unixソケット経由の制御とWebConsoleは別です。


## RouterdCluster

[RouterdCluster](resources/RouterdCluster.md)

ファイルのリースとOSファイルロックで、どのrouterdが変更処理を行うかを調整します。VRRPの選出や対向ネットワークの自動探索ではありません。


## Interface

[Interface](resources/Interface.md)

既存のOSインターフェースに論理名metadata.nameを付け、他のリソースから参照できるようにします。物理インターフェースを新規作成する設定ではありません。


## PPPoESession

[PPPoESession](resources/PPPoESession.md)


## WireGuardInterface

[WireGuardInterface](resources/WireGuardInterface.md)

WireGuardデバイスを作成・設定し、wg setconfでピア設定全体を適用します。アドレス割当と通常の経路設定は別リソースです。


## WireGuardPeer

[WireGuardPeer](resources/WireGuardPeer.md)

宣言済みWireGuardInterfaceのピアを定義します。AllowedIPsはWireGuard内の送信先・受信元選択を設定し、このコントローラーは通常のIP経路を自動作成しません。


## TailscaleNode

[TailscaleNode](resources/TailscaleNode.md)

Linuxのsystemd oneshotまたはFreeBSDのrc.dスクリプトを生成してTailscale登録オプションを適用し、tailscale status --jsonで状態を取得します。Tailscale・Headscaleサーバー側の承認は別操作です。


## IPsecConnection

[IPsecConnection](resources/IPsecConnection.md)


## VRF

[VRF](resources/VRF.md)

LinuxのVRFデバイス・経路表・参加インターフェースの意図を宣言します。現行実装は計画とartifact識別子の生成、経路表の保護を行いますが、VRF作成・メンバー接続のcontrollerはありません。plan表示をVRF作成済みの証拠とは扱わないでください。


## VXLANTunnel

[VXLANTunnel](resources/VXLANTunnel.md)


## IPv4StaticAddress

[IPv4StaticAddress](resources/IPv4StaticAddress.md)

論理インターフェースへ固定IPv4アドレスを宣言します。controllerが設定に収束させるアドレスであり、DHCPリースではありません。


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

管理下のrouterd-dhcpv4-client daemonを動かし、取得したリースのアドレス、必要に応じて既定経路とホストのDNS設定を反映します。


## ClusterNetworkRoute

[ClusterNetworkRoute](resources/ClusterNetworkRoute.md)

KubernetesのPod・ServiceネットワークへのIPv4経路を、明示したネクストホップ経由で設定します。クラスターのCIDRやノードを自動検出する機能ではありません。


## DHCPv4Server

[DHCPv4Server](resources/DHCPv4Server.md)


## DHCPv4Reservation

[DHCPv4Reservation](resources/DHCPv4Reservation.md)


## DHCPv6Address

[DHCPv6Address](resources/DHCPv6Address.md)

ホストのDHCPv6 clientで取得するIPv6アドレスを宣言します。DHCPv6PrefixDelegationとは別で、LAN向けプレフィックスの配布は行いません。


## IPv6RAAddress

[IPv6RAAddress](resources/IPv6RAAddress.md)

ホスト側でIPv6 RA/SLAACを受信する宣言です。RA clientであり、LANへRAを送るIPv6RouterAdvertisementとは別です。


## DHCPv6PrefixDelegation

[DHCPv6PrefixDelegation](resources/DHCPv6PrefixDelegation.md)


## IPv6DelegatedAddress

[IPv6DelegatedAddress](resources/IPv6DelegatedAddress.md)


## DHCPv6Information

[DHCPv6Information](resources/DHCPv6Information.md)

同じインターフェースを使うDHCPv6-PD daemonから、AFTR・DNS・SNTP・検索ドメインの情報を取り込みます。独立したInformation-request clientを起動するリソースではありません。


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

リソースの状態からデバイス・ゲートウェイを取得できる、IPv4の通常経路または破棄経路です。


## HealthCheck

[HealthCheck](resources/HealthCheck.md)


## EgressRoutePolicy

[EgressRoutePolicy](resources/EgressRoutePolicy.md)


## EventRule

[EventRule](resources/EventRule.md)

永続化されたローカルイベント履歴のパターンからイベントを生成します。相関キーごとに状態を分離できます。


## DerivedEvent

[DerivedEvent](resources/DerivedEvent.md)

リソースの現在状態の条件が変わると、成立・取り消しイベントを発行します。EventRuleのようなイベント列の判定ではありません。


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

ファイアウォールのパケットログ収集と、保存DB・Linux NFLOGグループ・描画時のログ設定を指定します。受理される項目の一部は、現在の実行処理で使われません。


## Hostname

[Hostname](resources/Hostname.md)

OSのホスト名を管理します。Router.metadata.nameや各リソース名とは別です。
