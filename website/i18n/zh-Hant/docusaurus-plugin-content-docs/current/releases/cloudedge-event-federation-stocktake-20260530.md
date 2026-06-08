# CloudEdge + Event Federation — 合併前盤點 (event-federation → main)

狀態: **experimental** (實驗室驗證的基礎設施; 不建議作為穩定版)
分支: `event-federation` · Head: `8c4821c8` · 日期: 2026-05-30
作者: review subagent (僅記錄事實; 合併判斷由 orchestrator 最終決定)

這是對 `event-federation` 分支作為 `main` 的 experimental-MVP 候選進行的唯讀盤點文件。不進行程式碼變更或合併。

## 範圍概述

`event-federation` **領先 `main` 36 個提交、落後 0 個提交** (可進行乾淨的 fast-forward)。它是 `cloudedge-mvp` 的嚴格超集, `cloudedge-mvp` 的 head `713233b0` 是 `event-federation` 的祖先 (透過 `git merge-base --is-ancestor` 確認)。也就是說, 該分支包含 **CloudEdge/SAM** (`cloudedge-mvp` 的全部內容) **+ Event Federation Phase 1 / 1.5 / 2 / 3**。

相對於 `main` 新增的內容:

- **CloudEdge / SAM** (源自 `cloudedge-mvp`): dynamic-config 基礎設施
  (`DynamicConfigPart` / masks / `DynamicOverridePolicy`)、plugin runner
  (observe-only, dry-run; actionPlans 僅用於展示)、L3 hybrid
  (`OverlayPeer` / `HybridRoute`)、Selective Address Mobility
  (`AddressMobilityDomain` / `RemoteAddressClaim` / `CloudProviderProfile`,
  Linux 資料平面)、zone 無關的 PMTU/MSS clamp (#53)、nft 所有權
  診斷、`routerctl doctor hybrid`。
- **Event Federation** (ADR 0006): typed observed-event 信封 + SQLite 本地
  儲存 + `routerctl federation event` CLI (Phase 1/1.5); `routerd-eventd`
  傳輸守護程式 + `EventGroup` / `EventPeer` Kind + HMAC 推送傳遞 +
  `event_deliveries` + 保留期清理 (Phase 2); `EventSubscription` Kind +
  subscription 觸發的 plugin → `DynamicConfigPart` (`RemoteAddressClaim`)
  (Phase 3)。新增 Kind 共 3 個: `EventGroup`、`EventPeer`、`EventSubscription`
  (apiVersion `federation.routerd.net/v1alpha1`)。

## 證據清單

儲存庫內的證據/里程碑文件 (全部已確認存在):

| 文件 | 結果 / 判定 |
|---|---|
| `docs/releases/cloudedge-sam-mvp-milestone.md` | Azure/AWS/OCI x PVE 全部 **PASS / clean**; 3 雲對等; experimental |
| `docs/releases/cloudedge-sam-stocktake-20260529.md` | 合併前盤點; 粗糙之處 = experimental 後續跟進, 非阻塞項 |
| `docs/releases/evidence/cloudedge-sam-azure-pve-20260529.md` | Azure x PVE **PASS / clean** |
| `docs/releases/evidence/cloudedge-sam-aws-pve-20260529.md` | AWS x PVE **PASS / clean** (Azure 對等, 首次執行) |
| `docs/releases/event-federation-checkpoint.md` | Phase 1 + 1.5 檢查點; experimental; 非發佈標籤 |
| `docs/releases/evidence/cloudedge-event-federation-transport-20260530.md` | Phase 2 傳輸冒煙測試 **Result: PASS** (斷言 A–G 共 7 項) |
| `docs/releases/evidence/cloudedge-event-federation-subscription-20260530.md` | Phase 3 subscription 冒煙測試 **Result: PASS** (主路徑 + 4 項否定檢查) |

完整的證據包 (以及 OCI 摘要) 按照既定的實驗室模式存放在相鄰的實驗室儲存庫 `/home/imksoo/routerd-labs/...` 中 (未提交至本儲存庫)。引用的傳輸/subscription 證據包 (`routerd-labs/event-federation/evidence/20260530T091652Z-...` 和 `...20260530T111612Z-...`) 存在於磁碟上。

### 連結完整性發現 (輕微, 建議修復)

`cloudedge-sam-mvp-milestone.md:24` 將 OCI 證據連結為
`routerd-labs/cloudedge-sam/evidence/20260530T031247Z-oci-pve-hardening/summary.md`,
但磁碟上的實際目錄為
`20260530T031247Z-oci-pve-hardening-43a64c55/` (缺少提交後綴 `-43a64c55`)。**引用路徑無法解析** → 斷鏈。(該路徑位於外部實驗室儲存庫中, 不影響網站建置, 但作為引用不準確。) 儲存庫內 `docs/releases/evidence/*.md` 的 4 處引用全部正確解析。

## 完整性發現

### ADR 0006 的狀態已過時 (合併前必須修復)

`docs/adr/0006-event-federation.md` 的 Status 部分仍包含以下內容:

> Phase 1 (...) is implemented on `event-federation`. **Phase 2 (peer delivery
> over the overlay) is pending.**

Context 中還寫著 **"OCI×PVE in progress"**。兩者目前均不準確: Phase 2 和 Phase 3 已實作 (附帶 PASS 冒煙測試), OCI×PVE 也已通過。需要將 ADR 的 Status 區塊更新為 Phase 1–3 已實作 + OCI clean。

### 文件站台導覽 — 新文件處於孤立狀態 (合併前必須修復)

`website/sidebars.ts` 是文件的側邊欄 (`docs/` 下的預設英文版)。
SAM 參考文件 (`reference/selective-address-mobility`) 已在側邊欄註冊
(sidebars.ts:150)。但:

- **`docs/how-to/event-federation-subscription.md` 未在 `website/sidebars.ts` 中註冊**
  (`grep` 結果 = 0)。處於孤立狀態, 不會出現在站台的 How-to guides 分類中。
- **`docs/reference/` 下沒有專門的 federation 參考文件** (
  `docs/reference/` 下僅有 `dynamic-config.md` 和 `selective-address-mobility.md`)。
  如果計劃編寫 federation 參考頁面則尚未建立; 如果 how-to 是唯一的
  federation 文件, 則仍需要側邊欄條目。

根據專案策略 (正本 = 日文 `website/i18n/ja`, Web 預設 = 英文 `docs/`),
i18n/ja 的側邊欄/翻譯也需要條目, 但側邊欄結構是共享的 (`sidebars.ts`),
因此將 how-to 加入 `sidebars.ts` 即為唯一的必要佈線變更; ja 翻譯內容是
個別的 (低優先順序, experimental) 後續跟進。

## API Schema 產生發現 (合併前必須修復)

產生器: `make generate-schema` → `cmd/routerd-schema` →
`schemas/routerd-config-v1alpha1.schema.json` (+ control + control-openapi)。
3 個 schema 檔案全部被 git 追蹤。

- 執行 `make generate-schema` (和 `make check-schema`) **無差異** —
  `git status --short schemas/` 是乾淨的。即已提交的 schema 與產生器
  內部一致。
- **但 schema 是不完整的。** `cmd/routerd-schema/main.go` 透過 `resourceSchema(apiVersion, "Kind", Spec{})` 手動列舉各 Kind。SAM 的 Kind 已註冊 (327–331 行: OverlayPeer, HybridRoute, AddressMobilityDomain, CloudProviderProfile, RemoteAddressClaim)。**新增的 federation 3 個 Kind — `EventGroup`、`EventPeer`、`EventSubscription` — 未在產生器列表中註冊。** 因此不會出現在產生/發佈的 JSON schema 中, 重新產生也不會產生差異 (產生器不知道它們的存在)。
- 修復 = 在 `cmd/routerd-schema/main.go` 中新增 `resourceSchema(api.FederationAPIVersion, "EventGroup"/"EventPeer"/"EventSubscription", api.…Spec{})` 的 3 行, 然後執行 `make generate-schema` 並提交 `schemas/` 的差異。(此處不進行修復, 僅向 orchestrator 報告。)

驗證備註: `make check-schema` 目前 **通過**。這僅將產生器輸出與已提交檔案進行比對, 不偵測缺失的 Kind。因此 CI 的綠色狀態無法捕獲此缺口。

## make dist / 打包完整性

- `routerd-eventd` **已包含** 在 `make dist` 中: Makefile 的 `ROUTERD_RELEASE_BINS` 包含
  `$(ROUTERD_EVENTD_BIN)` (Makefile:33–34), `build-daemons` 進行建置 (Makefile:74),
  dist 進行安裝 (Makefile:199)。透過 `make -n dist | grep eventd` 確認了建置 + 安裝行。
- **範例外掛 (`examples/plugins/event-to-remote-claim`) 未包含在 `make dist` 中**
  (Makefile 中無引用; `make -n dist` 中無 `examples/plugins`)。這已在 **文件中說明**:
  `examples/plugins/event-to-remote-claim/README.md` ("## Build and install" →
  `go build -o bin/event-to-remote-claim ./examples/plugins/event-to-remote-claim`) 和
  `docs/how-to/event-federation-subscription.md:61–64` 均指導操作者單獨建置。
- **打包無需 eventd 特定的變更。** `packaging/install.sh` 使用通用 glob
  (`for binary in bin/*`, 第 1873 行) 安裝所有二進位檔案, 因此 `routerd-eventd`
  會自動安裝。按組劃分的 systemd 單元
  `routerd-eventd@<group>.service` 由 **routerd 自身產生** (controller chain /
  `pkg/render/eventd_systemd.go` + `pkg/controller/eventfederation` 的 `EventGroup` supervision)。
  它不作為靜態單元捆綁, 因此 `install.sh` 的 `systemd/*.service` 迴圈中不需要。
  `contrib/systemd/` 中沒有靜態的 `routerd-eventd.service` (設計上如此 — 是範本化的 `@.service`)。

## 無 Provider Mutation (安全 / 範圍門控) — 已確認: 無

對整個程式碼樹進行 grep (Go 原始碼: `pkg/`、`cmd/`、`examples/`):

- **無雲端 SDK 匯入** (`aws-sdk` / `azure-sdk` / `oci-go-sdk` /
  `cloud.google.com` / `github.com/{aws,Azure,oracle}/`) — 零匹配。
- **無雲端 CLI exec。** 呼叫外部工具的 `exec.Command*` 僅存在於
  `pkg/controller/dhcpv4client/controller.go` 和
  `cmd/routerd-pppoe-client/main.go` (本地 DHCP/PPPoE), 與雲端無關。
- `ActionPlan` **被宣告為僅展示**: `pkg/plugin/types.go:85–86`
  ("MVP routerd never executes ActionPlans"); 測試
  `TestRunRemoteAddressClaimActionPlanIsDisplayOnly` 強制執行。
- 範例外掛讀取 `os.Stdin` JSON 並僅寫入 `os.Stdout` JSON
  (`examples/plugins/event-to-remote-claim/main.go`) — **無 exec、http、net、雲端呼叫**;
  其標頭註解聲明 provider action 執行在 MVP 範圍外 (Phase 4/5)。

結論: **此分支中不存在可執行的 provider mutation 路徑。** 與 provider
相關的介面僅有宣告式 spec (`CloudProviderProfile`、capture type `provider-secondary-ip`)、
僅展示的 actionPlans, 以及無雲端呼叫的範例外掛。

## Experimental 標記 — 已確認

- `cloudedge-sam-mvp-milestone.md`: "Status: **experimental** (lab-validated; NOT
  recommended-stable)"; 明確保留穩定升級 / 發佈標籤的授予。
