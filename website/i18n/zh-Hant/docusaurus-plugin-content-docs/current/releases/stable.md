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
| 版本 | **v20260526.2335** |
| 定位 | 推薦穩定版（取代 v20260526.2241；docs / CI 一致性 follow-up，無執行時行為變更） |
| 運行實績 | 已在正式環境路由器（homert02）通過 **三次連續的 in-place 升級**（1607 → 2152 → 2241 → 2335）驗證：每次 routerd 重新啟動時 `routerd-bgp` 均未被觸動（MainPID 2394269 跨四次升級保持不變），BGP 始終維持 2/2 Established，uptime 跨越每次重啟持續增長（1h19m → 1h27m → 2h0m → 2h15m → 3h7m → 3h10m），2-way ECMP（.38/.53）保持在 kernel 中，`routerctl doctor dslite` 結果為 pass=12 warn=0，Web Console Gateway Health 畫面 180s / 90 samples 觀察為 good=90 / bad=0，`install.sh` 以正規 cd-into-package-dir 模式 rc=0 |
| 二進位檔 | 靜態連結（`CGO_ENABLED=0`），通過 CI 與 Release workflow |

## 推薦 v20260526.2335 的理由

推薦理由是**維運成熟度，不是新功能數量**。v20260526.2335 完整承襲
v20260526.2241 的正式環境安全特性（而 v20260526.2241 又承襲自
v20260526.1607 的 Web Console secrets redaction、`gatewayHealth` 彙總、
可機讀 `routerctl doctor`、`ManagementAccess` apply 保護），並新增一項
docs / CI 加固：

- **推薦穩定版的顯示不會再靜默漂移。** 新增的 CI 守護指令稿
  (`scripts/check-active-stable.sh`) 以 `website/src/pages/index.tsx`
  的 `STABLE_VERSION` 為 source of truth，當 homepage hero、各 locale
  的 intro tip、announcement bar、`docusaurus.config.ts` 指向不同的
  `vYYYYMMDD.HHmm` 時於 CI 中 fail。這是為了防止 v20260526.2241
  promote 時出現的 5 處遺留為 `v20260526.1607` 類問題在未來 promote
  中再次出現。

由 v20260526.2241 承襲、並在 2335 的 homert02 apply 中再次驗證的
5 項維運契約：

- **routerd 二進位升級不再讓 BGP 工作階段中斷。** BGP 控制器在 reconcile
  入口會先 hydrate 已套用的策略狀態，因此 routerd 重新啟動不會再次 PUT
  內容未變的 import-policy 指派並重置 BGP 工作階段。在 homert02 上透過
  **兩次連續的 routerd 重新啟動** 驗證（PID 3368318 → 3407972 → 3428160）：
  BGP 始終維持 2/2 Established，uptime 跨越每次重啟持續增長而非重置，
  2-way ECMP（.38/.53）保持在 kernel 中無需重新安裝。
- **`routerctl doctor dslite` 與實體一致。** Doctor 現在會把 DSLiteTunnel
  `phase=Up` 視為健康，並透過 `status.selectedSource = "DSLiteTunnel/<name>"`
  辨識 EgressRoutePolicy 的選擇（同時保留舊有 `selectedCandidate` 名稱比對）。
  使用 `dslite-pd-balanced` 等彙總候選名稱的正式環境設定不再讓
  `gatewayHealth=ok` 的 DSLiteTunnel 顯示為 WARN。驗證結果：warn=4 →
  pass=12 warn=0。
- **Gateway Health UI 擁有獨立畫面且渲染穩定。** Web Console 將 Gateway
  Health 從 Overview 拆分到獨立畫面（與 Connections / Clients 一致），
  並顯示完整的 `selectedPath` / `preferredPath` / `fallbackReason` /
  `failedProbes` / `lastTransition` 佐證。Overview 僅保留彙整卡片。
  partial refresh 期間瞬時顯示 `Components 0 / Unknown` 的 flap 問題已
  修正：`reconcileSummary` 在新 snapshot 的 components 為空但舊的有資料
  時保留舊 `gatewayHealth`。驗證結果：**180s / 90 samples 中 good=90 /
  bad=0，確認 26 components**。
- **`install.sh` 不再 silent no-op。** 先前從 release tree 之外執行
  （例如 `cd /tmp/release && ./pkg/install.sh ...`）會讓 cwd 相對的
  `bin/*` 萬用字元一次也不展開，僅 `--with-ndpi-archive` 的 payload 會被
  裝上，指令稿卻仍以 `routerd upgrade completed` 退出 0。現在若 cwd 不
  含 `bin/routerd` payload，會以明確診斷訊息 `exit 2` 立即終止。新增的
  CI 回歸 smoke (`scripts/install-sh-cwd-smoke.sh`) 涵蓋缺漏 payload 與
  正規 cwd 兩種情境。homert02 驗證：cwd-mismatch antipattern **rc=2
  立即 fail**，正規 cd-into-package-dir 模式回傳 rc=0。

**承襲事項（來自 v20260526.1607 等）：** Web Console 的 `/api/v1/config`
與 generation 端點會在序列化前對 WireGuard `privateKey` / `preSharedKey`、
Tailscale `authKey`、BGP/PPPoE/IPsec `password`、WebConsole
`initialPassword`、bearer/token 欄位進行 redact。`/api/v1/summary` 彙總
DNSResolver / DSLiteTunnel / DHCPv6PrefixDelegation / EgressRoutePolicy /
NAT44Rule / HealthCheck 為 `gatewayHealth`。`routerctl doctor` 為 v1alpha1
的可機讀契約（`-o json`、文件化的 area / status 列舉 / summary，fail 時
以非 0 結束）。`ManagementAccess` apply preflight 在
`--allow-mgmt-lockout` 之外阻擋 lockout。DNS 解析器作為獨立長壽命服務
單元運行，routerd 重新啟動或升級期間 DNS 不中斷。`install.sh` 在二進位
升級時不會自動重新啟動 `routerd-bgp`，eBGP 工作階段與 ECMP 可跨 routerd
binary 更新保留。`routerctl ledger` 維護（`integrity-check` / `vacuum` /
`backup` / `prune-events`，非 dry-run prune 會發出稽核事件）。

## 已知觀察（不阻擋發布）

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
- **執行 `install.sh` 前請先 `cd` 進入解壓後的 release 目錄。** 從其他目錄執行（例如 `cd /tmp && sudo ./routerd-release-vYYYYMMDD.HHmm/install.sh ...`）會以 `exit 2` 立即終止。這是有意的設計——先前同樣的呼叫會 silent no-op，僅安裝 `--with-ndpi-archive` 的 payload。
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
