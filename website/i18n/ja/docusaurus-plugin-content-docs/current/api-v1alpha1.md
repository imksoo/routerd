---
title: リソース API v1alpha1
slug: /reference/api-v1alpha1
---

# リソース API v1alpha1

routerd の設定は、最上位の `Router` と、型付きリソースの一覧で構成します。
このページは現在の実装に合わせたリソース一覧です。
Phase 1.6 以降は RFC 表記に合わせ、DHCP 関連 Kind は `DHCPv4*` と `DHCPv6*` を使います。
旧名の互換別名はありません。

## 共通形

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: wan
spec:
  ifname: ens18
  adminUp: true
```

| フィールド | 意味 |
| --- | --- |
| `apiVersion` | API グループと版です。 |
| `kind` | リソース種別です。 |
| `metadata.name` | 同じ種別内の名前です。 |
| `spec` | 利用者が宣言する意図です。 |
| `status` | routerd または専用デーモンが観測した状態です。 |

## API グループ

| API グループ | 主な Kind |
| --- | --- |
| `routerd.net/v1alpha1` | `Router` |
| `net.routerd.net/v1alpha1` | インターフェース、再利用可能な `IPAddressSet`、DHCP、DNS、経路、トンネル、VIP、BGP、イベント、通信フローログ |
| `firewall.routerd.net/v1alpha1` | `FirewallZone`, `FirewallPolicy`, `FirewallRule`, `FirewallLog`, `ClientPolicy`, `PortForward`, `IngressService`, `LocalServiceRedirect` |
| `system.routerd.net/v1alpha1` | `Hostname`, `Sysctl`, `SysctlProfile`, `Package`, `NTPClient`, `LogSink`, `ObservabilityPipeline`, `RouterdCluster`, `LogRetention`, `WebConsole` |
| `plugin.routerd.net/v1alpha1` | プラグインマニフェスト |

## システム準備

| Kind | 役割 |
| --- | --- |
| `Package` | 他 resource から導出できない OS package だけを補う narrow override です。通常の runtime dependency は自動導出されます。 |
| `Sysctl` | router resource からまだ導出できない sysctl 値を補う narrow escape hatch です。`compare: exact` と `compare: atLeast` で読み戻し判定を選べます。 |
| `SysctlProfile` | ルーター向け sysctl 推奨値を補う narrow escape hatch です。通常の router sysctl は自動導出されます。 |
| `Hostname` | ホスト名を設定します。 |
| `NTPClient` | OS の NTP クライアントを有効にします。DHCPv4 / DHCPv6 の状態から時刻サーバーを導出し、空なら public NTP サーバーへ戻せます。 |
| `LogSink` | routerd のイベントを syslog や外部プログラムへ送ります。 |
| `ObservabilityPipeline` | OTLP environment と、stdout / syslog / Loki への routerd event forwarding を設定します。 |
| `RouterdCluster` | file lease により leader だけが host configuration を変更し、standby は status 観測に回ります。 |
| `LogRetention` | イベント、DNS、通信フロー、ファイアウォールログの保管期間を管理します。 |
| `WebConsole` | 読み取り専用の Web 画面を管理ネットワークで待ち受けます。 |

## インターフェース

| Kind | 役割 |
| --- | --- |
| `Interface` | routerd が扱う安定した名前と OS のインターフェース名を結び付け、下流 resource 向けの link/address status も提供します。 |
| `PPPoESession` | PPPoE 用の下位インターフェース設定を表します。 |
| `PPPoESession` | `routerd-pppoe-client` が管理する PPPoE セッションです。 |
| `WireGuardInterface` | WireGuard インターフェースを表します。 |
| `WireGuardPeer` | WireGuard の相手を表します。 |
| `TailscaleNode` | Tailscale ノードを設定します。Exit node と subnet router の広告を管理対象 systemd ユニットで行います。 |
| `IPsecConnection` | strongSwan の cloud VPN 向け接続定義を表します。 |
| `VRF` | Linux VRF デバイスと経路表を表します。 |
| `VXLANTunnel` | VXLAN トンネルを表します。 |

`PPPoESession.spec.disabled` を `true` にすると、PPPoE の定義は残したまま、管理対象の pppd ユニットを停止・無効化します。
通常運用では PPPoE セッション枠を使わず、必要なときだけ手動で試験する fallback 経路に使えます。

`TailscaleNode` は初回登録用に `authKey` を使えます。
本番設定では `authKeyEnv` と `authKeyFile` を推奨します。
これにより、秘密値を YAML と Git 履歴に残しません。
どちらも未指定の場合、`tailscaled` はログイン済みとみなします。
routerd は広告するノード設定だけを再適用します。
Tailscale の既定 UDP/41641 は予約済みとして扱います。
WireGuard の待ち受けポートには別の番号を使ってください。
詳しい設定手順は Tailscale の設定ガイドを参照してください。

`WireGuardInterface` は `privateKeyFile` を受け取れます。
秘密鍵を router YAML の外に置くためです。
`WireGuardPeer` も任意の PSK 用に `presharedKeyFile` を受け取れます。
インライン鍵フィールドは主に例とテスト向けです。
FreeBSD では、routerd が rc.d サービスを生成します。
そのサービスは `wg` インターフェースを作成し、ファイルから秘密鍵を読み込み、
宣言された peer と static address を適用します。

Kernel module と systemd-networkd/resolved の adoption drop-in は、router resource から自動導出されます。削除済みの `KernelModule`、`NetworkAdoption`、`Link`、`NixOSHost` が config に残っている場合、routerd は黙って無視せずエラーを返します。

## WAN アドレスと委譲

| Kind | 役割 |
| --- | --- |
| `IPv4StaticAddress` | 静的 IPv4 アドレスを付与します。 |
| `VirtualAddress` | IPv4 `/32` または IPv6 `/128` VIP を宣言します。`spec.family` は `ipv4` または `ipv6` です。`mode: vrrp` は Linux では keepalived、FreeBSD では CARP を使います。 |
| `DHCPv4Client` | `routerd-dhcpv4-client` が DHCPv4 リース、IPv4 アドレス、任意のデフォルト経路を管理します。 |
| `DHCPv6Address` | DHCPv6 IA_NA の意図を表します。 |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` が管理する DHCPv6-PD リースです。 |
| `DHCPv6Information` | DHCPv6 情報要求の結果です。DNS、SNTP、ドメイン検索、AFTR 情報を観測します。 |
| `IPv6DelegatedAddress` | 委譲プレフィックスから LAN 側アドレスを導出します。 |
| `IPv6RAAddress` | RA/SLAAC で得る IPv6 アドレスを表します。 |