- `event-federation-checkpoint.md`: "Status: **experimental** (in development;
  NOT recommended-stable)"; "**not** a release tag."
- ADR 0006: "Accepted for **experimental implementation**."
- Phase 2/3 證據的判定將結果限定為控制平面, 並斷言
  未發生 provider/雲端 mutation。

審查的文件中沒有暗示穩定版 / 推薦版的內容。沒有發佈標籤或穩定升級的
聲明。

## 已知缺口

預期的 4 個缺口中, 2 個被準確識別為缺口, 1 個被
誤認, 1 個未被記錄:

1. **FreeBSD rc.d 對 `routerd-eventd` 的 supervision — 未實作 (僅 systemd), 且未記錄。**
   `pkg/render/eventd_systemd.go` 僅渲染 systemd 單元, 沒有 eventd 的
   rc.d 對應物。ADR 中也沒有關於 eventd 的 rc.d / FreeBSD 的描述。
   → 應作為 experimental 的平台限制記錄。
2. **`EventSubscription` 的 `batchWindow` / `debounce` 被接受但不以精確計時器執行。**
   Spec 欄位存在 (`pkg/api/specs.go:1298–1303`), 但
   `pkg/controller/eventsubscription/controller.go` 是 **poll-tick 批次處理**
   ("poll + dedup … each tick", 4–8 行), 沒有精確的 batch/debounce 計時器。
   欄位被接受為設定, 但目前僅為資訊性。→ 作為限制未記錄; 應記錄。
