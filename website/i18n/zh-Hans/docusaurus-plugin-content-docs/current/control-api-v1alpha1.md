---
title: 控制 API v1alpha1
slug: /reference/control-api-v1alpha1
---

# 控制 API v1alpha1

![Diagram showing the Control API v1alpha1 local socket model from routerd main sockets and managed daemon sockets to status, events, commands, resource phases, and local-only client contracts](/img/diagrams/control-api-v1alpha1.png)

routerd 与受管理的守护进程会在本机的 Unix domain socket 上公开 HTTP+JSON API。
此 API 并非用于远程管理，而是供 `routerctl`、routerd 本体以及运维脚本在同一主机上读取状态所使用。

## routerd 本体

`routerd serve` 监听以下 socket：

```text
/run/routerd/routerd.sock
/run/routerd/routerd-status.sock
```

主控制 socket 供具有权限的本机客户端使用，并公开应用（apply）、删除等变更类 endpoint。只读的 status socket 仅公开状态查询类 endpoint，供普通用户确认系统状态。

主控制 socket 的读取 endpoint 可返回状态、事件及资源状态，主要示例如下：

| Method + Path | 用途 |
| --- | --- |
| `GET /api/control.routerd.net/v1alpha1/status` | routerd 自身的状态 |
| `GET /api/control.routerd.net/v1alpha1/connections` | 从 conntrack 或 pf state 获取的当前连接 |
| `GET /api/control.routerd.net/v1alpha1/dns-queries` | DNS 查询历史 |
| `GET /api/control.routerd.net/v1alpha1/traffic-flows` | 通信流量历史 |
| `GET /api/control.routerd.net/v1alpha1/firewall-logs` | 防火墙日志 |

## SAM enrollment endpoint

Dynamic SAM enrollment 也使用同一 API handler，但 RR 只能通过 enrollment runbook 中明确
配置的已认证 listener 或有权限的本地 socket 公开这些 endpoint。它们不是通用的远程管理 API。

| Method + Path | 用途 |
| --- | --- |
| `POST /api/control.routerd.net/v1alpha1/sam-enrollment-claims` | 接纳一个 leaf 的 client-authored enrollment claim。 |
| `POST /api/control.routerd.net/v1alpha1/sam-enrollment-claims/{name}/revoke` | 明确 revoke 已接纳的 claim；不要将其用于恢复。 |
| `GET /api/control.routerd.net/v1alpha1/sam-enrollment-topologies/{name}?claim=...&claimDigest=...&claimIdentityDigest=...` | 获取指定的 RRSet，以及仅在已证明时获取可选的直连 peer group。 |

`claimIdentityDigest` 是只包含 client-authored claim material 的 hash。配置 `joinTokenFrom` 时由
join HMAC 认证；未配置时属于 trusted control-plane。当前 RR 用它区分明确 revoke、clean boot 后
空的 admission store 和不同的 active identity。只有检查过所有 bootstrap RR 后，
`SAMEnrollmentClient` 才会把 identity-aware 缺失响应视为重新提交当前 direct claim 的许可。不同
active identity 只可在初次 admission 或本地 claim 变化时替换，periodic renewal 绝不替换。缺少
digest、旧版/混合 RR、revoke 或任何 request failure 都必须保留 RR-only fallback；调用方应使用
client/controller，而不是自行解释这些响应。

## Controller status

`Status.status.controllers` 与 `Controllers` endpoint 会返回控制器在配置上的 mode，以及运行时的调和（reconcile）状态。runtime 字段包含 `interval`、`lastTrigger`、`lastReconcileTime`、`nextReconcileTime`、`reconcileCount`、`reconcileErrorCount`、`consecutiveErrorCount`、`currentError`、`lastDuration`、`maxDuration`、`averageDuration`、`lastError`、`lastErrorTime`、`lastErrorClearedAt`。`reconcileErrorCount` 为累计值，如需判断当前是否处于失败状态，请使用 `currentError` 与 `consecutiveErrorCount`。这些均为观测值，若控制器尚未执行过，请视为字段不存在。

## 受管理的守护进程

具有状态的守护进程各有其专属的 socket：

```text
/run/routerd/dhcpv6-client/wan-pd.sock
/run/routerd/dhcpv4-client/wan.sock
/run/routerd/pppoe-client/wan-pppoe.sock
/run/routerd/healthcheck/internet.sock
```

在 FreeBSD 上，对应路径为 `/var/run/routerd/...`。

## 守护进程通用 endpoint

| Method + Path | 用途 |
| --- | --- |
| `GET /v1/healthz` | 存活确认（liveness check） |
| `GET /v1/status` | 守护进程的状态及相关资源的状态 |
| `GET /v1/events` | 事件日志。可通过 query 参数指定 `since`、`wait`、`topic` |
| `POST /v1/commands/reload` | 重新加载配置 |
| `POST /v1/commands/renew` | 各守护进程的主动操作（DHCPv6 Renew、DHCPv4 更新租约、立即执行健康探测等） |
| `POST /v1/commands/stop` | 安全停止 |

`renew` 的含义因守护进程而异。DHCPv6 为发送 Renew、DHCPv4 为更新租约、healthcheck 为立即执行探测。

## Phase 词汇

`ResourceStatus.phase` 跨资源使用通用词汇：

| Phase | 说明 |
| --- | --- |
| `Pending` | 等待必要的输入 |
| `Bound` | 持有 DHCP 等租约 |
| `Applied` | 已应用至主机端 |
| `Up` | tunnel 或 link 已启动 |
| `Installed` | 路由或配置文件已就位 |
| `Healthy` | 健康检查已达成功阈值 |
| `Unhealthy` | 健康检查已达失败阈值 |
| `Error` | 操作失败 |

每个 phase 均附有 `conditions` 数组。客户端程序应使用 `phase` 与 `conditions` 进行判断，而非解析日志字符串。

## 事件

事件具有 topic 与 attributes：

```json
{
  "topic": "routerd.dhcpv6.client.prefix.renewed",
  "attributes": {
    "resource.kind": "DHCPv6PrefixDelegation",
    "resource.name": "wan-pd"
  }
}
```

routerd 将事件持久化至 SQLite。
受管理的守护进程另外也会记录至各自的 `events.jsonl`。
`EventRule` 与 `DerivedEvent` 以此流为输入，发布虚拟事件。
