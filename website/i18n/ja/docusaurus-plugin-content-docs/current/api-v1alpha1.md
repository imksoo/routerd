---
title: リソース API v1alpha1
slug: /reference/api-v1alpha1
---

# リソース API v1alpha1

routerd の設定は、最上位の `Router` と、型付きリソースの一覧で構成します。
このページは、現在の実装に合わせたリソース一覧です。
Phase 1.6 以降は RFC の表記に合わせ、DHCP 関連の Kind は `DHCPv4*` と `DHCPv6*` を使います。
旧名の互換用別名はありません。

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
| `net.routerd.net/v1alpha1` | インターフェース、`ManagementAccess`、再利用可能な `IPAddressSet`、DHCP、DNS、経路、トンネル、VIP、BGP、イベント、通信フローログ |
| `firewall.routerd.net/v1alpha1` | `FirewallZone`, `FirewallPolicy`, `FirewallRule`, `FirewallEventLog`, `ClientPolicy`, `PortForward`, `IngressService`, `LocalServiceRedirect` |
| `system.routerd.net/v1alpha1` | `Hostname`, `Sysctl`, `SysctlProfile`, `Package`, `NTPClient`, `NTPServer`, `LogSink`, `ObservabilityPipeline`, `RouterdCluster`, `LogRetention`, `WebConsole` |
| `plugin.routerd.net/v1alpha1` | プラグインマニフェスト |
| `hybrid.routerd.net/v1alpha1` | `OverlayPeer`, `HybridRoute`, `AddressMobilityDomain`, `CloudProviderProfile`, `RemoteAddressClaim` |
| `mobility.routerd.net/v1alpha1` | `MobilityPool` |

## システム準備

| Kind | 役割 |
| --- | --- |
| `Package` | 他の resource から導出できない OS package だけを補う、限定的な override です。通常の runtime dependency は自動で導出されます。 |
| `Sysctl` | router resource からまだ導出できない sysctl 値を補う、限定的な escape hatch です。`compare: exact` と `compare: atLeast` で読み戻しの判定を選べます。 |
| `SysctlProfile` | ルーター向けの sysctl 推奨値を補う、限定的な escape hatch です。通常の router sysctl は自動で導出されます。 |
| `Hostname` | ホスト名を設定します。 |
| `NTPClient` | OS の NTP クライアントを有効にします。DHCPv4 / DHCPv6 の状態から時刻サーバーを導出し、空なら public NTP サーバーへ戻せます。 |
| `NTPServer` | LAN 向けのローカル NTP サーバーを動かします。クライアント許可範囲は、静的な `allowCIDRs` に加えて、`allowCIDRFrom` で `IPv6DelegatedAddress/<name>.address` や `DHCPv6PrefixDelegation/<name>.currentPrefix` などの status field から導出できます。 |
| `LogSink` | log event を syslog、OTLP、webhook、file、journald へ転送します。 |
| `ObservabilityPipeline` | OTLP の environment と、stdout / syslog / Loki への routerd event の転送を設定します。 |
| `RouterdCluster` | file lease により、leader だけが host configuration を変更し、standby は status 観測に回ります。 |
| `LogRetention` | イベント、DNS、通信フロー、firewall event log の保管期間を管理します。 |
| `WebConsole` | 読み取り専用の Web 画面を、管理ネットワークで待ち受けます。 |

## インターフェース

| Kind | 役割 |
| --- | --- |
| `Interface` | routerd が扱う安定した名前と OS のインターフェース名を結び付け、下流 resource 向けの link/address status も提供します。 |
| `ManagementAccess` | 管理用インターフェースと apply 前の lockout チェックを宣言します。宣言時は、管理 IF の欠落、firewall zone による遮断、WebConsole の全アドレス待ち受けを検出すると、`--allow-mgmt-lockout` なしの apply を止めます。 |
| `PPPoESession` | PPPoE 用の下位インターフェース設定を表します。 |
| `PPPoESession` | `routerd-pppoe-client` が管理する PPPoE セッションです。 |
| `WireGuardInterface` | WireGuard インターフェースを表します。 |
| `WireGuardPeer` | WireGuard の相手を表します。 |
| `TailscaleNode` | Tailscale ノードを設定します。Exit node と subnet router の広告を管理対象 systemd ユニットで行います。 |
| `IPsecConnection` | strongSwan の cloud VPN 向け接続定義を表します。 |
| `VRF` | Linux VRF デバイスと経路表を表します。 |
| `VXLANTunnel` | VXLAN トンネルを表します。 |

