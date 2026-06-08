# ADR 0012: BGP /32 位址行動性

![ADR 0012 的示意圖。將 lease 和 epoch 所有權替換為 BGP best-path /32 通告，活性標記、Route Reflector 路徑、FIB 匯入、後台 provider capture](/img/diagrams/adr-0012-bgp-address-mobility.png)

## 狀態

已核准。Phase 1 的 Clean Option B 已實作至 B6/B7 — 2026-06-03。

[ADR 0006](../adr/0006-event-federation.md)、
[ADR 0008](../adr/0008-capture-coordination-fencing.md)、
[ADR 0010](../adr/0010-capture-ownership-arbitration.md)、
[ADR 0011](../adr/0011-generalized-failover.md) 為 CloudEdge 行動性資料平面
引入的自訂 overlay 可達性正本被替換。舊有的 provider action、VRRP、
doctor 安全機制作為後台 reconciliation 和本機 capture 守衛保留在範圍內。

## 背景

CloudEdge 的 Selective Address Mobility 最初從 routerd 專有控制平面
建構 overlay 可達性：

- Event Federation 傳遞 observed/expired/heartbeat 事實；
- mobility 控制器將這些事件投影為 `AddressLease` 列；
- planner 將 lease 下降為 `AddressMobilityDomain`、`RemoteAddressClaim`、
  provider `ActionPlan`、`captureEpoch`、`ownershipEpoch` 狀態；
- SAM 將產生的 claim 下降為路由、proxy-ARP、provider secondary-IP action；
- provider action 控制器核准/執行雲端 mutation。

這證明了產品路徑，但也使故障切換依賴於一條長的 routerd 專有鏈。
實際 4-site 測試中，overlay/雲端故障切換受限於 reconcile tick、
lease/epoch 投影、action 匯入/自動執行、provider API 行為、
雲端 fabric 傳播。最近的冒煙結果顯示 AWS/OCI 雲端故障切換約需 120 秒，
而目標是 60 秒以下，overlay 流量最好在秒級。

routerd 已隨附基於 GoBGP 的 `routerd-bgp` daemon 和 BGP 控制器。
現有介面可完成 GoBGP 啟動、peer 和策略組態、透過 `AddPath` 通告
靜態 IPv4/IPv6 unicast 前綴、透過 `DeletePath` 撤回、
觀測 best path / 匯入 Linux IPv4 FIB。GoBGP v3.37.0 也支援
EVPN Type-2/Type-5 和 MAC mobility 序號，但 routerd
目前的 BGP 資源模型和 FIB syncer 僅公開 IPv4/IPv6 unicast。
最快的有用切入點是普通的 IPv4 unicast `/32` 行動性，而非 EVPN。

雲 provider fabric 是另一個約束。AWS VPC 路由表、Azure UDR/Route Server、
OCI VCN 路由表不會自動跟隨 VM 私有 GoBGP overlay 通告，除非
組態了顯式的雲端路由整合。provider 的 secondary-IP 配置、路由表目標變更、
Azure Route Server 等 provider 服務在雲原生入口時可能仍然需要。
BGP 可以將 provider API 呼叫從 overlay 可達性的關鍵路徑中移除，
但並不消除 provider 入口問題。

## 決策

將 CloudEdge 行動性的**overlay 可達性正本**遷移到 BGP RIB：

- `MobilityPool` 中的每個所擁有位址表示為 IPv4 unicast `/32` BGP 通告。
- 位址的 owner 是在該 `/32` 的 BGP best-path 選擇中勝出的節點。
- 非 owner 從 BGP best path 學習遠端所擁有位址，透過 BGP FIB importer
  而非產生的 SAM 投遞路由安裝 overlay 投遞路由。
- 行動性轉移表示為 BGP withdraw/advertise 和路徑優先順序變更。
  操作員意圖透過 `MobilityPool` 保持宣告式。操作員無需
  手動記述 lease、claim 或 provider action。
- best-path 仲裁優先使用標準 unicast 屬性：
  `LOCAL_PREF`/`MED`/communities + 確定性路由策略。可能新增
  路由序列 community 以提高可觀測性，但普通 BGP 不將
  「新序列勝出」作為原生規則。
- EVPN 明確延後。EVPN Type-2 MAC/IP 行動性是未來的互通選項，
  不是 Phase 1 的機制。

Provider secondary-IP 和轉送 action **降級為後台 reconciliation**：

- 對透過 VPC/VNet/VCN 進入的雲端 fabric 入口路徑仍然需要。
  作為已建立的 routerd overlay 路徑的替代。
- 從相同的 BGP 行動性檢視和 provider 清單/action 日誌
  進行 eventual reconcile。
- 不得成為 overlay 可達性的正本。

On-prem LAN capture 保持本機：

- VRRP master 閘門、proxy-ARP、GARP、非 master 的 fail-closed 行為、
  重複持有者 doctor 檢查維持不變。
- BGP 決定遠端 overlay 可達性。不替換本機 L2/ARP 權限守衛。

## Clean Option B 的最終狀態

預發布實作直接以 BGP 作為行動性的正本：

- **所有權:** 行動 `/32` 的 owner 是該前綴目前的 BGP best path。
  沒有單獨的 `AddressLease`、ownership epoch、capture epoch 登錄檔。
- **投遞:** 非 owner 將 BGP best path 匯入本機 FIB，
  透過 overlay next hop 路由 `/32`。MobilityPool 的
  route-mode 規劃和產生的 SAM 投遞 claim 不在主線中。
- **Capture/trap:** 雲端 provider secondary-IP action 從 BGP best-path 檢視和
  本機放置衍生。不是 overlay 可達性的前提，而是
  後台 fabric 入口 reconciliation。
