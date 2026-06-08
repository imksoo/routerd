---
title: CloudEdge Mobility 演示
---

# CloudEdge Mobility 演示

![4 站点 CloudEdge SAM mobility 演示的共享 /24 拥有者、MobilityPool、SAMTransportProfile IPIP 传输、BGP /32 交付、提供商或 proxy-ARP 捕获的流程](/img/diagrams/how-to-cloudedge-mobility-demo.png)

> 实验性功能（CloudEdge）。**Selective Address Mobility (SAM)** 的实验室演示。on-prem、AWS、Azure、OCI 共享一个逻辑 `/24`，每个站点都能提供其他站点*拥有的*地址 — 无 NAT 且不变更客户端的默认网关。可执行包位于 [`examples/cloudedge-mobility-demo/`](https://github.com/imksoo/routerd/tree/main/examples/cloudedge-mobility-demo)。

![CloudEdge Mobility 演示：4 个站点共享 10.77.60.0/24，每个拥有者地址通过生成的 SAM 传输在所有非拥有者站点被捕获，无 NAT 保留源 IP](../images/cloudedge-mobility-demo.png)

## 本演示展示的内容

- **1 个逻辑 `/24` 由 4 个站点共享** — on-prem / AWS / Azure / OCI 全部将 `10.77.60.0/24` 作为单一逻辑地址空间。
- **非拥有者站点捕获拥有者的地址** — 每个拥有者地址在*所有其他站点*变为可达（云端：提供商的**辅助 IP**，on-prem：**proxy ARP**）。进行单一拥有者仲裁。
- **12 方向 SSH 流通过** — 4 个演示客户端之间全方向通信。
- **无 NAT、源 IP 保留、网关无变更** — 连接保持实际源 IP，不进行 NAT，不触碰客户端的默认网关。
- **云端维护捕获迁移（D5）** — 捕获的地址在同一提供商内迁移到另一个路由器作为备用，流量通过新的持有者恢复。

## 地址设计

所有 4 个站点共享一个逻辑子网，每个站点仅拥有其中 1 个 `/32`。

| 站点 | routerd 节点 | 拥有者地址 | 捕获机制 |
| --- | --- | --- | --- |
| On-prem | `onprem-router` | `10.77.60.10/32` | LAN 上的 Proxy ARP |
| AWS | `aws-router-a` | `10.77.60.11/32` | ENI 辅助 IP |
| Azure | `azure-router` | `10.77.60.12/32` | NIC 辅助 ipConfig |
| OCI | `oci-router` | `10.77.60.13/32` | VNIC 辅助私有 IP |

逻辑子网：**`10.77.60.0/24`**。路由器间传输使用独立的 RFC1918 端点/内部地址体系（与链路本地 `169.254/16` 或 CGNAT `100.64/10` 分离）。地址约束请参见 [Selective Address Mobility](../reference/selective-address-mobility)。

## 数据平面

- **provider-secondary-ip 捕获** — 在每个云端路由器上，*其他*站点的拥有者地址作为辅助 IP 附加到该 ENI / NIC / VNIC，云端 fabric 将流量交付到该路由器。
- **proxy-ARP 捕获** — 在 on-prem，路由器在 LAN 上对其他站点的拥有者地址响应 ARP。
- **BGP `/32` 交付** — 每个拥有者广告其拥有的 `/32`，其他路由器导入最佳路径并通过 overlay 转发到拥有站点的路由器。
- **生成的 SAM 传输** — 路由器通过从 `SAMTransportProfile` 导出的 IPIP 隧道和 BGP 对等体互连。如果启用 WireGuard，则为端点限定的加密底层网络。其 `AllowedIPs` 包含传输端点前缀，不包含移动 `/32`。

交付是路由（而非 NAT），因此**源 IP 得以保留**，客户端的**默认网关不会变更**。

## 控制平面

操作员只需声明意图，其余均为导出。

- **MobilityPool** — 操作员描述的唯一意图（成员、捕获模式、交付、放置、维护 drain）。
- **北极星成员结构** — 每个渲染配置通过 `profiles.cloudCaptures`、`spec.values`、`targetFrom`、`subnetRefFrom` 完全声明自身站点，远程站点仅为 ID 对等条目。与 BGP 类似，节点需要知道对等体，但不需要对等体的提供商 NIC/子网实现细节。
- **SAMTransportProfile** — 从共享拓扑和内部前缀导出逐对等体的 `TunnelInterface`、端点 `/32` `IPv4Route`、`BGPPeer` 资源。
- **BGP `/32` mobility 路径** — 每个拥有者广告其拥有的主机路由，其他站点通过生成的 SAM 传输学习当前最佳路径。
- **提供商 trap 操作** — 云端路由器最终将远程拥有的 `/32` 作为辅助 IP assign/unassign 以进行本地捕获。这些操作不再位于关键转发路径上。
- **Event Federation** — `routerd.client.ipv4.observed` 事实在站点间传播（`EventGroup` / `EventPeer` / `EventSubscription`，参见 [Event Federation](../reference/event-federation.md)）。
- **提供商操作 executor** — 在 `ProviderActionPolicy` 下，使用实例自身的云原生 ID 执行门控云端变更（辅助 IP 的 assign / unassign、forwarding）（参见 [ADR 0007](../adr/0007-provider-action-execution.md)）。
- **pathSig fencing** — 提供商操作针对当前 BGP 期望路径签名和持有者进行 fence，因此过时的操作无法变更在其他地方已重新收敛的路由。

示例配置有意避免了旧式 remote-full 内联风格。在预发布期间旧风格仍可接受，但如果远程 `MobilityPool` 成员包含本地提供商的捕获或发现详细信息，`routerctl validate`、plan、apply 会发出警告。未来的预发布配置中，远程成员可能仅要求 ID。

## 运行方法

使用 `examples/cloudedge-mobility-demo/` 中的包。假设实验室实例、NIC/VNIC、ID 权限、SSH、可选的 WireGuard 端点密钥、提供商 CLI 已准备就绪 — 脚本**不会**配置云端资源。

```sh
cd examples/cloudedge-mobility-demo
cp env.example env
$EDITOR env            # 填写所有占位符。secret 不要放入 git
 
./run-demo.sh          # 渲染 + 部署、事件发布、D3 运行，然后 D5 迁移
./collect-evidence.sh  # 收集提供商状态、日志、连接信息
./reset-lab.sh         # 尽力清理。停止计算资源以避免闲置费用
```

无论是否失败，每次运行后都请执行 `reset-lab.sh`。

## 已验证的结果

- **D1** 位置自动反映：出现在 on-prem 的拥有者地址被各云端路由器识别。
- **D2** 云端 -> on-prem 捕获（proxy ARP）。
- **D3** 4 站点 **12 方向 ping + SSH 通过** — 源 IP 保留、无 NAT、默认网关无变更。
- **D4** on-prem HA / VRRP 捕获故障转移。
- **D5** 云端维护 / **捕获迁移通过** — drain `aws-router-a` 后，捕获的地址迁移到 `aws-router-b`，流量通过 B 恢复。过时的 pathSig 操作被 fence（`skipped: stale mobility desired path`）。参见 [D5 证据](../releases/evidence/cloudedge-mobility-d5-aws-maintenance-20260531.md)。

## 注意事项

- 这是**实验室演示**，不是生产就绪的交钥匙方案。
- **不是**完整的 L2 扩展 / EVPN — 没有广播/多播桥接。
- 这是**选择性 `/32` 地址移动性**：不是整个子网，而是选定的地址在站点间移动。
- 脚本假设预配置的实例，使用占位的非机密逻辑地址。绝不提交实际的账户/订阅/OCID/ENI/VNIC ID 或私钥。
