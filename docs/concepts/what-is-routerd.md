---
title: routerd とは
slug: /concepts/what-is-routerd
sidebar_position: 1
---

# routerd とは

routerd は、Linux ホスト・NixOS・FreeBSD をルーターとして動かすための宣言型の制御プレーンです。ルーターの構成を YAML リソースとして書くと、routerd がその意図を、インターフェース、アドレス、DHCP、DNS、NAT、経路、トンネル、ヘルスチェック、パッケージ、sysctl、サービスユニット、ログといった実際の状態へ反映します。

routerd はディストリビューションでも、集中管理サービスでもありません。各ルーターのホスト上でローカルに動き、systemd-networkd、dnsmasq、nftables、pppd、WireGuard、systemd といったホスト側の部品を、必要な範囲で使います。

## 解決する問題

ルーターを手作業で作ると、状態が多くの場所に散らばります。

- インターフェースのアドレスは、netplan・systemd-networkd・rc.d・NixOS 設定に分かれます。
- DHCP、DHCPv6、DHCP 中継、RA は、dnsmasq の設定に分かれます。
- DNS 転送とローカルレコードは、リゾルバごとの設定に分かれます。
- NAT、経路ポリシー、conntrack、ファイアウォールは、nftables と iproute2 に分かれます。
- DHCPv4、DHCPv6-PD、PPPoE、ヘルスチェック、ログは、別々のデーモンになります。
- パッケージ、sysctl、サービスユニットは、ホスト準備スクリプトに残りがちです。

routerd は、これらをまとめてリソースとして扱います。YAML を見ればルーターの意図が分かり、変更は git の diff で追え、実際に観測した状態は `routerctl` と Web 管理画面で確認できます。

## 現在の構成

`routerd serve` はリソースを読んで依存関係を解き、子デーモンを起動し、イベントを購読しながら、ホストを望ましい状態へ調整（リコンサイル）します。

長く動き続けるプロトコル状態は、小さな管理対象デーモンに分けています。

- `routerd-dhcpv6-client`: DHCPv6 のプレフィックス委任（PD）と情報要求を担当します。
- `routerd-dhcpv4-client`: DHCPv4 の WAN リースを担当します。
- `routerd-pppoe-client`: PPPoE セッションを担当します。
- `routerd-healthcheck`: TCP、DNS、HTTP、ICMP の疎通確認を担当します。
- `routerd-dns-resolver`: DNS ゾーン応答と、DoH・DoT・TCP・UDP の上流を担当します。
- `routerd-dhcp-event-relay`: dnsmasq のリース変化を routerd のイベントへ変換します。
- `routerd-firewall-logger`: ファイアウォールログを routerd のログ保存先へ取り込みます。

各デーモンは、Unix ソケット上のローカル HTTP+JSON で状態を公開し、必要な状態はファイルに保存します。routerd はそれらのイベントを読み、LAN サービス、DNS レコード、DS-Lite、NAT、経路ポリシー、ヘルスチェックによる経路選択、観測用の保存先へ反映します。

## 管理できるもの

現在の実装では、次を扱えます。

- DHCPv6-PD と、委任されたプレフィックスから作る IPv6 LAN アドレス
- DHCPv6 情報要求、AFTR の DNS 解決、DS-Lite
- DHCPv4 の WAN リース、DHCPv4 の LAN スコープ、固定割り当て
- DHCPv6 サーバーモード、IPv6 RA オプション
- DNS ゾーン、DHCP 由来のレコード、条件付き転送、DoH、DoT、TCP DNS、UDP フォールバック、複数待ち受け、キャッシュ
- NAT44、プライベート宛先の NAT 対象外指定、IPv4 経路ポリシー、reverse path filter、Path MTU 方針、TCP MSS 調整
- PPPoE、WireGuard、VXLAN、VRF、cloud VPN 向けの IPsec 接続定義、strongSwan `swanctl` 設定の生成
- パッケージ、sysctl プロファイル、ネットワーク引き継ぎ、systemd ユニット、NTP クライアント、ログ転送、ログ保管、Web 管理画面
- `EgressRoutePolicy`、`HealthCheck`、`EventRule`、`DerivedEvent` による状態の連携
- 状態、イベント、DNS クエリー、コネクション、通信フロー、ファイアウォールログの確認

## 意図して範囲を絞っていること

routerd は v1alpha1 のプレリリースです。ルーターを安全にし、設定を分かりやすくするためなら、互換用の別名を残さずに名前やフィールドを変えることがあります。

ステートフルなファイアウォールフィルターも、意図して範囲を絞っています。routerd が生成するのは、NAT44、ゾーンポリシー、管理対象サービス向けの許可、拒否ログ、通信の確認までで、汎用のファイアウォール規則言語ではありません。

NixOS と FreeBSD も同じリソースモデルを使い、反映先だけがそれぞれの OS に合った機構になります。プラットフォームごとの差は対応表に記載します。

## 次に読むもの

- [設計思想](./design-philosophy)
- [リソースモデル](./resource-model)
- [適用と生成](./apply-and-render)
- [状態と所有](./state-and-ownership)
- [インストール](../tutorials/install)
