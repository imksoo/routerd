# CloudEdge Mobility D5 AWS 维护冒烟测试

Result: PASS

日期: 2026-05-31
构建: main 99eb1d45
证据包: `/home/imksoo/routerd-labs/cloudedge-mobility/evidence/20260531T215831Z-d5-aws-rerun-99eb1d45`

## 场景

- 仅 AWS 的 D5 在线维护 / 捕获迁移。
- 复用现有活动路由器 A: `i-001f62ac01d66e782`, ENI-A `eni-0d17f203a6717e4d9`, 主 `10.77.60.4`。
- 为此次运行重新创建备用路由器 B: `i-045382a4f5bbf6fc0`, ENI-B `eni-017dd140722f5d819`, 主 `10.77.60.14`, `t3.small`。
- 复用 AWS 云客户端: `i-0c5d4e3578e7669a9`, `10.77.60.11`。
- 捕获地址: 本地客户端 `10.77.60.10/32`。

## 初始捕获

- A 导入并执行:
  - `assign-secondary-ip` epoch 1 (`10.77.60.10/32` 到 ENI-A)。
  - `ensure-forwarding-enabled` epoch 1 (针对 ENI-A)。
- 初始 execute 后的 AWS provider 状态:
  - ENI-A: `10.77.60.4,10.77.60.10`, `SourceDestCheck=false`。
  - ENI-B: `10.77.60.14`, `SourceDestCheck=true`。
- 迁移前数据平面:
  - cloud-client `10.77.60.11 -> 10.77.60.10` ping: `3/3`, `0% loss`。
  - 通过 SSH 以源地址保留到达本地客户端: `SSH_CONNECTION=10.77.60.11 ... 10.77.60.10 22`。

## Drain 和迁移

- 对路由器 A 宣告式地应用 `maintenance.drain=true`。
- A 导入 epoch 2 的 de-provision action:
  - `unassign-secondary-ip` (从 ENI-A 移除 `10.77.60.10/32`)。
  - `ensure-forwarding-disabled` (针对 ENI-A)。
- B 导入 epoch 2 的 active-capture action:
  - `assign-secondary-ip` (`10.77.60.10/32` 到 ENI-B)。
  - `ensure-forwarding-enabled` (针对 ENI-B)。
- A 的 unassign 成功执行, 从 ENI-A 删除 `.10`。
- B 的 assign 成功执行, 向 ENI-B 添加 `.10`。
- 迁移后的 AWS provider 状态:
  - ENI-A: `10.77.60.4`, `SourceDestCheck=true`。
  - ENI-B: `10.77.60.14,10.77.60.10`, `SourceDestCheck=false`。
- 捕获 epoch 收敛至持有者 `aws-router-b`, epoch `2`。

## Epoch 围栏

- A 的 epoch 1 action 在 drain 前成功。
- A 的 epoch 2 unassign 和 forwarding-disable 在执行前保留在日志中。
- B 的 epoch 2 assign 和 forwarding-enable 成功执行。
- 通过非 provider 的日志探针验证 stale 门控:
  - 将同一捕获键的 epoch 1 pending action 作为 `d5-rerun-stale-probe-epoch1` 插入;
  - `routerctl action import` 将其变更为 `status=skipped`;
  - 结果消息: `stale mobility capture epoch`。

## 迁移后数据平面

- B 侧 `doctor hybrid`: PASS。
- B 侧 `routerd_mss`: `ens5 -> wg-hybrid` 存在。
- 本地 `routerd_mss`: `ens21 -> wg-hybrid` 存在。
- neighbor 刷新后, cloud-client `10.77.60.11 -> 10.77.60.10` ping 在 3 轮连续中 `3/3` 通过。
- 通过 B 以 SSH 源地址保留到达本地客户端:
  - `SSH_CONNECTION=10.77.60.11 ... 10.77.60.10 22`。
- 客户端默认网关未变更: `default via 10.77.60.1`。

## Teardown

- 从 ENI-B 删除 `10.77.60.10`。
- ENI-A 和 ENI-B 恢复 `SourceDestCheck=true`。
- IAM 内联策略恢复到 B 范围之前的文档。
- 终止 B。
- 停止 A 和 cloud-client。
- 最终成本状态:
  - A: `stopped`。
  - cloud-client: `stopped`。
  - B: `terminated`。
  - ENI-A 基线: 仅 `10.77.60.4`, `SourceDestCheck=true`。
