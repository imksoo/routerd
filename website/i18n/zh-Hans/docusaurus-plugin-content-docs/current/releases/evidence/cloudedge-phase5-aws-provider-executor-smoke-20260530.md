# CloudEdge Phase 5.1 AWS Provider Executor 冒烟测试

Result: PASS

日期: 2026-05-31 UTC  
分支/构建: `main` / `routerd v20260528.2308 (92f4cc94)` (附带 `execute.providerAction` 的本地验证器修复)  
证据包: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260530T235341Z-phase5-aws-rebaseline-92f4cc94`

## 范围

- Provider mutation 对象: 仅 AWS。
- 账户/区域: `350538780953` / `ap-northeast-1`。
- 复用的 routerd 专用 SAM 实验室: `SourceLab=routerd-cloudedge-sam-aws-pve`。
- 目标路由器实例: `routerd-cloud-aws` / `i-05b6cfd2b3e4e0da6`。
- 目标客户端实例: `aws-cloud-client` / `i-0ae791389518353d6`。
- 目标 ENI: `eni-0904ccbed8d383f65`。
- 捕获地址: `10.88.60.9`。

## 重置基线

mutation 前, 将现有 SAM 实验室重置为全新的 provider 基线:

- 从 `eni-0904ccbed8d383f65` 删除 `10.88.60.9` 辅助私有 IP。
- ENI 恢复 `SourceDestCheck=true`。
- 重置后证据: `aws-router-eni-post-reset.json`, `aws-router-eni-post-reset-confirm.json`。

## IAM 门控

`routerd-cloud-aws` 接收了 executor 用的 EC2 实例配置文件。

内联策略仅允许:

- `ec2:DescribeNetworkInterfaces`
- `ec2:AssignPrivateIpAddresses`
- `ec2:UnassignPrivateIpAddresses`
- `ec2:ModifyNetworkInterfaceAttribute`

mutation 权限范围:

- 区域: `ap-northeast-1`
- ENI ARN: `arn:aws:ec2:ap-northeast-1:350538780953:network-interface/eni-0904ccbed8d383f65`
- 资源标签: `Project=routerd-cloudedge-phase5`

路由器上的实例角色 preflight 通过:

- `aws sts get-caller-identity` 返回 `arn:aws:sts::350538780953:assumed-role/routerd-phase5-aws-executor-role/i-05b6cfd2b3e4e0da6`。
- `aws ec2 describe-network-interfaces` 可读取目标 ENI。

## Executor 执行

在 `routerd-cloud-aws` 上构建并安装了 `aws-provider-executor`。

导入、批准、dry-run 并执行了 2 个 action journal 条目:

- `assign-secondary-ip`
  - Result: `succeeded`
  - Message: `assigned 10.88.60.9 to eni-0904ccbed8d383f65`
- `ensure-forwarding-enabled`
  - Result: `succeeded`
  - Message: `disabled SourceDestCheck on eni-0904ccbed8d383f65 (prior=true)`
  - 观察到的 journal fact: `priorSourceDestCheck=true`

mutation 后的 AWS 验证:

- ENI 主: `10.88.60.4`
- ENI 辅助: `10.88.60.9`
- `SourceDestCheck=false`

## 数据平面验证

云侧:

- `routerctl doctor hybrid`: `overall=pass`, `pass=12`, `warn=0`, `fail=0`, `skip=1`。
- 传递路由: `10.88.60.9 dev wg-hybrid metric 120`。
- 本地 OS 地址不存在: `10.88.60.9/32 absent from local interfaces`。
- MSS clamp: `routerd_mss covers ens5 -> wg-hybrid`。

本地侧:

- router07 `routerctl doctor hybrid`: `overall=pass`, `pass=13`, `warn=0`, `fail=0`, `skip=1`。
- 云客户端 `10.88.60.7` 的 Proxy ARP claim 保持健康。

客户端连接性:

- cloud-client `10.88.60.7` -> onprem-client `10.88.60.9` ping: `3/3`, `0% packet loss`。
- onprem-client `10.88.60.9` -> cloud-client `10.88.60.7` ping: `3/3`, `0% packet loss`。
- cloud -> onprem SSH 源地址保留:
  - `SSH_CONNECTION=10.88.60.7 ... 10.88.60.9 22`
- onprem -> cloud SSH 源地址保留:
  - `SSH_CONNECTION=10.88.60.9 ... 10.88.60.7 22`
- 默认网关未变更:
  - cloud-client: `default via 10.88.60.1 dev ens5`
  - onprem-client: `default via 10.88.60.1 dev eth0`
- NAT: 通过 SSH 源地址保留确认不存在。

## 回滚与恢复

通过 `routerctl action rollback` 执行了回滚:

- `ensure-forwarding-enabled` 回滚 dry-run: 预计重新启用 `SourceDestCheck`。
- `assign-secondary-ip` 回滚 dry-run: 预计取消分配 `10.88.60.9`。
- 实际回滚结果:
  - action 2: `rolledBack`, 恢复 `SourceDestCheck=true`。
  - action 1: `rolledBack`, 取消分配 `10.88.60.9`。

最终 teardown 使用选项 B: 恢复现有的 SAM 实验室状态。

- `10.88.60.9` 辅助私有 IP 重新存在。
- `SourceDestCheck=false`。
- `routerd-cloud-aws`: `stopped`。
- `aws-cloud-client`: `stopped`。

成本状态:

- EC2 计算已停止。
- 现有的 EIP/磁盘/NIC/VPC 实验室资源作为可复用的 SAM 实验室状态保留。

## 备注

- 执行期间发现代码 bug 并在本地修复: `PluginSpec` schema 和 executor resolver 支持 `execute.providerAction`, 但 `pkg/config/validate_plugin.go` 仍在拒绝。
- forwarding action 也需要 `target.address=10.88.60.9`。这使得 `ProviderActionPolicy.allowedCIDRs` 可以在不削弱策略的情况下对 action 进行门控。
