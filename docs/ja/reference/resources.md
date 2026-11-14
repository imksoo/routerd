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

用途別に分類し、分類内はKind名順に並べています。閲覧用の順序であり、YAMLの評価順・依存順・実行順ではありません。

## インターフェース・アドレス

### Interface

[Interface](resources/Interface.md)

既存のOSインターフェースに論理名metadata.nameを付け、他のリソースから参照できるようにします。物理インターフェースを新規作成する設定ではありません。

### IPv4StaticAddress

[IPv4StaticAddress](resources/IPv4StaticAddress.md)

論理インターフェースへ固定IPv4アドレスを宣言します。controllerが設定に収束させるアドレスであり、DHCPリースではありません。

### SelfAddressPolicy

[SelfAddressPolicy](resources/SelfAddressPolicy.md)

対応するDHCPv6静的生成で使用する、順序付き自己アドレス選択方針です。インターフェースへアドレスを割り当てるものではありません。

### VirtualAddress

[VirtualAddress](resources/VirtualAddress.md)

固定アドレス、または選出したノードの共有アドレスを維持します。Linuxは固定にiproute2・VRRPにkeepalived、FreeBSDはifconfig・CARPを使います。

### VRF

[VRF](resources/VRF.md)

LinuxのVRFデバイス・経路表・参加インターフェースの意図を宣言します。現行実装は計画とartifact識別子の生成、経路表の保護を行いますが、VRF作成・メンバー接続のcontrollerはありません。plan表示をVRF作成済みの証拠とは扱わないでください。

## WAN接続・アドレス取得

### DHCPv4Client

[DHCPv4Client](resources/DHCPv4Client.md)

管理下のrouterd-dhcpv4-client daemonを動かし、取得したリースのアドレス、必要に応じて既定経路とホストのDNS設定を反映します。

### DHCPv6Address

[DHCPv6Address](resources/DHCPv6Address.md)

ホストのDHCPv6 clientで取得するIPv6アドレスを宣言します。DHCPv6PrefixDelegationとは別で、LAN向けプレフィックスの配布は行いません。

### DHCPv6Information

[DHCPv6Information](resources/DHCPv6Information.md)

同じインターフェースを使うDHCPv6-PD daemonから、AFTR・DNS・SNTP・検索ドメインの情報を取り込みます。独立したInformation-request clientを起動するリソースではありません。

### DHCPv6PrefixDelegation

[DHCPv6PrefixDelegation](resources/DHCPv6PrefixDelegation.md)

上流でIPv6プレフィックスを取得します。下流・ホストのアドレス生成と適用はIPv6DelegatedAddressを使い、取得だけではクライアントへ広告しません。

### DSLiteTunnel

[DSLiteTunnel](resources/DSLiteTunnel.md)

AFTRへ向けてIPv4をIPv6にカプセル化します。トンネル作成、IPv4デフォルト経路、NAT、疎通確認は別の設定・処理です。

### IPv6DelegatedAddress

[IPv6DelegatedAddress](resources/IPv6DelegatedAddress.md)

DHCPv6PrefixDelegationのプレフィックスからローカルIPv6アドレスを生成・適用します。

### IPv6RAAddress

[IPv6RAAddress](resources/IPv6RAAddress.md)

ホスト側でIPv6 RA/SLAACを受信する宣言です。RA clientであり、LANへRAを送るIPv6RouterAdvertisementとは別です。

### PPPoESession

[PPPoESession](resources/PPPoESession.md)

Linuxのpppd・FreeBSDのmpd5を使うrouterd-pppoe-clientを監視実行し、交渉したIPv4セッション状態を公開します。一部の旧項目はFreeBSDの手動render出力だけに使われ、主デーモン経路には効きません。

## VPN・トンネル

### IPsecConnection

[IPsecConnection](resources/IPsecConnection.md)

strongSwanのswanctl接続を生成し、routerdの全IPsec接続をまとめて同期します。CHILD_SAは通信による起動用のtrap設定で、接続の読み込みはSA確立の証明ではありません。

### OverlayPeer

[OverlayPeer](resources/OverlayPeer.md)

HybridRouteと経路MTU方針の生成に使う、既存の転送手段を記述します。本リソースだけでWireGuard・Tailscale・IPsec・トンネルは構築しません。

