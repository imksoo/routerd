# CloudEdge / Selective Address Mobility — experimental MVP, 多云实验室验证完成

状态: **experimental** (实验室验证; 不推荐作为稳定版)
分支: `cloudedge-mvp` · 日期: 2026-05-29 (2026-05-30 更新: OCI 追加 → 3 云对等)

## 概述

CloudEdge Selective Address Mobility (SAM) MVP 已在 **3 个云** 完成多云实验室验证。Azure x PVE、AWS x PVE、OCI x PVE 均通过了同一子网 /32 移动性冒烟测试: 云 VM (`.7`) 和本地/PVE VM (`.9`) **通过 routerd 间 WireGuard overlay 进行双向通信 (ping + SSH + 100 MiB scp, 保留源地址), 无 NAT 且无需变更客户端默认网关**, 如同处于同一逻辑子网上。

这 **不是完整的 L2 扩展**。SAM 捕获选定的 /32 IPv4 地址, 并在保留源/目的地址的同时通过 overlay 进行传递。

## 验证结果

| 场景 | 结果 | 证据 |
|---|---|---|
| Azure x PVE 同子网 /32 移动性 | PASS / clean | `docs/releases/evidence/cloudedge-sam-azure-pve-20260529.md` |
| AWS x PVE 同子网 /32 移动性 | PASS / clean (Azure 对等, 首次运行) | `docs/releases/evidence/cloudedge-sam-aws-pve-20260529.md` |
| OCI x PVE 同子网 /32 移动性 | PASS / clean (PMTU/MSS clamp 修复 #53 后) | `routerd-labs/cloudedge-sam/evidence/20260530T031247Z-oci-pve-hardening-43a64c55/summary.md` |

3 次运行全部通过。AWS **无需任何 AWS 特定的代码变更** 即在首次运行时通过。OCI 最初在低 PMTU underlay 下 TCP 出现黑洞 (ping 通过, SSH/scp 超时) — 正如 #50 所预测的故障 — PMTU/MSS clamp 依赖于 `FirewallZone`, 而 SAM (纯转发平面) 不定义 FirewallZone, 因此任何云均未导出 `routerd_mss` clamp。修复 (#53) 使 clamp 成为 **FirewallZone 无关且接口类型无关**: 通过 `hybrid.EstimateMTU` 获取有效 overlay MTU, 为 overlay 隧道 MTU 存在实质降低的转发传递路径导出 MSS clamp (OCI 上 MSS 1300)。家用路由器 (PPPoE/DS-Lite) 无变更 (无 `RemoteAddressClaim` → 转发路径集为空 → zone 输出相同)。修复后, OCI x PVE 的 `routerd_mss` 在两侧均存在, `doctor hybrid` PASS, 状态干净。

## 已验证的抽象

- **capture — provider 特定**: Azure NIC 辅助私有 IP + NIC IP 转发;
  AWS ENI 辅助私有 IPv4 + EC2 source/destination check 禁用; OCI VNIC
  辅助私有 IP + `skipSourceDestCheck=true`。
- **delivery / claim / doctor — routerd 通用**: `RemoteAddressClaim` →
  `wg-hybrid` 上的 `/32` 传递路由; 本地 proxy-ARP 返回捕获; 无 NAT;
  源/目的保留; `routerctl doctor hybrid`。provider-secondary-ip 的 de-assign 加固和
  WireGuard stdin apply 已跨云通用化。

## 此分支的内容 (cloudedge-mvp, 与 main 的差异)

- Dynamic-config 基础设施: `DynamicConfigPart` / mask 指令 /
  `DynamicOverridePolicy`; effective-config = startup + active dynamic parts - masks。
- Plugin runner (observe-only, dry-run): `Plugin` / `DynamicConfigSource` /
  `PluginResult`; actionPlans 仅用于展示。
- L3 hybrid: `OverlayPeer` / `HybridRoute` (lowered 到既有的 IPv4Route install)。
- Selective Address Mobility: `AddressMobilityDomain` / `RemoteAddressClaim` /
  `CloudProviderProfile`; Linux 数据平面 (proxy-ARP 捕获 + /32 overlay 传递 +
  provider-secondary-ip OS 地址 de-assign)、`routerctl doctor hybrid`。
- nftables ownership marking (用于 stale-table 诊断)。

## 范围 / 已知限制 (experimental 而非稳定版的原因)

- 无云 provider API mutation (辅助 IP 分配 / 路由表由
  供应侧 / 手动完成; actionPlans 仅用于展示)。
- SAM 实时数据平面仅限 Linux。
- 无完整的 L2 / EVPN / BUM / 广播域扩展。
- GCP 未验证 (Azure / AWS / OCI 已验证; OCI 于 2026-05-30 追加)。
- OCI Ubuntu 镜像默认包含 `iptables` reject-all FORWARD/INPUT,
  阻塞 WG/overlay 转发路径 (#52) — `doctor hybrid` 检测并修复于主机侧
  (主机防火墙不在 routerd 核心范围内; routerd 不自动供应, 仅警告)。
- 生产拓扑变体未验证。
- 配置人机工程学的粗糙之处和手动引导/密钥流程仍存在 (例: WG
  `allowedIPs` 需与捕获目标的 `/32` 手动匹配; WireGuard 密钥和主机
  package/systemd 引导为手动)。完整列表参见合并前盘点:
  `docs/releases/cloudedge-sam-stocktake-20260529.md`。冒烟测试中的手动
  *修复* 均已迁移至 routerd 原生 (#41/#42/#43/#45/#47); 剩余项目为
  设计上的 provider 供应或已跟踪的 experimental 后续跟进。

## 建议

作为 **experimental** 的 CloudEdge/SAM MVP 功能合并至 `main` (标记为 experimental)。稳定升级 / 发布标签待进一步验证后再行决定。
