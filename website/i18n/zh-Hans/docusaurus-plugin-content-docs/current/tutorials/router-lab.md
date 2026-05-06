---
title: 搭建评估实验环境
---

# 搭建评估实验环境

将 routerd 部署到正式网络之前，建议在隔离的实验环境中进行评估。
本页演示如何在虚拟化平台上启动多台路由器 VM，跨多种操作系统验证 routerd 行为。

不限定特定硬件，Proxmox VE、KVM、VMware、Hyper-V 任何一种都可以。

## 想定场景

正式部署前希望确认以下几点：

- DHCPv6-PD、DS-Lite、PPPoE、NAT44、firewall、DNS 各功能是否符合预期
- Linux、FreeBSD、NixOS 哪一个更合适
- 将某项变更投入正式网络之前，是否能在沙盒中重现相同效果

实验环境的目的有三：与正式环境隔离、能自由更换 OS 与架构、可直接套用未来要部署的 routerd YAML。

## 硬件需求

全部以 VM 运行，不需要物理路由器。

- 一台虚拟化主机（Proxmox VE、KVM、VMware、Hyper-V 任一）
- 4 GB 以上空闲内存（每台 VM 约 512 MB）
- 上游网络（如要验证 DHCPv6-PD，需要可派发 IPv6 prefix 的 HGW 或模拟器）

## 推荐的 VM 拓扑

最少 **2 台 VM**，建议 **4-5 台**。

| 角色 | OS 示例 | 可验证内容 |
| --- | --- | --- |
| 测量主机 | Ubuntu / Debian | `iperf3`、`dig`、`curl`、`mtr` 等工具 |
| WAN 侧路由器 A | Ubuntu | `routerd-dhcpv6-client`、PPPoE、DS-Lite |
| WAN 侧路由器 B | NixOS | NixOS 模块路径封装 routerd |
| WAN 侧路由器 C | FreeBSD | FreeBSD `pf`、DS-Lite、rc.d unit |
| LAN 侧路由器 | Ubuntu | controller chain、DNS、firewall、NAT、`HealthCheck`、Web Console |

将 VM 接入共用 virtual switch（VLAN trunk 或 untagged bridge），并让该 switch 与上游隔离。
每台 VM 一个 NIC 接入「上游 vSwitch」，另一个 NIC 接入「实验 vSwitch」即可。

## 验收清单

按以下项目确认实验环境正常：

1. **DHCPv6-PD 取得**：每台路由器 VM 的 `routerctl status` 显示 `DHCPv6PrefixDelegation` 为 `Bound`。
2. **PD 不重叠**：超过 5 台时，所派发的 prefix 不重复（HGW 需按 IA_PD 分割）。
3. **IPv6 连通性**：实验 VM 之间以 link-local 与 GUA 双向 ping 都通。
4. **DS-Lite**：`routerctl describe DSLiteTunnel/<name>` 为 `Up`，对应 `HealthCheck` 为 `Healthy`，`curl --interface ds-lite-X http://example.com/` 有响应。
5. **NixOS 路径**：`nixos-rebuild test` 套用后重启仍维持相同状态。
6. **FreeBSD 路径**：`pfctl -sr` 显示 routerd 生成的 pf 规则，`service routerd onestatus` 为 active。

## 不需处理的事项

以下现象由 HGW、ISP 等外部设备决定，routerd 本身无法控制。请勿在实验环境中纠结重现：

- HGW 在 DHCPv6 information-request 不返回 AFTR option：用 `DSLiteTunnel.spec.aftrFQDN` 设定静态备援。
- 部分 ISP 仅对特定 IA_ID 返回 PD：在 client 端固定 IA_ID。
- 物理交换机在某 VLAN 上丢弃 IPv6 RA：用 vSwitch 内路由绕过该交换机。

以上案例已整理于 `docs/knowledge-base/`。

## 接下来

实验环境搭建完成后，可进入以下章节：

- [启动第一台路由器](./first-router.md)
- [配置 LAN 侧服务](./lan-side-services.md)
- [建立基本 firewall](./basic-firewall.md)
- [建立多 WAN](../how-to/multi-wan.md)