### TailscaleNode

[TailscaleNode](resources/TailscaleNode.md)

Linuxのsystemd oneshotまたはFreeBSDのrc.dスクリプトを生成してTailscale登録オプションを適用し、tailscale status --jsonで状態を取得します。Tailscale・Headscaleサーバー側の承認は別操作です。

### TunnelInterface

[TunnelInterface](resources/TunnelInterface.md)

非暗号化のIPv4外側トンネルと、任意の内側IPv4アドレスを生成・再調整します。metadata.nameが実OSトンネル名です。

### VXLANTunnel

[VXLANTunnel](resources/VXLANTunnel.md)

静的なフラッド先と任意のブリッジ接続を持つLinuxのVXLANデバイスを管理します。マルチキャスト探索や動的MAC学習は設定しません。

### WireGuardInterface

[WireGuardInterface](resources/WireGuardInterface.md)

WireGuardデバイスを作成・設定し、wg setconfでピア設定全体を適用します。アドレス割当と通常の経路設定は別リソースです。

### WireGuardPeer

[WireGuardPeer](resources/WireGuardPeer.md)

宣言済みWireGuardInterfaceのピアを定義します。AllowedIPsはWireGuard内の送信先・受信元選択を設定し、このコントローラーは通常のIP経路を自動作成しません。

## 経路・経路監視

### BFD

[BFD](resources/BFD.md)

vtysh経由でFRR bfddのセッションを設定・観測し、BGPPeerの障害処理に使います。GoBGPとは別バックエンドで、BFD DownによりBGPピアをリセットします。

### BGPDynamicPeer

[BGPDynamicPeer](resources/BGPDynamicPeer.md)

GoBGPの動的着信ピア受付グループを定義します。個々のピアやWireGuard・SAMの認証識別子ではありません。

### BGPPeer

[BGPPeer](resources/BGPPeer.md)

宣言したBGPRouterに1件以上の静的BGPピアを設定します。

### BGPRouter

[BGPRouter](resources/BGPRouter.md)

ローカルGoBGPインスタンス・受理経路・自起源広告を設定します。カーネルへの適用や対向疎通はBGPのEstablishedとは別です。

### ClusterNetworkRoute

[ClusterNetworkRoute](resources/ClusterNetworkRoute.md)

KubernetesのPod・ServiceネットワークへのIPv4経路を、明示したネクストホップ経由で設定します。クラスターのCIDRやノードを自動検出する機能ではありません。

### EgressRoutePolicy

[EgressRoutePolicy](resources/EgressRoutePolicy.md)

送信経路を選択・設定します。実際の動作は明示modeとhostTrafficによって異なります。

### HealthCheck

[HealthCheck](resources/HealthCheck.md)

IP宛先を確認し、経路選択用のHealthy・Unhealthy状態を発行します。成功が示す範囲は指定したプロトコルと経路に限られます。

### HybridRoute

[HybridRoute](resources/HybridRoute.md)

列挙したIPv4宛先網を、OverlayPeer経由の主経路表IPv4Routeへ展開します。

### IPv4Route

[IPv4Route](resources/IPv4Route.md)

リソースの状態からデバイス・ゲートウェイを取得できる、IPv4の通常経路または破棄経路です。

## DHCP配布・RA・リース同期

### DHCPv4Relay

[DHCPv4Relay](resources/DHCPv4Relay.md)

dnsmasqを使い、DHCPv4要求を明示した1つのIPv4サーバーへ中継します。

### DHCPv4Reservation

[DHCPv4Reservation](resources/DHCPv4Reservation.md)

宣言したDHCPv4Serverに対し、MACアドレスへIPv4アドレスを予約します。

### DHCPv4Server

[DHCPv4Server](resources/DHCPv4Server.md)

共有dnsmasqサービスでIPv4 DHCPプール、選別条件付きの追加プール、予約を提供します。

### DHCPv4ServerLeaseSync

[DHCPv4ServerLeaseSync](resources/DHCPv4ServerLeaseSync.md)

DHCPv4サーバーのリースファイルと存在する固定保持状態を、SSH経由のrsyncで相手ホストへ複製します。DHCPのフェイルオーバープロトコルではありません。

### DHCPv6PrefixDelegationLeaseSync