- **Fencing:** provider action 攜帶目前行動性路徑簽章
  （`mobilityPathSig`）+ desired 持有者和 observed provider/日誌轉換。
  當 desired BGP path 不再匹配時 stale action 被跳過。
  舊有 ownership/capture epoch 資料表已刪除。
- **活性:** 行動性故障切換依賴 BGP withdrawal 和 best-path 收斂。
  快速故障偵測由渲染到 FRR `bfdd` 的 `BFD` 資源提供。
  BGP hold 計時器作為 BFD 不穩定時路由 withdrawal 的非破壞性權威保留。
  自訂行動性心跳/staleness 投影已刪除。
- **On-prem LAN 權限:** VRRP master 閘門、proxy-ARP、GARP、
  非 master fail-closed 行為、重複 proxy-ARP doctor 檢查作為本機安全機制維持。
- **刪除的狀態:** B6 中實體刪除了行動性 lease、ownership epoch、capture epoch、
  deprovision 標記的資料表和 API。該階段淨減約 6,200 列。

## 非目標

- Phase 1 不實作 EVPN。
- Phase 1 不刪除 provider executor。
- 不聲稱僅 BGP 即可解決雲原生入口。
- 不新增共識、etcd、Raft、單寫入者 lease 資料庫。
- 不要求操作員為每個位址記述動態 BGP path 資源。
- 不全域刪除 Event Federation。在 BGP path 證明後
  僅退役行動性專有的使用。

## 模型

預期穩態對映：

| 現有概念 | BGP 行動性概念 |
| --- | --- |
| `AddressLease` 活躍 owner | `pool/address/32` 的 BGP best path |
| observed owner 事件 | 本機 `/32` advertise |
| expired/released 事件 | 本機 `/32` withdraw |
| `staticOwnedAddresses` | 所有成員的靜態本機 `/32` advertise |
| F3 交接 | release/withdraw 屏障，隨後新 owner advertise |
| `RemoteAddressClaim` 投遞路由 | 匯入的 BGP `/32` FIB 路由 |
| capture 放置的活躍成員 | 路徑優先順序 / origin 合格性 |
| overlay 路由的 `ownershipEpoch`/`captureEpoch` | best-path 檢視和可選路由中繼資料 |
| provider secondary-IP action | 後台 fabric 入口 reconciliation |
| on-prem proxy-ARP 權限 | 不變的 VRRP master 閘門 |

## Phase 1 範圍

Phase 1 建構了 BGP unicast path，並在發布前刪除了被替換的自訂
行動性 planner/狀態路徑。

1. 為 routerd 產生的 `/32` 通告新增來源感知的動態 BGP path 管理。
2. 將 `MobilityPool` 的 owner 狀態投影到 BGP 通告。
3. 消費 BGP best path 作為遠端位址投遞檢視。
4. 將故障切換和靜態交接的 overlay 可達性遷移到 BGP withdraw/advertise。
5. 將 provider secondary-IP 處理轉換為後台 reconciliation。
6. 對等證明後刪除舊有 lease/planner/epoch 路徑。

## 結論

正面影響：

- Overlay 故障切換變成路由收斂問題，而非 routerd 專有的
  lease/action/provider 序列工作流。
- 設計與 BGP 服務 VIP 和 pod/服務路由通告等
  Kubernetes 邊緣模式對齊。
- 最複雜的自訂狀態（`AddressLease` 投影、capture 放置、
  capture/ownership epoch 規劃、deprovision 標記）可在
  遷移後大幅精簡。
- D3/D5/D6/D7 的 overlay 可達性可在雲 provider secondary-IP reconciliation
  仍掛起時收斂。

負面影響 / 風險：

- 普通 BGP 需要顯式策略以避免相同前綴通告的歧義。
  序列 community 不是原生 fencing token。
- 除非部署也組態了雲端路由整合，否則在後台 provider 狀態追上之前
  provider fabric 入口可能不可用。
- 現有的實際展示和 acceptance 探針需要區分 overlay 可達性與
  雲原生入口。
- routerd 的 GoBGP 觀測目前基於輪詢。Phase 1 可能需要新增事件驅動的
  `WatchEvent` 路徑，否則 BGP 路由安裝迴圈會殘留輪詢延遲。
- 腦裂防止仍依賴 VRRP/provider fencing/doctor 檢查。
  BGP best path 選擇一條轉送路徑，但僅靠它不能移除 stale 的本機
  proxy-ARP 或 stale 的 provider 配置。

## 遷移規則

- 維持 `MobilityPool` 作為操作員記述的唯一行動性意圖。
- 將 MobilityPool 的預設投遞設為 BGP。舊 MobilityPool route-mode planner
  是遷移輔助，乾淨的預發布 API 不接受。
- 不在沒有確定性優先順序規則的情況下，對同一 `(pool, address)` 同時執行
  兩個路由下降來源。
- 在產生的 BGP path 上標記來源中繼資料，使靜態 BGP 通告不被
  行動性 reconciliation 誤撤回。
- 在 provider reconciliation 存續期間，維持 provider action 的冪等性和
  path 簽章 fencing。

## 退出條件

- 4-site 展示使用 BGP 學習的 `/32` overlay 路由通過定向 SSH 矩陣。
- 協調排空和 stale owner 故障切換透過 BGP 收斂，無需在 overlay 路徑上
  手動核准/執行 provider action。
- Provider secondary-IP action 的延遲或失敗不破壞 overlay 可達性。
- VRRP/proxy-ARP on-prem 的 fail-closed 語意未改變。
- 舊有行動性 lease/planner 路徑在測試和實際證據涵蓋 BGP 路徑後已刪除。
