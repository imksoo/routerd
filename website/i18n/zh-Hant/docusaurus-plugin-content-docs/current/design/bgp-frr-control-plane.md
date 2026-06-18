---
title: BGP / FRR Control Plane Integration Design
---

:::note 架構注意事項
routerd 目前使用以 GoBGP 為基礎的 `routerd-bgp` 常駐程式，而非 FRR，因此本頁部分內容描述的是舊架構。最新的建議架構請參閱「發布與穩定版」中的**穩定版里程碑**。
:::

# BGP / FRR Control Plane Integration Design

![Diagram showing the BGP FRR control-plane design from TCP VTY readiness problems through FRR service checks, vtysh readiness probes, frr-reload.py reloads, syntax validation, and running-config verification](/img/diagrams/design-bgp-frr-control-plane.png)

本文件說明 routerd 為了支援 BGP 及相關路由通訊協定，與 FRR 控制平面（vtysh、frr-reload.py、常駐程式 socket）互動的設計。

## 問題整理

在停用 TCP VTY 監聽的 FRR 建置版本中（`vty_serv_start()` 內 `port=0`），routerd 原本以 `tcp/2605`（bgpd 的 VTY 監聽埠）作為就緒判斷依據，導致該判斷永遠為假。結果控制器會不斷重啟 FRR，而非對已產生（render）的設定執行 `frr-reload.py`，使 FRR 的 BGP 實例始終未設定完成（缺少 `router bgp X` 段落，亦不監聽 `tcp/179`）。

手動執行 `frr-reload.py --reload /run/routerd/frr/routerd.conf` 即可恢復正常。這表示已產生的設定是正確的，且 frr-reload.py 能夠從無實例的狀態建立 BGP 實例。

## 已確認的 OSS 事實（原始碼層級）

- FRR `lib/vty.c` 的 `vty_serv_start(addr, port, path)`：TCP 監聽僅在 `port != 0` 時啟用。Unix 的 `<daemon>.vty` socket 與此獨立（`#ifdef VTYSH`）。即使是停用 TCP VTY 的發行版，Unix socket 仍存在於 `/run/frr/<daemon>.vty` 或 `/var/run/frr/<daemon>.vty`。
- FRR `tools/frr-reload.py` 的 `is_config_available()`：就緒判斷依據為 `vtysh -c "configure"` 成功執行，且未回報「configuration is locked」。不參考 TCP VTY 的監聽狀態。
- `frr-reload.py` 將「新的 BGP 實例」視為新的 context（`lines_to_add`）處理，因此從無實例狀態首次收斂也在此腳本的處理範圍內。
- `--stdout` 僅重新導向日誌，不影響重新載入行為。

## 設計

### 就緒探測

控制器以一次 `vtysh -c "show running-config"` 交互來探測 FRR 控制平面：

- 結束碼為 0 → 可到達 FRR 控制平面。此輸出同時用作就緒信號，以及 `runningConfigMatches` 的輸入，一次交互達成兩個目的。
- 結束碼非 0 且訊息為 `failed to connect to any daemons` → 無法到達控制平面。在同一次調和（reconcile）中，於逐路徑的逾時到期前重試，逾時後將 `FRRControlUnavailable` 呈現至 status，並由定期調和執行下次重試。
- 結束碼非 0 且為其他錯誤 → 將 stderr 記錄至 status，重試後視為無法到達控制平面。

廢除基於 TCP 的判斷方式。`/run/frr/<daemon>.vty`（及 `/var/run/frr/<daemon>.vty`）Unix socket 檔案的存在與否，僅作為診斷資訊記錄至 status，絕對不作為判斷依據。這是因為在常駐程式初始化或重啟競爭期間，檔案雖存在，vtysh 交互仍可能失敗。

### 調和流程

FRR 的服務狀態是所有調和的前提條件。控制器應將「FRR 正在運行」視為每個週期都需確認並恢復的事項，而非一次性的初始化步驟。這是從 v2007 熱修補中獲得的教訓——當時在移除錯誤的 TCP VTY 判斷時，也一併移除了首次啟動時用來啟動 FRR 的路徑。