[DHCPv6PrefixDelegationLeaseSync](resources/DHCPv6PrefixDelegationLeaseSync.md)

内蔵DHCPv6-PDクライアントのリーススナップショットを、復旧・引継ぎ用に相手へ複製します。

### DHCPv6Server

[DHCPv6Server](resources/DHCPv6Server.md)

共有dnsmasqでLAN向けDHCPv6アドレス・情報サービスと関連するRAを提供します。

### DHCPv6ServerLeaseSync

[DHCPv6ServerLeaseSync](resources/DHCPv6ServerLeaseSync.md)

DHCPv6サーバーのリースファイルをSSH経由のrsyncで相手へ複製します。DHCPのフェイルオーバープロトコルではありません。

### IPv6RouterAdvertisement

[IPv6RouterAdvertisement](resources/IPv6RouterAdvertisement.md)

LANのIPv6ルーター広告オプションと、以前の委任プレフィックスの非推奨化を設定します。

## DNS

### DNSForwarder

[DNSForwarder](resources/DNSForwarder.md)

1つのDNSResolverに対し、問い合わせ名に応じてローカルゾーンまたは上流DNSを選ぶ規則です。

### DNSResolver

[DNSResolver](resources/DNSResolver.md)

DNSForwarderの規則に接続する、UDP・TCP待受を備えたローカルDNSサービスです。

### DNSUpstream

[DNSUpstream](resources/DNSUpstream.md)

DNSForwarderが参照するDNSサーバーの接続先です。宣言だけで待受を開始したり問い合わせを転送したりはしません。

### DNSZone

[DNSZone](resources/DNSZone.md)

DNSForwarder.zoneRefs経由で提供するローカルA/AAAAレコードと自動PTR応答です。任意レコード型・ゾーン転送・署名を備えた汎用権威DNSではありません。

## NAT・サービス公開

### IngressService

[IngressService](resources/IngressService.md)

IPv4 TCP・UDPの公開先をヘルスチェックと転送先選択で制御し、任意でローカルDNS・内部折り返しNATを使います。L4 NATであり、HTTPプロキシ・TLS終端ではありません。

### IPAddressSet

[IPAddressSet](resources/IPAddressSet.md)

固定IPとDNS解決したIPを、対応するnftablesのNAT・経路・ファイアウォールルールで共有します。経路やDNSサービスを作成する機能ではありません。

### LocalServiceRedirect

[LocalServiceRedirect](resources/LocalServiceRedirect.md)

宣言したインターフェースで受信した対象通信を、nftablesのprerouting NATでルーター自身のサービスポートへ転送します。

### NAT44FlowDNATPinhole

[NAT44FlowDNATPinhole](resources/NAT44FlowDNATPinhole.md)

送信UDPからIPv4内部エンドポイントを学習し、受信UDPの宛先ポートで動的DNATします。Linux nftables専用で、現行PFは実装していません。

### NAT44Rule

[NAT44Rule](resources/NAT44Rule.md)

送信インターフェースまたはEgressRoutePolicyで選んだデバイス経由で、転送IPv4の送信元を変換します。転送許可・変換先アドレスの割当自体は行いません。

### NAT44SessionSync

[NAT44SessionSync](resources/NAT44SessionSync.md)

SSHイベントストリーム、または常時稼働のconntrackd FTFWリンクでLinux conntrack状態を複製します。FreeBSD PFの状態同期ではありません。

### PortForward

[PortForward](resources/PortForward.md)

TCP・UDPポートを1つのIPv4先へ転送し、任意で内部折り返しNATを設定します。LinuxはDNATと転送許可、FreeBSDはrdr passを生成します。

## ファイアウォール・クライアント制御

### ClientPolicy

[ClientPolicy](resources/ClientPolicy.md)

対象クライアント、または例外以外のクライアントへ、ゲスト用の自機サービス・転送宛先制限を適用します。Linuxは設定MAC、FreeBSD PFは明示IPで識別します。

### FirewallFlowPinhole

[FirewallFlowPinhole](resources/FirewallFlowPinhole.md)

ファイアウォールのゾーン組で、IPv4 UDPの戻り許可エンドポイントを学習します。アドレス変換は行いません。Linux nftables専用で、現行PFは生成しません。

### FirewallPolicy

