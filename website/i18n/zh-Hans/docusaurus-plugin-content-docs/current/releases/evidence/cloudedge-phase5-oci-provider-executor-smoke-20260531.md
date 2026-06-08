# CloudEdge Phase 5.1 OCI Provider Executor 冒烟测试

Result: PASS

日期: 2026-05-31 UTC  
分支/构建: `phase5-oci-azure-executors` / `routerd v20260528.2308 (67d96103)`  
证据包: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260531T005414Z-phase5-oci-live-67d96103`

## 范围

- Provider mutation 对象: 仅 OCI。
- 租户/区域: `ocid1.tenancy.oc1..aaaaaaaaby2raoa2kzgywrsz6ofjk4eks6uwtpczgtqxulach3xgksfx52qq` / `ap-tokyo-1`。
- 复用的 routerd 专用 SAM 实验室: `Project=routerd-cloudedge-sam-oci-pve`。
- 目标路由器实例: `routerd-cloud-oci` / `ocid1.instance.oc1.ap-tokyo-1.anxhiljr6yebb3qc2sucs3kor7u77ki2cg7zf3xlgmubj5utwfqeejmm7crq`。
- 目标客户端实例: `oci-cloud-client` / `ocid1.instance.oc1.ap-tokyo-1.anxhiljr6yebb3qc2biuwl7yyjglwn6aompawzlfmkohpbrqceuijiuf7dva`。
- 目标 VNIC: `ocid1.vnic.oc1.ap-tokyo-1.abxhiljrzn6c2b4hs2jljbs4cmbshywzr7ldugepftjdrvm77nlvcvbdzzkq`。
- 捕获地址: `10.77.60.9`。

## 重置基线

mutation 前, 将现有 SAM 实验室重置为全新的 provider 基线:

- 从路由器 VNIC 删除 `10.77.60.9` 辅助私有 IP。
- VNIC 恢复 `skipSourceDestCheck=false`。
- 重置后证据: `oci-router-vnic-post-reset.json`, `oci-router-private-ips-post-reset.json`, `retry-reset-summary.tsv`。

## 实例主体门控

`routerd-cloud-oci` 接收了 executor 用的 OCI 动态组和策略。

- 动态组: `routerd_phase5_oci_executor`。
- 初始的最小权限策略对 `private-ip create` 不够, 返回 `NotAuthorizedOrNotFound`。
- 推进优先的修复: 将此实验室动态组的策略扩大为 `manage virtual-network-family in tenancy`。

路由器上的实例主体 preflight 通过:

- `oci network vnic get` 可读取目标 VNIC。
- `oci network private-ip list` 可读取目标 VNIC 的私有 IP。

## Executor 执行

在 `routerd-cloud-oci` 上构建并安装了 `oci-provider-executor`。

导入、批准、dry-run 并执行了 2 个 retry2 action journal 条目:

- `assign-secondary-ip`
  - Result: `succeeded`
  - Message: `assigned 10.77.60.9 to <target VNIC>`
- `ensure-forwarding-enabled`
  - Result: `succeeded`
  - Message: `set skipSourceDestCheck=true on <target VNIC> (prior=false)`
  - 观察到的 journal fact: `priorSkipSourceDestCheck=false`

mutation 后的 OCI 验证:

- VNIC 主: `10.77.60.4`
- VNIC 辅助: `10.77.60.9`
- `skipSourceDestCheck=true`

## 数据平面验证

云侧:

- `routerctl doctor hybrid`: `overall=pass`, `pass=12`, `warn=0`, `fail=0`, `skip=1`。
- 传递路由: `10.77.60.9 dev wg-hybrid metric 120`。
- 本地 OS 地址不存在: `10.77.60.9/32 absent from local interfaces`。
- MSS clamp: `routerd_mss covers ens3 -> wg-hybrid`。

本地侧:

- router06 `routerctl doctor hybrid`: `overall=pass`, `pass=15`, `warn=0`, `fail=0`, `skip=1`。
- 云客户端 `10.77.60.7` 的 Proxy ARP claim 保持健康。

客户端连接性:

- cloud-client `10.77.60.7` -> onprem-client `10.77.60.9` ping: `3/3`, `0% packet loss`。
- onprem-client `10.77.60.9` -> cloud-client `10.77.60.7` ping: `3/3`, `0% packet loss`。
- cloud -> onprem SSH 源地址保留:
  - `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`
- onprem -> cloud SSH 源地址保留:
  - `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`
- 默认网关未变更:
  - cloud-client: `default via 10.77.60.1 dev ens3`
  - onprem-client: `default via 10.77.60.1 dev eth0`
- NAT: 通过 SSH 源地址保留确认不存在。

## 回滚与恢复

通过 `routerctl action rollback` 执行了回滚:

- action 4 `ensure-forwarding-enabled`: `rolledBack`, 恢复 `skipSourceDestCheck=false`。
- action 3 `assign-secondary-ip`: `rolledBack`, 取消分配 `10.77.60.9`。

回滚期间发现 1 个可修复的实验室问题: OCI 的 `private-ip delete` 可能超过 Plugin 原始的 `30s` 超时。将实验室的 Plugin 超时扩大到 `120s` 后, action 3 的回滚完成, journal 中记录了 `rolledBack`。

最终 teardown 使用选项 B: 恢复现有的 SAM 实验室状态。

- `10.77.60.9` 辅助私有 IP 重新存在。
- `skipSourceDestCheck=true`。
- `routerd-cloud-oci`: `STOPPED`。
- `oci-cloud-client`: `STOPPED`。

成本状态:

- OCI 计算已停止。
- 现有的公共 IP、启动卷、VNIC、子网、VCN、策略作为可复用的 SAM 实验室状态保留。

## 备注

- OCI Ubuntu 镜像带有终端的 iptables reject 规则。在数据平面验证前应用了与 OCI SAM 冒烟测试相同的实验室防火墙引导。
- 首次 executor 尝试发现实例主体策略对私有 IP 创建过窄。扩大实验室动态组策略后, retry2 的 action 对通过。
- 首次以普通用户执行回滚的尝试因 action DB 文件权限被拒绝。回滚使用 `sudo routerctl` 执行, 与 action DB 的所有权一致。
