---
title: Azure 与 PVE 的 same-subnet SAM 冒烟测试
---

# Azure 与 PVE 的 same-subnet SAM 冒烟测试

![Azure provider-secondary-IP 捕获、on-prem proxy-ARP 捕获、SAM /32 交付路由、转发检查、routerctl doctor 验证的流程](/img/diagrams/how-to-hybrid-azure-pve-same-subnet.png)

本指南总结了经过验证的操作模式：Azure 的 routerd 节点与本地 Proxmox VE 的 routerd 节点通过 Selective Address Mobility (SAM) 交换选定的 `/32` 地址。资源语义请参见[选择性地址移动性参考](../reference/selective-address-mobility)。

## Azure 侧

- Azure NIC 辅助 IP 保留在 Azure 侧。这个 provider-side 对象捕获发往 on-prem `/32` 的数据包。
- 不要让 Ubuntu 来宾 OS 持有已捕获的 `/32`。cloud-init 或 netplan 可能会自动为辅助 NIC IP 分配地址。请抑制或删除该配置。当 Claim 设置 `configureOSAddress: false` 时，routerd 在 reconcile 时会从本地接口 de-assign 该特定地址，并维持地址不存在的状态。
- 在 Azure NIC 和 Linux 上都启用 IP forwarding（`net.ipv4.ip_forward=1`）。

## 本地 PVE 侧

- 在能看到 local same-subnet 主机的 LAN/bridge 接口上使用 `proxy-arp` 捕获。
- 启用 Linux forwarding。SAM 中 routerd 通过常规 sysctl 路径启用 `ip_forward` 和 `proxy_arp`。
- 在 capture 接口和 WireGuard tunnel 之间，通过防火墙策略允许已捕获 `/32` 的转发。SAM 不添加防火墙规则或 NAT 规则。
- 对于云端来宾镜像，在判断 provider fabric 丢包之前，也要检查主机防火墙的默认值。路由器需要接受 WireGuard 的 UDP listen port，并允许 capture 接口和 `wg-hybrid` 之间的转发。`routerctl doctor hybrid` 会警告终端 iptables drop/reject 模式和 SAM MSS clamp 规则缺失。

## 隧道与路由

- WireGuard 从 on-prem 向 Azure public IP 拨号。
- 在 on-prem peer 上设置 `persistentKeepalive`，以维持 NAT 和 cloud edge 状态。
- 首次冒烟测试不使用 UDR。如果后续添加 UDR fallback，请注意 Azure 可能将已捕获 `/32` 回送到交付源路由器形成 same-subnet 环路。
- SAM 交付将每个 claim lower 为指向 tunnel 接口的 `/32` 路由。不变更默认路由。

## 验证

运行：

```sh
routerctl doctor hybrid
```

对于 `provider-secondary-ip` + `configureOSAddress: false`，确认已捕获的 `/32` 不存在于本地 `ip addr` 中、交付路由指向 tunnel、`ip_forward=1`。对于 `proxy-arp`，确认 `proxy_arp=1`、proxy neighbor、指向 tunnel 的交付路由、`ip_forward=1`。

在低 MTU overlay 中，`doctor hybrid` 会报告 SAM MSS clamp，确认 `nft list table inet routerd_mss` 中包含选定 `/32` 路径的 capture-to-tunnel 和 tunnel-to-capture 双向规则。
