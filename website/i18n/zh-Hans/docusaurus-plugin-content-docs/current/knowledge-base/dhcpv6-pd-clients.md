---
title: routerd 自行实现 DHCPv6-PD 客户端的原因
---

# routerd 自行实现 DHCPv6-PD 客户端的原因

![Diagram showing why routerd owns DHCPv6-PD from OS client variation and stale prefix risk through routerd-dhcpv6-client lease state, status, delegated LAN address inputs, and HA DUID operation](/img/diagrams/knowledge-base-dhcpv6-pd-clients.png)

routerd 当前的方针是由专属守护进程 `routerd-dhcpv6-client` 负责 DHCPv6-PD。
过去评估过的 OS 内置客户端方案，并未纳入现行配置示例。

## 改采专属守护进程的原因

DHCPv6-PD 不仅止于获取前缀，Renew、重启后的还原，以及事件日志同样重要。
若只是为 OS 内置客户端生成配置，难以将 routerd 的状态模型与 LAN 侧的应用过程整合得干净利落。

改为专属守护进程后，具备以下能力：

- 将租约保存至 `lease.json`
- 启动时还原租约
- 将 Renew 结果记录至事件日志
- 通过 `/v1/status` 返回 `Bound` / `Pending` 状态
- 发出供其他控制器（LAN 地址派生、RA、DHCPv6 服务器、DS-Lite、DNS）消费的事件

## 二进制与部署位置

```text
routerd-dhcpv6-client
```

| 路径 | 用途 |
| --- | --- |
| `/run/routerd/dhcpv6-client/<name>.sock` | 各资源的控制插槽 |
| `/var/lib/routerd/dhcpv6-client/<name>/lease.json` | 租约持久化 |
| `/var/lib/routerd/dhcpv6-client/<name>/events.jsonl` | 仅追加的事件日志 |

## 评估后未采用的替代方案

我们比较了 `systemd-networkd`、WIDE/KAME 系客户端及其他 DHCP 客户端，
最终采用由 routerd 自行拥有的守护进程。
这些调查结果作为背景资料仍具参考价值，但不包含在当前的出货配置中。

当前的 Kind 为 `DHCPv6PrefixDelegation`，并未提供用于选择 OS 内置实现的 `client` 字段，此为刻意设计。

## 操作注意事项

请勿在同一个 WAN 接口上同时运行多个 DHCPv6-PD 客户端。
同时发出两个客户端会造成上游混乱，导致无法收到 Reply。

迁移至 routerd 管理时，请先停止旧有客户端
（包含其租约文件，以及启动该客户端的 systemd / rc.d 配置），
再启动 `routerd-dhcpv6-client`。
