---
title: Hybrid cloud edge design
---

# Hybrid cloud edge design

CloudEdge 的目標是讓 cloud automation 不直接編輯 operator 管理的 startup YAML，
同時把 local edge 與 cloud-side fact 放入同一個 declarative router model。
目前實作包括 dynamic config、plugin I/O、BGP mode selective address mobility、
產生的 SAM transport resource，以及 gated provider action execution。

![CloudEdge SAM 圖：MobilityPool 與 SAMTransportProfile 產生 DynamicConfigPart、IPIP TunnelInterface delivery、BGP peer、ECMP FIB path，以及 endpoint-only WireGuard underlay](/img/diagrams/cloudedge-sam-ipip.png)

## config layers

CloudEdge 使用三個 layer：

- **startup-config**：operator 管理的普通 `router.yaml`。plugin 不會編輯它。
- **dynamic-config**：trusted local source 產生的 runtime intent，儲存為
  `DynamicConfigPart`。
- **effective-config**：startup config、active dynamic part 與 active mask merge 後的
  reconcile target。controller、renderer、plan、dry-run 與 status 都使用該 view。

provider action plan 在 dynamic-config 中是 inert 的。只有匯入 action journal，並通過
`ProviderActionPolicy`、approval、allowlist 與 executor plugin gate 後，才可能執行。

## current scope

目前 CloudEdge foundation 包含：

- `DynamicConfigPart` / `DynamicOverridePolicy` 提供 runtime intent 與 mask。
- trusted local plugin 支援 observation、dynamic resource、provider action proposal 與
  executor plugin。
- 以 `MobilityPool` 為中心的 selective address mobility intent。
- `SAMTransportProfile` 產生 IPIP/GRE `TunnelInterface`、endpoint `/32` `IPv4Route`
  與 `BGPPeer`。
- BGP mode SAM delivery。owner advertise IPv4 `/32` path，non-owner 將 BGP best path
  import 到 local FIB。
- Linux SAM capture。支援 provider-secondary-IP、proxy-ARP、on-prem VRRP/single-router
  gate、active transition GARP，以及 conservative on-demand ARP discovery。
- experimental/default-off 的 provider action execution。

仍在 scope 外的是 remote plugin install、public plugin registry、consensus-based ownership、
full L2 extension，以及任意 cloud/OS mutation 的自動 rollback。

## SAM authoring model

CloudEdge SAM 的主要 authoring surface 是 `MobilityPool` 與 `SAMTransportProfile`。
`MobilityPool` 表示 address ownership/capture intent，`SAMTransportProfile` 表示
transport/BGP intent。

`AddressMobilityDomain` 與 `RemoteAddressClaim` 仍作為低層相容 resource 保留，但不再是目前
CloudEdge Mobility 的主角。

## provider actions

Cloud inventory plugin 可以觀測 provider state 並返回 dynamic resource。provider capture
planner 可以輸出 `assign-secondary-ip`、`ensure-forwarding-enabled` 等 `actionPlans`。

`actionPlan` 不會 merge 到 effective-config，也不會由 dynamic-config controller 執行。
operator 將其匯入 journal，並通過 policy/approval/allowlist/max action limit/executor plugin
gate 後，才會成為 live mutation。routerd core 不保存 cloud credential。