`PPPoESession.spec.enabled: false` にすると、PPPoE の定義は残したまま、管理対象の pppd ユニットを停止・無効化します。
通常運用では PPPoE セッション枠を使わず、必要なときだけ手動で試験するフォールバック経路として使えます。

`TailscaleNode` は、初回登録用に `authKey` を使えます。
本番設定では `authKeyEnv` と `authKeyFile` を推奨します。
これにより、秘密値を YAML と Git 履歴に残さずに済みます。
どちらも未指定の場合、`tailscaled` はログイン済みとみなします。
routerd は、広告するノード設定だけを再適用します。
Tailscale の既定 UDP/41641 は予約済みとして扱います。
WireGuard の待ち受けポートには、別の番号を使ってください。
詳しい設定手順は、Tailscale の設定ガイドを参照してください。

`WireGuardInterface` は `privateKeyFile` を受け取れます。
秘密鍵を router YAML の外に置くためです。
`WireGuardPeer` も、任意の PSK 用に `presharedKeyFile` を受け取れます。
インラインの鍵フィールドは、主に例とテスト向けです。
FreeBSD では、routerd が rc.d サービスを生成します。
そのサービスは `wg` インターフェースを作成し、ファイルから秘密鍵を読み込み、
宣言された peer と static address を適用します。

Kernel module と、systemd-networkd/resolved の adoption drop-in は、router resource から自動で導出されます。削除済みの `KernelModule`、`NetworkAdoption`、`Link`、`NixOSHost` が config に残っている場合、routerd は黙って無視せず、エラーを返します。

## WAN アドレスと委任

