---
title: 通过门控 executor 执行 AWS SAM 提供商操作
---

# 通过门控 executor 执行 AWS SAM 提供商操作

![通过只读 preflight、门控日志审批、executor IAM 身份、可逆 AWS 变更来执行 AWS SAM 提供商操作的流程](/img/diagrams/how-to-aws-provider-action-execution.png)

:::warning 实验性功能 — Phase 5.1
这是 CloudEdge 提供商操作执行的**门控实时变更**路径。
**实验性**功能，仅限 AWS。
构建在 [ADR 0007: Provider Action Execution](../adr/0007-provider-action-execution.md) 和
[Selective Address Mobility](../reference/selective-address-mobility)
数据平面之上。**请勿**对生产环境或共享资源执行实时变更步骤。实时执行仅在审查本 Runbook 和只读 preflight 证据后，**获得所有者明确批准后**进行。
:::

SAM 数据平面已在 AWS x PVE（ENI 辅助私有 IP + source/dest check 禁用）上通过实际云端验证。此前这些 attach/detach 操作都是**操作员手动**执行的。本指南介绍 `aws-provider-executor` 插件，该插件通过**门控日志化**执行路径（ADR 0007）代替手动操作来执行相同的变更。

## 1. 范围和边界

- **仅限 AWS。只有一个提供商。** 本 Runbook 不涉及 Azure 或 OCI。
- **拓扑:** 1 台 `routerd-cloud` 节点 + 1 台 cloud-client + 1 台 on-prem-client，从 on-prem 迁移到 cloud ENI 的捕获 **`/32` 仅 1 个**。实验室地址遵循 SAM 参考：cloud-client 为 `.7`，on-prem-client 为 `.9`。
- **仅限专用实验室。** 使用为此测试创建的一次性 VPC / 子网 / 实例。**不使用生产或共享资源。** 没有其他依赖的 EIP、安全组、路由表或实例。
- **实时执行仅在所有者明确批准后进行。** 只读 preflight（第 4 节）可随时执行。第 7 节的变更操作是门控的。

## 2. Executor 设计

`aws-provider-executor` 是一个通告 `execute.providerAction` 能力（`PluginSpec.Capabilities` 的 Phase 5 枚举值）的插件。它作为**独立进程**运行，通过 AWS CLI 使用 **EC2 实例 IAM 角色（实例配置文件）** 进行认证。**routerd 核心不传递任何凭据** — executor 仅使用云原生身份，遵循 ADR 0007 的硬性不变量。

executor 从 **stdin** 读取 1 个 `ExecuteActionRequest`，向 stdout 输出 1 个 `ExecuteActionResult`。请求规范包含 `Action`、`Provider`、`ProviderRef`、`Target`（提供商键：AWS 为 `nicRef` = ENI id、`address`、`region`）、`Parameters`、`Mode`（`dry-run` | `execute`）、`IdempotencyKey`、允许列表中的 `Context`。结果包含 `Status`（`succeeded` | `failed` | `skipped`）、`Message`、`Observed`（日志记录的非机密事实）、`UndoAvailable`、`Error`。

**`dry-run` 模式不执行任何变更** — 仅进行 describe / 只读调用。`execute` 模式执行变更。

### `assign-secondary-ip`

将捕获的 `/32` 附加到 cloud ENI。

- **dry-run**（只读）：describe ENI 并报告当前辅助 IP，输出 `would assign <address> to <eni>`。

  ```sh
  aws ec2 describe-network-interfaces \
    --network-interface-ids "<eni-id>" --region "<region>"
  ```

- **execute**（变更）：

  ```sh
  aws ec2 assign-private-ip-addresses \
    --network-interface-id "<eni-id>" \
    --private-ip-addresses "<address>" --region "<region>"
  ```

### `ensure-forwarding-enabled`

禁用 ENI 的 source/dest check，使 cloud 节点能够转发捕获地址的流量。

- **dry-run**（只读）：describe 当前 `SourceDestCheck`，输出 `would set SourceDestCheck=false`。

- **execute**（变更）：**首先 describe 当前 `SourceDestCheck`，将变更前的值记录到 `Observed`，然后**禁用。

  ```sh
  # 1. 变更前捕获先前状态（只读）
  aws ec2 describe-network-interfaces \
    --network-interface-ids "<eni-id>" --region "<region>" \
    --query 'NetworkInterfaces[0].SourceDestCheck'

  # 2. 变更
  aws ec2 modify-network-interface-attribute \
    --network-interface-id "<eni-id>" \
    --no-source-dest-check --region "<region>"
  ```

  结果的 `Observed` 中**必须**包含 `priorSourceDestCheck=<true|false>`。这样日志就记录了此操作执行前存在的状态。undo 步骤依赖此值。

