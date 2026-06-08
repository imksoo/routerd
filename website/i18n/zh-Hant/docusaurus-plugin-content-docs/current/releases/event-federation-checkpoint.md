# CloudEdge Event Federation — 檢查點 (Phase 1 + 1.5 完成)

狀態: **experimental** (開發中; 不建議作為穩定版)
分支: `event-federation` · 檢查點提交: `2bfd8b4d` · 日期: 2026-05-30

## 概述

CloudEdge Event Federation (ADR 0006) 的 Phase 1 和 Phase 1.5 清理已在 `event-federation` 上完成。這是 routerd 間 typed 事件匯流排的本地專用基礎設施: observed-fact 信封、`EventGroup` Kind、SQLite 本地儲存、用於事件 emit/list 的 CLI。**尚無跨節點傳遞** — 那是 Phase 2。

## 此檢查點包含的內容

- `EventGroup` Kind (`federation.routerd.net/v1alpha1`) + 驗證。
- `federation.Event` 信封 (observed fact; 既非設定也非命令),
  附帶 `Normalize`/`Validate`/`IsExpired`。
- SQLite `federation_events` 表, 冪等的 `RecordFederationEvent`
  (`ON CONFLICT(id) DO NOTHING`), 帶過濾的 `ListFederationEvents`
  (group 過濾 + 讀取時過期過濾)。
- `routerctl federation event emit/list` (別名 `fed`)。
- 單元測試 + CLI 測試; ADR 0006 已更新至實作狀態。

此處確定的語義 (後續 phase 不應回退): 儲存的冪等性以事件 **`id`** 為鍵; **`dedupeKey`** 是 subscription 側的分組鍵, 在 Phase 1 中不是 DB 的唯一約束。

## 下一步: Phase 2 — 僅傳輸

`routerd-eventd` + `EventPeer` 實現 overlay 上的推送傳遞 + HMAC +
`event_deliveries` + 保留期清理。Phase 2 **明確排除的範圍**:
`EventSubscription`、plugin 觸發、`DynamicConfigPart` 產生、
ARP/Clients observer 以及所有 provider mutation (這些屬於 Phase 3 及以後)。

這是分支檢查點筆記, **不是** 發佈標籤。
