# CloudEdge / SAM — 合并前盘点 (Azure x PVE + AWS x PVE + OCI x PVE 冒烟测试)

日期: 2026-05-29 (2026-05-30 以 OCI x PVE 更新) · 分支 `cloudedge-mvp` · 目的:
对 3 次干净冒烟测试期间观察到的手动干预、配置人机工程学、routerd 功能缺口进行
盘点。确定 experimental main 合并和后续跟进的范围。

## 1. 冒烟测试期间的手动变通 — 全部已由 routerd 原生解决

| 变通方案 (当时为手动) | 解决方案 |
|---|---|
| Azure: 辅助 `/32` 被来宾 OS 自动添加 (cloud-init/netplan) → `ip addr del` + suppress | **#41 / 439ec316** — provider-secondary-ip de-assign 强制 |
| Azure: `wg setconf <tempfile>` EACCES → `/dev/stdin` | **#43 / 439ec316** — WireGuard 的 stdin 方式应用 |
| Azure: 旧的 `routerd_filter` nft 表丢弃转发 → 手动删除 | **#42 / 439ec316** doctor 警告 + 文档; **#47 / f60e7d9a** nft 所有权诊断 |
| `routerctl describe` 无 `-o` → 纯文本输出 | **#45 / 40a99208** |
| AWS: 辅助 `.9` 一时出现在 OS 上 | **无手动步骤** — routerd de-assign (#41) 自动处理 (验证修复跨 provider 通用化) |
| OCI: 低 PMTU underlay 下 TCP 黑洞 (ping OK, SSH/scp 超时) | **#53 / 3c540656** — PMTU/MSS clamp 变为 FirewallZone 无关 + 类型无关; 为 SAM 转发路径导出 `routerd_mss` (通过 `hybrid.EstimateMTU` 得到 MSS 1300)。#50 已预测。 |
| OCI: Ubuntu 镜像默认 `iptables` reject-all FORWARD/INPUT 阻塞 WG/overlay 转发 | **#52** — `doctor hybrid` 检测 + 显示所需主机规则; 主机防火墙由主机侧处理 (routerd 不自动供应, 仅警告) |

→ 冒烟测试期间的 routerd 级修复现已全部由 routerd 自身处理。AWS 运行中无需任何修复。OCI 运行中发现了 #53 PMTU/MSS 缺口 (实际 bug, 已在 routerd 核心修复) 和 #52 主机防火墙前提条件 (设计上由主机侧处理, doctor 检测)。

## 2. 主机/云引导 — 手动 (部署缺口, 大部分不在 routerd 核心范围内)

- routerd tarball 的构建/复制/安装、systemd 单元的创建/启用、实时配置的放置、
  validate/plan/apply 的执行 — 手动。未来: 实验室引导脚本 / 黄金镜像;
  与发现已有的 OS 引导自动化相关。(后续跟进。)
- 运行时前提条件 (`wireguard-tools`、`tcpdump`、`jq`、`curl`) 的安装 — 手动;
  应作为 routerd 运行时前提条件写入文档 / 在打包中处理。(后续跟进。)
- AWS: user-data apt 遇到镜像同步失败 → 手动 `apt` 重试 (实验室引导的脆弱性)。
- AWS: PVE router07 的 DHCP/guest-agent 前提失败 → 使用静态 mgmt IP 重新创建
  (PVE 实验室自动化, 不是 routerd)。

## 3. 配置人机工程学 (配置描述的粗糙之处) — 可操作

- **WireGuardPeer.allowedIPs 需与捕获目标的 `/32` (+ overlay `/32`) 手动匹配** —
  与 `RemoteAddressClaim` 的隐式耦合; 容易出错 (宽泛 allowedIPs 问题)。
  候选: WG peer 的 allowedIPs 是否覆盖各传递 `/32` 的 validation / `doctor` 交叉检查
  (或自动导出)。**最高价值的人机工程学修复。** (后续跟进。)
- `nicRef`: Azure 的完整 ARM ID vs AWS 的 ENI ID — provider 格式差异, 手动查找,
  容易出错。候选: provider 别文档 + 轻量级验证。(后续跟进。)
- `capture.interface` (proxy-arp) 必须为实际 OS NIC 名 (ens21/eth1) — 手动确认。
- overlay `/32`、共享子网、`ownerSide`、`domain.peerRef` vs `delivery.peerRef` 需
  手动对齐; 两个 peerRef 部分冗余。(后续跟进: 简化/明确化。)
- `configureOSAddress=false` 的语义在 #41 之前是模糊的 (现已明确为 "routerd
  强制 OS 本地不存在")。
- `doctor` 的 FORWARD 策略跳过在 Azure 时可读性差 (`exit status 1`); AWS 时有改善。

## 4. WireGuard 密钥供应 — 手动

- private/public 密钥的生成、放置、公钥交换全部手动; routerd 仅读取 `privateKeyFile`。
  候选: 不存在时自动生成 + 公开公钥用于交换。(后续跟进。)
- (实验室 SSH 密钥临时放置在客户端用于客户端发起的 SSH 证据, 之后删除 — 仅测试工具,
  不在 routerd 范围内。)

## 5. Provider 供应 — 设计上手动 (routerd MVP 范围外)

- Azure: RG/VNet/子网/NSG/公共 IP/NIC/VM/磁盘, NIC 辅助 `.9`, NIC IP 转发,
  启动/deallocate — 设计上手动 (MVP 无云 API mutation; actionPlan /
  CloudProviderProfile 是未来的钩子)。
- AWS: VPC/子网/IGW/路由表/SG/EIP/EC2/ENI 辅助 `.9`, source/dest check 禁用,
  停止 — 设计上手动。
- PVE: VM/网桥/NIC — 实验室基础设施, 设计上手动。

## experimental 合并的要点

- 数据平面和冒烟测试中的修复为 routerd 原生, 已在 **3 个云**
  (Azure / AWS / OCI) 验证, 全部干净。
- 多云测试的效果: OCI 的低 PMTU underlay 发现了 **routerd 核心的实际 bug**
  (#53 — PMTU/MSS clamp 被 FirewallZone 门控, 因此 SAM 在任何云上均无 clamp;
  仅在 underlay PMTU 足够低时才表现为黑洞)。修复是通用的
  (FirewallZone 无关 + 接口类型无关) 且对家用路由器安全。
- 剩余的手动操作为 **设计上手动 (provider 供应, MVP 范围外)** 或
  **experimental 的粗糙之处** (allowedIPs/nicRef/peerRef/密钥的配置人机工程学、
  主机引导、OCI 主机防火墙前提条件 #52)。这些是
  **experimental** 标签的依据, 非合并阻塞项, 作为后续跟进追踪。
