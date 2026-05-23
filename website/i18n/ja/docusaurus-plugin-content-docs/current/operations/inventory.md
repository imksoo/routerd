---
title: ホストインベントリ
slug: /operations/inventory
---

# ホストインベントリ

routerd は、ホストの OS、利用できるコマンド、ネットワーク機能を調べます。
このインベントリは、レンダラーと適用処理が、OS 固有の判断を検証または計画の段階で明示するために使います。

## routerd が確認する項目

- OS とリリース
- サービス管理方式（systemd、rc.d、NixOS module）
- 利用できるコマンド（iproute2、nftables、conntrack、dnsmasq、radvd、pppd、WireGuard、strongSwan など）
- カーネル機能（IPv6、VRF、VXLAN、WireGuard）
- `/run/routerd` と `/var/lib/routerd` の利用可否

## 振る舞いへの影響

- Ubuntu では、systemd と Linux ネットワークスタックを対象にします。
- NixOS では、実行時の変更よりも宣言型の生成を優先します。
- FreeBSD では、`daemon(8)` と rc.d でサービスを制御します。

ホストが提供しない機能に依存する設定は、`apply` の途中で失敗するのではなく、検証または計画の段階で明示します。

## routerd が探す代表的なコマンド

| コマンド | 用途 |
| --- | --- |
| `ip`, `bridge` | アドレス、経路、DS-Lite、VRF、VXLAN |
| `nft` | NAT、ファイアウォール、route mark |
| `dnsmasq` | DHCPv4、DHCPv6、RA |
| `conntrack` | IPv4/IPv6 コネクションの観測 |
| `pppd`, `ppp` | PPPoE |
| `wg` | WireGuard |
| `tailscale` | Tailscale の exit node と subnet router の広告 |
| `swanctl` | IPsec |
| `radvd` | radvd 経由の RA（任意） |
| `sysctl` | カーネル設定 |
| `systemctl`, `resolvectl`, `networkctl`, `journalctl` | systemd 環境の管理 |
| `service`, `sysrc`, `pfctl` | FreeBSD 環境の管理 |
| `dig`, `ping`, `ping6`, `tcpdump`, `tracepath`, `traceroute`, `netstat`, `sockstat` | 障害調査 |

## 関連項目

- [対応プラットフォーム](../platforms.md)
- [Reconcile と削除](./reconcile)
