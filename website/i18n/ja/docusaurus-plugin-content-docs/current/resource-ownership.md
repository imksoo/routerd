---
title: リソース所有
slug: /reference/resource-ownership
---

# リソース所有と反映モデル

routerd は、ホスト上の構成物をリソースに対応付けて管理します。
どのリソースが何を作ったかを記録することで、差分確認、削除、障害調査をしやすくします。

## 所有の種類

| 種類 | 意味 |
| --- | --- |
| 作成 | routerd が構成物を新しく作ります。 |
| 取り込み | 既存の構成物を routerd の管理対象として扱います。 |
| 観測 | routerd は状態を見るだけで変更しません。 |

## 主な構成物

| リソース | ホスト側の構成物 |
| --- | --- |
| `Interface` | OS インターフェース名、管理状態 |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` の socket、lease、events |
| `DHCPv4Client` | `routerd-dhcpv4-client` の socket、lease、events |
| `PPPoESession` | `routerd-pppoe-client` の socket、state、pppd/ppp 設定 |
| `HealthCheck` | `routerd-healthcheck` の socket、state、events |
| `DHCPv4Server` / `DHCPv6Server` / `IPv6RouterAdvertisement` | 管理対象 dnsmasq 設定 |
| `DNSZone` | `routerd-dns-resolver` のローカル権威ゾーン |
| `DNSResolver` | `routerd-dns-resolver` の socket、state、events、待ち受け設定 |
| `DNSForwarder` | `routerd-dns-resolver` の runtime forwarding rule。resolver config に導出されます |
| `DNSUpstream` | `routerd-dns-resolver` の runtime upstream endpoint。forwarder rule に導出されます |
| `DSLiteTunnel` | Linux `ip6tnl` インターフェース |
| `IPAddressSet` | Linux renderer から参照される nftables IPv4/IPv6 named set |
| `IPv4Route` | カーネル経路 |
| `ClusterNetworkRoute` | Pod / Service CIDR を指定 next hop 経由にする生成済み `IPv4StaticRoute` intent |
| `NAT44Rule` | nftables `routerd_nat` テーブル |
| `PortForward` / `IngressService` | Linux nftables の `routerd_nat` / `routerd_filter` DNAT、任意の hairpin SNAT、または FreeBSD `pf.conf` の `rdr pass` / 任意の NAT reflection ルール |
| `BGPRouter` / `BGPPeer` | local GoBGP gRPC で制御する長寿命 `routerd-bgp` daemon state。学習した IPv4 best path は routerd 所有の protocol/metric で kernel FIB に投入 |
| `BFD` | BFD intent のみ。FRR なしの BFD 実装が入るまでは GoBGP backend が unsupported として報告 |
| `VirtualAddress` | `ip addr` / `ifconfig` による static VIP、または Linux keepalived / FreeBSD CARP による VRRP/VRRPv3 VIP ownership |
| `ObservabilityPipeline` | process 内 routerd event exporter と managed unit 向け OpenTelemetry environment |
| `RouterdCluster` | `spec.leasePath` の file lease。leader のみ apply/controller mutation を実行 |
| `WireGuardInterface` / `WireGuardPeer` | WireGuard 設定 |
| `TailscaleNode` | `routerd-tailscale-<name>` service unit / script と `tailscale up` 引数 |
| `VRF` | Linux VRF デバイスと経路表 |
| `VXLANTunnel` | VXLAN デバイス |
| `Package` | package override。通常の host package intent は router resource から自動導出 |
| `Sysctl` | sysctl 値 |
| `SysctlProfile` | 複数の sysctl 値 |
| Derived host runtime | router resource から導出される kernel module load 状態と systemd-networkd / resolved drop-in |
| `generated service artifacts` | systemd unit、FreeBSD rc.d script、または OpenRC init script と enable 状態 |
| `NTPClient` | NTP クライアント設定 |

## 削除時の考え方

routerd は、知らない構成物を勝手に削除しません。
YAML からリソースが消えた場合でも、削除できるのは routerd が所有していると分かる構成物だけです。

現在は完全なロールバック機能を目標にしていません。
特に本番ネットワークへ影響する変更では、次の順序を守ります。

1. 検証します。
2. 計画を確認します。
3. 予行実行します。
4. 管理用接続が消えないことを確認します。
5. 適用します。
6. 状態と疎通を確認します。

## 古い構成の扱い

Phase 4 で、旧 DHCPv6 実験用パッケージや旧レンダラは削除しました。
現在の DHCPv6-PD は `routerd-dhcpv6-client` が担当します。
過去の `dhcpcd` や `dhcp6c` 経路の記述は、現在の設定例として使いません。
