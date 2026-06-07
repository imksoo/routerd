---
title: 状态与拥有权
slug: /concepts/state-and-ownership
sidebar_position: 5
---

# 状态与拥有权

routerd 将声明的意图与观测到的状态分开处理。
YAML 是用户管理的意图。
SQLite、租约文件、events.jsonl 则是 routerd 及专属守护进程观测到的状态。

![lifecycle GC 图：effective config、ownership ledger、object status 与 host inventory 输入 GC planner 和 teardown registry](/img/diagrams/lifecycle-gc.png)

## 状态的存放位置

正式版安装时，配置的正本存放在 `/usr/local/etc/routerd/router.yaml`。
routerd 可执行文件存放在 `/usr/local/sbin`。

Linux 上的状态存放位置如下所示。

| 种类 | 示例 |
| --- | --- |
| routerd 状态数据库 | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD 租约 | `/var/lib/routerd/dhcpv6-client/wan-pd/lease.json` |
| DHCPv4 租约 | `/var/lib/routerd/dhcpv4-client/wan/lease.json` |
| PPPoE 状态 | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| 健康检查状态 | `/var/lib/routerd/healthcheck/<name>/state.json` |
| 运行时 socket | `/run/routerd/.../*.sock` |

FreeBSD 上，配置与可执行文件同样存放在 `/usr/local` 下。
运行时 socket 存放在 `/var/run/routerd`。
持久状态存放在 `/var/db/routerd`。

## 拥有权的概念

routerd 在主机端创建的配置对象，各自有其拥有来源资源。
例如 dnsmasq 配置由 DHCP 和 RA 各资源生成（render），`routerd-dns-resolver` 的配置由 `DNSResolver` 和 `DNSZone` 生成，nftables 的 NAT 表格由 `NAT44Rule` 生成。
从多个隧道汇集而来的 TCP MSS clamp 表格则由最上层的 `Router` 拥有。

明确掌握拥有来源后，可以判断以下事项：

- 这个配置对象是否允许 routerd 修改。
- 从 YAML 中删除资源时，是否也可以删除主机端的对应配置。
- 只是纳管既有配置，还是由 routerd 全新创建。

owner key 是 `apiVersion/kind/name`；apply generation 不属于该 identity。
resource status 包含 owner 与 lifecycle metadata，使 stale cleanup path 也能区分
routerd-managed resource 与 adopted/external object。

## lifecycle GC

routerd 保存具体 host artifact 的 ownership ledger，以及 resource-specific teardown
所需的 object status。在 apply、serve startup 与 delete flow 中，generic GC planner
会将这些记录与 apply 使用的同一份 effective config 比较。effective config 包含
`when` filtering 之后的 startup YAML、active dynamic config 与生成的 SAM resource。

GC plan 可以表示 owned artifact 删除、resource teardown、ledger row forget、stale status
row 删除、event 记录，以及破坏性 cleanup 前所需的 state backup。不支持的 OS integration
会被跳过，adopted 或 externally managed status 会被保留。

resource 对应的 artifact map 与 teardown contract 请参阅
[资源拥有权](../resource-ownership.md)。

## 不使用过时状态

租约和观测值虽然方便，但持续使用过时的值是危险的。
特别是 DHCPv6-PD 的前缀，只有在确认为 Bound 状态时才会向下游展开。
无法确认时，会停止应用 AAAA 记录、RA、DHCPv6 服务器和 LAN IPv6 地址。

## 事件

routerd 及专属守护进程会将状态变化记录为事件。
事件保存在 SQLite 的 `events` 表格，以及各守护进程的 `events.jsonl` 中。
EventRule 和 DerivedEvent 会利用这些事件和状态，生成虚拟的状态变化。

## 应用世代

status 中显示的 `generation` 是最后完成的应用世代编号。
此值在 `routerd apply` 更新主机端意图、并将应用完成记录至 SQLite 时递增。
这不是调和（reconcile）循环的执行次数。
dry-run 计划、守护进程事件、健康检查、控制器链的定期调和均不会使其递增。
新的应用世代会保存当时应用的 YAML 快照。
Web 管理界面使用此快照，以只读方式显示世代历史记录，以及世代间的差异（unified diff）。
在 YAML 保存功能导入之前的记录仍作为历史保留，但无法作为差异显示的对象。

## 有状态数据包过滤器

在 Linux 上，routerd 以一次 `nft -f` 事务更新 nftables 的管理表格。
生成（render）的规则集会在必要时创建管理表格。
之后在同一个 nftables 批次中清空表格，并载入新的链。
防火墙区域的接口 set 或 client-policy 的 MAC set 等由 routerd 拥有的具名 set，
在重新定义之前只会删除受管理的 set。
这样可以防止已删除的 set 元素在重新载入后残留。
不会将运行中的 NAT 表格或过滤器表格删除后重建。
因此，即使 routerd 重新启动或进行一般配置变更，现有的 conntrack 条目仍会保留在内核的状态表格中。

在 FreeBSD 上，routerd 以 `pfctl -f` 载入生成的 pf 规则。
pf 在重新载入规则时，只要不明确清除状态，就会保留现有的状态表格。
routerd 的一般应用处理不会清除 pf 的状态。
