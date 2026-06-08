---
title: CloudEdge 自主实验室 (cloudedge-labctl)
---

# CloudEdge 自主实验室 (`cloudedge-labctl`)

![cloudedge-labctl 的实验室生命周期、冒烟与故障转移操作、证据收集、dry-run 默认、TTL 标签、清理守卫的流程](/img/diagrams/how-to-cloudedge-autonomous-lab.png)

> 实验性功能（CloudEdge）。这是一个单命令线束，允许代理在无需人工审查 Runbook 的情况下运行 CloudEdge **Selective Address Mobility (SAM)** 故障转移实验室。该线束固定接口，并实现所有非云端逻辑（run-id/标签规范、TTL + 清理成本守卫、故障原语、连接矩阵、证据组装）。实际的逐提供商配置要么封装现有的 [`examples/cloudedge-mobility-demo/`](https://github.com/imksoo/routerd/tree/main/examples/cloudedge-mobility-demo) 包，要么标记为 `TODO(lab-operator)` 以供 Terraform/CLI 集成。

线束为 `scripts/cloudedge-labctl.sh`，有 2 个辅助工具：

- `scripts/cloudedge-connectivity-matrix.sh` — 有向 ping+ssh 矩阵 + 断言。
- `scripts/cloudedge-evidence-schema.json` — 运行证据的 JSON schema。

`--help`、dry 路径、`down --expired` **不需要云端凭据**。

## 生命周期

```sh
scripts/cloudedge-labctl.sh up        --profile full --provider aws,oci,azure,onprem --ttl 4h
scripts/cloudedge-labctl.sh deploy    --commit HEAD          # 或 --build <dist path>
scripts/cloudedge-labctl.sh smoke     --matrix d3 --out /tmp/matrix.json
scripts/cloudedge-labctl.sh failover  --provider aws --fault stop-active
scripts/cloudedge-labctl.sh smoke     --matrix d3 --out /tmp/matrix-after.json
scripts/cloudedge-labctl.sh evidence  collect --out evidence/<run-id> --matrix-json /tmp/matrix-after.json
scripts/cloudedge-labctl.sh down      --run-id <run-id> --force
```

`up` 将 **run-id** 输出到 stdout。请捕获此值并传递给后续命令。云端变更**默认为 DRY**（`CE_DRY_RUN=1`）。在凭据和预算获批后设置 `CE_DRY_RUN=0` 实际执行。

## 配置文件

| 配置文件 | 站点数 | 用途 |
| --- | --- | --- |
| `minimal` | on-prem + 1 个云端 | 最低成本冒烟，接口/CI 验证 |
| `provider` | 1 个提供商的 A/B 路由器 + 客户端 | 提供商对等性（AWS/OCI/Azure seize） |
| `full` | on-prem + AWS + OCI + Azure | 4 站点 `/24` 12 流演示 |
| `soak` | 持续 TTL 全程的 `full` 运行 | 长时间再收敛检查 |

`soak` 在操作上是一个使用较长 `--ttl` 维持的 `full` 运行（TTL 到期前不要执行 `down`）。用于 BFD/BGP 再收敛验证。

## TTL 和成本策略

所有云端资源**必须**附加以下标签（辅助函数 `cloudedge_tags()` 输出，`up` 盖章）：

```text
routerd.cloudedge.run_id          <UTCdate>T<time>-cloudedge-<scenario>
routerd.cloudedge.owner
routerd.cloudedge.ttl_expires_at  绝对 UTC RFC3339
routerd.cloudedge.provider
routerd.cloudedge.purpose
```

成本守卫规则：

- `up --ttl <dur>` 盖章 `ttl_expires_at`。请为运行选择适当的最短 TTL。
- `down --run-id <id>` 清理一个运行。`down --expired` 清理所有超过 TTL 的运行（无实验室时安全地 no-op — exit 0）。
- `up` 预先验证 `--ttl`，对无效时长进行**硬性失败**（非零退出）。不会在成本守卫损坏/已过期的情况下启动实验室。
- 线束的 **EXIT trap** 会在 `up` 被中断或提供商**启动途中**失败时清理运行（仅在进行中的阶段激活，正常完成时解除。正常的 `up` 让实验室存续到显式 `down` 或 TTL 到期）。指定 `up --keep` 可保留部分状态供调查。
- 无论是否失败，每次运行后都必须执行 `down`（或由 janitor 执行 `down --expired`）。超过 TTL 的实验室可在无 run-id 的情况下清理。

## 故障原语 (`failover --fault`)

| 故障 | 含义 | 初始配线 |
| --- | --- | --- |
| `stop-active` | 停止活跃路由器 VM/实例 | 提供商 stop CLI（参见 `reset-lab.sh`） |
| `drain` | 在活跃的 MobilityPool 上设置 `maintenance.drain=true` | 复用 `run-demo.sh` 的 `*-drain.yaml` |
| `routerd-bgp-stop` | 停止 `routerd-bgp`（BGP 会话断开） | ssh `systemctl stop routerd-bgp` |
| `executor-fail` | 提供商操作 executor 拒绝（ID 范围缩小） | ID 策略 |
| `stale-replay` | 重放过时的 pathSig 操作。**必须**被 fence | `probe_stale_gate_on_aws_b` |

注入故障后重新运行 `smoke` 和 `evidence collect`，证明恢复情况。

## 证据 schema

`evidence collect --out <dir>` 输出 `<dir>/result.json`。通过 `scripts/cloudedge-evidence-schema.json` 验证，同时输出 `summary.md` 和（如指定）连接矩阵 JSON。格式：

```json
{
  "runId": "20260601T031500Z-cloudedge-aws-failover",
  "commit": "<sha>",
  "scenario": "aws-active-stop-seize",
  "result": "pass",
  "providers": {
    "aws":    {"dataplane": "pass", "providerState": "pass"},
    "oci":    {"dataplane": "pass", "providerState": "pass"},
    "azure":  {"dataplane": "pass", "providerState": "pass"},
    "onprem": {"dataplane": "pass", "providerState": "pass"}
  },
  "assertions": [
    {"name": "ownership_epoch_bumped", "result": "pass"},
    {"name": "allow_reassignment_maintained_until_success", "result": "pass"},
    {"name": "source_ip_preserved", "result": "pass"},
    {"name": "default_gateway_unchanged", "result": "pass"},
    {"name": "old_holder_residue_absent", "result": "pass"},
    {"name": "stale_action_fenced", "result": "pass"}
  ],
  "costGuard": {"ttlHours": 4, "teardown": "completed"}
}
```

数据平面检查和 `source_ip_preserved` / `default_gateway_unchanged` 从连接矩阵自动导出。seize/fencing 断言（`ownership_epoch_bumped`、`allow_reassignment_maintained_until_success`、`old_holder_residue_absent`、`stale_action_fenced`）和 `providerState` 初始为 `na`，由实验室操作员从提供商清单、BGP mobility 路径、提供商 trap 操作计划、操作日志中导入（参见 `collect-evidence.sh`）。运行判定为 **PASS** 的条件是 `result == pass` 且所有必需断言通过。

## 连接矩阵

`cloudedge-connectivity-matrix.sh` 在共享 `/24` 上执行所有有向 `src -> dst` 流，并对每个流断言：

- **source-IP-preserved** — 目的端将客户端站点的实际源 IP（mobility `/32`）识别为对等地址（无 NAT）。
- **default-gw-unchanged** — 源客户端的默认网关未变更。
- **no-NAT** — ping 到达目的端，SSH 的对等 IP 与源 IP 一致。

执行通过 `MATRIX_RUNNER` 间接指定，因此矩阵可**离线独立运行**（为 `MATRIX_RUNNER` 设置桩）。在实际实验室中，默认运行器对演示环境使用 `ssh`/`ping`。输出为逐流 JSON，可通过 `evidence collect --matrix-json` 导入。

## 自主性章程（概要）

代理拥有**实验室启动 -> 部署 -> 故障注入 -> 数据平面验证 -> 证据 -> 清理 -> issue/PR 更新**的完整循环，无需人工阅读 Runbook 即可执行。云端操作默认为 dry，由显式的凭据/预算审批进行门控。代理必须始终将实验室保持在已清理或 TTL 成本守卫内的状态，PASS 必须附带 schema 有效的证据包。

## 人工门控

以下情况仅需人工参与。其余全部自动化：

1. **预算** — 批准支出 / 提升 TTL 或预算上限。
2. **凭据/权限** — 提供云端凭据和 executor 使用的最小权限 ID/角色（secret 不提交，也不传递给插件）。
3. **合并** — PR 的最终审批。
4. **生产** — 生产环境部署（实验室线束绝不执行）。

## 注意事项

- 这是**实验室线束**，不是生产就绪的交钥匙方案。
- 初始实现：实际的逐提供商分配/清理/节点推送是 `TODO(lab-operator)` 的桩或演示包的薄层封装 — 请接入使用 run-id 标签过滤的 Terraform/OpenTofu 或提供商 CLI。
- 绝不提交实际的账户 ID / 订阅 ID / OCID / ENI/VNIC ID / secret / 私钥。使用 `env.example` 中的占位逻辑地址。