[FirewallPolicy](resources/FirewallPolicy.md)

ゾーン・明示ルールと併用する共通の暗黙ファイアウォール設定です。複数定義は独立せず集約します。

### FirewallRule

[FirewallRule](resources/FirewallRule.md)

ゾーン間の許可・破棄・拒否ルールです。アドレス・プロトコル・ポート条件と、Linuxのレート・接続数条件を指定できます。

### FirewallZone

[FirewallZone](resources/FirewallZone.md)

設定済みインターフェースを名前付きの信頼区分にまとめ、ファイアウォール生成に使います。OSインターフェースや独立した経路・ブリッジ領域は作りません。

## システム・管理

### ControlAPI

[ControlAPI](resources/ControlAPI.md)

状態の取得と設定変更に使うHTTP Control APIを有効化します。Unixソケット経由の制御とWebConsoleは別です。

### Hostname

[Hostname](resources/Hostname.md)

OSのホスト名を管理します。Router.metadata.nameや各リソース名とは別です。

### ManagementAccess

[ManagementAccess](resources/ManagementAccess.md)

反映前の管理経路確認で検査するインターフェースを宣言します。管理用の待受やファイアウォール許可ルールを生成するリソースではありません。

### NTPClient

[NTPClient](resources/NTPClient.md)

上流との時刻同期クライアントを管理、または観測します。

### NTPServer

[NTPServer](resources/NTPServer.md)

chronyまたはFreeBSD ntpdを使い、時刻サーバーと上流サーバーを設定します。

### Package

[Package](resources/Package.md)

指定したOSパッケージの導入を確認します。稼働ホストのOSに完全一致する、最初のパッケージセットを選びます。

### RouterdCluster

[RouterdCluster](resources/RouterdCluster.md)

ファイルのリースとOSファイルロックで、どのrouterdが変更処理を行うかを調整します。VRRPの選出や対向ネットワークの自動探索ではありません。

### Sysctl

[Sysctl](resources/Sysctl.md)

カーネルのsysctlを1項目反映し、必要ならsysctl.dへ永続設定を書きます。他のrouterdリソースへの参照項目はありません。

### SysctlProfile

[SysctlProfile](resources/SysctlProfile.md)

ルーター向けのsysctlセットを名前で選び、必要ならキー単位で上書きします。Sysctlリソースの名前を参照する設定ではありません。

### WebConsole

[WebConsole](resources/WebConsole.md)

routerdから読取り用のWebコンソールを公開します。設定変更を行うControlAPIとは別のHTTP待受です。

## ログ・監視

### FirewallEventLog

[FirewallEventLog](resources/FirewallEventLog.md)

ファイアウォールのパケットログ収集と、保存DB・Linux NFLOGグループ・描画時のログ設定を指定します。受理される項目の一部は、現在の実行処理で使われません。

### LogRetention

[LogRetention](resources/LogRetention.md)

運用ログのSQLiteレコードを削除し、必要なら削除前に保管先へ転送します。任意のOSログファイルを整理する設定ではありません。

### LogSink

[LogSink](resources/LogSink.md)

typeで選ぶ運用イベントログの出力先です。ObservabilityPipeline内のログ出力先とは別の設定です。

### ObservabilityPipeline

[ObservabilityPipeline](resources/ObservabilityPipeline.md)

管理サービス向けOpenTelemetry環境の宣言と、独立した組込み運用イベントログconsumerをまとめた設定です。

### Telemetry

[Telemetry](resources/Telemetry.md)

生成する管理サービスにOpenTelemetryの環境変数を設定します。LogSinkや独立したcollectorプロセスを定義するリソースではありません。

### TrafficFlowLog

[TrafficFlowLog](resources/TrafficFlowLog.md)

接続のスナップショットをSQLiteのフローDBに記録し、利用可能ならDPIキャッシュの情報を補います。パケットキャプチャーや全パケットの記録ではありません。

## イベント・イベント連携

### DerivedEvent

[DerivedEvent](resources/DerivedEvent.md)

リソースの現在状態の条件が変わると、成立・取り消しイベントを発行します。EventRuleのようなイベント列の判定ではありません。

### EventGroup

[EventGroup](resources/EventGroup.md)

