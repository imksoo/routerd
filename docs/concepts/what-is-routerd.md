---
title: routerd とは
slug: /concepts/what-is-routerd
sidebar_position: 1
---

# routerd とは

routerd は、Linux や FreeBSD のホストをルーターとして動かすための宣言的な制御ソフトウェアです。
ルーターの意図を YAML に書き、routerd がその意図に合わせてインターフェース、アドレス、DHCP、DNS、NAT、経路、トンネル、ヘルスチェックを調整します。

routerd はディストリビューションでも、集中管理サービスでもありません。
各ルーターホストの上でローカルに動き、ホストのカーネル機能、dnsmasq、nftables、pppd、WireGuard などを必要な範囲で使います。

## 解決する問題

手作業でルーターを作ると、設定は多くの場所に散らばります。

- インターフェース名とアドレスは netplan、systemd-networkd、rc.d、NixOS 設定に分かれます。
- DHCP、DNS、RA は dnsmasq や別の設定ファイルに分かれます。
- NAT と経路は nftables、iproute2、sysctl に分かれます。
- DHCPv4、DHCPv6-PD、PPPoE、ヘルスチェックは別々のデーモンになります。

routerd はこれらを「リソースの集合」として扱います。
YAML を見ればルーターの意図が分かり、git の差分で変更を確認できます。

## 現在の主な構成

routerd 本体は、リソースを読み、依存関係を解き、各リソースを調整します。
長く動く処理やプロトコル状態を持つ処理は、専用デーモンに分けています。

- `routerd-dhcpv6-client`: DHCPv6-PD と DHCPv6 情報要求を担当します。
- `routerd-dhcpv4-client`: DHCPv4 リースを担当します。
- `routerd-pppoe-client`: PPPoE セッションを担当します。
- `routerd-healthcheck`: TCP、DNS、HTTP、ICMP のヘルスチェックを担当します。
- `routerd serve`: リソース調整、イベントバス、制御 API を担当します。

各デーモンは Unix ドメインソケットで HTTP+JSON API を公開し、状態ファイルとイベントログを持ちます。
routerd 本体はそれらの状態を読み、LAN 側サービス、経路、DNS 応答、WAN 出口選択へ反映します。

## できること

現在の実装では、次のような構成を扱えます。

- DHCPv6-PD で受け取ったプレフィックスを LAN へ展開します。
- DHCPv6 情報要求、条件付き DNS 転送、DS-Lite、NAT44 を組み合わせます。
- DHCPv4 サーバー、DHCPv6 サーバー、RA は管理対象 dnsmasq が、DNS の応答と転送は `routerd-dns-resolver` がそれぞれ提供します。
- DHCPv4 クライアントは `routerd-dhcpv4-client` が担当します。
- PPPoE、WireGuard、VXLAN、VRF、IPsec 設定生成の土台を扱います。
- EgressRoutePolicy、HealthCheck、EventRule、DerivedEvent で状態に応じた選択を行います。
- OpenTelemetry の設定がある場合は、メトリクス、ログ、トレースを送信できます。

## まだ割り切っていること

routerd は開発中の v1alpha1 です。
現在の名前やフィールドは、将来の移行コストを下げるために互換性なしで整理することがあります。

特に、状態を持つファイアウォール、DoH 代理、BGP/OSPF、高可用化、遠隔プラグイン配布はまだ本線ではありません。
NixOS と FreeBSD は実機検証済みの範囲がありますが、Ubuntu と同じ機能差なしの対象ではありません。

## 次に読むもの

- [設計思想](./design-philosophy)
- [リソースモデル](./resource-model)
- [適用と生成](./apply-and-render)
- [状態と所有](./state-and-ownership)
- [インストール](../tutorials/install)