### `unassign-secondary-ip`（`assign-secondary-ip` 的 undo）

```sh
aws ec2 unassign-private-ip-addresses \
  --network-interface-id "<eni-id>" \
  --private-ip-addresses "<address>" --region "<region>"
```

### `ensure-forwarding-disabled`（`ensure-forwarding-enabled` 的 undo）

**恢复日志 `Observed.priorSourceDestCheck` 中记录的变更前状态。**
这是支撑安全性的关键规则：

- `priorSourceDestCheck == true` → 操作前 check 是启用的 → 恢复：

  ```sh
  aws ec2 modify-network-interface-attribute \
    --network-interface-id "<eni-id>" \
    --source-dest-check --region "<region>"
  ```

- `priorSourceDestCheck == false` → 操作前**已经禁用**（ENI 已是转发器）→ **不做任何操作**。返回 `Status=skipped`。**不要**强制重新启用 check。

**不要将 undo 硬编码为启用 check。** 盲目地"undo 时重新启用 source/dest-check"会破坏因自身原因已作为转发器运行的设备/ENI。undo 必须读回观测值，仅恢复实际变更的部分。

## 3. IAM 最小权限

附加到 executor EC2 实例的实例配置文件应仅授予**以下 4 个 EC2 操作**：

| 操作 | 使用场景 |
|------|---------|
| `ec2:DescribeNetworkInterfaces` | dry-run + preflight + 变更前状态捕获 |
| `ec2:AssignPrivateIpAddresses` | `assign-secondary-ip` 的 execute |
| `ec2:UnassignPrivateIpAddresses` | `unassign-secondary-ip` 的 undo |
| `ec2:ModifyNetworkInterfaceAttribute` | forwarding 的启用/禁用 execute |

为将范围限定到实验室 ENI / VPC，在 API 支持的范围内设置资源 ARN 和条件（变更类 ENI 操作可按实验室 ENI ARN 限定资源范围，`Describe*` 不支持资源范围限定，因此使用 `ec2:Region` / `ec2:Vpc` 等条件键限制）：

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "DescribeEnis",
      "Effect": "Allow",
      "Action": "ec2:DescribeNetworkInterfaces",
      "Resource": "*",
      "Condition": { "StringEquals": { "ec2:Region": "<region>" } }
    },
    {
      "Sid": "MutateLabEni",
      "Effect": "Allow",
      "Action": [
        "ec2:AssignPrivateIpAddresses",
        "ec2:UnassignPrivateIpAddresses",
        "ec2:ModifyNetworkInterfaceAttribute"
      ],
      "Resource": "arn:aws:ec2:<region>:<account-id>:network-interface/<eni-id>"
    }
  ]
}
```

**不需要更多 EC2 权限。不需要 IAM/STS 写权限。不需要其他 AWS 服务。** 如果所需调用不在此列表中，请停止 Runbook 而不是扩大角色权限。

## 4. 只读 preflight

在**变更之前**，对专用实验室执行以确认目标。**这些操作均不执行变更。** lab-codex 执行这些操作并捕获输出，作为所有者批准前审查的证据。

```sh
# 目标 ENI + 当前辅助私有 IP + 当前 SourceDestCheck
aws ec2 describe-network-interfaces \
  --network-interface-ids "<eni-id>" --region "<region>" \
  --query 'NetworkInterfaces[0].{Eni:NetworkInterfaceId,SrcDstCheck:SourceDestCheck,PrivateIps:PrivateIpAddresses[*].PrivateIpAddress}'

# ENI 附加的实例
aws ec2 describe-instances \
  --filters "Name=network-interface.network-interface-id,Values=<eni-id>" \
  --region "<region>"

# ENI 的子网
aws ec2 describe-subnets \
  --subnet-ids "<subnet-id>" --region "<region>"

# 该子网的路由表（确认默认网关无意外变更）
aws ec2 describe-route-tables \
  --filters "Name=association.subnet-id,Values=<subnet-id>" \
  --region "<region>"
