---
title: Hybrid cloud edge design
---

# Hybrid cloud edge design

CloudEdge は、人が管理する startup YAML を cloud automation が直接編集せずに、
local edge と cloud-side fact を同じ宣言的 router model へ載せる設計です。
現在の実装は dynamic config、plugin I/O、BGP mode selective address mobility、
生成 SAM transport resource、gated provider action execution を含みます。

## config layers

CloudEdge は 3 つの layer を使います。

- **startup-config**: operator が管理する通常の `router.yaml`。plugin は編集しません。
- **dynamic-config**: trusted local source が生成する runtime intent。
  `DynamicConfigPart` として state database に保存されます。
- **effective-config**: startup config、active dynamic part、active mask を merge した
  reconcile target。controller、renderer、plan、dry-run、status はこの view を見ます。

provider action plan は dynamic-config の中では inert です。action journal に import し、
`ProviderActionPolicy`、approval、allowlist、executor plugin gate を通った場合だけ実行できます。

## current scope

現在の CloudEdge foundation には次が含まれます。

- `DynamicConfigPart` / `DynamicOverridePolicy` による runtime intent と mask。
- trusted local plugin による observation、dynamic resource、provider action proposal、
  executor plugin。
- `MobilityPool` を中心とした selective address mobility intent。
- `SAMTransportProfile` による IPIP/GRE `TunnelInterface`、endpoint `/32`
  `IPv4Route`、`BGPPeer` の生成。
- BGP mode SAM delivery。owner は IPv4 `/32` path を advertise し、non-owner は
  BGP best path を local FIB に import します。
- Linux SAM capture。provider-secondary-IP と proxy-ARP、on-prem VRRP/single-router
  gate、active transition の GARP、conservative on-demand ARP discovery を扱います。
- experimental/default-off の provider action execution。

scope 外は、remote plugin install、public plugin registry、consensus-based ownership、
full L2 extension、任意の cloud/OS mutation の自動 rollback です。

## SAM authoring model

CloudEdge SAM の主な authoring surface は `MobilityPool` と
`SAMTransportProfile` です。`MobilityPool` は address ownership/capture intent、
`SAMTransportProfile` は transport/BGP intent を表します。

`AddressMobilityDomain` と `RemoteAddressClaim` は低レベル互換 resource として残りますが、
現在の CloudEdge Mobility では主役ではありません。

## provider actions

Cloud inventory plugin は provider state を観測して dynamic resource を返せます。
provider capture planner は `assign-secondary-ip` や `ensure-forwarding-enabled` などの
`actionPlans` を出せます。

`actionPlan` は effective-config に merge されず、dynamic-config controller からも
実行されません。operator が journal に import し、policy/approval/allowlist/max action
limit/executor plugin の gate を通したときだけ live mutation になります。routerd core は
cloud credential を保持しません。
