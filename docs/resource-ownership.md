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
| `DHCPv4Lease` | `routerd-dhcpv4-client` の socket、lease、events |
| `PPPoESession` | `routerd-pppoe-client` の socket、state、pppd/ppp 設定 |
| `HealthCheck` | `routerd-healthcheck` の socket、state、events |
| `DHCPv4Server` / `DHCPv6Server` / `IPv6RouterAdvertisement` / `DNSAnswerScope` | 管理対象 dnsmasq 設定 |
| `DNSResolverUpstream` | dnsmasq の既定上流と条件付き転送設定 |
| `DSLiteTunnel` | Linux `ip6tnl` インターフェース |
| `IPv4Route` | カーネル経路 |
| `NAT44Rule` | nftables `routerd_nat` テーブル |
| `WireGuardInterface` / `WireGuardPeer` | WireGuard 設定 |
| `VRF` | Linux VRF デバイスと経路表 |
| `VXLANTunnel` | VXLAN デバイス |
| `Sysctl` | sysctl 値 |
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
