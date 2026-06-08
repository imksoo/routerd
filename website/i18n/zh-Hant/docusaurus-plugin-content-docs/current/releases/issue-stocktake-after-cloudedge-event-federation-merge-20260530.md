# Issue 盤點 — CloudEdge SAM + Event Federation 合併後 (2026-05-30)

狀態: 唯讀盤點。未進行 issue 的關閉、留言、標記或建立。
以下所有操作均為供 orchestrator/使用者套用的 **建議**。

## 概述

PR #54 (`event-federation → main`) **已合併** (合併提交 `baeaff16`)。作為 `cloudedge-mvp` 的嚴格超集, 將 CloudEdge/SAM + Event Federation Phase 1 / 1.5 / 2 / 3 以 **experimental** 身分落地 — 無發佈標籤, 不建議作為穩定版。PR #49 (`cloudedge-mvp → main`) 已作為被 #54 **取代而關閉**。

4 個 issue 保持開放 (#50、#51、#52、#53), 全部屬於 CloudEdge SAM, 且都帶有現已過時的 `branch cloudedge-mvp` 標籤。其中:

- **#53 和 #50 事實上已解決** — 由已合併的 zone 無關 PMTU/MSS clamp (提交 `3c540656`) 完成。兩個 codex OCI x PVE 重測留言確認 PASS (routerd_mss 存在, MSS 1300, doctor hybrid PASS, ping/SSH/scp 全部通過)。可以關閉。
- **#52 部分已解決** — 已合併的 `doctor hybrid` 可 *偵測/警告* reject-all FORWARD/INPUT 主機防火牆, 但文件成果物 (將 OCI 映像防火牆引導記入 SAM how-to) 是剩餘的開放部分。作為文件後續跟進保持開放, 或如判斷 doctor 警告已足夠則附註關閉。
- **#51 (wizard OCI provider) 不受合併影響** — wizard 是實驗室原型, 非核心; OCI provider 產生未新增。保持開放; Phase 4.1 的自然候選。

開放或已關閉的 issue 均不阻塞 Phase 4.0 最小權限 plugin 上下文框架 (Plugin 上下文 allowlist + secret 脫敏)。該工作是全新領域, 應作為 **新** issue 提交。

## 已合併基線

- main = `baeaff16` (PR #54 合併提交)。
- PR #54 合併 2026-05-30T12:20 — "Experimental: CloudEdge SAM + Event Federation Phase 1–3"。
- PR #49 關閉 (被 #54 取代)。
- Experimental: **無發佈標籤**, 未升級為穩定版。
- 相關已合併提交:
  - `3c540656` — SAM 轉發路徑的 zone 無關 PMTU/MSS clamp (#53) + doctor hybrid PMTU/防火牆檢查 (#52)。影響 `pkg/render/mtu.go`、`cmd/routerctl/doctor.go`、golden SAM fixture、`docs/adr/0006-event-federation.md`。
  - `713233b0` — OCI x PVE SAM 乾淨冒煙測試 + 3 雲對等記錄。
  - Event Federation Phase 1→3 區塊 (`9c785db8` … `515fe7e8`), Phase 2/3 冒煙測試證據
    (`docs/releases/evidence/cloudedge-event-federation-{transport,subscription}-20260530.md`)。
- 已關閉的上下文 issue: #41–#48 (以及更早的 #2–#40) 在 SAM/先前工作期間關閉。特別是
  #12 ("MSS clamp can raise lower MSS / ignores source iface MTU") 和 #9 ("routerd_mss reported
  as orphan") 是 #50/#53 背後的歷史 MSS 譜系; #42 ("forwarded /32 dropped by
  FORWARD policy — doctor visualize") 和 #48 ("doctor hybrid classify FORWARD skip reasons") 是
  #52 doctor 工作的已關閉前身。

## Issue 分類表

| # | 標題 | 目前狀態 | #54 合併的影響 | 建議操作 | 可關閉? |
|---|---------|-----------------|-----------------|---------------|--------------|
| 53 | SAM OCI: TCP/SSH stalls after ping without MSS handling | OPEN, 無標籤; codex 重測留言 = PASS | 由 `3c540656` **解決** (zone 無關 clamp → MSS 1300; OCI 重測 PASS) | **關閉** (done-by-main-merge) | **是** |
| 50 | SAM: surface/derive PMTU/MSS for wg-hybrid delivery paths | OPEN, `enhancement` + `branch cloudedge-mvp`; codex 重測 = PASS | 由 `3c540656` **解決** (clamp 匯出 + `doctor hybrid` PMTU/MSS 警告); OCI 重測 PASS | **關閉** (done-by-main-merge); 關閉前/時變更標籤 | **是** |
| 52 | SAM OCI: Ubuntu image iptables rejects WireGuard/FORWARD | OPEN, `documentation` + `branch cloudedge-mvp`; codex 重測 = doctor 警告 + 實驗室引導 | **部分**: doctor 警告 (`3c540656`); **文件 how-to** 部分仍開放 | **保持** (文件後續跟進) 或附註關閉; 變更標籤 | 否 (文件部分) |
| 51 | cloudedge-sam wizard: add OCI provider support | OPEN, `enhancement` + `branch cloudedge-mvp` | **無影響**: wizard 是實驗室原型, 無核心變更; OCI provider 產生未新增 | **保持** (仍然相關 / Phase 4.1 候選); 變更標籤 | 否 |

分類對應:
- **done-by-main-merge**: #53、#50
- **docs-i18n / 文件後續跟進**: #52 (剩餘文件部分)
- **still-relevant / phase4.1-follow-up**: #51, 以及 #52 的 doctor-FORWARD-pattern 擴展
- **phase4.0-blocker**: 無
- **superseded-by-#54**: PR #49 (已關閉); issue 無
- **obsolete-duplicate**: 無

## 建議關閉 (草案留言)

### #53 — 作為 done-by-main-merge 關閉
> 由 `3c540656` 解決 (PR #54 → main `baeaff16` 合併)。PMTU/MSS clamp 被 FirewallZone 門控; SAM 作為無 zone 的轉發平面因此未匯出 clamp, 在 OCI 的低 PMTU underlay 下 ICMP 通過但 TCP 黑洞。修復使 FirewallZone 無關、介面類型無關的 MSS clamp 針對 RemoteAddressClaim 傳遞路徑使用有效 overlay MTU (約 1392 inner → MSS 1300) 匯出。OCI x PVE 重測 PASS: `routerd_mss` 兩側存在 (MSS 1300), `doctor hybrid` PASS, 雙向 ping/SSH (來源位址保留) 和 100MiB scp x3 全部通過; 3 雲乾淨對等。關閉。(Experimental — 無發佈標籤。)

### #50 — 作為 done-by-main-merge 關閉
> 由 `3c540656` 解決 (PR #54 → main `baeaff16`)。SAM 傳遞路徑現在匯出有範圍的 TCP MSS clamp, `doctor hybrid` 顯示 PMTU/MSS 態勢 (SAM 傳遞路徑無 clamp 時警告) — 正是此處要求的 2 個行為。先前 `routerd_mss` 不存在的 OCI 重測輸出 MSS 1300, 雙向 ping/SSH/scp 通過。關閉。(標籤變更備註: `branch cloudedge-mvp` 已過時 — 分支已合併, PR #49 已關閉。)

### #52 — 作為文件後續跟進保持開放 (或附註關閉)
> 由 `3c540656` (PR #54 → main `baeaff16`) 部分處理: `doctor hybrid` 現在偵測並警告阻塞 wg/overlay 轉發的 reject-all FORWARD/INPUT 主機防火牆, 不自動修改, 顯示所需的主機設定。重測確認: 引導前 doctor 警告, 套用範圍限定的實驗室 allow 規則後 PASS。**仍開放**: 將 OCI Ubuntu 映像防火牆引導前提條件 (UDP/51820 INPUT, FORWARD `<vnic> <-> wg-hybrid`) 記入 CloudEdge SAM how-to。作為文件任務保持開放; 標籤變更為僅 `documentation`。

### #51 — 保持開放 (Phase 4.1 候選)
> PR #54 未處理 — cloudedge-sam wizard 是實驗室原型, 合併中未向核心新增 OCI provider 產生。保持開放。自然歸入 Phase 4.1 provider actionPlan plugin 工作 (aws/azure/oci provider profile 產生)。標籤變更: 移除 `branch cloudedge-mvp`。

## 建議標籤變更 — `branch cloudedge-mvp` 已過時

4 個開放 issue (#50、#51、#52、#53) 全部帶有 `branch cloudedge-mvp`。該分支已透過 PR #54 合併至 main, 對應的 PR #49 已關閉, 因此標籤不再指向實際存在的分支。

建議 (此處 **不** 套用):
- #50、#53: 關閉時移除 `branch cloudedge-mvp`。
- #51: 移除 `branch cloudedge-mvp` → 保留 `enhancement` (未來如引入 Phase 4.1 / cloudedge 追蹤標籤則新增)。
- #52: 移除 `branch cloudedge-mvp`, 保留 `documentation`。

建議引入穩定的 `cloudedge` 或 `event-federation` 標籤以替代分支範圍的標籤用於未來追蹤。

## 建議的新/後續跟進 issue (草案 — 未建立)

1. **i18n: Event Federation how-to + 參考的 ja/zh 翻譯**
   按照文件區域設定策略, 將 event-federation-subscription how-to 和 federation 參考頁面翻譯為 ja (正本) 和 zh-Hans/zh-Hant。Phase 3 合併後目前僅有英文版。(新 issue; 無現有匹配。)

2. **FreeBSD rc.d 的 `routerd-eventd` supervision**
   Phase 2 透過 controller/systemd 新增了 EventGroup 自動 supervision (`1791cd5a`)。為 FreeBSD 路由器 (router04 對等) 新增 FreeBSD rc.d 對應物使 `routerd-eventd` 被 supervised。(新 issue; 無現有匹配。)

3. **EventSubscription batchWindow / debounce 精確計時器**
   Phase 3 的 EventSubscriptionController 為 poll + dedup; 新增精確的 debounce/batchWindow 計時器使突發事件在 plugin 呼叫前確定性合併 (與 ADR 0006 的遲滯 / 防抖不變量相關)。(新 issue。)

4. **Observer 自捕獲不變量 (Phase 4 迴圈防止)**
   強制 ADR 0006 的不變量: 路由器不重新 emit 自身捕獲位址的事件。在 provider plugin 開始修改雲端狀態之前, 在 observe→federate 路徑上新增迴歸測試/守衛。(新 issue; Phase 4 前提條件。)

5. **實驗室清理: router03 / router05 保留 `515fe7e8` 二進位檔案**
   Phase 3 實驗室冒煙測試的二進位檔案仍部署在 router03/router05 上。重新部署為已合併 main 的工件 (或推薦穩定建置), 追蹤以確保實驗室路由器不滯留在 experimental 提交上。(新 issue; 實驗室清理。)

6. **Phase 4.1: Provider actionPlan plugin (aws/azure/oci) — dry-run**
   實作將 RemoteAddressClaim 轉換為 provider API 呼叫 (AWS/Azure/OCI 輔助 IP 分配) 的 provider actionPlan plugin, 從 dry-run/observe-only 開始。**包含 #51** (wizard OCI provider 產生) — 不重複提交, 將 #51 作為 OCI 切片連結。(新 issue; Phase 4.1。)

7. **Phase 4.0: Plugin 上下文 allowlist + secret 脫敏**
   最小權限 plugin 上下文框架。脫敏策略 A: 脫敏內聯 secret, 省略 secret 檔案路徑, 省略 `SecretValueSourceSpec`, 不暴露完整 `router.yaml`, 不暴露 provider 憑據, 上下文層不進行 provider mutation。這是所有 provider mutation plugin 的 **Phase 4.0 阻塞項**。(新 issue; 無現有匹配 — 全新領域。)

## Phase 4.0 阻塞項 (顯式)

**4 個開放 issue (#50–#53) 均不阻塞 Phase 4.0** (最小權限 plugin 上下文 allowlist + secret 脫敏; 防止意外 provider mutation / 憑據洩漏)。對已關閉 issue (#2–#48) 的掃描也 **未發現** 關於 plugin 上下文、secret 洩漏、憑據脫敏的 issue。Phase 4.0 框架是全新工作, 應在 provider actionPlan plugin (Phase 4.1) 被允許執行 mutation 之前以上述草案 #7 提交。

## Phase 4.1 候選

- **#51** — wizard OCI provider 支援 → 輸入 provider profile 產生; 包含在 Phase 4.1 provider actionPlan plugin issue (草案 #6) 中。
- **#50 / #52 / #53** — 已解決, 但其 PMTU/防火牆 *provider 上下文* 知識 (有效 overlay MTU, 主機 FORWARD 態勢) 為 Phase 4.1 provider plugin 需要透過 Phase 4.0 上下文 allowlist 呈現的資料提供了啟示。無需重新開啟; 作為設計輸入引用。

## 無穩定升級 / 無發佈標籤

CloudEdge SAM + Event Federation 工作透過 PR #54 **僅以 experimental** 身分落地到 main。**無發佈標籤**, 未 **升級** 為穩定版。發佈標籤由使用者判斷, 不在此盤點範圍內。
