---
title: routerd とは
slug: /concepts/what-is-routerd
sidebar_position: 1
---

# routerd とは

routerd は、Linux ホストをルーターとして動かすための宣言的な制御プレーンです。
NixOS と FreeBSD の土台も整備中です。
ルーターの意図を YAML リソースとして書きます。
routerd はその意図を、インターフェース、アドレス、DHCP、DNS、NAT、経路、
トンネル、ヘルスチェック、パッケージ、sysctl、サービスユニット、ログ、
状態へ反映します。

routerd はディストリビューションでも、集中管理サービスでもありません。
各ルーターホストの上でローカルに動きます。
systemd-networkd、dnsmasq、nftables、pppd、WireGuard、systemd など、
ホスト側の部品を必要な範囲で使います。

## 解決する問題

手作業でルーターを作ると、状態は多くの場所に散らばります。

- インターフェースのアドレスは netplan、systemd-networkd、rc.d、NixOS 設定に分かれます。
- DHCP、DHCPv6、DHCP 中継、RA は dnsmasq 設定に分かれます。
- DNS 転送とローカルレコードはリゾルバーごとの設定に分かれます。
- NAT、経路ポリシー、conntrack、ファイアウォールは nftables と iproute2 に分かれます。
- DHCPv4、DHCPv6-PD、PPPoE、ヘルスチェック、ログは別々のデーモンになります。
- パッケージ、sysctl、サービスユニットはホスト準備スクリプトに残りがちです。

routerd はこれらをリソースとして扱います。
YAML を見ればルーターの意図が分かります。
git の diff で変更を確認できます。
`routerctl` と Web Console で実際に観測した状態を確認できます。

## 現在の構成

`routerd serve` はリソースを読み、依存関係を解きます。
子デーモンを起動し、イベントを購読し、ホストを望ましい状態へ調整します。

長く動くプロトコル状態は、小さな管理対象デーモンへ分けています。

- `routerd-dhcpv6-client`: DHCPv6 プレフィックス委譲と情報要求を担当します。
- `routerd-dhcpv4-client`: DHCPv4 WAN リースを担当します。
- `routerd-pppoe-client`: PPPoE セッションを担当します。
- `routerd-healthcheck`: TCP、DNS、HTTP、ICMP の疎通確認を担当します。
- `routerd-dns-resolver`: DNS ゾーン応答と DoH、DoT、DoQ、UDP 上流を担当します。
- `routerd-dhcp-event-relay`: dnsmasq のリース変化を routerd イベントへ変換します。
- `routerd-firewall-logger`: ファイアウォールログを routerd のログ保存先へ取り込みます。

各デーモンは Unix ソケットでローカル HTTP+JSON 状態を公開します。
必要な状態はファイルへ保存します。
routerd はそれらのイベントを読み、LAN サービス、DNS レコード、DS-Lite、
NAT、経路ポリシー、ヘルスチェック由来の選択、観測用保存先へ反映します。

## 管理できるもの

現在の実装では、次を扱えます。

- DHCPv6-PD と、委譲プレフィックスからの IPv6 LAN アドレス
- DHCPv6 情報要求、AFTR DNS 解決、DS-Lite
- DHCPv4 WAN リース、DHCPv4 LAN スコープ、固定割り当て
- DHCPv6 サーバーモード、IPv6 RA オプション
- DNS ゾーン、DHCP 由来レコード、条件付き転送、DoH、DoT、DoQ、
  UDP フォールバック、複数待ち受け、キャッシュ
- NAT44、プライベート宛先の NAT 対象外指定、IPv4 経路ポリシー、
  reverse path filter、Path MTU 方針、TCP MSS 調整
- PPPoE、WireGuard、VXLAN、VRF、cloud VPN 向け IPsec 設定の土台
- パッケージ、sysctl プロファイル、ネットワーク引き継ぎ、systemd ユニット、
  NTP クライアント、ログ転送、ログ保管、Web Console
- `EgressRoutePolicy`、`HealthCheck`、`EventRule`、`DerivedEvent` による状態連携
- 状態、イベント、DNS クエリー、コネクション、通信フロー、ファイアウォールログの確認

## まだ割り切っていること

routerd は v1alpha1 のプレリリースです。
ルーターを安全にし、設定を分かりやすくするためであれば、
名前やフィールドは互換別名なしで変えることがあります。

状態を持つファイアウォールフィルターは土台段階です。
routerd はまだ汎用ファイアウォール規則言語ではありません。
NixOS と FreeBSD は対応を進めていますが、Ubuntu と同等ではありません。
プラットフォームごとの差は対応表に記載します。

## 次に読むもの

- [設計思想](./design-philosophy)
- [リソースモデル](./resource-model)
- [適用と生成](./apply-and-render)
- [状態と所有](./state-and-ownership)
- [インストール](../tutorials/install)
