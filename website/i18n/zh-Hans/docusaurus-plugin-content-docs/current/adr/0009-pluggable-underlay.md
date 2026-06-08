# ADR 0009: 可插拔 overlay underlay（ipip / gre，然后 fou / gue）

![ADR 0009 的示意图。TunnelInterface、IPIP 或 GRE 投递、可选的 WireGuard 加密 underlay、MTU 开销推导、MSS clamp 的安全性](/img/diagrams/adr-0009-pluggable-underlay.png)

## 状态

已提议。批准为实验性实现 — 2026-06-01。

以 CloudEdge overlay/SAM 数据平面（[ADR 0006](../adr/0006-event-federation.md)、
[Selective Address Mobility](../reference/selective-address-mobility)）和
zone 无关的 PMTU/MSS clamp（#53/#68）为基础。实验性。

## 背景

CloudEdge overlay（`OverlayPeer`）目前使用 **WireGuard** 作为唯一已实现的 underlay。
在*可信的*私有 underlay — ExpressRoute、DirectConnect、FastConnect、VPC/VNet 对等 —
上，WireGuard 的加密是冗余的，约 80 字节的开销纯粹是代价。当 underlay 已经可信时，
我们希望操作员能选择更轻量、低开销的 L3 传输，而**不改变**地址的投递方式。

Overlay 已在适当的接缝处抽象（代码中已确认）：

- **投递与 underlay 无关。** `hybrid.RouteTarget(peer)` 将
  `OverlayPeer.Underlay.Type` 映射到 `(device, gateway)`，`/32` 投递路由
  （`RemoteAddressClaim` / `HybridRoute`）指向该设备。新增传输只需
  新增 `switch` 分支。
- **MTU / MSS clamp 已参数化。** `hybrid.EstimateMTU = underlayMTU(interface)
  − overheadFor(type)`。zone 无关的 clamp 遵循 `EstimateMTU`。新传输只需
  开销值和接口 MTU，clamp 即自动跟随。

唯一实质的缺口：**设备创建是 WireGuard 专有的**（专用的
`WireGuardInterface` Kind + 控制器）。新的 L3 传输需要
"创建隧道设备"的等价资源 + 控制器。

## 决策

### 新 Kind `TunnelInterface`（`hybrid.routerd.net/v1alpha1`）

`WireGuardInterface` 的镜像：一个 OS 隧道设备的 desired state 资源。
`OverlayPeer.Underlay` 保持为*投递选择*的引用。`TunnelInterface` 是
*设备 desired state* — 清晰的分离（`OverlayPeer` 的内联字段会
为每个 peer 增殖设备规格，使设备的所有权/幂等性/删除变得模糊）。

Phase 1 字段：

- `mode`: `ipip | gre`。
- `local`、`remote`: underlay（物理）端点 IP（必需）。
- `address`: overlay 内侧地址（可选。否则与 WireGuard 相同，由
  `ipv4-static-address` 控制器设置）。
- `mtu`（可选）、`ttl`（可选，默认 64）、`key`（仅 GRE。设置时
  +4 开销）。
- `trustedUnderlay: true` — **必需**（参见安全性）。

Phase 2 在同一 Kind 上扩展 IPIP-over-UDP：

- `mode`: `fou | gue` 表示带 Linux UDP 封装（`encap fou` 或 `encap gue`）的
  `ipip` 隧道设备。
- `encapSport`、`encapDport`: 本地 UDP 源/监听端口和 peer 目的端口。
  `fou`/`gue` 时两者必需。

`OverlayPeer.Underlay.Type` 枚举增加 `ipip`、`gre`、`fou`、`gue`。
`.Interface` 按名称引用 `TunnelInterface`。

### 新控制器 `tunnel`

reconcile `TunnelInterface` 的 `framework.FuncController`（Phase 1 仅 Linux。
其他平台报告 unsupported 状态而非使链报错）：