| Kind | 役割 |
| --- | --- |
| `IPv4StaticAddress` | 静的 IPv4 アドレスを付与します。 |
| `VirtualAddress` | IPv4 `/32` または IPv6 `/128` VIP を宣言します。`spec.family` は `ipv4` または `ipv6` です。`mode: vrrp` は Linux では keepalived、FreeBSD では CARP を使います。 |
| `DHCPv4Client` | `routerd-dhcpv4-client` が DHCPv4 リース、IPv4 アドレス、任意のデフォルト経路を管理します。 |
| `DHCPv6Address` | DHCPv6 IA_NA の意図を表します。 |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` が管理する DHCPv6-PD リースです。 |
| `DHCPv6Information` | DHCPv6 情報要求の結果です。DNS、SNTP、ドメイン検索、AFTR 情報を観測します。 |
| `IPv6DelegatedAddress` | 委任プレフィックスから LAN 側アドレスを導出します。 |
| `IPv6RAAddress` | RA/SLAAC で得る IPv6 アドレスを表します。 |

`DHCPv6PrefixDelegation` は、旧来の OS クライアント選択フィールドを持ちません。
DHCPv6-PD は `routerd-dhcpv6-client` が担当します。

## LAN 側サービス

| Kind | 役割 |
| --- | --- |
| `DHCPv4Server` | dnsmasq の DHCPv4 service と任意のアドレスプールを提供します。 |
| `DHCPv4Reservation` | MAC アドレスごとの固定割り当てを表します。 |
| `DHCPv4Relay` | dnsmasq の DHCPv4 中継を表します。 |
| `IPv6RouterAdvertisement` | RA、PIO、RDNSS、DNSSL、M/O フラグ、MTU、優先度、寿命を生成します。 |
| `RogueRADetector` | RA を送出する interface 上で観測された、自身以外の IPv6 Router Advertisement を status として表示する、自動導出の resource です。 |
| `DHCPv6Server` | dnsmasq の DHCPv6/RA service です。`stateless`、`stateful`、`both`、`ra-only` を扱います。 |
| `DNSZone` | ローカル権威ゾーンを表します。手動レコードと DHCP リース由来のレコードを扱います。 |
| `DNSResolver` | `routerd-dns-resolver` の daemon instance、待ち受け、cache、metrics、query log を表します。 |
| `DNSForwarder` | 1 つの resolver に対する DNS の match rule です。`DNSZone` を応答するか、名前付きの `DNSUpstream` へ転送します。 |
| `DNSUpstream` | `udp`、`tcp`、`dot`、`doh` のいずれかで、1 つの上流 endpoint を表します。状態由来の address、bootstrap resolver、TLS 名、送信元 interface も指定できます。 |

Android は DHCPv6 の DNS だけでは名前解決を完結できないため、IPv6 LAN では `IPv6RouterAdvertisement.spec.rdnss` を設定します。

dnsmasq は、DHCPv4、DHCPv6、中継、RA だけを担当します。
DNS の待ち受けと応答は `DNSResolver` が担当します。
LAN の DNS suffix は、`DHCPv4Server.spec.domainFrom`、
`IPv6RouterAdvertisement.spec.dnsslFrom`、`DHCPv6Server.spec.domainSearchFrom`
から `DNSZone/<name>.zone` を参照することで、ローカルゾーンと一致させられます。
`DNSResolver.spec.listen[].sources` には、その listener が使う `DNSForwarder` 名を並べます。
省略した listener は、その resolver を参照するすべての `DNSForwarder` を使います。
user YAML の `DNSResolver.spec.sources` は受け付けません。旧来のインライン source は
`DNSForwarder` と `DNSUpstream` に分割してください。

`DNSForwarder.spec.match` には、`home.example` や、既定の上流を表す `.` を指定します。
`spec.zoneRefs` は local の `DNSZone` を応答し、`spec.upstreams` は `DNSUpstream` へ転送します。
DNSSEC validation は `DNSForwarder.spec.dnssecValidate` に書きます。

`DNSUpstream.spec.protocol` は `udp`、`tcp`、`dot`、`doh` のいずれかです。
`addressFrom` では、`DHCPv6Information/<name>.dnsServers` などから UDP の上流 address を導出できます。
`sourceInterface` は Linux で送信先 interface を束縛し、`bootstrap` は DoH/DoT の endpoint 名の解決に使う補助 resolver です。

## DS-Lite、経路、NAT

| Kind | 役割 |
| --- | --- |
| `DSLiteTunnel` | AFTR へ `ip6tnl` トンネルを張ります。AFTR は IPv6 を直接指定するか、FQDN、または DHCPv6 情報から得ます。 |
| `MobilityPool` | CloudEdge mobility の唯一の operator-authored intent です。pool prefix、federation group、node-to-site membership、member ごとの capture/delivery policy、owner 別の `deliveryTo`、provider action 向けの non-secret `capture.target`、lease policy を宣言し、routerd は observed federation event から `AddressLease` runtime state と SAM dynamic config を導出します。 |
| `IPAddressSet` | 直接指定したアドレスや FQDN から、再利用可能な IP address set を定義します。Linux nftables renderer はこれを named set として出力し、redirect、NAT、policy routing から参照できます。 |
| `IPv4Route` | IPv4 経路を追加します。DS-Lite 経由の既定経路や、明示的な破棄経路にも使います。 |
| `ClusterNetworkRoute` | Kubernetes の Pod / Service CIDR を、worker の next hop 経由の static IPv4 route に展開します。 |
| `BGPRouter` | ローカルの BGP router を宣言します。現在の backend は長寿命の `routerd-bgp` GoBGP daemon で、import policy は default deny です。 |
| `BGPPeer` | `BGPRouter` にぶら下がる、GoBGP 管理の BGP peer を宣言します。Kubernetes BGP speaker などに使います。 |
| `BFD` | BFD session の intent を宣言します。GoBGP backend では、FRR なしの BFD 実装が入るまで unsupported として報告します。 |
| `NAT44Rule` | nftables の `routerd_nat` テーブルで IPv4 NAPT を行います。 |
| `PortForward` | WAN 側の IPv4 TCP/UDP ポートを、1 つの内部 IPv4 宛先へ DNAT します。 |
| `IngressService` | WAN 側の IPv4 TCP/UDP サービスを公開します。複数 backend、TCP/HTTP health check、`failover` / `sourceHash` / `random` selection を受け付けます。 |
| `LocalServiceRedirect` | LAN 側 client から `IPAddressSet` 宛てに出る IPv4/IPv6 通信を、router の local port へ redirect します。平文 DNS/NTP の集約を想定し、DoH や DoT の port には触れません。 |
| `EgressRoutePolicy` | 既定経路の選択、mark ベースの IPv4 policy routing、複数 target への hash 分散を表します。 |

`MobilityPool.spec.capturePolicy.deprovisionHoldDuration` は、生成済みの cloud
capture がこの node の desired capture set から外れた後、provider 側の
de-provision action plan を出すまでの待ち時間です。

`EgressRoutePolicy` は、CIDR 指定に加えて `destinationSetRefs` と
`excludeDestinationSetRefs` を持ちます。これにより、FQDN-backed な宛先 set を policy
resource にアドレス展開せず、経路制御や除外条件として使えます。
`mode: priority` は既定経路の failover、`mode: mark` は 1 つの mark 付き route
table、`mode: hash` または `candidates[].targets` は複数 route table への
source/destination の hash 分散に使います。

routerd は、reverse path filter sysctl、tunnel MTU、RA MTU、TCP MSS clamp を
router role、tunnel、firewall zone、RA/DHCPv6 resource から自動で導出します。
config では LAN/WAN と tunnel の intent を宣言し、`IPv4ReversePathFilter` や
`PathMTUPolicy` は書きません。
`tailscale0` のように外部が管理する source interface が低い MTU を持つ場合は、
`Interface.spec.mtu` を設定します。routerd はその source path にだけ使い、
無関係な LAN path を低い MTU に引っ張りません。

`EgressRoutePolicy` は `excludeDestinationCIDRs` を持ちます。これにより、LAN 内部、管理網、HGW LAN、RFC 1918 の内部網などを policy routing の対象から外せます。

`ClusterNetworkRoute` は、Kubernetes node 向けの補助 resource です。
`spec.pods.cidrs` と `spec.services.cidrs` に Pod / Service CIDR を並べ、
`spec.via[]` に worker または VIP の next hop を指定すると、routerd は
対応する `IPv4StaticRoute` の intent を生成します。同じ weight は同じ metric
として扱われ、複数 next hop の ECMP に使えます。異なる weight は metric の差に
変換され、優先経路とフォールバック経路を表します。

`FirewallRule` は、宛先 CIDR に加えて `destinationSetRefs` と
`excludeDestinationSetRefs` を持ちます。これにより、再利用可能な FQDN-backed set
を各 rule にアドレス展開せず、許可・拒否・reject の条件として使えます。
stateful rule expression は、`sourcePorts`、`destinationPorts`、ICMP / ICMPv6 の
type matching、`rateLimit`、`connLimit` も扱えます。`port` は単一の
destination port の shorthand として引き続き受け付けますが、新しい例では
`destinationPorts` を使います。

`NAT44Rule` は、`outboundInterface`、`sourceCIDRs`、`translation` による単純な
source NAT と、`type`、`egressInterface` または `egressPolicyRef`、`sourceRanges`
による policy-aware NAT を扱います。さらに `destinationCIDRs`、`destinationSetRefs`、
`excludeDestinationCIDRs`、`excludeDestinationSetRefs` を持ちます。これにより、
インターネット向け通信だけをマスカレードし、静的経路を持つプライベート宛先や
再利用可能な address set は NAT しない構成にできます。

`PortForward` と `IngressService` は、Linux nftables と FreeBSD pf に DNAT を生成します。
`spec.hairpin.enabled: true` と `spec.hairpin.interfaces` を指定すると、LAN
クライアントから WAN アドレス経由で同じサービスへ到達するための hairpin NAT も生成します。
hairpin には `listen.address` または `listen.addressFrom` が必須で、routerd は LAN 側の
DNAT と、戻り経路用の masquerade/NAT reflection を生成します。
`listen.addressFrom` と backend の `addressFrom` は、`IPv4StaticAddress/<name>.address`
や `VirtualAddress/<name>.address` のような、静的に描画できるアドレスリソースを参照できます。
`IngressService` では、`spec.hairpin.mode` の未指定を `auto` として扱います。
listen address と選択済み backend が、listen interface に宣言された同じ prefix 上に
ある場合、routerd は、LAN client が VIP を使うために必要な、同一 interface の戻り
SNAT を自動生成します。YAML に listen interface の prefix が宣言されていない場合でも、
private IPv4 の listen/backend address が同じ `/24` にあれば、hairpin が必要と判断します。
これは Live ISO のように、boot 環境から interface address を引き継ぐ構成をカバーするためです。
抑止する場合は `spec.hairpin.mode: off`、明示指定する場合は `manual` と `interfaces` を使います。
`VirtualAddress.spec.vrrp.authentication` は、keepalived では `auth_pass`、
FreeBSD CARP では `pass` として描画されます。本番構成では routerd YAML に
共有 secret を残さないため、`VirtualAddress.spec.vrrp.authenticationFrom`
を優先してください。`authenticationFrom.file` は local の secret file、
`authenticationFrom.env` は環境変数を読み、`base64: true` で base64 値を
decode します。生成済みの keepalived/CARP 設定や host interface state は secret
として扱ってください。
VRRP authentication は VRRPv3（RFC 5798）では deprecated です。routerd は L2 隔離を前提にするため、
authentication は、周辺ネットワークの方針で必要な場合や、単純な誤設定対策に限って使ってください。
`IngressService` は、複数 backend、TCP/HTTP health check、`failover`、`sourceHash`、
`random` selection を受け付けます。runtime controller は backend の FQDN を解決し、
DNS が一時的に失敗した場合は、直前の解決済み IPv4 にフォールバックします。healthy なバックエンドが
複数ある場合、Linux nftables は `sourceHash` では `jhash ip saddr`、`random` では
`numgen random` で分配します。healthy な backend が 1 つだけになった場合は failover に
降格します。validator は、`IngressService`、`LocalServiceRedirect`、routerd 管理 daemon の
listen port が同じ interface/protocol で衝突する設定を拒否します。

`IPAddressSet` は、直接指定した IPv4/IPv6 address を apply 時に nftables の named set へ
出力します。FQDN の `A`/`AAAA` record は runtime controller が解決し、参照されている
set を、firewall、NAT、policy table 全体を reload せずにその場で更新します。次回の更新は、
観測した最小 DNS TTL の半分を基本とし、60 秒より短くはしません。`refreshInterval` は、
より積極的に更新したい場合の上限として使えます。

`IPAddressSet.spec.names` は、完全一致の DNS 名だけを扱います。`microsoft.com` は
`microsoft.com` 自体の `A`/`AAAA` record を意味し、`www.microsoft.com`、
`login.microsoft.com`、`*.microsoft.com`、さらに深いサブドメインは含みません。
ワイルドカードや suffix 形式のサービス判定には、単純な FQDN 解決ではなく、
DNS query の観測や provider endpoint feed を扱う、別のリソースが必要です。

`BGPRouter` と `BGPPeer` は、長寿命の `routerd-bgp` daemon を使います。
routerd は resource spec を local gRPC Unix socket 経由で型付きの GoBGP API object に
直接 map し、`ListPeer` と `ListPath` で status を観測します。FRR の text config、
`frr-reload.py`、`vtysh` の parse、GoBGP の file config は使いません。
`apply --once` は host artifact の render だけを行い、
BGP は `routerd serve` の管理として status に出します。`routerctl show bgp` は、保存された
GoBGP の観測から router、peer、message counter、route selection state、直近の error を
表示します。prefix status には、`best`、`valid`、`installed`、`stale`、`nextHop`、
observed community が含まれます。`spec.importPolicy.allowedPrefixes` に一致する
学習済みの IPv4 best path は、routerd 所有の protocol/metric で kernel FIB に投入されます。
既定では、GoBGP import policy が受理した eBGP next-hop を、学習元の peer address に
書き換えます（`spec.importPolicy.nextHopRewrite: peer-address`）。これは旧 FRR の
`set ip next-hop peer-address` と同じ意味で、広告 next-hop が downstream speaker を
指す Kubernetes edge 経路でも、peer address の ECMP として投入できます。広告された next-hop を
そのまま kernel に入れたい場合だけ、`nextHopRewrite: unchanged` を指定してください。
同一 prefix の equal best path は、ECMP の next-hop として入ります。

`BGPRouter.spec.convergenceProfile: fast` は、graceful restart の stale-path 保持よりも
速い収束を優先する Kubernetes/edge router 向けです。fast profile は peer timer を
短くし、`spec.gracefulRestart.enabled` が明示されていない場合は graceful restart を
無効化します。import policy は default deny です。Kubernetes LoadBalancer pool など、
受け入れたい prefix を `spec.importPolicy.allowedPrefixes` に列挙してください。
`BGPPeer.spec.ebgpMultihop` は、loopback peering や、lab から本番 router への
検証のように、直結でない eBGP session に使います。未指定、`0`、`1` は、直結 eBGP
の既定動作です。`2` から `255` を指定すると、その peer group の GoBGP multihop
TTL として設定します。
router ID は TCP の source address と同一である必要はありませんが、peer 側には、実際に
host が使う BGP source address を設定する必要があります。LAN に複数の address がある
場合は、Linux なら `ip route get <peer-address>` で source address を確認し、明確な理由が
なければ router ID もその運用上の source address に寄せると、混乱を避けられます。

`BGPRouter` は、connected/static IPv4 route を個別の `allowedPrefixes` 付きで広告できます。
`BGPRouter.spec.exportPolicy.allowedPrefixes` または redistribute の allow-list に明示された
prefix だけが、GoBGP の local path として追加されます。BGP community policy は router または
peer に `communities.send`、`communities.accept`、`communities.set.in/out` として宣言でき、
GoBGP が報告する observed route community は status に保存されます。watcher は既定で
15 秒間隔、prefix status は 4096 entries が上限です。`BGPRouter.spec.watcher` で
`pollInterval`、`maxPrefixes`、`peerStateChangeThrottle` を調整できます。validation は、
3 秒未満の interval と、1,000,000 以上の prefix cap を拒否します。GoBGP MVP は
router ごとに 1 つの `BGPRouter` を support し、`spec.vrf` は未対応です。
multi-router、VRF、BFD resource は、黙って無視せず Pending として報告します。
`spec.listen.address` と `spec.listen.port` は、`routerd-bgp` の GoBGP listener を bind します。

`VirtualAddress` の `mode: vrrp` は、Linux では keepalived、FreeBSD では CARP を使います。
`spec.family: ipv4` は IPv4 `/32`、`spec.family: ipv6` は IPv6 `/128` を要求します。
IPv6 VIP は keepalived VRRPv3 の
`family inet6` として描画され、FreeBSD では `inet6` の CARP alias になります。
Linux VRRP は明示的な unicast peer を使い、既定は `nopreempt` です。
FreeBSD CARP は親 interface 上の multicast advertisement を使うため、
`spec.vrrp.peers` は FreeBSD では無視されます。`preempt: true` は、自動 failback が必要な場合だけ使います。advertisement や
failback の低レベルな timing は、resource ごとの field ではなく、routerd の profile 既定値で扱います。`track` で `BGPRouter`、`BGPPeer`、`IngressService`
などの状態に応じて priority を下げられます。既定では、unhealthy が 3 回連続で penalty
を適用し、healthy が 2 回連続で解除します。`spec.hostname` は、DNSResolver が配信する
対応する `DNSZone` へ VIP を自動公開できます。IPv4 VIP は A record、IPv6 VIP は AAAA
record になります。外部の AD DNS などが名前を管理する場合は、
`spec.externalDNS: true` を設定してください。routerd は hostname 構文だけを検証し、
DNSZone の coverage warning と自動公開を行いません。`routerctl show vrrp` は、role、
priority、peer、transition からの経過時間を表示します。

### VRRP production tuning

制御プレーン VIP のように自動 failback が必要な場所だけ、`preempt: true` を
使います。家庭 LAN や DS-Lite 周辺の VIP では、優先 owner に戻すことよりも安定性を
優先し、既定の non-preemptive な挙動を使うのが扱いやすいです。backup が VIP を
持ったあとは、その node が落ちるか、明示的に移動するまで保持します。完全な resource fragment は
`examples/vrrp-tuning-presets.yaml` を参照してください。

`BGPPeer.spec.password` は、GoBGP peer の TCP MD5 authentication password として
渡されます。
本番構成では routerd YAML に共有 secret を残さないため、`BGPPeer.spec.passwordFrom`
を優先してください。`passwordFrom.file` は local の root-owned secret file、
`passwordFrom.env` は環境変数を読み、`base64: true` で base64 値を decode します。


`IngressService` は、複数バックエンド、TCP のヘルスチェック、フェイルオーバー方針を扱います。
ランタイムコントローラーがバックエンドの FQDN を解決し、DNS 失敗時は直前の解決済み IPv4 を
フォールバックとして使います。Linux の nftables は、次回の NAT 調整時に、status の
active backend を転送先に使います。既存の conntrack は消さないため、既存の flow は
旧 backend に残り、新規の flow が選択済みの backend へ向かいます。`spec.hostname` は、
listen address の A record として DNSResolver に自動反映できます。外部の DNS が名前を
管理する場合は、`spec.externalDNS: true` を設定してください。
`routerctl show ingress` は、active backend と backend ごとの health を表示します。
`routerctl show ingress --verbose` は、live dataplane の `ip_forward`、nftables の
DNAT/SNAT rule 数、該当する conntrack flow 数も表示します。`DETAIL` column には、
`hairpinMode`、hairpin が必要か、期待される nftables SNAT rule が present/missing
のどちらか、も出します。Ingress、NAT 系、DS-Lite、IPv6 PD/RA、routing resource から、
forwarding、redirect suppression、reverse path filter exception、interface ごとの RA 受信など、
必要なランタイムの sysctl を導出します。`routerd apply --once` は派生設定をプランしレンダリングしますが、
ホスト側への変更は明示的な `Sysctl` / `SysctlProfile` のエスケープハッチだけに限定します。
派生したランタイム設定の適用は、`routerd serve` のコントローラー調整ループが担当します。
保守中は `routerctl drain
ingress/<service> backend=<name> --duration 10m` で、backend を runtime state 上の
drain 状態にできます。controller は、duration が切れるか `routerctl undrain
ingress/<service> backend=<name>` で解除されるまで、該当する backend を reason `Drained`
の unhealthy として扱います。

`LocalServiceRedirect` は、Linux nftables の `prerouting` に `redirect` rule を生成します。
指定した interface から入ってきた packet と、`IPAddressSet` 宛先だけを対象にします。
router 自身が発信する通信や health check は、この hook を通りません。

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

DS-Lite、IPv4 既定経路、NAT44 は、実際の lab で動作確認済みです。

## 状態連携

| Kind | 役割 |
| --- | --- |
| `HealthCheck` | target、protocol、cadence、threshold から到達性 probe の intent を宣言します。`EgressRoutePolicy` の candidate/target から参照されると、routerd が health-check daemon、source binding、socket mark を自動で導出します。 |
| `EgressRoutePolicy` | 準備完了の候補の中から、重みの高い外向き経路を選びます。`destinationCIDRs` と、candidate の `gatewaySource`、`gateway` を持ちます。 |
| `EventRule` | イベント列に対して、all_of、any_of、sequence、window、absence、throttle、debounce、count を評価します。 |
| `DerivedEvent` | 複数リソースの状態から仮想イベントを発行します。 |
| `SelfAddressPolicy` | 自ホストアドレスの選択方針を表します。 |

`HealthCheck.spec.enabled: false` にすると、daemon ユニットは生成しますが、停止・無効化します。
`EgressRoutePolicy` の候補にも `enabled: false` を指定できます。
無効化した候補は、最後の観測状態が Healthy のままでも選択されません。
`mode: priority` でも、candidate の `weight` が選択の第一キーで、`priority` は
tie-break と policy-rule の priority です。candidate を削除すると、ledger-owned な
policy-route の rule/table も削除されます。

## `spec.when`

`spec.when` を持つ resource は、routerd の local state store に対する predicate が一致したときだけ有効になります。従来の単一 predicate 構文も引き続き使えます。

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

各 `when` node は、`state`、`all`、`any` のどれか 1 つだけを持ちます。
`state` は state variable 名を key にし、`exists`、`equals`、`in`、`contains`、
`status`、`for` で照合します。要素が 1 つの `all` は、単一 predicate 構文と等価です。
状態管理専用の resource kind は公開しません。条件付きの activation は、依存する resource の
`spec.when` に直接書きます。

`HealthCheck.spec.sourceInterface` は、実行時に OS のインターフェース名へ解決されます。
Linux では `SO_BINDTODEVICE` を使います。`fwmark` を指定した場合は、
`SO_MARK` も設定します。`HealthCheck` が `EgressRoutePolicy` の candidate や
target から参照されている場合は、routerd がその route target の mark から
`SO_MARK` を自動で導出します。
直接の `fwmark` 指定は、route target に紐づかない低レベルな probe 向けです。
FreeBSD では、指定したインターフェースから送信元アドレスを選びます。
FreeBSD には、Linux と同じ socket option がないためです。

## システム

| Kind | 役割 |
| --- | --- |
| `Hostname` | ホスト名を管理します。 |
| `Sysctl` | sysctl 値を管理します。 |
| `NTPClient` | NTP クライアント設定を管理します。`serverFrom` で `DHCPv4Client.status.ntpServers` や `DHCPv6Information.status.sntpServers` を参照できます。 |
| `LogSink` | ログの送信先を表します。 |
| `WebConsole` | 状態、イベント、IPv4/IPv6 のコネクション観測を表示する、読み取り専用画面です。 |

`Telemetry` は、routerd 自身と管理対象 daemon の metrics / traces / logs を
OpenTelemetry の endpoint へ出すための resource です。`LogSink` は、運用イベント
や観測ログの転送経路を表します。OTLP へログ転送する場合は、collector の endpoint を
重複して書かず、`LogSink.spec.otlp.telemetryRef` で `Telemetry` を参照してください。

`WebConsole.spec.listenAddressFrom` は、ほかのリソースの状態から HTTP の待ち受けアドレスを導出します。
たとえば、`Interface/mgmt.status.ipv4Addresses` を参照できます。
管理アドレスを DHCP、IPAM、別の宣言リソースから得る場合は、固定の `listenAddress` ではなく、こちらを使います。

## Status Provides Contract

`addressFrom`、`gatewayFrom`、`dnsServerFrom`、`dependsOn[].field`
などの参照フィールドは、参照先の kind がこの contract で宣言した
field だけを参照できます。存在しない resource や、`provides` に無い field は、
validator がエラーにします。

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
| `MobilityPool` | `activeLeases` (int), `dynamicSource` (string), `expiredLeases` (int), `generatedActions` (int), `generatedClaims` (int), `groupRef` (string), `holdingLeases` (int), `leaseCount` (int), `phase` (string), `plannerPhase` (string), `prefix` (string), `projectedAt` (timestamp) |
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

## ファイアウォール

| Kind | 役割 |
| --- | --- |
| `FirewallZone` | インターフェースをゾーンへ割り当て、`untrust`、`trust`、`mgmt` の役割を設定します。 |
| `FirewallPolicy` | 拒否ログなど、全体の設定を表します。 |
| `FirewallRule` | 役割の組み合わせでは表せない例外を表します。送信元 CIDR、宛先 CIDR、`IPAddressSet` 宛先参照で範囲を絞れます。 |
| `ClientPolicy` | MAC アドレスでクライアントを分類し、Linux nftables でゲスト隔離を行います。 |
| `PortForward` | 単一宛先の ingress DNAT ルールを追加します。routerd が firewall table も管理している場合は、内部の forward accept も生成します。任意の hairpin mode では、LAN 側の DNAT と戻り経路の SNAT も生成します。 |
| `IngressService` | `PortForward` と同じ ingress DNAT を追加します。複数 backend、選択方針、health check の intent を受け付け、runtime の failover state は controller path で扱います。任意の hairpin mode も `PortForward` と同じです。 |
| `LocalServiceRedirect` | 明示的な `IPAddressSet` 宛ての通信を local service へ redirect します。firewall renderer は、送信元 zone から該当する local input port への開口も生成します。 |

状態を持つフィルターは、nftables の `inet routerd_filter` テーブルに生成します。
確立済みの通信、loopback、必要な ICMPv6 は常に許可します。
DHCP、DNS、DS-Lite などに必要な開口は、routerd が内部で生成します。

`ClientPolicy` は、`mode: include` では「一覧に書いた MAC アドレスを guest として扱う」動作です。
`mode: exclude` では「一覧に書いた MAC アドレスを trusted とし、対象インターフェース上の残りを guest として扱う」動作です。
`spec.macs` は短縮形です。`classification[]` は構造化された形式で、各 entry は
`mode: trusted|guest|isolated` と、`match.macs`、`match.ouiPrefixes`、
`match.hostnamePatterns`、`match.dhcpFingerprints` の selector を持ちます。
match field は OR として評価します。`ipv4Reservation` は、Ethernet source address を
直接 match できない platform で、address ベースの rendering を安定させるためにも使えます。
`spec.isolation` では、internet 許可、LAN/mgmt 拒否、mDNS/SSDP/NetBIOS discovery 拒否といった、典型的な guest の intent を表現できます。
FreeBSD pf は同じ MAC ベースの routed filtering モデルを持たないため、このリソースは FreeBSD では未対応として扱います。

## 管理面（Management plane）

`ManagementAccess` は、非 dry-run の `apply` で運用者が自分自身を締め出すのを
防ぐため、routerd が到達可能なまま保つべき管理インターフェースと管理元 CIDR を
宣言します。`ManagementAccess` が 1 つでも存在すると、適用前検証が以下を
確認し、`--allow-mgmt-lockout` 未指定なら **適用を中止**します。`validate` /
`plan` / `show` は影響を受けず、dry-run の `apply` は判定結果を表示するだけで中止しません。

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
  requireWebConsoleBound: true  # 既定値
```

