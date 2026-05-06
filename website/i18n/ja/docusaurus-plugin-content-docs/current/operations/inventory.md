---
title: ホストインベントリ
slug: /operations/inventory
---

# ホストインベントリ

routerd はホストの OS、利用可能なコマンド、ネットワーク機能を inspect します。
このインベントリは、レンダラと apply path が OS 固有の判断を validation または planning 段階で明示するために使います。

## routerd が確認する項目

- OS とリリース
- サービス管理方式 (systemd、rc.d、NixOS module)
- 利用可能なコマンド (iproute2、nftables、conntrack、dnsmasq、radvd、pppd、WireGuard、strongSwan 等)
- カーネル機能 (IPv6、VRF、VXLAN、WireGuard)
- `/run/routerd` と `/var/lib/routerd` の利用可否

## 振る舞いへの影響

- Ubuntu では systemd と Linux ネットワークスタックを対象。
- NixOS では runtime mutation より宣言的生成を優先。
- FreeBSD では `daemon(8)` と rc.d でサービス制御。

ホストが提供しない機能に依存する設定は、`apply` の途中で失敗するのではなく validation または planning 段階で明示されます。

## routerd が探す代表コマンド

| コマンド | 用途 |
| --- | --- |
| `ip`, `bridge` | アドレス、経路、DS-Lite、VRF、VXLAN |
| `nft` | NAT、firewall、route mark |
| `dnsmasq` | DHCPv4、DHCPv6、RA |
| `conntrack` | IPv4/IPv6 コネクション観測 |
| `pppd`, `ppp` | PPPoE |
| `wg` | WireGuard |
| `swanctl` | IPsec |
| `radvd` | radvd 経由の RA (任意) |
| `sysctl` | カーネル設定 |
| `systemctl`, `resolvectl`, `networkctl`, `journalctl` | systemd 環境の管理 |
| `service`, `sysrc`, `pfctl` | FreeBSD 環境の管理 |
| `dig`, `ping`, `ping6`, `tcpdump`, `tracepath`, `traceroute`, `netstat`, `sockstat` | 障害調査 |

## 関連項目

- [対応プラットフォーム](../platforms.md)
- [Reconcile と削除](./reconcile.md)