共有するイベントグループと、このルーターの識別名・配送先取得・受信・ローカル保持設定を宣言します。

### EventPeer

[EventPeer](resources/EventPeer.md)

EventGroupの相手側配送先を1件宣言します。通信認証と自ノード識別名はグループ側で指定します。

### EventRule

[EventRule](resources/EventRule.md)

永続化されたローカルイベント履歴のパターンからイベントを生成します。相関キーごとに状態を分離できます。

### EventSubscription

[EventSubscription](resources/EventSubscription.md)

期限内の共有イベントを永続DBから選び、ローカルのPluginを実行して結果をDynamicConfigPartとして保存します。

### FederationSLO

[FederationSLO](resources/FederationSLO.md)

routerctl doctorが共有イベント配送・購読を診断するときのグループ別閾値です。配送再試行やPlugin実行方針自体は変更しません。

## SAM・モビリティ

### MobilityPool

[MobilityPool](resources/MobilityPool.md)

共有SAMトポロジー・ローカル所有権観測・BGPと連携状態を組み合わせ、アドレス移動とローカル取得を計画します。YAMLは自ノードの要求で、共有識別情報はSAMNodeSetに置きます。

### SAMEnrollmentClaim

[SAMEnrollmentClaim](resources/SAMEnrollmentClaim.md)

SAM登録へ送るleaf自身の識別・トランスポート・所有権の要求を宣言します。期限・失効はRR・コントローラーの管理状態で、leafのjoin HMACには含めません。

### SAMEnrollmentClient

[SAMEnrollmentClient](resources/SAMEnrollmentClient.md)

ローカル登録要求を起動用RRのControlAPIへ送り、許可されたトポロジーを取得して動的SAMRRSet・任意のdirectピアグループとして既存コントローラーへ渡します。クラウド基盤の新規作成は行いません。

### SAMEnrollmentPolicy

[SAMEnrollmentPolicy](resources/SAMEnrollmentPolicy.md)

RR側でSAM leafの識別・トンネルアドレス・任意のjoin認証・所有許可を規定します。leafはこのポリシーを複製せず、許可されたトポロジーを取得します。

### SAMNodeSet

[SAMNodeSet](resources/SAMNodeSet.md)

トランスポート・イベントピア・WireGuard・MobilityPoolメンバーで共有するSAMのノード識別・トポロジーを宣言します。ローカルのプロバイダー取得・探索設定はここではなくMobilityPoolの自ノード上書きに置きます。

### SAMSubnetPolicy

[SAMSubnetPolicy](resources/SAMSubnetPolicy.md)

シャード割当を連携イベントとして公開し、各ノードのMobilityPool所有権探索を担当IPv4サブネットに絞ります。探索範囲の割当であり、IP経路を直接設定するリソースではありません。

### SAMTransportProfile

[SAMTransportProfile](resources/SAMTransportProfile.md)

共有トポロジーや許可済みRR・directスナップショットからノード間SAMトンネル・終端/32経路・任意のBGP/BFDを生成します。

## 拡張・動的設定・プロバイダー操作

### CloudProviderProfile

[CloudProviderProfile](resources/CloudProviderProfile.md)

プロバイダー操作の計画で使う識別情報・対象メタデータの既定値です。この宣言自体はクラウド認証や資源作成を行いません。

### DynamicConfigSource

[DynamicConfigSource](resources/DynamicConfigSource.md)

プラグインを使う動的設定取得元の宣言です。現行実装には型の読込みと検証はありますが、DynamicConfigSourceSpecを消費するruntime処理はなく、宣言だけではプラグインを実行しません。

### DynamicOverridePolicy

[DynamicOverridePolicy](resources/DynamicOverridePolicy.md)

動的設定の有効期間中、指定した取得元が起動設定の特定リソースをmaskできるよう許可します。保存済みの起動設定自体は書き換えません。

### Plugin

[Plugin](resources/Plugin.md)

routerdのプラグインプロトコルで実行する外部プログラムを登録します。明示的な実行、またはEventSubscriptionなどの利用側から呼び出します。

### ProviderActionPolicy

[ProviderActionPolicy](resources/ProviderActionPolicy.md)

実行用Pluginに渡すプロバイダー操作の許可条件です。ポリシーの宣言だけでは操作やクラウドへのログインは行いません。
