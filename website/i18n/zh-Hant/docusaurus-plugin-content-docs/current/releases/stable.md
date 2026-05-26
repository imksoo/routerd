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
| 版本 | **v20260526.1607** |
| 定位 | 推薦穩定版（取代 v20260525.1631） |
| 運行實績 | 已在正式環境路由器（homert02）驗證：routerd 重新啟動與 install 期間 DNS 不中斷（NG 0），`/api/v1/config` 回傳的原始 secrets 偵測數為 0，`gatewayHealth` 彙總 26 components 且 overall=ok，`routerctl doctor` rc=0（pass=32 warn=4 fail=0 skip=1），install 跨過程中 BGP 2/2 與 2-way ECMP 維持 |
| 二進位檔 | 靜態連結（`CGO_ENABLED=0`），通過 CI 與 Release workflow |

## 推薦 v20260526.1607 的理由

推薦理由是**維運成熟度，不是新功能數量**。
v20260526.1607 承襲前一推薦版的正式環境安全 DNS 與 BGP 升級行為，
並新增 4 項已在正式環境路由器（homert02）驗證過的維運契約：

- **Web Console 不再洩漏 secrets。** `/api/v1/config` 與 generation 的
  config / diff 端點會在序列化前對 WireGuard `privateKey` /
  `preSharedKey`、Tailscale `authKey`、BGP/PPPoE/IPsec `password`、
  WebConsole `initialPassword`、bearer/token 欄位等進行 redact。鍵保留並
  以標記值取代，UI 結構不受影響。homert02 實流量驗證：
  **原始 secrets 偵測 0**。
- **`gatewayHealth` 彙總整條出口路徑。** `/api/v1/summary` 現在統一彙總
  DNSResolver / DSLiteTunnel / DHCPv6PrefixDelegation / EgressRoutePolicy /
  NAT44Rule / HealthCheck。Web Console 橫幅顯示 selected 與 preferred
  egress path 的對應關係，啟用 fallback 候選時會明顯警告。homert02 驗證：
  **overall=ok / 26 components**。
- **`routerctl doctor` 成為機器可讀的穩定契約。** `-o json` 輸出作為
  v1alpha1 維運契約（area、status 列舉、summary、結束碼）已文件化；
  fail 時以非 0 結束，便於腳本使用。homert02 驗證：
  **rc=0（pass=32 warn=4 fail=0 skip=1）**。
- **`ManagementAccess` 宣告式 apply 保護。** apply 前的 preflight 在管理
  介面缺失、firewall 會阻擋 SSH、WebConsole 繫結到所有位址時**中止非
  dry-run apply**（可用 `--allow-mgmt-lockout` 覆寫）。相同檢查也由
  `routerctl doctor mgmt` 公開。

**承襲事項（來自 v20260525.1631 等）：** DNS 解析器作為獨立的長壽命服務
單元運行，routerd 重新啟動或升級期間 DNS 不中斷（homert02 驗證：
`routerd.service` 重新啟動與 install 中 DNS probe NG 均為 0）。
`install.sh` 在二進位升級時不會自動重新啟動 `routerd-bgp`，eBGP 工作階段
與 ECMP 可跨 routerd binary 更新保持（homert02 驗證：2/2 Established、
2-way ECMP、HTTP 200 跨 install 維持）。完整 BGP 控制平面（不使用 FRR；
#26 next-hop 改寫、#28 OpenRC live ISO 啟動）。`routerctl ledger` 維護
（`integrity-check` / `vacuum` / `backup` / `prune-events`，非 dry-run
prune 會發出稽核事件）。

## 已知觀察（不阻擋發布）

- **DS-Lite doctor 可能在 egress 健康時仍出現 WARN。** 當 AFTR 的 AAAA
  探測或 tunnel device 觀測偶發雜訊時，doctor 的 `dslite` area 可能回報
  WARN，即便 `gatewayHealth=ok` 且實際 egress（HTTP 200）正常。這屬於
  保守型診斷雜訊，並非 dataplane 故障。後續調整將讓 DS-Lite doctor
  severity 與 `gatewayHealth` 的 selected-path 證據對齊。
- **`install.sh` 後 `routerd-bgp` 可能以舊 executable inode 繼續運行。**
  這是預期行為：`install.sh` 在升級時不會自動重新啟動 `routerd-bgp`，
  從而保留已建立的 BGP 工作階段與 ECMP 跨 routerd binary 更新。直到維運
  人員在 Graceful Restart 時機執行 `systemctl restart routerd-bgp` 之前，
  程序將持續持有舊 inode。
- **未宣告 `ManagementAccess` 時 `routerctl doctor mgmt` 會 SKIP。**
  這是 live config 的選擇，並非發布缺陷——該保護為 opt-in。要啟用 apply
  鎖出保護與 doctor mgmt 的判定，請宣告 `ManagementAccess` 資源
  （參見 [`examples/home-router-mgmt-protected.yaml`](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)）。

:::warning 升級注意事項
- **從 v20260523.1542 或更早版本升級：** 已移除 `disabled:` 欄位（請改用 `enabled: false`）以及無作用的 `--controller-chain*` / `--observe-interval` 旗標。請在升級前重寫相關設定與主機 service unit。
- **DNS 解析器服務單元化：** 解析器現在作為 `routerd-dns-resolver@<name>.service` 運行。首次升級到此模式時會進行一次「子程序 → 單元」切換，期間有一次短暫的 DNS 中斷；此後 routerd 的重新啟動與升級不再中斷 DNS。
:::

## 「穩定版」的定義與注意事項

:::warning API 仍為 v1alpha1
「穩定版里程碑」代表**此版本具備正式環境所需的品質**，並**不保證 API（資源 schema）的向下相容性**。
:::

- routerd 的資源 API 目前為 **v1alpha1**。**版本之間可能包含破壞性變更。**
- 升級時，請勿依賴向下相容性，應以**配合新 schema 重新撰寫設定（YAML）**為前提進行。
- 本專案不提供遷移相容層。各版本的變更內容請參閱[變更記錄（Changelog）](./changelog.md)。

## 安裝與升級

安裝程序請參閱[安裝與升級](../install-and-upgrade.md)。建議以推薦里程碑版本為起點進行升級。