`DHCPv6PrefixDelegation` は旧 OS クライアント選択フィールドを持ちません。
DHCPv6-PD は `routerd-dhcpv6-client` が担当します。

## LAN 側サービス

| Kind | 役割 |
| --- | --- |
| `DHCPv4Server` | dnsmasq の DHCPv4 service と任意のアドレスプールを提供します。 |
| `DHCPv4Reservation` | MAC アドレスごとの固定割り当てを表します。 |
| `DHCPv4Relay` | dnsmasq の DHCPv4 中継を表します。 |
| `IPv6RouterAdvertisement` | RA、PIO、RDNSS、DNSSL、M/O フラグ、MTU、優先度、寿命を生成します。 |
| `DHCPv6Server` | dnsmasq の DHCPv6/RA service です。`stateless`、`stateful`、`both`、`ra-only` を扱います。 |
| `DNSZone` | ローカル権威ゾーンを表します。手動レコードと DHCP リース由来のレコードを扱います。 |
| `DNSResolver` | `routerd-dns-resolver` の daemon instance、待ち受け、cache、metrics、query log を表します。 |
| `DNSForwarder` | 1 つの resolver に対する DNS match rule です。`DNSZone` を応答するか、名前付き `DNSUpstream` へ転送します。 |
| `DNSUpstream` | `udp`、`tcp`、`dot`、`doh` のいずれかで 1 つの上流 endpoint を表します。状態由来 address、bootstrap resolver、TLS 名、送信元 interface も指定できます。 |

