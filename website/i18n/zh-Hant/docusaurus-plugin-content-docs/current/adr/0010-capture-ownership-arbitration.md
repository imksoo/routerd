# ADR 0010: Capture 所有權仲裁（多執行個體所有權對映 + ownershipEpoch fencing）

![ADR 0010 的示意圖。從重複持有者的風險出發，到 ownershipEpoch 和所有權對映的設計決策，VRRP 或單一路由器的 capture 護欄](/img/diagrams/adr-0010-capture-ownership-arbitration.png)

## 狀態

已提議。核准為實驗性實作 — 2026-06-01。

以 [ADR 0008: 基於 fencing token 的 Capture 協調](../adr/0008-capture-coordination-fencing.md)
和 [Selective Address Mobility](../reference/selective-address-mobility) 資料平面為基礎。
對應 issue #76。消費者為
[ADR 0011: 通用故障切換](../adr/0011-generalized-failover.md)（#74）。
實驗性。

## 背景

規模化時，單一雲端路由器無法持有所有 capture 的 secondary IP
（ENI/NIC/VNIC 插槽限制），因此 `N+1` 組態的同 provider 路由器需要
**分散** capture 的位址。目前 routerd **沒有跨節點所有權對映，
也沒有互斥控制**：

- 協調是**單節點本機投影**：每個節點從相同的 federation 事件流
  獨立投影到相同的 `AddressLease` 狀態
  （`pkg/controller/mobility/controller.go`）。**無分散式鎖、無仲裁、無共識**。
- 「單一擁有者」是*隱式的*（capturePolicy `all-non-owner-sites` + 確定性
  `evaluatePlacement`），而 `captureEpoch`
  （`pkg/state/mobility_capture_epoch.go`）是**每節點、每
  (pool, address, captureDomain)** 的單調遞增 token，在匯入/執行閘門處
  圍欄 stale 的 provider action（ADR 0008）。
- 預留欄位 `MobilityPoolSpec.Authority` 未使用。

#76 要求集中式所有權對映、競爭排除和腦裂防止。
ADR 0008 有意**迴避共識**（Paxos/Raft/etcd），
從單調遞增 fencing token + provider 的結構性單一配置 + 冪等收斂
建構安全性。此 ADR 延續該理念。

### 「所有權」在無共識下能保證什麼、不能保證什麼（誠實的範圍）

這**不是 linearizable 的分散式鎖**。事件順序仲裁 +
fencing + 雲端的單一配置語意保證以下：

1. 看到同一事件流的所有節點**收斂到相同的 owner 對映**；
2. 看到 ownershipEpoch *N+1* 的節點不會執行 epoch-*N* 的 action
   （在閘門處圍欄）；
3. 雲端 secondary IP 恰好屬於一個 NIC，因此 provider 狀態
   **收斂到單一配置**。

**不能保證的**：從 federation 分割的（未看到 *N+1* 的）
舊 owner，如果仍然存活，透過 provider API 重新取得位址 —
排除這一點需要共識 / STONITH / provider 的條件式 fencing，
但不新增。因此屬性為**「圍欄式 eventual 所有權 +
provider 強制的單一配置」**，而非「腦裂防止」。
On-prem 的 **proxy-ARP** 更弱（無 provider 單一配置）：
上限為 VRRP master 權限 + fail-closed（遵循 ADR 0008）。

## 決策

### `ownershipEpoch` — 每 (pool, address) 的叢集圍欄 token

引入 **`ownershipEpoch`**。比 `captureEpoch` 更高層次的概念：
每 (pool, address) 的單調遞增 token，**僅在確認的 owner 變更時**遞增
（lease 在 candidate/holding 階段不遞增）。跨雲端 / on-prem /
provider / action 的圍欄 token。`captureEpoch`
作為相容性/衍生註解保留。正本遷移到 `ownershipEpoch`。

### 所有權對映 — 無 leader 的確定性收斂

**沒有選舉的 leader**（leader 選舉需要共識）。所有權對映是
每個節點從 federation 事件流確定性建構的**收斂檢視**：

- 每個 `(pool, address)` 的 owner 透過確定性仲裁選擇：
  **preferNodes → 放置優先順序 → 穩定 tie-break** 對*合格*成員套用
  （合格性由 ADR 0011 定義：未排空、健康、存活、適用時 VRRP master）。
- 多執行個體分散：placement group 內每個位址仲裁到一個 owner。
  位址集分散到合格成員（未來：最小負載）。1 IP → 同時 1 個 owner。
- 對映**可視化**（status DB + 指標 + control/`routerctl`），
  操作員可以看到「哪個 IP 被哪個節點所有」 —
  以收斂檢視而非單一寫入者儲存實現 #76 要求的「集中式所有權對映」。

### `MobilityPool` 的 `ipOwnershipPolicy`

```yaml
spec:
  ipOwnershipPolicy:
    type: centralized          # 收斂的確定性對映（唯一模式）
    epochLocking: true         # 用 ownershipEpoch 為 action 打戳+圍欄
    preferNodes: [aws-router-a, aws-router-b]
    autoFailover: true         # ADR 0011（活性驅動 seize）消費
```

`preferNodes` 為仲裁施加偏向。`epochLocking` 啟用
ownershipEpoch fencing。`autoFailover` 是 ADR 0011 使用的鉤子。
`type` 目前僅一個模式（`centralized` = 收斂的確定性）。

### Action 冪等性 key

provider action 的冪等性 key 至少包含 `pool / address / ownerNode /
ownershipEpoch / actionVerb / provider / nicRef`。stale epoch 或
錯誤 owner 的 action 被確定性圍欄。

## 階段劃分（此 ADR）

- **Phase 1（此 ADR 的最小範圍）**: `ownershipEpoch` token、
  確定性所有權記錄 + 仲裁（preferNodes/priority/tie-break）、
  `ipOwnershipPolicy` spec + 驗證、**所有權對映的可視化**（status +
  指標 + `routerctl`）。**無自動 seizure** — Phase 1 僅
  *計算並發布* desired 所有權，用 ownershipEpoch 圍欄 action。
  現有靜態放置繼續驅動誰來執行。
- 活性驅動的故障切換/seize 在 **ADR 0011**。

## 結論

- routerd 取得了一個叢集收斂、圍欄式的所有權模型，用於在 N+1 同 provider 路由器間
  分散 capture 的 IP，而無需新增共識儲存。
- 安全性範圍被誠實地陳述（「圍欄式 eventual 所有權」，而非分散式鎖）。
  雲端結構性地強，on-prem 是 VRRP 權限的盡力而為。
- `ownershipEpoch` 是單一的跨切面圍欄 token，供 ADR 0011 的 seize 和
  Phase 4 的雲端清單/漂移偵測建構其上。
