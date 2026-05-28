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
| 版本 | **v20260528.1805** |
| 定位 | 推薦穩定版（取代 v20260528.0402；新增收尾 fd 洩漏工作的 heap 洩漏修正，並經 2 小時正式環境 soak 驗證） |
| 運行實績 | 已在正式環境路由器（homert02）驗證 fd 與 heap 皆 bounded。fd 在 2 小時 / 24 個取樣的 soak 中完全 flat（all_fd=24、sockets=16、SQLite ledger 系=4 每次取樣都不變，NRestarts=0，PID 不變）。`RssAnon`（不含 page cache 的真實 heap）在第 1 小時從 ~70 MB warm-up 到 ~104 MB 穩態區間後，第 2 小時在 96–107 MB 之間振盪並 plateau，伴隨明顯的 GC 回收 dip——這是 bounded working set 的特徵，而非洩漏。BGP 維持 2/2 Established，`routerctl doctor dslite` 回傳 pass=12 / warn=0，`routerctl doctor reconcile` 回傳 pass=1 / warn=0。在 v20260528 系列中，3 個 fd 洩漏根因（#39 SQLite ledger、#40 control/status socket 的 keep-alive、#40 BGP gobgp client）與 2 個 heap 成長源（每請求的 OTel instrument 重建、無上限的 reverse DNS cache）已分別釐清並修正 |
| 二進位檔 | 靜態連結（`CGO_ENABLED=0`），通過 CI 與 Release workflow |

## 推薦 v20260528.1805 的理由

推薦理由是**維運成熟度，不是新功能數量**。v20260528.1805 完整承襲
v20260528.0402 的正式環境安全特性（fd 洩漏修正 #39 / #40、#36 / #37 /
#38 可觀測性契約、BGP 的 idempotent reconcile、doctor dslite 的
selectedSource 對齊、Gateway Health 獨立畫面、`install.sh` 立即失敗、
secrets 伏字化、`ManagementAccess` apply 保護、可機讀的
`routerctl doctor`、推薦穩定版顯示的一致性 CI 守護），並新增收尾這次
長期資源洩漏調查的 heap 洩漏修正：

- **`/api/v1/summary` 輪詢不再使 heap 無限成長。**
  `recordConsoleMetrics` 先前每次請求都重建 7 個 OpenTelemetry gauge，
  現在透過 `sync.Once` 單例 (`getConsoleMetrics`) 只建構一次。
  `reverseDNSCache` 先前僅用 TTL 決定是否重新查詢，既不清理過期項目也
  沒有大小上限，使得 firewall log / connection 表 / traffic flow 中
  每個不同的遠端位址都成為永久條目。現在會清理過期項目，並在呼叫
  入口與出口兩處都強制 4096 條硬上限。homert02 的 2 小時 soak 確認
  `RssAnon` 進入 plateau 而非單調成長。這些作為 v20260528.0402 fd
  洩漏工作對應的 heap 側修正，收尾整個調查。

自 v20260528.0402 承襲並在 homert02 v20260528.1805 上重新驗證的 2 項
影響正式環境的 fd 洩漏修正與 3 項可觀測性契約：

- **routerd serve 不再洩漏 SQLite ledger fd。** `resource.LoadLedger`
  先前每次呼叫都會針對 `/var/lib/routerd/routerd.db` 開啟新的
  `*sql.DB`，而 `Ledger` 沒有 `Close()`。
  `IPv4PolicyRouteController.cleanupLedgerOwnedPolicyRoutes` 的
  reconcile 路徑約每 30 秒執行一次，每個週期都會新增一組
  `routerd.db` / `routerd.db-wal` fd — homert02 v20260526.2335
  上 SQLite fd 已累積到約 300。修正為 `Ledger` 介面加入
  `Close()`，在所有 `LoadLedger` 呼叫位置 `defer` 它，並為
  `OpenSQLiteLedger` 設 `SetMaxOpenConns(1)` / `SetMaxIdleConns(1)`
  作為兜底。兩個 Linux 限定的回歸測試驗證 10 次 open / close 迴圈
  後 `/proc/self/fd` 不會成長。驗證結果：homert02 上 SQLite ledger
  系 fd 由約 300 降為 flat 4（#39）。

