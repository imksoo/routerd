# CloudEdge / Selective Address Mobility — experimental MVP, 多雲實驗室驗證完成

狀態: **experimental** (實驗室驗證; 不建議作為穩定版)
分支: `cloudedge-mvp` · 日期: 2026-05-29 (2026-05-30 更新: OCI 追加 → 3 雲對等)

## 概述

CloudEdge Selective Address Mobility (SAM) MVP 已在 **3 個雲端** 完成多雲實驗室驗證。Azure x PVE、AWS x PVE、OCI x PVE 均通過了同一子網 /32 移動性冒煙測試: 雲端 VM (`.7`) 和本地/PVE VM (`.9`) **透過 routerd 間 WireGuard overlay 進行雙向通訊 (ping + SSH + 100 MiB scp, 保留來源位址), 無 NAT 且無需變更用戶端預設閘道**, 如同處於同一邏輯子網上。

這 **不是完整的 L2 擴展**。SAM 捕獲選定的 /32 IPv4 位址, 並在保留來源/目的位址的同時透過 overlay 進行傳遞。

## 驗證結果

| 場景 | 結果 | 證據 |
|---|---|---|
| Azure x PVE 同子網 /32 移動性 | PASS / clean | `docs/releases/evidence/cloudedge-sam-azure-pve-20260529.md` |
| AWS x PVE 同子網 /32 移動性 | PASS / clean (Azure 對等, 首次執行) | `docs/releases/evidence/cloudedge-sam-aws-pve-20260529.md` |
| OCI x PVE 同子網 /32 移動性 | PASS / clean (PMTU/MSS clamp 修復 #53 後) | `routerd-labs/cloudedge-sam/evidence/20260530T031247Z-oci-pve-hardening-43a64c55/summary.md` |

3 次執行全部通過。AWS **無需任何 AWS 特定的程式碼變更** 即在首次執行時通過。OCI 最初在低 PMTU underlay 下 TCP 出現黑洞 (ping 通過, SSH/scp 逾時) — 正如 #50 所預測的故障 — PMTU/MSS clamp 依賴於 `FirewallZone`, 而 SAM (純轉發平面) 不定義 FirewallZone, 因此任何雲端均未匯出 `routerd_mss` clamp。修復 (#53) 使 clamp 成為 **FirewallZone 無關且介面類型無關**: 透過 `hybrid.EstimateMTU` 取得有效 overlay MTU, 為 overlay 隧道 MTU 存在實質降低的轉發傳遞路徑匯出 MSS clamp (OCI 上 MSS 1300)。家用路由器 (PPPoE/DS-Lite) 無變更 (無 `RemoteAddressClaim` → 轉發路徑集為空 → zone 輸出相同)。修復後, OCI x PVE 的 `routerd_mss` 在兩側均存在, `doctor hybrid` PASS, 狀態乾淨。

## 已驗證的抽象

- **capture — provider 特定**: Azure NIC 輔助私有 IP + NIC IP 轉發;
  AWS ENI 輔助私有 IPv4 + EC2 source/destination check 停用; OCI VNIC
  輔助私有 IP + `skipSourceDestCheck=true`。
- **delivery / claim / doctor — routerd 通用**: `RemoteAddressClaim` →
  `wg-hybrid` 上的 `/32` 傳遞路由; 本地 proxy-ARP 返回捕獲; 無 NAT;
  來源/目的保留; `routerctl doctor hybrid`。provider-secondary-ip 的 de-assign 加固和
  WireGuard stdin apply 已跨雲端通用化。

## 此分支的內容 (cloudedge-mvp, 與 main 的差異)

- Dynamic-config 基礎設施: `DynamicConfigPart` / mask 指令 /
  `DynamicOverridePolicy`; effective-config = startup + active dynamic parts - masks。
- Plugin runner (observe-only, dry-run): `Plugin` / `DynamicConfigSource` /
  `PluginResult`; actionPlans 僅用於展示。
- L3 hybrid: `OverlayPeer` / `HybridRoute` (lowered 到既有的 IPv4Route install)。
- Selective Address Mobility: `AddressMobilityDomain` / `RemoteAddressClaim` /
  `CloudProviderProfile`; Linux 資料平面 (proxy-ARP 捕獲 + /32 overlay 傳遞 +
  provider-secondary-ip OS 位址 de-assign)、`routerctl doctor hybrid`。
- nftables ownership marking (用於 stale-table 診斷)。

## 範圍 / 已知限制 (experimental 而非穩定版的原因)

- 無雲端 provider API mutation (輔助 IP 分配 / 路由表由
  供應側 / 手動完成; actionPlans 僅用於展示)。
- SAM 即時資料平面僅限 Linux。
- 無完整的 L2 / EVPN / BUM / 廣播網域擴展。
- GCP 未驗證 (Azure / AWS / OCI 已驗證; OCI 於 2026-05-30 追加)。
- OCI Ubuntu 映像預設包含 `iptables` reject-all FORWARD/INPUT,
  阻塞 WG/overlay 轉發路徑 (#52) — `doctor hybrid` 偵測並修復於主機側
  (主機防火牆不在 routerd 核心範圍內; routerd 不自動供應, 僅警告)。
- 生產拓撲變體未驗證。
- 設定人因工程的粗糙之處和手動引導/金鑰流程仍存在 (例: WG
  `allowedIPs` 需與捕獲目標的 `/32` 手動匹配; WireGuard 金鑰和主機
  package/systemd 引導為手動)。完整列表參見合併前盤點:
  `docs/releases/cloudedge-sam-stocktake-20260529.md`。冒煙測試中的手動
  *修復* 均已遷移至 routerd 原生 (#41/#42/#43/#45/#47); 剩餘項目為
  設計上的 provider 供應或已追蹤的 experimental 後續跟進。

## 建議

作為 **experimental** 的 CloudEdge/SAM MVP 功能合併至 `main` (標記為 experimental)。穩定升級 / 發佈標籤待進一步驗證後再行決定。
