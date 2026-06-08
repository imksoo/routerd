# CloudEdge SAM AWS x PVE 冒烟测试证据

日期: 2026-05-29

分支/构建: `cloudedge-mvp`, `routerd f60e7d9a`

Result: PASS (干净 — 无手动变通; Azure 对等)

这是 Selective Address Mobility 首次在第 2 个公有云 (AWS VPC/EC2) 上运行的验证。无需任何 AWS 特定的代码变更, provider-secondary-ip 捕获 + de-assign 加固和 WireGuard stdin apply (在 Azure 周期中实现) 按设计通用化。provider 特定的工作仅在供应侧 (AWS ENI 辅助 IP + EC2 source/destination check 禁用)。

## 拓扑

- 云客户端 (AWS EC2): `10.88.60.7/24`
- 本地客户端 (PVE VM): `10.88.60.9/24`
- 云路由器 (AWS EC2): 主 `10.88.60.4/24`, ENI 辅助捕获 `10.88.60.9`
- 本地路由器 (PVE, router07): `10.88.60.1/24` (`vmbr470`)
- Overlay: `wg-hybrid`, `169.254.120.1/32` (云) ↔ `169.254.120.2/32` (本地)
- 区域: ap-northeast-1。WireGuard: 本地 -> AWS 公共端点, persistent keepalive。

## AWS 捕获前提条件 (供应侧)

- ENI: 主 `10.88.60.4`, 辅助私有 IPv4 `10.88.60.9`。
- EC2 source/destination check: DISABLED (AWS 对应 Azure NIC IP 转发)。
- routerd-cloud 来宾 OS: routerd 将 `10.88.60.9` 从本地地址中删除
  (`provider-secondary-ip` + `configureOSAddress=false` de-assign 强制)。

## 断言 (全部 PASS)

- 云传递路由: `10.88.60.9 dev wg-hybrid metric 120`。
- 本地: `10.88.60.7` 的 proxy ARP; 传递路由 `10.88.60.7 dev wg-hybrid metric 120`。
- 阶段 A: AWS 路由器 NIC 的 tcpdump 捕获到 `.7 -> .9` ICMP request/reply。
- `.7 -> .9` ping 3/3 (0% loss); `.9 -> .7` ping 3/3 (0% loss)。
- SSH 双向, 源地址保留:
  - `SSH_CONNECTION=10.88.60.7 ... 10.88.60.9 22`
  - `SSH_CONNECTION=10.88.60.9 ... 10.88.60.7 22`
- 无 NAT; 两个客户端的默认网关均未变更。
- doctor hybrid: AWS 侧 overall pass (pass 10 / warn 0 / fail 0 / skip 1);
  PVE 侧 overall pass (pass 13 / warn 0 / fail 0 / skip 1)。

## 备注

- 无 AWS 特定的故障; 未提交新 issue。
- Azure x PVE 对 (router06) 未变更。
- 成本: EC2 实例在证据捕获后停止 (为重新运行保留); EIP/EBS 保留至完全 teardown。完整的本地证据包:
  `routerd-labs/cloudedge-sam/evidence/20260529T233145Z-aws-pve-f60e7d9a`。
