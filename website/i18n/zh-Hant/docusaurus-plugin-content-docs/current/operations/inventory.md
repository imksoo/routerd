---
title: 主機清單
slug: /operations/inventory
---

# 主機清單

routerd 會調查主機的 OS、可用指令及網路功能。
此清單供產生器與套用處理在驗證或計畫階段，明確標示各 OS 特定的判斷。

## routerd 確認的項目

- OS 與發行版本
- 服務管理方式（systemd、rc.d、NixOS module）
- 可用指令（iproute2、nftables、conntrack、dnsmasq、radvd、pppd、WireGuard、strongSwan 等）
- 核心功能（IPv6、VRF、VXLAN、WireGuard）
- `/run/routerd` 與 `/var/lib/routerd` 的可用性

## 對行為的影響

- 在 Ubuntu 上，以 systemd 與 Linux 網路堆疊為目標。
- 在 NixOS 上，優先使用宣告式產生而非執行時期變更。
- 在 FreeBSD 上，以 `daemon(8)` 與 rc.d 控制服務。

依賴主機未提供功能的設定，會在驗證或計畫階段明確標示，而不是在 `apply` 中途失敗。

## routerd 搜尋的代表性指令

| 指令 | 用途 |
| --- | --- |
| `ip`, `bridge` | 位址、路由、DS-Lite、VRF、VXLAN |
| `nft` | NAT、防火牆、route mark |
| `dnsmasq` | DHCPv4、DHCPv6、RA |
| `conntrack` | IPv4/IPv6 連線觀測 |
| `pppd`, `ppp` | PPPoE |
| `wg` | WireGuard |
| `tailscale` | Tailscale exit node 與 subnet router 廣播 |
| `swanctl` | IPsec |
| `radvd` | 透過 radvd 的 RA（選用） |
| `sysctl` | 核心設定 |
| `systemctl`, `resolvectl`, `networkctl`, `journalctl` | systemd 環境管理 |
| `service`, `sysrc`, `pfctl` | FreeBSD 環境管理 |
| `dig`, `ping`, `ping6`, `tcpdump`, `tracepath`, `traceroute`, `netstat`, `sockstat` | 疑難排解 |

## 相關參考

- [支援平台](../platforms.md)
- [Reconcile 與刪除](./reconcile)
