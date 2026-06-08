# CloudEdge Phase 5.1 Azure Provider Executor 冒烟测试

Result: PASS

日期: 2026-05-31 UTC  
分支/构建: `phase5-oci-azure-executors` / `routerd v20260528.2308 (c51ba0ca)`  
证据包: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260531T013055Z-phase5-azure-live-c51ba0ca`

## 范围

- Provider mutation 对象: 仅 Azure。
- 租户/订阅/区域: `53a7de65-6b1f-4878-a424-acad5e25db4b` / `26412fa4-cd3a-4128-9794-72ee01876d84` / `japaneast`。
- 复用的 routerd 专用 SAM 实验室: 资源组 `cloudedge-lab`。
- 目标路由器 VM: `routerd-cloud`, 私有 `10.77.60.4`, 公共 `20.46.113.237`。
- 目标客户端 VM: `cloud-client`, 私有 `10.77.60.7`。
- 目标 NIC: `ce-router-nic`。
- 捕获地址: `10.77.60.9`。

## 重置基线

mutation 前, 将现有 Azure SAM 实验室重置为全新的 provider 基线:

- 从 `ce-router-nic` 删除辅助 ipconfig `ipconfig-onprem-capture` / `10.77.60.9`。
- `ce-router-nic` 恢复 `enableIPForwarding=false`。
- 重置后证据: `azure-router-nic-post-reset.json`, `post-reset-nic-summary.tsv`。

重置后状态:

- `ce-router-nic`: `ipForwarding=false`。
- IP configs: 仅主 `10.77.60.4`。

## 托管标识门控

`routerd-cloud` 接收了系统分配的托管标识:

- 主体 ID: `4b9423bc-01e3-4244-a898-b911f140cb6f`。
- 为 executor 在 `routerd-cloud` 上安装了 Azure CLI。
- 路由器上的托管标识 preflight 通过:
  - `az login --identity --allow-no-subscriptions`
  - `az network nic show --ids <ce-router-nic>`

初始的 NIC 范围 Network Contributor 角色对 `ip-config create` 不够。Azure 还要求关联 NSG 的 `join/action` 权限。作为推进优先的修复, 在实验室资源组和 NSG 范围添加了 Network Contributor。之后 executor 的 mutation 成功。

## Executor 执行

在 `routerd-cloud` 上构建并安装了 `azure-provider-executor`。

路由器配置包含:

- `ProviderActionPolicy/azure-live-mutation`
- `Plugin/azure-executor`
- Plugin timeout `120s`
- `AZURE_CONFIG_DIR=/var/lib/routerd/azure`

Action 执行:

- `ensure-forwarding-enabled`
  - Action ID: `4`
  - Result: `succeeded`
  - 观察到的 journal fact: `priorIpForwarding=false`
  - 结果消息: `set ipForwarding=true`
- `assign-secondary-ip`
  - Action ID: `7`
  - Result: `succeeded`
  - 结果消息: `assigned 10.77.60.9 to ce-router-nic (ip-config ipconfig-onprem-capture)`

mutation 后的 Azure 验证:

- `ce-router-nic`: `ipForwarding=true`。
- IP configs: `10.77.60.4`, `10.77.60.9`。
- 证据: `azure-router-nic-after-mutation.json`, `azure-router-nic-after-mutation-summary.tsv`。

## 数据平面验证

云侧:

- `routerctl doctor hybrid`: `overall=pass`, `warn=0`, `fail=0`, `skip=1`。
- 传递路由: `10.77.60.9 dev wg-hybrid metric 120`。
- 本地 OS 地址不存在: `10.77.60.9/32 absent from local interfaces`。
- MSS clamp: `routerd_mss covers eth0 -> wg-hybrid`。

本地侧:

- router06 `routerctl doctor hybrid`: `overall=pass`, `warn=0`, `fail=0`, `skip=1`。
- 云客户端 `10.77.60.7` 的 Proxy ARP claim 保持健康。
- MSS clamp: `routerd_mss covers ens21 -> wg-hybrid`。

客户端连接性:

- cloud-client `10.77.60.7` -> onprem-client `10.77.60.9` ping: `3/3`, `0% packet loss`。
- onprem-client `10.77.60.9` -> cloud-client `10.77.60.7` ping: `3/3`, `0% packet loss`。
- cloud -> onprem SSH 源地址保留:
  - `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`
- onprem -> cloud SSH 源地址保留:
  - `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`
- 默认网关未变更:
  - cloud-client: `default via 10.77.60.1 dev eth0`
  - onprem-client: `default via 10.77.60.1 dev eth0`
- NAT: 通过 SSH 源地址保留确认不存在。

## 回滚与恢复

通过 `routerctl action rollback` 执行了回滚:

- action 7 `assign-secondary-ip`: `rolledBack`, 取消分配 `ipconfig-onprem-capture`。
- action 4 `ensure-forwarding-enabled`: `rolledBack`, 恢复 `ipForwarding=false`。

回滚期间发现 1 个可修复的实验室问题: 路由器配置重新应用后, Plugin 环境不再暴露 `AZURE_CONFIG_DIR`, Azure CLI 报告 `Please run 'az login'`。修正配置并在 `/var/lib/routerd/azure` 下重新创建托管标识登录后, 回滚通过。

最终 teardown 使用选项 B: 恢复现有的 Azure SAM 实验室状态。

- `10.77.60.9` 辅助 ipconfig 重新存在。
- `ipForwarding=true`。
- `routerd-cloud`: `VM deallocated`。
- `cloud-client`: `VM deallocated`。

成本状态:

- Azure 计算已 deallocate。
- 现有的公共 IP、NIC、磁盘、VNet、NSG、托管标识/角色分配作为可复用的 SAM 实验室状态保留。

## 备注

- 在云端 `RemoteAddressClaim` 实验室配置中添加了 `capture.interface: eth0`。使新的 MSS/PMTU doctor 检查能够证明 `eth0 -> wg-hybrid` 的覆盖。
- 首次 action 尝试因托管标识的角色范围过窄而失败。最终成功的 action 为 ID 4 和 7。
- `rtk` 包装器会截断较长的 Azure 资源 ID。需要精确资源 ID 的命令在 `rtk bash -lc` 内使用原始 `az`。
