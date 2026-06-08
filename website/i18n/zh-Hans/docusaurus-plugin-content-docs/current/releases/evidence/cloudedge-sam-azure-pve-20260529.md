# CloudEdge SAM Azure x PVE 冒烟测试证据

日期: 2026-05-29

分支/构建: `cloudedge-mvp`, `routerd v20260528.2308 (439ec316)`

Result: PASS

证据包:
`/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260529T161157Z-439ec316-clean`

## 拓扑

- 云客户端: `10.77.60.7/24`
- 本地客户端: `10.77.60.9/24`
- 云路由器主: `10.77.60.4/24`
- 云路由器 Azure NIC 辅助捕获地址: `10.77.60.9`
- 本地路由器: router06, `10.77.60.1/24` (`ens21`)
- Overlay: `wg-hybrid`, `169.254.110.1/32` <-> `169.254.110.2/32`

## Azure 捕获

- `ce-router-nic` 已启用 IP 转发。
- 主私有 IP 为 `10.77.60.4`。
- 辅助私有 IP 为 `10.77.60.9`。
- routerd 的 reconciliation 后, `routerd-cloud` 的来宾 OS 未将 `10.77.60.9` 保留为本地接口地址。
- `10.77.60.9/32` 通过 `wg-hybrid` 传递。

## 云侧

- `RemoteAddressClaim/onprem-client-10-77-60-9` 为 `Ready`。
- 捕获类型为 `provider-secondary-ip`。
- `captureDeassignedOSAddress.enforced=true`。
- 传递路由已安装: `10.77.60.9 dev wg-hybrid scope link metric 120`。
- `ip route get 10.77.60.9` 选择 `wg-hybrid`。
- `10.77.60.9/32` 在本地接口上不存在。
- `routerctl doctor hybrid` 为 `overall=pass`, `fail=0`。

## 本地侧

- `RemoteAddressClaim/cloud-client-10-77-60-7` 为 `Ready`。
- 捕获类型为 `ens21` 上的 `proxy-arp`。
- Proxy neighbor 存在: `10.77.60.7 proxy`。
- 传递路由已安装: `10.77.60.7 dev wg-hybrid scope link metric 120`。
- `ens21.proxy_arp=1`。
- `routerctl doctor hybrid` 为 `overall=pass`, `fail=0`。

## 连接性

- 云客户端到本地客户端 ping: 3/3 收到, 0% loss。
- 本地客户端到云客户端 ping: 3/3 收到, 0% loss。
- 云客户端到本地客户端 SSH 源地址保留成功:
  `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`。
- 本地客户端到云客户端 SSH 源地址保留成功:
  `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`。
- 未观察到 NAT。
- 客户端默认网关未变更。

## 干净运行加固检查

- Azure Ubuntu 在 routerd 启动前将 `10.77.60.9/24` 重新引入到 `eth0`。
- routerd `439ec316` 在无需手动 `ip addr del` 变通的情况下 de-assign 了该地址。
- routerd 在无需先前手动 `/dev/stdin` 变通的情况下应用了 WireGuard。
- 证据在 Azure VM deallocate 前捕获。
- Azure VM 在证据捕获后 deallocate; 资源组未 tear down。

## 已知备注

- `routerd_filter` 表不可用时, FORWARD 策略的 doctor 检查被跳过; 数据平面冒烟测试仍然通过。
- router06 的全局状态保持 `Pending`, 但 `doctor hybrid` 通过且 SAM 数据平面路径健康。
- 稳态下 `captureDeassignedOSAddress.deassigned=false` 表示在该 reconcile 中没有需要删除的地址; `enforced=true` + 本地地址的 doctor 通过是相关断言。