Android は DHCPv6 の DNS だけでは名前解決を完結できないため、IPv6 LAN では `IPv6RouterAdvertisement.spec.rdnss` を設定します。

dnsmasq は DHCPv4、DHCPv6、中継、RA だけを担当します。
DNS の待ち受けと応答は `DNSResolver` が担当します。
LAN の DNS suffix は、`DHCPv4Server.spec.domainFrom`、
`IPv6RouterAdvertisement.spec.dnsslFrom`、`DHCPv6Server.spec.domainSearchFrom`
から `DNSZone/<name>.zone` を参照して、ローカルゾーンと一致させられます。
`DNSResolver.spec.listen[].sources` には、その listener が使う `DNSForwarder` 名を並べます。
省略した listener は、その resolver を参照するすべての `DNSForwarder` を使います。
user YAML の `DNSResolver.spec.sources` は受け付けません。旧 inline source は
`DNSForwarder` と `DNSUpstream` に分割してください。

`DNSForwarder.spec.match` には `home.example` や既定上流を表す `.` を指定します。
`spec.zoneRefs` は local `DNSZone` を応答し、`spec.upstreams` は `DNSUpstream` へ転送します。
DNSSEC validation は `DNSForwarder.spec.dnssecValidate` に書きます。

`DNSUpstream.spec.protocol` は `udp`、`tcp`、`dot`、`doh` です。
`addressFrom` では `DHCPv6Information/<name>.dnsServers` などから UDP 上流 address を導出できます。
`sourceInterface` は Linux で送信先 interface を束縛し、`bootstrap` は DoH/DoT endpoint 名の解決に使う補助 resolver です。

## DS-Lite、経路、NAT

| Kind | 役割 |
| --- | --- |
| `DSLiteTunnel` | AFTR へ `ip6tnl` トンネルを張ります。AFTR は直接 IPv6、FQDN、または DHCPv6 情報から得ます。 |
| `IPAddressSet` | 直接指定したアドレスや FQDN から、再利用可能な IP address set を定義します。Linux nftables renderer はこれを named set として出力し、redirect、NAT、policy routing から参照できます。 |
| `IPv4Route` | IPv4 経路を追加します。DS-Lite 経由の既定経路や、明示的な破棄経路にも使います。 |
| `ClusterNetworkRoute` | Kubernetes の Pod / Service CIDR を worker next hop 経由の static IPv4 route に展開します。 |
| `BGPRouter` | ローカル BGP router を宣言します。初期 backend は FRR で、import policy は default deny です。 |
| `BGPPeer` | `BGPRouter` にぶら下がる FRR 管理の BGP peer を宣言します。Kubernetes BGP speaker などに使います。 |
| `NAT44Rule` | nftables の `routerd_nat` テーブルで IPv4 NAPT を行います。 |
| `PortForward` | WAN 側の IPv4 TCP/UDP ポートを 1 つの内部 IPv4 宛先へ DNAT します。 |
| `IngressService` | WAN 側の IPv4 TCP/UDP サービスを公開します。複数 backend、TCP/HTTP health check、`failover` / `sourceHash` / `random` selection を受け付けます。 |
| `LocalServiceRedirect` | LAN 側 client から `IPAddressSet` 宛てに出る IPv4/IPv6 通信を router の local port へ redirect します。平文 DNS/NTP の集約を想定し、DoH や DoT の port には触れません。 |
| `EgressRoutePolicy` | 既定経路の選択、mark ベースの IPv4 policy routing、複数 target への hash 分散を表します。 |

