---
title: 状态数据库
slug: /operations/state-database
---

# 状态数据库

![Diagram showing routerd state database paths, daemon lease and event files, routerctl get events access, and backup philosophy where YAML remains authoritative and event databases provide forensic history](/img/diagrams/operations-state-database.png)

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
日常运维请使用 `routerctl get events`，而非直接操作 `sqlite3`：

```sh
routerctl get events --limit 20
routerctl get events --topic routerd.resource.status.changed
routerctl get events --resource DNSResolver/lan-resolver -o json
```

### Mobility holder transitions

CloudEdge SAM failover 会发出 `routerd.mobility.holder.transition` 事件，
其中包含 `transitionKind`、`address`、`timestamp`、`issuedAt`、
`fromNode`、`toNode`、`mobilityPathSig`、`assignmentGeneration` 等机器可读属性。

在 provider-secondary-IP capture 流程中，`seize-complete` 表示 active `/32`
`bgpCaptureAssignment` 对应的 provider capture assign action 已在 action journal
中记录为 succeeded。`issuedAt` 使用 journal 的 `ExecutedAt`，因此
`timestamp - issuedAt` 表示从 provider 接受写入到事件记录之间的延迟。
`T_seize` 是 provider 接受写入的时间。

`capture-confirmed` 仍然基于 discovery 观测。`T_confirm` 是本地进程观测到
provider capture 生效的时间。两者共同度量从接受到生效的区间。

节点重启或 rejoin 后的重新确认事件可能会复用原始 journal 接受时间作为
`issuedAt`。此时，`timestamp - issuedAt` 会包含节点停止或缺席的时间。
请将这类差值视为重新确认经过时间，不要解释为收敛延迟。

对于 static-owned、static-handover、local-home 等非 capture 流程，
`seize-complete` 仍来自 active-holder 加 self-identity 的 BGP 观测。目前 lab
实证仅覆盖 capture 流程；static/handover completion event 尚未在真实环境中实证。

## 备份思路

状态 DB 保存的是**已观测到**的状态，无法取代配置。
意图的正本是 YAML 配置文件，请以 git 管理。
重建主机时，比起还原 SQLite，应用配置文件并让 routerd 进行调和（reconcile）更为可靠。

若出于取证目的需要保留操作事件历史，请定期为 `events.db`、`dns-queries.db`、`traffic-flows.db`、`firewall-logs.db` 创建快照。这些文件为仅追加模式，不需要像 `routerd.db` 那样进行特定时间点的备份。

## 相关说明

- [日志存储](../concepts/log-storage.md)
- [调和（Reconcile）与删除](./reconcile)