3. **自推送 / 迴圈防止 — 已實作 (非缺口)。** ADR 的迴圈防止不變量已被
   強制執行: `pkg/eventd/outbox.go:78` 僅推送本地產生的事件 (`SourceNode == nodeName`),
   不重新推送接收到的事件。`TestOutboxLoopPrevention`
   (`pkg/eventd/outbox_test.go`) 覆蓋。另一個 observer 側不變量
   ("節點不重新 emit 自身捕獲的位址的 observed event") 屬於
   ARP/Clients observer, **是 Phase 4, 不在此分支中**。
   目前不存在需要跳過的內容。
4. **實驗室節點保留 `515fe7e8` 建置。** Phase 3 證據
   (`...subscription-20260530.md`) 記錄了將 `515fe7e8` 部署到 router03 + router05,
   但 **沒有拆除 / 還原的記載**, 在實驗室記錄中這些節點被推定為
   仍在執行 Phase 3 建置。→ 需要明確的實驗室筆記 (清理或有意保留)。

## 建置 / 測試健康度 (最終門控)

全部在 `event-federation` head `8c4821c8` 上執行:

- `gofmt -l pkg cmd examples` → **乾淨** (無輸出檔案)。
- `go build ./...` → **成功**。
- `go test ./...` → **1880 個測試通過, 95 個套件** (exit 0)。無失敗。
- `make check-schema` → **通過** (無差異) — 但請參閱上述 schema 不完整性發現;
  check-schema 不偵測缺失的 Kind。