`EgressRoutePolicy` は、CIDR 指定に加えて `destinationSetRefs` と
`excludeDestinationSetRefs` を持ちます。FQDN-backed な宛先 set を policy
resource にアドレス展開せず、経路制御や除外条件として使えます。
`mode: priority` は既定経路 failover、`mode: mark` は 1 つの mark 付き route
table、`mode: hash` または `candidates[].targets` は複数 route table への
source/destination hash 分散に使います。

routerd は reverse path filter sysctl、tunnel MTU、RA MTU、TCP MSS clamp を
router role、tunnel、firewall zone、RA/DHCPv6 resource から自動導出します。
config では LAN/WAN と tunnel の intent を宣言し、`IPv4ReversePathFilter` や
`PathMTUPolicy` は書きません。

`EgressRoutePolicy` は `excludeDestinationCIDRs` を持ちます。これにより、LAN 内部、管理網、HGW LAN、RFC 1918 の内部網などを policy routing の対象から外せます。

`ClusterNetworkRoute` は Kubernetes node 向けの補助 resource です。
`spec.pods.cidrs` と `spec.services.cidrs` に Pod / Service CIDR を並べ、
`spec.via[]` に worker または VIP の next hop を指定すると、routerd は
対応する `IPv4StaticRoute` intent を生成します。同じ weight は同じ metric
として扱われ、複数 next hop の ECMP に使えます。異なる weight は metric 差に
変換され、優先経路と fallback 経路を表します。

`FirewallRule` は宛先 CIDR に加えて `destinationSetRefs` と
`excludeDestinationSetRefs` を持ちます。これにより、再利用可能な FQDN-backed set
を各 rule にアドレス展開せず、許可・拒否・reject の条件として使えます。
stateful rule expression は `sourcePorts`、`destinationPorts`、ICMP / ICMPv6
type matching、`rateLimit`、`connLimit` も扱えます。`port` は単一の
destination port shorthand として引き続き受け付けますが、新しい例では
`destinationPorts` を使います。

`NAT44Rule` は `outboundInterface`、`sourceCIDRs`、`translation` による単純な
source NAT と、`type`、`egressInterface` または `egressPolicyRef`、`sourceRanges`
による policy-aware NAT を扱います。さらに `destinationCIDRs`、`destinationSetRefs`、
`excludeDestinationCIDRs`、`excludeDestinationSetRefs` を持ちます。これにより、
インターネット向け通信だけをマスカレードし、静的経路を持つプライベート宛先や
再利用可能な address set は NAT しない構成にできます。

`PortForward` と `IngressService` は Linux nftables と FreeBSD pf に DNAT を生成します。
`spec.hairpin.enabled: true` と `spec.hairpin.interfaces` を指定すると、LAN
クライアントから WAN アドレス経由で同じサービスへ到達する hairpin NAT も生成します。
hairpin は `listen.address` または `listen.addressFrom` が必須で、routerd は LAN 側
DNAT と戻り経路用の masquerade/NAT reflection を生成します。
`listen.addressFrom` と backend の `addressFrom` は `IPv4StaticAddress/<name>.address`
や `VirtualAddress/<name>.address` のような静的に描画できるアドレスリソースを参照できます。
`IngressService` では `spec.hairpin.mode` 未指定を `auto` として扱います。
listen address と選択済み backend が listen interface に宣言された同じ prefix 上に
ある場合、routerd は LAN client が VIP を使うために必要な同一 interface の戻り
SNAT を自動生成します。YAML に listen interface の prefix が宣言されていない場合も、
private IPv4 の listen/backend address が同じ `/24` にあれば hairpin が必要と判断します。
これは Live ISO のように boot 環境から interface address を引き継ぐ構成をカバーします。
抑止する場合は `spec.hairpin.mode: off`、明示指定する場合は `manual` と `interfaces` を使います。
`VirtualAddress.spec.vrrp.authentication` は keepalived では `auth_pass`、
FreeBSD CARP では `pass` として描画されます。本番構成では routerd YAML に
共有 secret を残さないため、`VirtualAddress.spec.vrrp.authenticationFrom`
を優先してください。`authenticationFrom.file` は local secret file、
`authenticationFrom.env` は環境変数を読み、`base64: true` で base64 値を
decode します。生成済み keepalived/CARP 設定や host interface state は secret
として扱ってください。
VRRP authentication は VRRPv3 (RFC 5798) では deprecated です。routerd は L2 隔離を前提にするため、
authentication は周辺ネットワーク方針で必要な場合や単純な誤設定対策に限って使ってください。
`IngressService` は複数 backend、TCP/HTTP health check、`failover`、`sourceHash`、
`random` selection を受け付けます。runtime controller は backend FQDN を解決し、
DNS が一時的に失敗した場合は直前の解決済み IPv4 に fallback します。healthy backend が
複数ある場合、Linux nftables は `sourceHash` では `jhash ip saddr`、`random` では
`numgen random` で分配します。healthy backend が 1 つだけになった場合は failover に
降格します。validator は `IngressService`、`LocalServiceRedirect`、routerd 管理 daemon の
listen port が同じ interface/protocol で衝突する設定を拒否します。

