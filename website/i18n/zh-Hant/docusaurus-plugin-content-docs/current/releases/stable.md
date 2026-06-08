---
title: 穩定版里程碑
sidebar_label: 穩定版里程碑
sidebar_position: 0
---

# 穩定版里程碑

routerd 以 `vYYYYMMDD.HHmm` 格式頻繁發布版本，其中經過評估**可供正式環境使用**的版本，會在每個里程碑時選定為「穩定版里程碑」。初次部署時，請使用本頁所列的版本。

## 目前推薦版本

| 項目 | 內容 |
| --- | --- |
| 版本 | **v20260608.0642** |
| 定位 | 推薦穩定版（取代 v20260528.2308。ADR 0014 CLI 體系重新設計 — `routerd` 僅作為守護程式，`routerctl` 作為管理 CLI。OpenRC 管理可靠性提升、DNS 解析器支援 VRRP VIP 監聽、forcefrag 移至 prerouting、BGP peer watch 穩定化） |
| 運行實績 | 在 lab 環境（router06/router07/k8s-rt-01/k8s-rt-02）和生產路由器（homert02）上驗證完畢。Cloud VM 測試（lab + k8s）全部 PASS。解決 12 個 issue，合併 12 個 PR |
| 二進位 | 靜態連結（`CGO_ENABLED=0`），通過 CI 和 Release 工作流程 |

## 推薦 v20260608.0642 的理由

本版繼承了 v20260528.2308 的所有生產安全特性，在此基礎上以 **CLI 體系重新設計**（ADR 0014）和 **OpenRC / init 指令碼可靠性提升**為核心，加入了 40 個提交的改進。

### ADR 0014 — CLI 體系重新設計

routerd 的 CLI 被清晰地拆分為「守護程式」和「管理工具」。

- **`routerd`** 僅作為守護程式。唯一的子命令是 `routerd serve`。
- **`routerctl`** 是管理 CLI：`validate` / `plan` / `apply` / `doctor` / `get` / `describe` / `status` / `ledger` / `dns-queries` / `traffic-flows` 等全部管理操作。
- 舊有的 `routerd apply` / `routerd validate` / `routerd run` 已移除。`--once` 旗標也已廢棄。
- 文件和指令碼中的命令參考已全部更新為新的 verb 體系（#254–#262）。

### OpenRC / init 指令碼可靠性

針對 FreeBSD 和 OpenRC 環境的 init 指令碼管理套用了 6 項修復。

- **消除 OpenRC DNS 解析器的雙重管理**（#306）— 此前 `routerd serve` 和 OpenRC 同時嘗試管理 DNS 解析器，導致雙重啟動。
- **OpenRC 升級時停止舊的 `routerd serve`**（#311, #313）— 修復升級過程中舊程序殘留的問題。
- **OpenRC 重啟時清理託管的 helper**（#315）— 防止孤兒 helper 程序累積。
- **DNS 解析器 helper 監控**（#283）— OpenRC 現在能正確監控和啟動 DNS 解析器的 helper 程序。
- **殘留 helper 更新**（#280）和 **OpenRC 重啟的 nodeps 化**（#278）— 解決升級時的服務依賴問題。

### 網路功能改進

- **DNS 解析器可在 VRRP VIP 上監聽**（#319）— `IP_FREEBIND` / `IPV6_FREEBIND` socket 選項允許繫結尚未指派的位址。DNS 服務可在 VRRP 備份節點上預先啟動。
- **forcefrag DF 清除移至 prerouting hook**（#328）— forward hook 使用的 `oifname` 在 prerouting 中不可用，改為使用 `fib daddr oifname` 查詢路由表。修復了 MSS clamp 未正確套用的情況。
- **消除 BGP peer watch 的不必要更新**（#329）— `desiredPeerMatches()` 使用 `reflect.DeepEqual` 導致每次 reconcile 都因 `dynamicExportPrefixes` 變化和 GracefulRestart 格式不一致（`"2m"` vs `"120s"`）而觸發 `UpdatePeer`。引入穩定比較函式 `stableDesiredPeerEqual`，在設定語義相同時抑制更新。
- **`routerd serve` 啟動時自動啟用 loopback**（#321）— 在 Live ISO 和容器環境中 `lo` 可能處於 down 狀態時，自動執行 `ip link set lo up`。