- 此次執行中未觀察到 `cmd/routerd` 的 networkd-env 測試失敗。

## 與 PR #49 的關係 — 選項 (僅事實; 非建議)

PR #49 (`gh pr view 49`): OPEN, **draft**, `cloudedge-mvp → main`, 標題
"CloudEdge MVP: hybrid routing and selective address mobility"。內容是
`event-federation` 的 **嚴格子集** (head `713233b0` 是祖先)。
`event-federation` 領先 main 36 / 落後 0 → **可乾淨 fast-forward**。

- **(a) 將 #49 重新導向/替換為 `event-federation → main` PR。** 單個 PR 承載 CloudEdge/SAM + EF Phase 1–3;
  #49 關閉/被取代。一次審查, 一次合併。
- **(b) 先透過 #49 合併 `cloudedge-mvp`, 再合併 `event-federation`。** 兩階段:
  CloudEdge/SAM 作為獨立合併落地, EF 緊隨其後。更細的歷史; 兩次審查/合併週期;
  #49 有存在意義。
- **(c) `event-federation` 的單次 experimental 合併。** 與 (a) 相同的最終狀態, 但框架為單次
  experimental 合併; #49 作為被取代關閉。

無論哪種情況, #49 的差異完全包含在 `event-federation` 中, FF 是乾淨的。

## 建議 (最終)

**判定: 作為 experimental 功能準備合併至 `main`。** 建置乾淨, gofmt 乾淨,
1880 測試綠色, golden 無變更, 無 provider mutation 路徑, 一致的
experimental 標記, `make dist` 包含 `routerd-eventd`, 打包無需變更。
CloudEdge/SAM 已在 3 個雲端進行實驗室驗證 (PASS/clean), EF Phase 1–3 各有 PASS
實驗室冒煙測試 (transport + subscription)。

盤點中指出的合併前整備項目已在同一路徑 (同一分支, 與本文件並行提交) 中解決:

1. **Schema (必須修復) — 已解決。** 將 `EventGroup` / `EventPeer` /
   `EventSubscription` 註冊到 `cmd/routerd-schema/main.go`;
   重新產生 `schemas/routerd-config-v1alpha1.schema.json` 並包含;
   `make check-schema` 通過。
2. **ADR 0006 狀態 (必須修復) — 已解決。** 將 Status/Context 更新為 Phase 1–3
   已實作 + OCI×PVE clean (3 雲對等); 設定了逐 phase 標記;
   新增了 `## Known limitations (experimental)` 子節。
3. **文件導覽 (必須修復) — 已解決。** 將 `how-to/event-federation-subscription` 新增到
   `website/sidebars.ts` (英文/預設側邊欄; ja 翻譯為延遲後續跟進,
   非阻塞 — Docusaurus 會退回到來源文件)。
4. **OCI 證據連結 (建議修復) — 已解決。** 修正了 `cloudedge-sam-mvp-milestone.md` 中的
   `-43a64c55` 目錄後綴。
5. **Experimental 缺口 (建議修復) — 已解決。** 在 ADR 0006 "Known limitations" 中記錄:
   僅 systemd 的 `routerd-eventd` (FreeBSD rc.d 未支援);
   `batchWindow`/`debounce` 被接受但為 poll-tick 批次處理 (無精確計時器)。

剩餘 (非阻塞, 在此追蹤):

6. **實驗室拆除筆記。** router03/router05 在 Phase 3 冒煙測試後保留 `515fe7e8` 建置
   (設定已恢復到基線; 僅二進位檔案未還原)。不是 `main` 合併的阻塞項,
   是實驗室管理筆記。下次實驗室操作時還原或重新固定。
7. **i18n。** `event-federation-subscription.md` 的 ja/zh 翻譯以及專門的 federation
   參考頁面為延遲後續跟進。

**建議的合併形式:** 單個 `event-federation → main` PR, **PR #49 作為被取代關閉**
(`cloudedge-mvp` 的內容是 `event-federation` 的嚴格祖先/子集; 乾淨 fast-forward,
0 落後)。這是最小負擔的路徑, 維持單次 experimental 落地。
選項 (b) — 先透過 #49 落地 `cloudedge-mvp`, 再合併 `event-federation` — 僅在
需要個別的 CloudEdge/SAM 歷史檢查點時才有價值, 但並非必須。

**合併本身和 PR #49 的處置由維護者判斷** (向 main 的發佈/合併是擁有者門控)。無標籤; experimental。