`IPAddressSet` は直接指定した IPv4/IPv6 address を apply 時に nftables named set へ
出力します。FQDN の `A`/`AAAA` record は runtime controller が解決し、参照されている
set を firewall、NAT、policy table 全体を reload せずにその場で更新します。次回更新は
観測した最小 DNS TTL の半分を基本にし、60 秒より短くはしません。`refreshInterval` は
より積極的に更新したい場合の上限として使えます。

`IPAddressSet.spec.names` は完全一致の DNS 名だけを扱います。`microsoft.com` は
`microsoft.com` 自体の `A`/`AAAA` record を意味し、`www.microsoft.com`、
`login.microsoft.com`、`*.microsoft.com`、さらに深いサブドメインは含みません。
ワイルドカードや suffix 形式のサービス判定には、単純な FQDN 解決ではなく、
DNS query 観測や provider endpoint feed を扱う別リソースが必要です。

`BGPRouter` と `BGPPeer` は初期 backend として FRR を使います。routerd は
生成した FRR 設定を `vtysh -C -f` で検証し、`frr-reload.py --reload` で差分適用します。
`BGPStateWatcher` は FRR JSON status を読み、peer、prefix、event、OTel metric に
反映します。`routerctl show bgp` は router、peer、message counter、BFD status、
直近 error を table で表示します。`BGPPeer.spec.bfd` は sub-second failure
detection が必要な peer 向けに FRR BFD を有効にします。managed peer のいずれかが
BFD を使う場合、routerd は FRR daemons file の `bgpd=yes` と `bfdd=yes` も維持します。
daemon toggle が変わったときだけ `frr.service` を restart します。
watcher は default で 15 秒間隔、prefix status 4096 entries cap です。
`BGPRouter.spec.watcher` で `pollInterval`、`maxPrefixes`、
`peerStateChangeThrottle` を調整できます。validation は 3 秒未満の interval と
1,000,000 以上の prefix cap を拒否します。
import policy は default deny で、受け入れた経路には
`set ip next-hop peer-address` を付けます。`BGPRouter` は connected/static IPv4
route を個別の `allowedPrefixes` 付きで redistribute できます。routerd は FRR の
`redistribute connected/static route-map` を生成し、明示的な export prefix がない限り
peer outbound route-map は default deny のままにします。外向き広告は
`BGPRouter.spec.exportPolicy.allowedPrefixes`、または peer ごとの
`BGPPeer.spec.exportPolicy.allowedPrefixes` で明示します。BGP community policy は
router または peer に `communities.send`、`communities.accept`、
`communities.set.in/out` として宣言できます。FRR JSON に community が含まれる場合、
watcher は観測した route community を status に保存します。複数の `BGPRouter`
resource は、追加 router に `spec.vrf` を指定することで別々の FRR BGP instance として
動かせます。`spec.vrf` は routerd の `VRF` resource を参照し、
`router bgp <asn> vrf <ifname>` を生成します。routerd は
`BGPPeer.spec.routerRef` に従って、観測した BGP status を `BGPRouter` ごとに保存します。
`spec.listen.address` は routerd 側の listen collision check に使います。FRR の
address bind 自体は integrated config stanza ではなく bgpd daemon invocation option です。

