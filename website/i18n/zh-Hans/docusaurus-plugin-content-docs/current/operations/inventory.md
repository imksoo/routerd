---
title: 主机清单
slug: /operations/inventory
---

# 主机清单

![Diagram showing host inventory detection of OS, service manager, commands, kernel features, and paths feeding platform decisions and validation or planning output](/img/diagrams/operations-inventory.png)

routerd 会调查主机的 OS、可用命令及网络功能。
此清单供生成器与应用处理在验证或计划阶段，明确标示各 OS 特定的判断。

## routerd 确认的项目

- OS 与发行版本
- 服务管理方式（systemd、rc.d、NixOS module）
- 可用命令（iproute2、nftables、conntrack、dnsmasq、radvd、pppd、WireGuard、strongSwan 等）
- 内核功能（IPv6、VRF、VXLAN、WireGuard）
- `/run/routerd` 与 `/var/lib/routerd` 的可用性

## 对行为的影响

- 在 Ubuntu 上，以 systemd 与 Linux 网络堆栈为目标。
- 在 NixOS 上，优先使用声明式生成而非运行时变更。
- 在 FreeBSD 上，以 `daemon(8)` 与 rc.d 控制服务。

依赖主机未提供功能的配置，会在验证或计划阶段明确标示，而不是在 `apply` 中途失败。

## routerd 搜索的代表性命令

| 命令 | 用途 |
| --- | --- |
| `ip`, `bridge` | 地址、路由、DS-Lite、VRF、VXLAN |
| `nft` | NAT、防火墙、route mark |
| `dnsmasq` | DHCPv4、DHCPv6、RA |
| `conntrack` | IPv4/IPv6 连接观测 |
| `pppd`, `ppp` | PPPoE |
| `wg` | WireGuard |
| `tailscale` | Tailscale exit node 与 subnet router 广播 |
| `swanctl` | IPsec |
| `radvd` | 通过 radvd 的 RA（可选） |
| `sysctl` | 内核配置 |
| `systemctl`, `resolvectl`, `networkctl`, `journalctl` | systemd 环境管理 |
| `service`, `sysrc`, `pfctl` | FreeBSD 环境管理 |
| `dig`, `ping`, `ping6`, `tcpdump`, `tracepath`, `traceroute`, `netstat`, `sockstat` | 故障排查 |

## 相关参考

- [支持平台](../platforms.md)
- [Reconcile 与删除](./reconcile)