- **基于 argv 的 `ip` 调用**（非字符串拼接 shell）。`ip link show` →
  add/modify/`ip link del` 实现幂等：
  - `ip link add <dev> type ipip|gre local <L> remote <R> ttl <t> [key <k>]`
  - `fou`/`gue` 时：`ip fou add port <sport> ipproto 4|gue`，然后
    `ip link add <dev> type ipip local <L> remote <R> ttl <t> encap fou|gue
    encap-sport <sport> encap-dport <dport>`
  - `ip link set <dev> mtu <m> up`
- 地址由现有 `ipv4-static-address` 控制器处理（与 WireGuard 相同）。
- 状态: phase、device、mode、local、remote、mtu。

### 开销、投递、MTU

- `overheadFor`: `ipip = 20`、`gre = 24`（外层 IPv4 20 + GRE base 4）、`fou = 28`
  （外层 IPv4 + UDP）、`gue = 32`（外层 IPv4 + UDP + 最小 4 字节 GUE 头）。
  GRE `key` 时 +4。
- `RouteTarget`: `ipip`、`gre`、`fou`、`gue` → `(device, "")`（`/32` 路由
  与 WireGuard 相同指向隧道设备）。
- `EstimateMTU` 和 PMTU/MSS clamp 自动跟随。`pathMTUResourceMTU` 回退中
  增加 `TunnelInterface` 默认值（或 `spec.mtu` 生效）。

### 验证

- `OverlayPeer.Underlay.Type` 枚举 += `ipip`、`gre`、`fou`、`gue`。
- `TunnelInterface`: `mode ∈ {ipip, gre, fou, gue}`。`local`/`remote` 必需，有效 IP。
  `trustedUnderlay == true` 必需（否则以清晰消息拒绝）。
  MTU/TTL/key/encap 端口的范围检查。

## 安全性（硬性不变量）

`ipip`、`gre`、`fou`、`gue` **既不加密也不认证** — 与 WireGuard 根本不同。
仅在已可信的 underlay 上才安全。

- **WireGuard 保持为默认。**
- `TunnelInterface` 除非设置 **`trustedUnderlay: true`** 否则被拒绝 —
  操作员对 underlay 为明文的明确确认。仅靠文档/doctor 的
  警告太弱。这是验证门控。

## 阶段划分

- **Phase 1**: `TunnelInterface` Kind + `tunnel` 控制器
  （Linux `ipip`/`gre`）+ `trustedUnderlay` 门控 + `RouteTarget`/开销/MTU +
  验证 + 单元/fixture 测试 + 示例配置。测试包含
  **删除顺序**不变量：`OverlayPeer`/claim 删除使 `/32` 路由下线，
  `TunnelInterface` 删除输出设备删除计划。路由安装
  需要容忍设备不存在的情况。
- **Phase 2（已实现）**: `fou` / `gue`（IPIP-over-UDP）。GRE-over-FOU/GUE
  有意不公开。需要 inner-mode 字段或复合类型字符串。
  增加 `ip fou add` 的 encap-port 设置。最小头开销假设
  连同现有的显式 `mtu` 逃逸舱口一起记录。
- **Phase 3**: FreeBSD（`gif` for ipip、`gre`）— 配置/状态界面不同，
  不塞入 Linux 控制器。
- **Phase 4**: 防火墙自动打洞（raw `ipip` = IP proto 4、`gre` = IP proto 47、
  `fou`/`gue` = UDP）+ `doctor hybrid` 检查。

## 结论

- 操作员为可信 underlay 获得轻量的 overlay 传输。
  投递和 MSS clamp 无需更改，自动跟随新的开销。
- 加密的权衡是明确的且有门控（`trustedUnderlay: true`），
  不会在不可信路径上误选轻量传输。
- `TunnelInterface` 是通用的设备 desired state 资源，
  Phase 2-3 可扩展（encap、FreeBSD）而无需触及投递/MTU 接缝。
- WireGuard 的行为和现有部署不受影响（默认不变）。