`VirtualAddress` の `mode: vrrp` は Linux では keepalived、FreeBSD では CARP を使います。
`spec.family: ipv4` は IPv4 `/32`、`spec.family: ipv6` は IPv6 `/128` を要求します。
IPv6 VIP は keepalived VRRPv3 の
`family inet6` として描画され、FreeBSD では `inet6` CARP alias になります。
Linux VRRP は明示的な unicast peer を使い、既定は `nopreempt` です。
FreeBSD CARP は親 interface 上の multicast advertisement を使うため、
`spec.vrrp.peers` は FreeBSD では無視されます。`preempt: true` の場合、Linux では
`preemptDelay` で取り戻しを遅らせられます。FreeBSD には直接対応する
`preemptDelay` はありません。`track` で `BGPRouter`、`BGPPeer`、`IngressService`
などの状態に応じて priority を下げられます。既定では unhealthy 3 回連続で penalty
を適用し、healthy 2 回連続で解除します。`spec.hostname` は DNSResolver が配信する
対応 `DNSZone` へ VIP を自動公開できます。IPv4 VIP は A record、IPv6 VIP は AAAA
record になります。外部 AD DNS などが名前を管理する場合は
`spec.externalDNS: true` を設定してください。routerd は hostname 構文だけを検証し、
DNSZone coverage warning と自動公開を行いません。`routerctl show vrrp` は role、
priority、peer、transition 経過時間を表示します。

### VRRP production tuning

制御プレーン VIP のように高速 failover が重要な場所だけ、短い advertisement を
使います。Kubernetes API VIP では `advertInterval: 1s`、`preempt: true`、
`preemptDelay: 30s` が典型です。優先 router が VIP を取り戻しますが、復帰直後の
揺れで即 failback しないように待ち時間を入れます。

家庭 LAN や DS-Lite 周辺の VIP では、優先 owner に戻すことより安定性を優先する
設定が扱いやすいです。保守的な preset は `advertInterval: 3s` と
`preempt: false` です。backup が VIP を持った後は、その node が落ちるか明示的に
移動するまで保持します。完全な resource fragment は
`examples/vrrp-tuning-presets.yaml` を参照してください。

`BGPPeer.spec.password` は FRR 設定に `neighbor ... password ...` として描画されます。
本番構成では routerd YAML に共有 secret を残さないため、`BGPPeer.spec.passwordFrom`
を優先してください。`passwordFrom.file` は local root-owned secret file、
`passwordFrom.env` は環境変数を読み、`base64: true` で base64 値を decode します。
生成済み FRR config は secret として扱ってください。
FRR の listen address 制限は managed FRR config の通常 stanza ではなく、bgpd 起動時の
`-l` / `--listenon` option です。特定 interface に限定したい場合は firewall zone と
service-manager 側の bgpd option を合わせて管理してください。