```
1. 產生（render） /run/routerd/frr/routerd.conf 與 /etc/frr/daemons。
2. 透過平台服務管理員確認 FRR 的服務狀態
   （`systemctl is-active frr`）：
     - active/running → 不重啟，繼續執行。
     - inactive/stopped → 啟用並啟動 FRR。
     - failed → 重啟 FRR。
     - unknown → 記錄日誌，視同 failed 處理。
   無論 /etc/frr/daemons 是否有變更，每次調和均執行此步驟。
3. 若 /etc/frr/daemons 有變更：
     啟用並重啟 FRR（在上述狀態處理之外額外執行）。
     執行 waitFRRControlReady(ctx, 30s)。
4. 否則：
     執行 waitFRRControlReady(ctx, 5s)。
4. 若就緒等待逾時：
     status = FRRControlUnavailable（若調和內的重試預算仍有餘裕，
     則為 FRRStarting）。回傳 Pending。定期調和（預設 15s）自然重試。
5. vtysh -C -f /run/routerd/frr/routerd.conf（語法驗證）。
   若結束碼非 0：
     status = FRRSyntaxInvalid（終止狀態，需使用者修正設定）。
6. frr-reload.py --reload --stdout /run/routerd/frr/routerd.conf。
   對暫時性的 "configuration is locked" 輸出，使用現有的
   transient-lock 退避（500ms）重試。
   其他結束碼非 0 的情況：
     status = FRRReloadFailed。保存 stderr。回傳 Pending，
     下次調和時重試。
7. 使用相同的 vtysh -c "show running-config" 確認 runningConfigMatches。
   - 結束碼 0 且包含產生的 `router bgp <asn>` 段落 → Healthy。
   - 結束碼 0 但無該段落 → 不一致 → 再次重新載入
     （連續驗證失敗 N 次後升級為 FRRReloadIncomplete，繼續重試）。
   - 結束碼非 0（failed to connect） → FRRControlUnavailable。
```

`waitFRRControlReady` 是可重複使用的輔助函式，用於常駐程式重啟路徑（較長逾時）和僅重新載入路徑（較短逾時）。內部會持續輪詢 `vtysh -c "show running-config"`，直到成功或逾時，並在每次輪詢時將 Unix socket 檔案的存在與否記錄為診斷資訊。

### Status 欄位

BGPRouter / BGPPeer 的 status 物件公開以下欄位：

- `LastControlProbeAt`, `LastControlProbeError`：最近一次 vtysh 交互的結果。
- `LastReloadAttemptAt`, `LastReloadStderr`：最近一次 frr-reload.py 執行的內容（含 transient-lock 重試）。
- `LastReloadDurationMs`, `TransientLockRetries`：運維指標。
- `Phase` enum 新增以下值：
  - `Healthy`
  - `Pending`
  - `Error`
- 原因與狀態碼：
  - `FRRStarting`（暫時性，在調和內的重試預算範圍內）
  - `FRRControlUnavailable`（逾時已超過，由定期調和重試）
  - `FRRSyntaxInvalid`（終止狀態，需使用者修正產生的設定）
  - `FRRReloadFailed`（下次調和時重試）
  - `FRRReloadIncomplete`（重新載入回傳成功，但 runningConfig 中尚無產生的段落，下次調和時重試）
  - `Healthy`

### 逾時與重試預算

| 路徑 | 逾時 | 輪詢間隔 | 定期調和 |
|---|---|---|---|
| 常駐程式重啟 → 就緒 | 30 s | 1 s | 繼承 15 s |
| 僅重新載入 → 就緒 | 5 s | 500 ms | 繼承 15 s |
| configure-locked 暫時重試 | 每次 500 ms | 最多 3 次 | — |

不設指數退避，亦無絕對失敗閾值。定期調和會自然地無限重試。介入與否交由操作員判斷，並透過上述明確的原因碼將狀態呈現出來。

### Healthy 判斷（AND 條件）

BGPRouter 必須滿足以下所有條件，才會進入 `Healthy` 狀態：

- 平台服務管理員確認 FRR 的服務狀態為 `active/running`。
- 所有宣告的 FRR 常駐程式（`/etc/frr/daemons` 中列出的）均未處於 `FAILED` 狀態，且正在運行。
- `vtysh -c "show running-config"` 回傳結束碼 0。
- 設定的位址上有 `:179` 在監聽（BGP 常駐程式正在運行）。
- 輸出包含產生的 `router bgp <our-asn>` 段落。

