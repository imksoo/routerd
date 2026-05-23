---
title: 状态数据库
slug: /operations/state-database
---

# 状态数据库

routerd 将状态与事件持久化至 SQLite。每个守护进程除此之外还各自拥有自身的租约或状态文件与事件日志。

## 主要路径

| 种类 | 路径 |
| --- | --- |
| routerd 状态 DB | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD 租约 | `/var/lib/routerd/dhcpv6-client/<name>/lease.json` |
| DHCPv4 租约 | `/var/lib/routerd/dhcpv4-client/<name>/lease.json` |
| PPPoE 状态 | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| HealthCheck 状态 | `/var/lib/routerd/healthcheck/<name>/state.json` |
| 守护进程别事件 | `/var/lib/routerd/<daemon>/<name>/events.jsonl` |

## events 数据表

事件总线会将事件持久化至 SQLite。`EventRule` 与 `DerivedEvent` 以此流作为输入。
日常运维请使用 `routerctl events`，而非直接操作 `sqlite3`：

```sh
routerctl events --limit 20
routerctl events --topic routerd.resource.status.changed
routerctl events --resource DNSResolver/lan-resolver -o json
```

## 备份思路

状态 DB 保存的是**已观测到**的状态，无法取代配置。
意图的正本是 YAML 配置文件，请以 git 管理。
重建主机时，比起还原 SQLite，应用配置文件并让 routerd 进行调和（reconcile）更为可靠。

若出于取证目的需要保留操作事件历史，请定期为 `events.db`、`dns-queries.db`、`traffic-flows.db`、`firewall-logs.db` 创建快照。这些文件为仅追加模式，不需要像 `routerd.db` 那样进行特定时间点的备份。

## 相关说明

- [日志存储](../concepts/log-storage.md)
- [调和（Reconcile）与删除](./reconcile)