`IngressService` は複数 backend、TCP health check、failover policy を扱います。
runtime controller が backend FQDN を解決し、DNS 失敗時は直前の解決済み IPv4 を
fallback として使います。Linux nftables は次回 NAT reconcile で status の
active backend を転送先に使います。既存 conntrack は消さないため、既存 flow は
旧 backend に残り、新規 flow が選択済み backend に向かいます。`spec.hostname` は
listen address の A record として DNSResolver に自動反映できます。外部 DNS が名前を
管理する場合は `spec.externalDNS: true` を設定してください。
`routerctl show ingress` は active backend と backend ごとの health を表示します。
`routerctl show ingress --verbose` は live dataplane の `ip_forward`、nftables
DNAT/SNAT rule 数、該当 conntrack flow 数も表示します。`DETAIL` column には
`hairpinMode`、hairpin が必要か、期待される nftables SNAT rule が present/missing
のどちらかも出します。Ingress、NAT 系、DS-Lite、IPv6 PD/RA、routing resource から、
forwarding、redirect suppression、reverse path filter exception、interface ごとの RA 受信など
必要な runtime sysctl を導出します。`routerd apply --once` は派生設定を plan / render しますが、
host 変更は明示的な `Sysctl` / `SysctlProfile` escape hatch だけに限定します。
派生 runtime 設定の適用は `routerd serve` の controller reconcile が担当します。
保守中は `routerctl drain
ingress/<service> backend=<name> --duration 10m` で backend を runtime state 上の
drain 状態にできます。controller は duration が切れるか `routerctl undrain
ingress/<service> backend=<name>` で解除されるまで、該当 backend を reason `Drained`
の unhealthy として扱います。

`LocalServiceRedirect` は Linux nftables の `prerouting` に `redirect` rule を生成します。
指定した interface から入ってきた packet と `IPAddressSet` 宛先だけを対象にします。
router 自身が発信する通信や health check はこの hook を通りません。

例:

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

DS-Lite、IPv4 既定経路、NAT44 は実 lab で動作確認済みです。

## 状態連携

| Kind | 役割 |
| --- | --- |
| `HealthCheck` | `routerd-healthcheck` または開発用の組み込み実行器で到達性を測ります。`sourceInterface` はネットワークリソース名を受け取り、実行時に OS のインターフェース名へ解決します。`via`、`fwmark`、`sourceAddress`、`sourceAddressFrom` も指定できます。 |
| `EgressRoutePolicy` | 準備完了の候補から重みの高い外向き経路を選びます。`destinationCIDRs` と candidate の `gatewaySource`、`gateway` を持ちます。 |
| `EventRule` | イベント列に対して all_of、any_of、sequence、window、absence、throttle、debounce、count を評価します。 |
| `DerivedEvent` | 複数リソースの状態から仮想イベントを発行します。 |
| `SelfAddressPolicy` | 自ホストアドレスの選択方針を表します。 |

`HealthCheck.spec.disabled` を `true` にすると、daemon ユニットは生成しますが停止・無効化します。
`EgressRoutePolicy` の候補にも `disabled: true` を指定できます。
無効化した候補は、最後の観測状態が Healthy のままでも選択されません。

## `spec.when`

`spec.when` を持つ resource は、routerd の local state store に対する predicate が一致したときだけ有効になります。既存の単一 predicate 構文は引き続き使えます。

```yaml
when:
  state:
    wan.ipv6.mode:
      equals: pd-ready
```

AND は `all`、OR は `any` で表します。任意の深さで nest できます。

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

各 `when` node は `state`、`all`、`any` のどれか 1 つだけを持ちます。
`state` は state variable 名を key にし、`exists`、`equals`、`in`、`contains`、
`status`、`for` で照合します。1 要素の `all` は単一 predicate 構文と等価です。
状態管理専用の resource kind は公開しません。条件付き activation は依存する resource の
`spec.when` に直接書きます。

`HealthCheck.spec.sourceInterface` は実行時に OS のインターフェース名へ解決されます。
Linux では `SO_BINDTODEVICE` を使います。`fwmark` を指定した場合は
`SO_MARK` も設定します。`HealthCheck` が `EgressRoutePolicy` の candidate や
target から参照されている場合は、routerd がその route target の mark から
`SO_MARK` を自動導出します。
直接の `fwmark` 指定は、route target に紐づかない低レベルな probe 向けです。
FreeBSD では、指定したインターフェースから送信元アドレスを選びます。
FreeBSD には Linux と同じ socket option がないためです。