任一條件不滿足，控制器即呈現對應的原因碼（詳見 status 欄位清單），並保持 `Pending` 或 `Error` 狀態。FRR 停止期間，status 路徑不得崩潰至 `Healthy`。v2007 的迴歸問題（所有 FRR 常駐程式均處於 `FAILED` 狀態，但 routerctl status 仍回報 `Healthy`）正是此 AND 條件所要防範的失敗模式。

## 驗收標準

- 啟動時 FRR 服務處於 `FAILED` 狀態，控制器能偵測並自行恢復。
- FRR 停止期間，或 `:179` 未監聽期間，`routerctl status` 不回報 `Healthy`。
- 在啟用 TCP VTY 的 Linux 發行版上不產生迴歸。
- `runningConfigMatches` 不將 `failed to connect` 視為一致。
- 上述所有 status 原因碼在對應的失敗模式下均能產生。

## 測試情境

1. Linux 發行版首次啟動（tcp/2605 在監聽）：執行重新載入，runningConfig diff 與 status 均無迴歸。
2. 從損壞狀態恢復：在無 BGP 實例的 FRR 運行中的路由器上升級 routerd 二進位 → 無需手動介入即執行重新載入。
3. 常駐程式重啟期間 vtysh 暫時 `failed to connect` → 控制器在就緒預算內等待，vtysh 恢復後繼續進行驗證與重新載入。
4. vtysh 永久失敗 → 逾時後顯示 `FRRControlUnavailable`，定期調和重試。
5. `vtysh -C -f` 拒絕語法 → `FRRSyntaxInvalid`。不執行重新載入，不產生 churn。
6. `frr-reload.py` 回傳非 0 → `FRRReloadFailed`。下次調和時重試。
7. `frr-reload.py` 回傳 0，但 running-config 中尚無產生的段落 → `FRRReloadIncomplete`。下次調和時重試。
8. 暫時發生 configure-lock → 現有的 transient-lock 重試路徑成功完成。
9. 啟動時 FRR 服務處於 FAILED 狀態：routerd 必須重啟 FRR，無需手動介入即可恢復常駐程式。常駐程式啟動前，status 應反映 FAILED 狀態。
10. status 正確性：在曾達到 Healthy 狀態後強制停止 FRR（`systemctl stop frr`），下次調和必須呈現 `FRRControlUnavailable` 或 `FRRServiceDown`，而非 `Healthy`。失敗期間，BGPRouter status 的 `lastSuccessTime` 不得推進。

## FRR Issue #8403（graceful-restart 的結束碼 != 0）

FRR 8.4.x 前後的版本中，包含 `bgp graceful-restart` 設定時，`frr-reload.py` 可能回傳非 0 的結束碼。需先取得 `frr -v` 的 Phase 0 記錄，確認附帶版本受影響後，才加入對應處理。不在熱修補中加入投機性的版本偵測程式碼。

## 架構後續對應（熱修補後）

熱修補合入後，將 FRR 探測與重新載入的職責切出至 `pkg/frr/`，提供 `Prober` 介面及封裝所有 vtysh / frr-reload.py 呼叫的 `DefaultProber`，其方法包含 `Probe`、`Validate`、`Reload`、`RunningConfig`。如此一來，BGP 控制器將成為對 `Prober` 的薄層 dispatch，可進行獨立的 mock 測試，並可供未來的控制器（OSPF、IS-IS 等）重複使用。

熱修補本身為將差異最小化，暫時保留在 BGP 控制器中，並在後續發布中制定明確的遷移計畫以移至 `pkg/frr/`。

## 參考資料

- FRR `lib/vty.c` 的 `vty_serv_start`, `vty_serv_un`
- FRR `tools/frr-reload.py` 的 `is_config_available`, context-diff
- FRR 文件：`docs.frrouting.org/en/latest/frr-reload.html`
- FRR Issue #8403（graceful-restart 結束碼）
- VyOS `python/vyos/frr.py`（參考：無預先探測的重新載入）
- k8s-rt-02 的 Phase 0 記錄（`/tmp/bgp-pre-reload/`）
