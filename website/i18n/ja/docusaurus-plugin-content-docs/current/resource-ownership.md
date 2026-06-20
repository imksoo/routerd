---
title: リソース所有
slug: /reference/resource-ownership
---

# リソース所有と反映モデル

routerd は、ホスト上の構成物をリソースに対応付けて管理します。
どのリソースが何を作ったかを記録することで、差分の確認、削除、障害調査をしやすくします。

![desired resource、ledger row、object status、host inventory、teardown contract、skip protection、backup、audit event を示す owner-reference lifecycle GC 図](/img/diagrams/lifecycle-gc.png)

## 所有の種類

| 種類 | 意味 |
| --- | --- |
| 作成 | routerd が構成物を新しく作ります。 |
| 取り込み | 既存の構成物を routerd の管理対象として扱います。 |
| 観測 | routerd は状態を見るだけで変更しません。 |

安定した所有者識別子は `apiVersion/kind/name` です。
apply の世代番号は所有者キーに含めません。
同じリソースは世代が変わっても同じ所有者として、生成済みの構成物の置換や削除を行えます。
object status も所有者メタデータとライフサイクルクラスを持ち、古い状態の整理が apply と同じ判断を行えるようにします。

## 主な構成物

| リソース | ホスト側の構成物 |
| --- | --- |
| `Interface` | OS のインターフェース名と管理状態 |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` のソケット、リース、イベント |
| `DHCPv4Client` | `routerd-dhcpv4-client` のソケット、リース、イベント |
| `PPPoESession` | `routerd-pppoe-client` のソケット、状態、pppd/ppp 設定 |
| `HealthCheck` | `routerd-healthcheck` のソケット、状態、イベント |
| `DHCPv4Server` / `DHCPv6Server` / `IPv6RouterAdvertisement` | 管理対象の dnsmasq 設定 |
| `RogueRADetector` | 自動導出された `routerd-ra-observer` のソケット、受動的な IPv6 RA 観測、不正 RA イベント |
| `DNSZone` | `routerd-dns-resolver` のローカル権威ゾーン |
| `DNSResolver` | `routerd-dns-resolver` のソケット、状態、イベント、待ち受け設定 |
| `DNSForwarder` | `routerd-dns-resolver` の転送ルール。リゾルバー設定として生成されます |
| `DNSUpstream` | `routerd-dns-resolver` の上流エンドポイント。転送ルールとして生成されます |
| `DSLiteTunnel` | Linux の `ip6tnl` インターフェース |
| `TunnelInterface` | Linux の `ipip` / `gre` tunnel device。FOU/GUE mode では対応する `ip fou` listener port も ensure します |
| `SAMTransportProfile` | 生成された `TunnelInterface`、endpoint `/32` `IPv4Route`、`BGPPeer` を含む `DynamicConfigPart` |
| `MobilityPool` | 動的な SAM 捕捉/制御プレーンリソース、BGP `/32` advertisement、プロバイダー action plan、所有権の観測 |
| `RemoteAddressClaim` | 低レベル SAM 捕捉状態、proxy-ARP sysctl/neighbor 状態、プロバイダー secondary 捕捉状態、リソース固有の teardown |
| `IPAddressSet` | Linux の生成器が参照する nftables の IPv4/IPv6 named set |
| `IPv4Route` | カーネルの経路 |
| `ClusterNetworkRoute` | Pod / Service CIDR を指定した next hop 経由にする、生成済みの `IPv4StaticRoute` の意図 |
| `NAT44Rule` | nftables の `routerd_nat` テーブル |
| `PortForward` / `IngressService` | Linux では nftables の `routerd_nat` / `routerd_filter` の DNAT と、任意の hairpin SNAT。FreeBSD では `pf.conf` の `rdr pass` と、任意の NAT reflection ルール |
| `BGPRouter` / `BGPPeer` | ローカルの GoBGP gRPC で制御する、長寿命の `routerd-bgp` デーモンの状態。学習した IPv4 の best path は、routerd が所有する protocol/metric でカーネル FIB に投入します |
| `BFD` | Linux FRR `bfdd` session 設定と、参照先 GoBGP peer の BFD 観測状態 |
| `VirtualAddress` | `ip addr` / `ifconfig` による静的 VIP、または Linux の keepalived / FreeBSD の CARP による VRRP/VRRPv3 の VIP 所有 |
| `ObservabilityPipeline` | プロセス内の routerd イベント exporter と、管理対象ユニット向けの OpenTelemetry 環境変数 |
| `RouterdCluster` | `spec.leasePath` のファイルリース。leader だけが apply とコントローラーの変更を実行します |
| `WireGuardInterface` / `WireGuardPeer` | WireGuard 設定 |
| `TailscaleNode` | `routerd-tailscale-<name>` のサービスユニット / スクリプトと、`tailscale up` の引数 |
| `VRF` | Linux の VRF デバイスと経路表 |
| `VXLANTunnel` | VXLAN デバイス |
| `Package` | パッケージの上書き設定。通常のホストパッケージの意図は、router リソースから自動導出します |
| `Sysctl` | sysctl の値 |
| `SysctlProfile` | 複数の sysctl 値 |
| 派生するホストランタイム | router リソースから導出する、カーネルモジュールの読み込み状態と systemd-networkd / resolved の drop-in |
| `generated service artifacts` | systemd ユニット、FreeBSD の rc.d スクリプトと、その有効化状態 |
| `NTPClient` | NTP クライアント設定 |

## lifecycle contract

すべての config resource kind は lifecycle registry に宣言されています。
宣言は resource class と、次のいずれか 1 つの teardown contract を持ちます。

- **`ArtifactKinds`**：resource が具体的な host artifact を ownership ledger に記録し、generic artifact teardown registry がその artifact kind の削除方法を知っています。
- **`TeardownLifecycle: resource`**：kernel route、WireGuard の adopted/external 保護、SAM proxy-ARP cleanup など、object status から resource 固有 teardown を実行します。
- **`NoHostTeardownReason`**：renderer input、external policy、dynamic source など、単独の host artifact を所有しない理由を明示します。

CI は、全 config kind が明示的な contract を持つことと、`ArtifactKinds` に書いた artifact kind が teardown registry に存在することを検査します。
新しい resource が cleanup を黙って迂回しないための guard です。

## 削除時の考え方

routerd は、知らない構成物を勝手に削除しません。
YAML からリソースが消えても、削除できるのは routerd が作成した、または明示的に取り込んだと分かる構成物だけです。

GC planner は current effective resource set、ownership ledger、object status、host inventory を比較し、dry-run 可能な plan を作ります。
plan には artifact removal、resource-specific teardown、ledger forget、stale status deletion、state backup、audit event が含まれます。

desired set は apply と serve が使う effective view です。
`FilterRouterByWhen`、dynamic SAM resource、`DynamicConfigPart` の merge 後を使うため、`when: false` の resource や、まだ profile が存在する SAM 生成 tunnel/BGP/route resource を orphan と誤判定しません。

`SAMTransportProfile` を削除すると、profile の dynamic part は空の active part に置き換わります。
その結果、生成された `TunnelInterface` / `BGPPeer` / endpoint route が effective config から消え、各生成 resource の owner に従って cleanup されます。

破壊的 cleanup は state backup と event 記録を伴います。
未対応 OS の integration は破壊せず skip します。
adopted または external managed の object status は、resource lifecycle GC の teardown 対象にしません。

現在は、完全なロールバック機能を目標にしていません。
本番ネットワークへ影響する変更では、次の順序を守ります。

1. 検証します。
2. 計画を確認します。
3. 予行実行します。
4. 管理用接続が消えないことを確認します。
5. 適用します。
6. 状態と疎通を確認します。

## 古い構成の扱い

フェーズ 4 で、旧 DHCPv6 実験用パッケージと旧生成器を削除しました。
現在の DHCPv6-PD は `routerd-dhcpv6-client` が所有します。
過去の `dhcpcd` や `dhcp6c` の経路に関する記述は、現在の設定例としては使いません。