適用前検証の項目は以下のとおりです。

| チェック | 失敗条件 |
| --- | --- |
| インターフェースの存在 | `interfaces[]` で宣言した IF が `Interface` リソースに存在しない（管理 IF が削除・改名されている）。 |
| ファイアウォール経由の自分宛て到達性 | `FirewallZone` が 1 つでも存在し（ファイアウォール有効）、宣言された管理 IF がロール `mgmt` / `trust` の `FirewallZone` に属していない。input チェーンの `policy drop` がルーター自身宛ての SSH を落とします。 |
| WebConsole の待ち受けアドレス | `WebConsole` が有効で `0.0.0.0` / `::` に bind されている。`requireWebConsoleBound: true`（既定）では不合格、`false` では警告として扱います。 |

同じチェックは `routerctl doctor mgmt` でも実行できます（apply はしない）。

`spec.allowSourceCIDRs` は現状**情報的**な扱いで（status と doctor 表示に使う）、firewall ガードによる強制はまだ行いません。

`--allow-mgmt-lockout` は**緊急用の上書き**フラグです。管理 IF を新 VLAN に移すなど、ブロックされる構成を意図的に適用する場合（かつ PVE console 等の復旧手段が用意できている場合）に限り使います。通常運用では使いません。

## 名前変更の要点

Phase 1.6 で、次のように名前を整理しました。

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

バイナリ名も、`routerd-dhcpv4-client`、`routerd-dhcpv6-client` です。
