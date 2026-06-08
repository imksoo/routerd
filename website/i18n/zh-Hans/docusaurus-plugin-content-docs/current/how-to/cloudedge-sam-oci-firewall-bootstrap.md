---
title: CloudEdge SAM - OCI Ubuntu 镜像的防火墙引导
---

# CloudEdge SAM：OCI Ubuntu 镜像的防火墙引导

![OCI Ubuntu 来宾防火墙默认阻止 WireGuard 和 SAM 转发，以及所需的引导许可和 routerctl doctor 检查](/img/diagrams/how-to-cloudedge-sam-oci-firewall-bootstrap.png)

> 实验性功能（CloudEdge SAM）。这是**主机引导/提供商镜像行为**问题，不是 routerd 数据平面的问题。适用于作为 SAM 路由器使用的 OCI Canonical Ubuntu 镜像。

## 症状

在 OCI 中，Canonical Ubuntu 24.04 镜像在启动时启用了 `iptables-nft` 过滤规则，**reject SSH/ICMP 以外的入站流量，并 reject 所有 FORWARD 流量**。在此默认设置下，SAM 路由器：

- 即使 OCI 安全列表允许 `UDP/51820` 且 VNIC 设置了 `skipSourceDestCheck=true`，也**无法**接收 WireGuard 握手 — 主机防火墙在入站 WireGuard 数据包到达 `wg-hybrid` 监听器之前就将其丢弃。
- **无法**转发捕获/overlay 流量 — 默认的 `FORWARD` reject 阻止了 VNIC 接口和 `wg-hybrid` 之间的 SAM 交付路径。

这与云端安全列表 / VNIC 的 source-dest-check 无关。它们在 fabric 层运作。**来宾 OS 防火墙**是独立的层，需要单独许可 SAM 路径。

## 所需许可（来宾 OS）

在每台 OCI SAM 路由器上，确保主机防火墙许可以下内容：

- 到 `wg-hybrid` WireGuard 监听器的**入站 `UDP/51820`**。
- OCI VNIC 接口（如 `ens3`）和 `wg-hybrid` 之间的双向 **`FORWARD`**。

这些应作为路由器配置中主机引导的一部分以声明方式记述，而不是依赖临时的 `iptables` 规则（重建时会丢失）（与其他"路由器前提条件"一样，要能在干净主机上证明）。

## 诊断方法

`routerctl doctor hybrid` 会检测来宾防火墙中阻止 WireGuard / SAM 路径的 reject-all `FORWARD`/`INPUT` 模式，因此许可遗漏会以报告形式显示，而不是静默的"无握手"。部署后在 OCI 路由器上运行：

```
routerctl doctor hybrid
```

如果 WireGuard 端点未显示握手但对端正在发送 keepalive，请先检查来宾防火墙（本 How-to），然后检查 OCI 安全列表，再检查 VNIC 的 source-dest-check。

## 相关内容

- [Selective Address Mobility](../reference/selective-address-mobility)
- OCI Ubuntu 镜像的默认 `iptables-nft` 配置与 AWS/Azure 镜像不同。AWS/Azure 的 SAM 冒烟测试未出现此问题，是因为那些镜像默认不会对 `FORWARD` 做 reject-all。
