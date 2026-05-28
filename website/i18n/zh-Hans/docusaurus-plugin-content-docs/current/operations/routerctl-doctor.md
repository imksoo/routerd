---
title: routerctl doctor
sidebar_label: routerctl doctor
---

# routerctl doctor — 运行时健康诊断

`routerctl doctor` 执行一组只读检查，回报当前 routerd 是否作为家庭网关正常工作。
不会变更主机状态。面向运维人员、CI、监控代理与下游工具（Prometheus exporter、
Web Console、LLM 辅助诊断等）。

## 使用方式

```sh
# 全部 area（默认）
routerctl doctor

# 单一 area
routerctl doctor dns

# 不执行主机命令（仅基于资源 status）
routerctl doctor --no-host

# 机器可读输出
routerctl doctor -o json
routerctl doctor -o yaml
```

选项与 `diagnose` 一致: `--config`, `--state-file`,
`--no-host` / `--host`, `-o` / `--output`, `--timeout`。

## Areas

| Area | 检查内容 |
| --- | --- |
| `wan` | `EgressRoutePolicy` 与 `HealthCheck` 的资源 status；IPv4 / IPv6 默认路由（`ip -4/-6 route show default`）。 |
| `dns` | `DNSResolver` 的资源 status；通过 `dig @127.0.0.1` 进行 A 记录探测。 |
| `dslite` | `DSLiteTunnel` 的资源 status；AFTR FQDN 的 AAAA 探测；tunnel device 存在性（`ip link show`）。 |
| `dhcpv6-pd` | `DHCPv6PrefixDelegation` 的 status（Bound、委派前缀）。PD 未取得时按设计为 **WARN**（不在 LAN 上广告损坏的 IPv6）。 |
| `nat` | `NAT44Rule` 的资源 status；`nft list table ip routerd_nat` 存在。 |
| `firewall` | `FirewallZone` / `FirewallPolicy` 的 status；`nft list table inet routerd_filter` 存在且 input 链 `policy drop`（否则视为 permissive）。 |
| `rollback` | 至少存在一个已保存世代，使 `routerctl rollback --to` 可用。 |
| `disk` | `/var/lib/routerd` 与 `/run/routerd` 的容量。≥90% 或 `<256 MiB` 时 WARN，≥98% 或 `<64 MiB` 时 FAIL。 |
| `mgmt` | 管理接口的存在性（从 `ManagementAccess` 或 `FirewallZone role=mgmt` 推断）；WebConsole 的 bind（`0.0.0.0` / `::` 为 WARN/FAIL）。 |
| `reconcile` | 从只读状态 socket 读取每个 controller 的 reconcile 失败历史。`--since <duration>` 限定时间窗口。窗口内 ≥1 条为 WARN，≥10 条为 FAIL；detail 中最多展示 5 条样本。 |
| `runtime` | 从只读状态 socket 读取 routerd 自身的 heap / goroutine / fd：`heapAlloc`、`heapObjects`、`numGoroutine`、`numGC`、`openFds`/`maxFds`。当 `numGoroutine` 超过 10000，或打开的 fd 达到 `RLIMIT_NOFILE` 的 80% 以上时为 WARN。仅作观测，不会 FAIL。 |

每个检查返回 `pass`、`warn`、`fail`、`skip`（资源或信号不存在）之一。

## JSON 输出契约

`routerctl doctor -o json` 是**稳定的**机器可读接口。形式：

```jsonc
{
  "summary": {
    "overall": "pass",      // "pass" | "warn" | "fail" | "skip"
    "pass": 7,
    "warn": 1,
    "fail": 0,
    "skip": 2
  },
  "checks": [
    {
      "area":   "dns",                          // 见上方 Areas 表
      "name":   "DNSResolver/lan-resolver",     // 人类可读对象名
      "status": "warn",                         // "pass" | "warn" | "fail" | "skip"
      "detail": "phase=Degraded,waiting=...",   // 可选
      "remedy": "wait for or repair dependency wan-pd" // 可选
    }
    // ...
  ]
}
```

保证：

- `summary.overall` 取 `checks[].status` 的最差值（`fail` > `warn` > `unknown`/`skip` > `pass`）。
- `summary.pass/warn/fail/skip` 为整数计数，其和等于 `len(checks)`。
- `checks[].status` 仅取 `pass`、`warn`、`fail`、`skip`（不会出现其他值）。
- `checks[].area` 取自 Areas 表中的稳定标识符集合。
- `checks[].name` 为人类可读，请勿对其精确形式做模式匹配。
- `detail` / `remedy` 为可选自由文本，面向运维人员。

例如 `routerctl doctor runtime -o json` 会从只读状态 socket 展示 routerd
自身的进程 footprint：

```jsonc
{
  "summary": { "overall": "pass", "pass": 1, "warn": 0, "fail": 0, "skip": 0 },
  "checks": [
    {
      "area": "runtime",
      "name": "process",
      "status": "pass",
      "detail": "heapAlloc=11.0MiB heapObjects=84213 numGoroutine=187 numGC=14 openFds=23/1024"
    }
  ]
}
```

## 退出码

- `0` — 没有 `fail` 检查（`pass`、`warn`、`skip` 均不视为失败）。
- 非 0 — 至少一个 `fail`。可写为 `routerctl doctor || alert`。

`warn` 不会让退出码变为非 0（例如开机后 DHCPv6-PD 尚未 Bound 这类信息性情况）。
若要更严的门禁，请明确选择 area（如 `routerctl doctor wan` 仅在 `wan` fail 时非 0）。

## 稳定性

JSON 形式、area 标识符与 status 枚举是 v1alpha1 的运维契约。后续版本**可能新增 area 或可选字段**，
但已有的 area 名称与 status 取值在 v1alpha1 的 minor 版本之间不会改名或改义。

## 参见

- [调整（reconcile）与回滚](./reconcile.md)
- [`routerctl ledger` 维护](./reconcile.md#删除)
