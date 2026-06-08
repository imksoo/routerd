---
title: CloudEdge Mobility 展示
---

# CloudEdge Mobility 展示

![4 站點 CloudEdge SAM mobility 展示的共享 /24 擁有者、MobilityPool、SAMTransportProfile IPIP 傳輸、BGP /32 交付、提供者或 proxy-ARP 捕獲的流程](/img/diagrams/how-to-cloudedge-mobility-demo.png)

> 實驗性功能（CloudEdge）。**Selective Address Mobility (SAM)** 的實驗室展示。on-prem、AWS、Azure、OCI 共享一個邏輯 `/24`，每個站點都能提供其他站點*擁有的*位址 — 無 NAT 且不變更用戶端的預設閘道。可執行套件位於 [`examples/cloudedge-mobility-demo/`](https://github.com/imksoo/routerd/tree/main/examples/cloudedge-mobility-demo)。

![CloudEdge Mobility 展示：4 個站點共享 10.77.60.0/24，每個擁有者位址透過產生的 SAM 傳輸在所有非擁有者站點被捕獲，無 NAT 保留來源 IP](/img/diagrams/how-to-cloudedge-mobility-demo.png)

## 本展示呈現的內容

- **1 個邏輯 `/24` 由 4 個站點共享** — on-prem / AWS / Azure / OCI 全部將 `10.77.60.0/24` 作為單一邏輯位址空間。
- **非擁有者站點捕獲擁有者的位址** — 每個擁有者位址在*所有其他站點*變為可達（雲端：提供者的**次要 IP**，on-prem：**proxy ARP**）。進行單一擁有者仲裁。
- **12 方向 SSH 流通過** — 4 個展示用戶端之間全方向通訊。
- **無 NAT、來源 IP 保留、閘道無變更** — 連線保持實際來源 IP，不進行 NAT，不觸碰用戶端的預設閘道。
- **雲端維護捕獲遷移（D5）** — 捕獲的位址在同一提供者內遷移到另一個路由器作為備用，流量透過新的持有者恢復。

## 位址設計

所有 4 個站點共享一個邏輯子網路，每個站點僅擁有其中 1 個 `/32`。

| 站點 | routerd 節點 | 擁有者位址 | 捕獲機制 |
| --- | --- | --- | --- |
| On-prem | `onprem-router` | `10.77.60.10/32` | LAN 上的 Proxy ARP |
| AWS | `aws-router-a` | `10.77.60.11/32` | ENI 次要 IP |
| Azure | `azure-router` | `10.77.60.12/32` | NIC 次要 ipConfig |
| OCI | `oci-router` | `10.77.60.13/32` | VNIC 次要私有 IP |

邏輯子網路：**`10.77.60.0/24`**。路由器間傳輸使用獨立的 RFC1918 端點/內部位址體系（與鏈結本機 `169.254/16` 或 CGNAT `100.64/10` 分離）。位址限制請參見 [Selective Address Mobility](../reference/selective-address-mobility)。

## 資料平面

- **provider-secondary-ip 捕獲** — 在每個雲端路由器上，*其他*站點的擁有者位址作為次要 IP 附加到該 ENI / NIC / VNIC，雲端 fabric 將流量交付到該路由器。
- **proxy-ARP 捕獲** — 在 on-prem，路由器在 LAN 上對其他站點的擁有者位址回應 ARP。
- **BGP `/32` 交付** — 每個擁有者廣告其擁有的 `/32`，其他路由器匯入最佳路徑並透過 overlay 轉發到擁有站點的路由器。
- **產生的 SAM 傳輸** — 路由器透過從 `SAMTransportProfile` 導出的 IPIP 隧道和 BGP 對等體互連。若啟用 WireGuard，則為端點限定的加密底層網路。其 `AllowedIPs` 包含傳輸端點前綴，不包含行動 `/32`。

交付是路由（而非 NAT），因此**來源 IP 得以保留**，用戶端的**預設閘道不會變更**。

## 控制平面

操作員只需宣告意圖，其餘均為導出。

- **MobilityPool** — 操作員描述的唯一意圖（成員、捕獲模式、交付、放置、維護 drain）。
- **北極星成員結構** — 每個渲染組態透過 `profiles.cloudCaptures`、`spec.values`、`targetFrom`、`subnetRefFrom` 完全宣告自身站點，遠端站點僅為 ID 對等項目。與 BGP 類似，節點需要知道對等體，但不需要對等體的提供者 NIC/子網路實作細節。
- **SAMTransportProfile** — 從共享拓撲和內部前綴導出逐對等體的 `TunnelInterface`、端點 `/32` `IPv4Route`、`BGPPeer` 資源。
- **BGP `/32` mobility 路徑** — 每個擁有者廣告其擁有的主機路由，其他站點透過產生的 SAM 傳輸學習目前最佳路徑。
- **提供者 trap 操作** — 雲端路由器最終將遠端擁有的 `/32` 作為次要 IP assign/unassign 以進行本機捕獲。這些操作不再位於關鍵轉發路徑上。
- **Event Federation** — `routerd.client.ipv4.observed` 事實在站點間傳播（`EventGroup` / `EventPeer` / `EventSubscription`，參見 [Event Federation](../reference/event-federation.md)）。
- **提供者操作 executor** — 在 `ProviderActionPolicy` 下，使用執行個體自身的雲端原生 ID 執行閘控雲端變更（次要 IP 的 assign / unassign、forwarding）（參見 [ADR 0007](../adr/0007-provider-action-execution.md)）。
- **pathSig fencing** — 提供者操作針對目前 BGP 期望路徑簽章和持有者進行 fence，因此過時的操作無法變更在其他地方已重新收斂的路由。

範例組態有意避免了舊式 remote-full 內嵌風格。在預發布期間舊風格仍可接受，但如果遠端 `MobilityPool` 成員包含本機提供者的捕獲或探索詳細資訊，`routerctl validate`、plan、apply 會發出警告。未來的預發布組態中，遠端成員可能僅要求 ID。

## 運行方法

使用 `examples/cloudedge-mobility-demo/` 中的套件。假設實驗室執行個體、NIC/VNIC、ID 權限、SSH、可選的 WireGuard 端點金鑰、提供者 CLI 已準備就緒 — 指令碼**不會**佈建雲端資源。

```sh
cd examples/cloudedge-mobility-demo
cp env.example env
$EDITOR env            # 填寫所有佔位符。secret 不要放入 git

./run-demo.sh          # 渲染 + 部署、事件發布、D3 運行，然後 D5 遷移
./collect-evidence.sh  # 收集提供者狀態、日誌、連線資訊
./reset-lab.sh         # 盡力清理。停止運算資源以避免閒置費用
```

無論是否失敗，每次運行後都請執行 `reset-lab.sh`。

## 已驗證的結果

- **D1** 位置自動反映：出現在 on-prem 的擁有者位址被各雲端路由器辨識。
- **D2** 雲端 -> on-prem 捕獲（proxy ARP）。
- **D3** 4 站點 **12 方向 ping + SSH 通過** — 來源 IP 保留、無 NAT、預設閘道無變更。
- **D4** on-prem HA / VRRP 捕獲故障轉移。
- **D5** 雲端維護 / **捕獲遷移通過** — drain `aws-router-a` 後，捕獲的位址遷移到 `aws-router-b`，流量透過 B 恢復。過時的 pathSig 操作被 fence（`skipped: stale mobility desired path`）。參見 [D5 證據](../releases/evidence/cloudedge-mobility-d5-aws-maintenance-20260531.md)。

## 注意事項

- 這是**實驗室展示**，不是生產就緒的交鑰匙方案。
- **不是**完整的 L2 擴展 / EVPN — 沒有廣播/多播橋接。
- 這是**選擇性 `/32` 位址行動性**：不是整個子網路，而是選定的位址在站點間移動。
- 指令碼假設預佈建的執行個體，使用佔位的非機密邏輯位址。絕不提交實際的帳戶/訂閱/OCID/ENI/VNIC ID 或私鑰。
