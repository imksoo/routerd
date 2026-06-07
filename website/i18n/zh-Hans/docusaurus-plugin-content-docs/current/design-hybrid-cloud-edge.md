---
title: Hybrid cloud edge design
---

# Hybrid cloud edge design

CloudEdge 的目标是让 cloud automation 不直接编辑 operator 管理的 startup YAML，
同时把 local edge 与 cloud-side fact 放入同一个 declarative router model。
当前实现包括 dynamic config、plugin I/O、BGP mode selective address mobility、
生成的 SAM transport resource，以及 gated provider action execution。

## config layers

CloudEdge 使用三个 layer：

- **startup-config**：operator 管理的普通 `router.yaml`。plugin 不会编辑它。
- **dynamic-config**：trusted local source 生成的 runtime intent，保存为
  `DynamicConfigPart`。
- **effective-config**：startup config、active dynamic part 与 active mask merge 后的
  reconcile target。controller、renderer、plan、dry-run 与 status 都使用该 view。

provider action plan 在 dynamic-config 中是 inert 的。只有导入 action journal，并通过
`ProviderActionPolicy`、approval、allowlist 与 executor plugin gate 后，才可能执行。

## current scope

当前 CloudEdge foundation 包含：

- `DynamicConfigPart` / `DynamicOverridePolicy` 提供 runtime intent 与 mask。
- trusted local plugin 支持 observation、dynamic resource、provider action proposal 与
  executor plugin。
- 以 `MobilityPool` 为中心的 selective address mobility intent。
- `SAMTransportProfile` 生成 IPIP/GRE `TunnelInterface`、endpoint `/32` `IPv4Route`
  与 `BGPPeer`。
- BGP mode SAM delivery。owner advertise IPv4 `/32` path，non-owner 将 BGP best path
  import 到 local FIB。
- Linux SAM capture。支持 provider-secondary-IP、proxy-ARP、on-prem VRRP/single-router
  gate、active transition GARP，以及 conservative on-demand ARP discovery。
- experimental/default-off 的 provider action execution。

仍在 scope 外的是 remote plugin install、public plugin registry、consensus-based ownership、
full L2 extension，以及任意 cloud/OS mutation 的自动 rollback。

## SAM authoring model

CloudEdge SAM 的主要 authoring surface 是 `MobilityPool` 与 `SAMTransportProfile`。
`MobilityPool` 表示 address ownership/capture intent，`SAMTransportProfile` 表示
transport/BGP intent。

`AddressMobilityDomain` 与 `RemoteAddressClaim` 仍作为低层兼容 resource 保留，但不再是当前
CloudEdge Mobility 的主角。

## provider actions

Cloud inventory plugin 可以观测 provider state 并返回 dynamic resource。provider capture
planner 可以输出 `assign-secondary-ip`、`ensure-forwarding-enabled` 等 `actionPlans`。

`actionPlan` 不会 merge 到 effective-config，也不会由 dynamic-config controller 执行。
operator 将其导入 journal，并通过 policy/approval/allowlist/max action limit/executor plugin
gate 后，才会成为 live mutation。routerd core 不保存 cloud credential。
