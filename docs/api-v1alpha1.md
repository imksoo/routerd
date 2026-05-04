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
| `net.routerd.net/v1alpha1` | インターフェース、DHCP、DNS、経路、トンネル、イベント |
| `firewall.routerd.net/v1alpha1` | `FirewallZone`, `FirewallPolicy`, `FirewallRule` |
| `system.routerd.net/v1alpha1` | `Hostname`, `Sysctl`, `Package`, `NTPClient`, `LogSink`, `NixOSHost` |
| `plugin.routerd.net/v1alpha1` | プラグインマニフェスト |

## システム準備

| Kind | 役割 |
| --- | --- |
| `Package` | OS ごとのパッケージ名を宣言し、不足していれば導入します。 |
| `Sysctl` | 実行時の sysctl 値を設定します。永続化も指定できます。 |
| `SysctlProfile` | ルーター向け sysctl 推奨値をまとめて設定します。 |
| `Hostname` | ホスト名を設定します。 |
| `NTPClient` | OS の NTP クライアントを有効にします。 |
| `LogSink` | routerd のイベントを syslog や外部プログラムへ送ります。 |

## インターフェースとリンク

| Kind | 役割 |
| --- | --- |
| `Interface` | routerd が扱う安定した名前と OS のインターフェース名を結び付けます。 |
| `Link` | 下流のリソースが参照するリンク状態を表します。 |
| `PPPoEInterface` | PPPoE 用の下位インターフェース設定を表します。 |
| `PPPoESession` | `routerd-pppoe-client` が管理する PPPoE セッションです。 |
| `WireGuardInterface` | WireGuard インターフェースを表します。 |
| `WireGuardPeer` | WireGuard の相手を表します。 |
| `IPsecConnection` | strongSwan の cloud VPN 向け接続定義を表します。 |
| `VRF` | Linux VRF デバイスと経路表を表します。 |
| `VXLANTunnel` | VXLAN トンネルを表します。 |

## WAN アドレスと委譲

| Kind | 役割 |
| --- | --- |
| `IPv4StaticAddress` | 静的 IPv4 アドレスを付与します。 |
| `DHCPv4Address` | 旧来のホスト DHCP クライアント経路です。新しい実装では `DHCPv4Lease` を優先します。 |
| `DHCPv4Lease` | `routerd-dhcpv4-client` が管理する DHCPv4 リースです。 |
| `DHCPv6Address` | DHCPv6 IA_NA を表す土台です。 |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` が管理する DHCPv6-PD リースです。 |
| `DHCPv6Information` | DHCPv6 情報要求の結果です。DNS、SNTP、ドメイン検索、AFTR 情報を観測します。 |
| `IPv6DelegatedAddress` | 委譲プレフィックスから LAN 側アドレスを導出します。 |
| `IPv6RAAddress` | RA で得る IPv6 アドレスの土台です。 |

`DHCPv6PrefixDelegation` は旧 OS クライアント選択フィールドを持ちません。
DHCPv6-PD は `routerd-dhcpv6-client` が担当します。

## LAN 側サービス

| Kind | 役割 |
| --- | --- |
| `DHCPv4Server` | dnsmasq で DHCPv4 アドレスプールを提供します。 |
| `DHCPv4Scope` | DHCPv4 のプール範囲を表します。 |
| `DHCPv4Reservation` | MAC アドレスごとの固定割り当てを表します。 |
| `DHCPv4Relay` | dnsmasq の DHCPv4 中継を表します。 |
| `IPv6RouterAdvertisement` | RA、PIO、RDNSS、DNSSL、M/O フラグ、MTU、優先度、寿命を生成します。 |
| `DHCPv6Server` | dnsmasq の DHCPv6 サーバーです。`stateless`、`stateful`、`both` を扱います。 |
| `DHCPv6Scope` | DHCPv6 の範囲を表します。 |
| `DNSZone` | ローカル権威ゾーンを表します。手動レコードと DHCP リース由来のレコードを扱います。 |
| `DNSResolver` | `routerd-dns-resolver` が管理する DNS 待ち受け、応答元、上流、キャッシュを表します。 |

Android は DHCPv6 の DNS だけでは名前解決を完結できないため、IPv6 LAN では `IPv6RouterAdvertisement.spec.rdnss` を設定します。

dnsmasq は DHCPv4、DHCPv6、中継、RA だけを担当します。
DNS の待ち受けと応答は `DNSResolver` が担当します。
`DNSResolver.spec.sources` では、ローカルゾーン、条件付き転送、既定の上流を優先順に並べます。
`https://` は DoH、`tls://` は DoT、`quic://` は DoQ、`udp://` は平文 DNS です。
`listen` は複数指定できます。
待ち受けごとに利用する `sources` の部分集合を選べます。
`sources[].viaInterface` は特定インターフェース経由の送信を指定します。
`sources[].bootstrapResolver` は DoH や DoT の名前解決に使う補助 DNS サーバーです。
DNSSEC は `DNSZone.spec.dnssec` と `DNSResolver.spec.sources[].dnssecValidate` で指定します。