- **routerd serve 也不再洩漏 Unix socket fd。** 修正了兩個互相獨立
  的問題：(a) 在控制 / 狀態 `http.Server` 上呼叫
  `SetKeepAlivesEnabled(false)`，並讓 `controlapi.NewUnixClient` 設
  `Transport.DisableKeepAlives: true` — 之前 polling 用戶端在
  `IdleTimeout` 內持續重複使用 keep-alive 連線，導致伺服器端
  accept 的連線始終未關閉。(b) BGP 控制器的 gobgp HTTP 用戶端
  (`pkg/controller/bgp/gobgp_client.go`) 每次 ~30 秒 reconcile 都會
  對 `/run/routerd/bgp/control.sock` 進行 2 次 dial，是唯一未採用
  `DisableKeepAlives` / `req.Close` / `defer CloseIdleConnections()`
  模式的內部 HTTP 用戶端，正是 +4 fd / 分鐘 殘餘漂移的真正原因。
  驗證結果：homert02 v20260528.0402 在 16 分鐘 4 個 5 分取樣中
  `all_fd=24` 與 `sockets=16` 完全 flat，Unix-stream ESTAB 由 71
  降至 9（#40）。

- **HealthCheck 探測現在記錄 egress / source / route 佐證，並依
  resource 維護一份滾動失敗歷史。** 每個結果都攜帶 `FailureKind`
  （timeout / connection_refused / network_unreachable /
  host_unreachable / no_route / dns_error / tls_error / ...）、
  `EgressInterface`、`SourceAddress`、`SourceOrigin`（pd / ra /
  static / dynamic）、`NextHop`、`OutInterface`、`RouteSource`、
  `TunnelLocal`、`TunnelRemote`。`State` 暴露 `FirstFailureTime` /
  `LastFailureTime` / `LastSuccessTime` / `FailureCount` 與可設定
  的 20 筆 `History []ProbeRecord`。`cmd/routerd-healthcheck` 新增
  `--source-origin` / `--tunnel-local` / `--tunnel-remote` 維運提示
  flag。事件屬性與既有的 `StatusMap` 也已納入新欄位，因此
  `routerctl show / describe` 可自動呈現（#37）。

- **每個 controller 的 reconcile 失敗歷史透過 control API 公開。**
  `ControllerStatus` 新增 `ReconcileErrorHistory []ReconcileErrorEntry`
  與 `MaxDurationAt *time.Time`。每筆記錄含 `StartedAt` /
  `CompletedAt` / `Duration` / `DurationMs` / `Trigger` /
  `ResourceKind` / `ResourceName` / `Error`。controller framework
  新增可選 `ResourceObserver` 介面，讓 runtime store 在每次
  reconcile 中接收 resource kind / name（既有 Observer 實作完全
  相容）。`routerctl status --show-errors` 在 table 模式下於每個
  controller 列下方以縱向區塊呈現歷史；JSON / YAML 透過既有
  StatusMap 路徑自動包含新欄位。新增 `routerctl doctor reconcile
  --since <duration>` 會查詢 status socket，並依 pass / warn (≥1) /
  fail (≥10) 判定，detail 中給出最多 5 筆樣本。homert02
  v20260528.0402 上 `doctor reconcile` 回傳 `pass=1 warn=0`，機制
  已在正式環境運行（#38）。

- **dns-queries / traffic-flows 新增絕對時間範圍、過濾與彙整。**
  `--from` / `--to` 接受 `RFC3339`、`2006-01-02T15:04:05`（省略
  時區視為 UTC）、`2006-01-02 15:04:05`。新增過濾項：DNS 的
  `--rcode` / `--upstream` / `--qname-suffix` / `--duration-min`、
  flows 的 `--peer-suffix` / `--protocol` / `--asymmetric`。新增
  `--agg` / `--stats` 模式輸出 `SUMMARY`，DNS 列出 `BY RESPONSE
  CODE` / `BY CLIENT` / `BY UPSTREAM` / `BY QNAME SUFFIX`，flows
  列出 `BY CLIENT` / `BY PEER` / `BY PROTOCOL`，並附 duration 的
  p50 / p95 / p99。直接讀取 DB 時支援 `--chunk-size` 分塊，每個
  chunk 擁有自己的 ctx 截止時間。`DeadlineExceeded` 時錯誤訊息包含
  已取得列數。`--limit` 預設值由 100 提升到 500，`--timeout` 由
  5 秒提升到 30 秒，內部 `DNSQueryFilter` / `TrafficFlowFilter`
  的上限由 1000 提升到 10000。Web Console 新增
  `/api/v1/dns-queries/aggregate` 與
  `/api/v1/traffic-flows/aggregate` 端點（#36）。

doctor 失敗 detail、子指令 --help、推薦穩定版的顯示一致性，均自
v20260526.2335 承襲並在 homert02 v20260528.0402 上重新驗證：

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