### 安裝器改進

- **bootstrap 安裝器可靠清理暫存目錄**（#324）— `exec sh ./install.sh` 導致 EXIT trap 不觸發的問題已修復。
- **安裝器 apply state 警告修復**（#327）— 將 `routerctl get status` 輸出格式改為 `-o json` 以準確判定 `lastApplyTime`。
- **BGP peer state watch 實現 status 即時更新**（#304）— BGP 會話狀態變化即時反映到 status。
- **重啟不活躍的 keepalived 以修復 VRRP**（#299）— 修復某些情況下 VRRP 故障轉移不正常的問題。

### 文件

- **新增日語正本翻譯 37 篇 + 中文翻譯 80 篇**（#322）— 涵蓋所有分類：ADR / explainer / how-to / ops / reference / releases / evidence / slides。日語作為正本，zh-Hans / zh-Hant 作為翻譯。
- **所有文件圖表以 gpt-image-2 重新產生**（#261）— 統一視覺風格。

### 從 v20260528.2308 繼承的事項

v20260528.2308 的所有生產安全特性均已繼承。

- fd 洩漏修復（#39 SQLite ledger、#40 Unix socket / BGP gobgp client）
- heap 洩漏修復（OTel instrument singleton、bounded reverse DNS cache）
- `routerctl doctor runtime` 持續資源監控
- BGP 會話跨 routerd 二進位升級存活
- `doctor dslite` selectedSource 對齊
- 閘道健康度獨立畫面
- `install.sh` 即時失敗（載荷缺失偵測）
- 金鑰脫敏
- `ManagementAccess` 套用守衛
- 機器可讀 `routerctl doctor`（`-o json`）

## 已知觀測（非發布阻塞項）

- **`install.sh` 後 `routerd-bgp` 可能仍以舊 inode 運行。** 這是設計如此。`install.sh` 在升級時不自動重啟 `routerd-bgp`，以便已建立的 BGP 會話和 ECMP 在 routerd 二進位更新後存活。
- **未宣告 `ManagementAccess` 的設定中 `routerctl doctor mgmt` 顯示 SKIP。** 這是運行設定的選擇，非發布缺陷。

:::warning 升級注意
- **從 v20260528.2308 升級時：** ADR 0014 變更了 CLI verb 體系。`routerd apply` → `routerctl apply`、`routerd validate` → `routerctl validate` 等。如果服務單元或指令碼中使用了舊命令，請重寫。`install.sh` 會自動部署新的服務單元，因此 systemd 管理的單元會自動更新。
- **務必先 `cd` 到解壓後的發布目錄再執行 `install.sh`。**
- **從 v20260523.1542 及更早版本升級時：** `disabled:` 欄位（請用 `enabled: false`）和 `--controller-chain*` / `--observe-interval` 旗標已刪除。
- **DNS 解析器服務單元化：** 解析器以 `routerd-dns-resolver@<name>.service` 運行。首次升級時會有短暫的 DNS 中斷。
:::

## 「穩定版」的意義與注意

:::warning API 仍為 v1alpha1
「穩定版里程碑」表示**該版本的品質達到了生產可用的水準**，但**不承諾 API（資源 schema）的向後相容**。
:::

- routerd 的資源 API 目前為 **v1alpha1**。版本間**可能出現破壞性變更**。
- 升級時請勿依賴向後相容。請以**按照新 schema 重寫設定（YAML）**為前提進行。
- 策略上不提供遷移相容層。各版本的變更請查閱[變更日誌](./changelog.md)。

## 安裝與升級

安裝步驟請參閱[安裝與升級](../install-and-upgrade.md)。建議以推薦的里程碑版本為升級起點。