## DS-Lite、経路、NAT

| Kind | 役割 |
| --- | --- |
| `DSLiteTunnel` | AFTR へ `ip6tnl` トンネルを張ります。AFTR は直接 IPv6、FQDN、または DHCPv6 情報から得ます。 |
| `IPv4Route` | IPv4 経路を追加します。DS-Lite 経由の既定経路にも使います。 |
| `NAT44Rule` | nftables の `routerd_nat` テーブルで IPv4 NAPT を行います。 |
| `IPv4SourceNAT` | 旧来の IPv4 送信元 NAT の土台です。 |
| `IPv4PolicyRoute` | IPv4 ポリシールーティングを表します。 |
| `IPv4PolicyRouteSet` | 複数のポリシールートをまとめます。 |
| `IPv4DefaultRoutePolicy` | 既定経路の方針を表します。 |
| `IPv4ReversePathFilter` | reverse path filter を表します。 |
| `PathMTUPolicy` | MTU と TCP MSS 調整の方針を表します。 |

Phase 1.5e では router05 で DS-Lite、IPv4 既定経路、NAT44 の実適用を確認しています。

## 状態連携

| Kind | 役割 |
| --- | --- |
| `HealthCheck` | `routerd-healthcheck` または開発用の組み込み実行器で到達性を測ります。`sourceInterface` はネットワークリソース名を受け取り、実行時に OS のインターフェース名へ解決します。`via` と `sourceAddress` も指定できます。 |
| `EgressRoutePolicy` | 準備完了の候補から重みの高い外向き経路を選びます。`destinationCIDRs` と candidate の `gatewaySource`、`gateway` を持ちます。 |
| `EventRule` | イベント列に対して all_of、any_of、sequence、window、absence、throttle、debounce、count を評価します。 |
| `DerivedEvent` | 複数リソースの状態から仮想イベントを発行します。 |
| `SelfAddressPolicy` | 自ホストアドレスの選択方針を表します。 |
| `StatePolicy` | 状態管理の方針を表します。 |

## システム

| Kind | 役割 |
| --- | --- |
| `Hostname` | ホスト名を管理します。 |
| `Sysctl` | sysctl 値を管理します。 |
| `NTPClient` | NTP クライアント設定を管理します。 |
| `LogSink` | ログ送信先を表します。 |
| `NixOSHost` | NixOS 宣言設定の生成に使います。 |

## ファイアウォール

| Kind | 役割 |
| --- | --- |
| `FirewallZone` | インターフェースをゾーンへ割り当て、`untrust`、`trust`、`mgmt` の役割を設定します。 |
| `FirewallPolicy` | 拒否ログなど、全体の設定を表します。 |
| `FirewallRule` | 役割の組み合わせでは表せない例外を表します。 |

状態を持つフィルタは nftables の `inet routerd_filter` テーブルに生成します。
確立済み通信、loopback、必要な ICMPv6 は常に許可します。
DHCP、DNS、DS-Lite などに必要な開口は routerd が内部で生成します。

## 名前変更の要点

Phase 1.6 で次の名前に整理しました。

| 旧名 | 現在の名前 |
| --- | --- |
| `IPv4DHCPAddress` | `DHCPv4Address` |
| `IPv4DHCPServer` | `DHCPv4Server` |
| `IPv4DHCPReservation` | `DHCPv4Reservation` |
| `IPv4DHCPScope` | `DHCPv4Scope` |
| `IPv6DHCPAddress` | `DHCPv6Address` |
| `IPv6PrefixDelegation` | `DHCPv6PrefixDelegation` |
| `IPv6DHCPServer` / `IPv6DHCPv6Server` | `DHCPv6Server` |
| `IPv6DHCPScope` | `DHCPv6Scope` |
| `DHCPRelay` | `DHCPv4Relay` |

バイナリ名も `routerd-dhcpv4-client`、`routerd-dhcpv6-client` です。
