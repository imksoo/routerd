---
title: CloudEdge SAM Phase G — 説明図
---

# CloudEdge Selective Address Mobility (Phase G) — 説明図

![SAMTransportProfile IPIP delivery（オプション WireGuard underlay 上）、BGP /32 所有権と liveness marker、provider または on-prem capture、NAT なし source 保持を示す CloudEdge SAM Phase G の図](/img/diagrams/explainer-cloudedge-sam-phase-g.png)

CloudEdge SAM Phase G は、AWS / Azure / OCI / on-prem をまたいで選択した `/32`
サービス/クライアントアドレスを、**NAT なし・source IP 保持・default gateway 不変**で
到達可能にし、ルーターノード障害時に同一サイトの standby が自律的に所有権を取得して
L3 到達性を復旧する仕組みです。

設計の核は **clean Option B**:

- **ownership = BGP best-path** — `/32` の所有者は BGP の最良経路が決める（中央ロックや
  lease/epoch を持たない単一の真実源）。
- **liveness = per-node marker** — 各ノードが overlay `/32` + identity community の
  marker を広告。active marker の消失が failover トリガ。
- **trap = RIB-driven** — RIB の変化（remote-owned `/32` の best-path）を routerd が trap。
- **seize = liveness-driven** — active marker 消失で同一サイトの standby が seize。

---

## 1. トポロジ — SAM transport + iBGP hub-spoke

各サイトの routerd は `SAMTransportProfile` が生成する IPIP transport 上で iBGP を張り、
on-prem の Route-Reflector(RR) をハブにする hub-spoke 構成。暗号化が必要な環境では
WireGuard を endpoint 専用 underlay として下に敷く。

```mermaid
graph TB
  subgraph onprem["on-prem (hub)"]
    RR["routerd<br/>iBGP Route-Reflector<br/>VRRP master / backup<br/>owns 10.77.60.10/32"]
  end
  subgraph aws["AWS"]
    AWSR["routerd-aws A / B<br/>ENI secondary IP"]
  end
  subgraph azure["Azure"]
    AZR["routerd-azure A / B<br/>NIC ipConfig"]
  end
  subgraph oci["OCI"]
    OCIR["routerd-oci A / B<br/>VNIC secondary IP"]
  end
  AWSR ===|"IPIP SAM transport + iBGP"| RR
  AZR  ===|"IPIP SAM transport + iBGP"| RR
  OCIR ===|"IPIP SAM transport + iBGP"| RR
```

- logical `/24` = `10.77.60.0/24`。各 site の owner `/32`（例 on-prem `.10` / AWS `.11`
  / Azure `.12` / OCI `.13`）を全 site から到達可能にする。
- default delivery は IPIP。WireGuard を使う場合も `AllowedIPs` は transport endpoint
  prefix だけにし、mobile `/32` は BGP と FIB が扱う。

---

## 2. 所有権と自律フェイルオーバー

active が所有 `/32` と liveness marker を高 local-pref で広告。ノード障害で marker が
withdraw されると、同一サイトの standby が **手動操作ゼロ**で seize する。

```mermaid
sequenceDiagram
  autonumber
  participant A as Active (owner)
  participant RR as iBGP RR (on-prem)
  participant S as Standby (same site)
  participant P as Provider API / L2
  A->>RR: advertise owned /32 (high local-pref) + liveness marker
  Note over A,RR: BGP best-path = owner
  A--xRR: ノード障害 — marker /32 が withdraw
  RR-->>S: best-path 喪失 / active marker 消失
  S->>S: liveness-driven seize（election）
  S->>P: capture（secondary IP 付与 / proxy-ARP+GARP）
  S->>RR: 自 /32 + marker を高 local-pref で再広告
  Note over S,P: dataplane 復旧 — source 保持 / default-gw 不変
```

実測収束時間（acceptance）: AWS 16.9s / Azure 56.7s / OCI seize / **on-prem VRRP 8s**、
すべて `manualProviderAction=false`（自律）。目標 60s 以下。

---

## 3. capture の実現 — trap から各 provider / on-prem へ

RIB の best-path 変化を trap し、cloud では provider secondary IP、on-prem では
VRRP-master gated な proxy-ARP + GARP で `/32` を捕捉する。

```mermaid
flowchart LR
  RIB["BGP RIB 変化<br/>remote-owned /32 の best-path"] --> TRAP{"routerd trap<br/>(RIB-driven)"}
  TRAP -->|"cloud 非 owner site"| SEC["provider secondary IP<br/>AWS ENI / Azure NIC ipConfig / OCI VNIC"]
  TRAP -->|"on-prem VRRP master"| PARP["proxy-ARP + GARP<br/>(backup = fail-closed)"]
  SEC --> DP["dataplane<br/>NAT なし / source IP 保持 / default-gw 不変"]
  PARP --> DP
```

- on-prem は **VRRP-master hard-gate**: master のみ proxy-ARP/GARP を出し、backup は
  fail-closed（`proxy_arp=0`、ARP 応答しない）。`routerctl doctor hybrid` が split-brain
  を deterministically FAIL（loop-free by design）。
- cloud capture の provider mutation は最小権限 identity（AWS ENI-scoped / OCI compartment /
  Azure custom role）で自律実行。

---

## 4. データプレーン不変条件

```mermaid
flowchart LR
  C["client<br/>(任意 site)"] -->|"src = 自 /32 owner<br/>dst = 相手 /32"| OVL["IPIP SAM transport<br/>+ iBGP best-path route"]
  OVL --> SVR["server<br/>(owner site / seize 先)"]
  SVR -. "応答も同 /32、NAT translation なし" .-> C
```

- **NAT なし** — translation signature が出ない（tcpdump で確認）。
- **source IP 保持** — server から見える source は client の `/32`。
- **default gateway 不変** — client の既定経路は変わらない。
- **MTU/PMTU** — overlay 実効 MTU に追従して MSS clamp(`routerd_mss`)、必要なら
  IPv4 force-fragment(P2-b, ADR 0013, default off)で DF blackhole を回避。
- 透過性 acceptance: FTP(active/passive) / NFS / RPC(rpcbind) / 100MB bulk が
  fragment/blackhole なく完走（source 保持・no-NAT 確認済）。

---

## 5. 旧モデルとの対比

```mermaid
flowchart TB
  subgraph OLD["旧 (superseded)"]
    direction LR
    MP["MobilityPool"] --> AL["AddressLease"] --> OE["ownershipEpoch"] --> AP["ActionPlan"] --> PA1["ProviderAction"]
  end
  subgraph NEW["Phase G (clean Option B)"]
    direction LR
    BGP["BGP best-path<br/>ownership"] --> MK["per-node<br/>liveness marker"] --> TR["RIB-driven trap"] --> CAP["provider / on-prem<br/>capture realization"]
  end
  OLD -. "ADR 0012 が ADR 0006 を supersede" .-> NEW
```

複数の真実源（lease / epoch / heartbeat / action journal）が絡む複雑さを撤去し、
**BGP を唯一の ownership plane** にしたことで、ネットワーク的に説明しやすく堅牢になった。

---

## 関連

- ADR 0012: BGP /32 Address Mobility（clean Option B）
- ADR 0009: Pluggable Overlay Underlay（ipip/gre/fou/gue）
- ADR 0013: IPv4 Force Fragmentation
- reference: Selective Address Mobility
- how-to: cloudedge-mobility-demo / cloudedge-autonomous-lab
- スライド: `docs/slides/cloudedge-sam-phase-g.md`
