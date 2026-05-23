---
title: リソース所有
slug: /reference/resource-ownership
---

# リソース所有と反映モデル

routerd は、ホスト上の構成物をリソースに対応付けて管理します。
どのリソースが何を作ったかを記録することで、差分の確認、削除、障害調査をしやすくします。

## 所有の種類

| 種類 | 意味 |
| --- | --- |
| 作成 | routerd が構成物を新しく作ります。 |
| 取り込み | 既存の構成物を routerd の管理対象として扱います。 |
| 観測 | routerd は状態を見るだけで変更しません。 |

## 主な構成物

| リソース | ホスト側の構成物 |
| --- | --- |
| `Interface` | OS のインターフェース名と管理状態 |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` のソケット、リース、イベント |
| `DHCPv4Client` | `routerd-dhcpv4-client` のソケット、リース、イベント |
| `PPPoESession` | `routerd-pppoe-client` のソケット、状態、pppd/ppp 設定 |
| `HealthCheck` | `routerd-healthcheck` のソケット、状態、イベント |
| `DHCPv4Server` / `DHCPv6Server` / `IPv6RouterAdvertisement` | 管理対象の dnsmasq 設定 |
| `DNSZone` | `routerd-dns-resolver` のローカル権威ゾーン |
| `DNSResolver` | `routerd-dns-resolver` のソケット、状態、イベント、待ち受け設定 |
| `DNSForwarder` | `routerd-dns-resolver` の転送ルール。リゾルバ設定として生成されます |
| `DNSUpstream` | `routerd-dns-resolver` の上流エンドポイント。転送ルールとして生成されます |
| `DSLiteTunnel` | Linux の `ip6tnl` インターフェース |
| `IPAddressSet` | Linux の生成器が参照する nftables の IPv4/IPv6 named set |
| `IPv4Route` | カーネルの経路 |
| `ClusterNetworkRoute` | Pod / Service CIDR を指定した next hop 経由にする、生成済みの `IPv4StaticRoute` の意図 |
| `NAT44Rule` | nftables の `routerd_nat` テーブル |
| `PortForward` / `IngressService` | Linux では nftables の `routerd_nat` / `routerd_filter` の DNAT と、任意の hairpin SNAT。FreeBSD では `pf.conf` の `rdr pass` と、任意の NAT reflection ルール |
| `BGPRouter` / `BGPPeer` | ローカルの GoBGP gRPC で制御する、長寿命の `routerd-bgp` デーモンの状態。学習した IPv4 の best path は、routerd が所有する protocol/metric でカーネル FIB に投入します |
| `BFD` | BFD の意図のみを保持します。FRR を使わない BFD 実装が入るまでは、GoBGP バックエンドが unsupported として報告します |
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
| `generated service artifacts` | systemd ユニット、FreeBSD の rc.d スクリプト、または OpenRC の init スクリプトと、その有効化状態 |
| `NTPClient` | NTP クライアント設定 |

## 削除時の考え方

routerd は、知らない構成物を勝手に削除しません。
YAML からリソースが消えても、削除できるのは routerd が所有していると分かる構成物だけです。

現在は、完全なロールバック機能を目標にしていません。
特に本番ネットワークへ影響する変更では、次の順序を守ります。

1. 検証します。
2. 計画を確認します。
3. 予行実行します。
4. 管理用接続が消えないことを確認します。
5. 適用します。
6. 状態と疎通を確認します。

## 古い構成の扱い

Phase 4 で、旧 DHCPv6 実験用パッケージと旧生成器を削除しました。
現在の DHCPv6-PD は `routerd-dhcpv6-client` が所有します。
過去の `dhcpcd` や `dhcp6c` の経路に関する記述は、現在の設定例としては使いません。