```

确认事项：

1. **IAM 角色仅具有第 3 节的 4 个权限** — 检查实例配置文件附加的策略，验证没有宽泛的 EC2 权限、IAM/STS 写权限或其他服务。（这是策略文档的只读检查，不做任何变更。）
2. **地址尚未分配** — 确认 `<address>` **尚未**包含在上述第一个 describe 获取的 ENI `PrivateIpAddresses` 中。如果已包含，assign 将是空操作，说明实验室状态不干净 — 停止并调查。
3. **`SourceDestCheck` 的当前值已记录** — 此值是 executor 在 execute 时作为 `priorSourceDestCheck` 捕获的值。

## 5. 冒烟测试依赖的操作日志字段

`action_executions` 日志为每个操作记录以下内容：

- `idempotencyKey` — 去重键。已成功的键不会被重新执行。
- `provider` — `aws`。
- `action` — 如：`assign-secondary-ip`、`ensure-forwarding-enabled`。
- `target` — `eni`、`address`、`region`。
- `status` — `pending` / `approved` / `succeeded` / `failed` / `skipped` / `rolledBack`。
- `Observed.priorSourceDestCheck` — `true` | `false`。变更前捕获的值，`ensure-forwarding-enabled` 的 undo 读取此值。
- `executedAt` — 时间戳。
- `result` / `error` — `ExecuteActionResult` 的消息 / `Error`。

日志是执行内容和幂等性守卫的唯一可信来源。凭据**绝不**记录在日志中。

## 6. Undo / 清理计划

按逆序撤销已应用的操作。所有步骤必须在实时执行**之前**就能描述。

1. **Forwarding 的 undo** — `ensure-forwarding-disabled`。应用第 2 节的**变更前状态恢复规则**：如果 `Observed.priorSourceDestCheck` 为 `true`，执行 `--source-dest-check` 重新启用。如果为 `false`，**不做任何操作**（skipped）。不要盲目启用 check。
2. **取消辅助 IP 分配** — `unassign-secondary-ip`：

   ```sh
   aws ec2 unassign-private-ip-addresses \
     --network-interface-id "<eni-id>" \
     --private-ip-addresses "<address>" --region "<region>"
   ```
3. **停止/终止实验室实例并释放产生费用的资源** — 停止或终止 `routerd-cloud`、cloud-client、on-prem-client 实验室实例。释放已分配的 **EIP**，删除孤立的 **EBS** 卷，删除为此测试专门创建的 VPC/子网/SG。

**在捕获证据后，停止或删除所有产生费用的资源。** 不要让实验室实例处于空闲状态。

## 7. 实时变更冒烟计划 + 验收

此冒烟测试验证整个门控路径。仅在第 9 节的门控获得批准后执行。

序列：

1. `actionPlan` 生成（planner、dry-run，与 Phase 4.1 相同）。
2. 操作作为 `pending` **导入**到日志中（以 `idempotencyKey` 为键）。
3. 操作被**批准**（`routerctl action approve`）。
4. 操作由 **`aws-provider-executor` 执行**（`routerctl action execute --approved`）。
5. 日志中显示 `succeeded`。

验收条件（必须全部满足）：

- [ ] actionPlan 生成 -> 导入 -> 批准 -> 执行 -> 日志 `succeeded`。
- [ ] **辅助 IP 存在于 ENI 上**（`describe-network-interfaces` 中 `<address>` 显示在 `PrivateIpAddresses` 中）。
- [ ] ENI 的 **Source/dest check 已禁用**（`SourceDestCheck=false`）。日志中记录了 `Observed.priorSourceDestCheck`。
- [ ] 如果 `configureOSAddress=false`，`routerd-cloud` **不持有**该地址作为 OS 本地地址（捕获仅用于路由/转发，无 OS 地址）。
- [ ] `RemoteAddressClaim` 达到 **Ready** 状态。
- [ ] `routerctl doctor` 的 hybrid 检查**通过**。
- [ ] cloud-client **`.7`** 和 on-prem-client **`.9`** — **双向 ping 和 ssh** 成功。
- [ ] 捕获路径上**不存在 NAT**（流量被路由/转发，而非转换）。
- [ ] 所有节点的**默认网关未变更**。
- [ ] 第 6 节的 **Teardown / undo 成功**（包括 source/dest-check 的变更前状态恢复规则）。
- [ ] 证据捕获后**产生费用的资源已停止/删除**。

## 8. 硬性停止

如果出现以下任何情况，立即中止（不采取"变通方案"）：

1. 凭据**经过 routerd 核心传递**（不允许 — executor 仅使用自身的实例配置文件）。
2. 操作**影响非实验室资源**。
3. **涉及多个提供商**。
4. **无法事先描述回滚/清理计划。**
5. 提供商 API 返回**模糊/部分成功**。
6. **产生费用的资源在没有活跃测试的情况下持续运行。**
7. 云资源运行期间等待人工判断**超过 10 分钟** → **停止并释放资源**（停止实例以降低成本）。判断后恢复。
8. 任何命令**意味着对生产或共享资源的变更**。

## 9. 实时执行的门控

实时变更仅在所有者审查**本 Runbook** 和**只读 preflight 证据**（第 4 节）后**明确批准后**执行。在获得批准前，只能执行只读步骤。