## システム

| Kind | 役割 |
| --- | --- |
| `Hostname` | ホスト名を管理します。 |
| `Sysctl` | sysctl 値を管理します。 |
| `NTPClient` | NTP クライアント設定を管理します。`serverFrom` で `DHCPv4Client.status.ntpServers` や `DHCPv6Information.status.sntpServers` を参照できます。 |
| `LogSink` | ログ送信先を表します。 |
| `WebConsole` | 状態、イベント、IPv4/IPv6 コネクション観測を表示する読み取り専用画面です。 |

`WebConsole.spec.listenAddressFrom` は、ほかのリソース状態から HTTP 待ち受けアドレスを導出します。
たとえば、`Interface/mgmt.status.ipv4Addresses` を参照できます。
管理アドレスを DHCP、IPAM、別の宣言リソースから得る場合は、固定の `listenAddress` ではなくこちらを使います。

## ファイアウォール

| Kind | 役割 |
| --- | --- |
| `FirewallZone` | インターフェースをゾーンへ割り当て、`untrust`、`trust`、`mgmt` の役割を設定します。 |
| `FirewallPolicy` | 拒否ログなど、全体の設定を表します。 |
| `FirewallRule` | 役割の組み合わせでは表せない例外を表します。送信元 CIDR、宛先 CIDR、`IPAddressSet` 宛先参照で範囲を絞れます。 |
| `ClientPolicy` | MAC アドレスでクライアントを分類し、Linux nftables でゲスト隔離を行います。 |
| `PortForward` | 単一宛先の ingress DNAT ルールを追加します。routerd が firewall table も管理している場合は内部の forward accept も生成します。任意の hairpin mode では LAN 側 DNAT と戻り経路 SNAT も生成します。 |
| `IngressService` | `PortForward` と同じ ingress DNAT を追加します。複数 backend、選択方針、health check intent を受け付け、runtime failover state は controller path で扱います。任意の hairpin mode も `PortForward` と同じです。 |
| `LocalServiceRedirect` | 明示的な `IPAddressSet` 宛ての通信を local service へ redirect します。firewall renderer は送信元 zone から該当 local input port への開口も生成します。 |

状態を持つフィルタは nftables の `inet routerd_filter` テーブルに生成します。
確立済み通信、loopback、必要な ICMPv6 は常に許可します。
DHCP、DNS、DS-Lite などに必要な開口は routerd が内部で生成します。

`ClientPolicy` は `mode: include` で「一覧に書いた MAC アドレスを guest」として扱います。
`mode: exclude` では「一覧に書いた MAC アドレスを trusted」とし、対象インターフェース上の残りを guest として扱います。
`spec.macs` は短縮形で、`classification[]` は名前や予約参照を残したい場合に使います。
`spec.isolation` では internet 許可、LAN/mgmt 拒否、mDNS/SSDP/NetBIOS discovery 拒否といった典型的な guest intent を表現できます。
FreeBSD pf は同じ MAC ベースの routed filtering モデルを持たないため、このリソースは FreeBSD では未対応として扱います。

## 名前変更の要点

Phase 1.6 で次の名前に整理しました。

| 旧名 | 現在の名前 |
| --- | --- |
| `IPv4DHCPServer` | `DHCPv4Server` |
| `IPv4DHCPReservation` | `DHCPv4Reservation` |
| `IPv4DHCPScope` | `DHCPv4Server` |
| `IPv6DHCPAddress` | `DHCPv6Address` |
| `IPv6PrefixDelegation` | `DHCPv6PrefixDelegation` |
| `IPv6DHCPServer` / `IPv6DHCPv6Server` | `DHCPv6Server` |
| `IPv6DHCPScope` | `DHCPv6Server` |
| `DHCPRelay` | `DHCPv4Relay` |

バイナリ名も `routerd-dhcpv4-client`、`routerd-dhcpv6-client` です。
