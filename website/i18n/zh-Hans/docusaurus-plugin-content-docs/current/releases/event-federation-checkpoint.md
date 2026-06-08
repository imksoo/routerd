# CloudEdge Event Federation — 检查点 (Phase 1 + 1.5 完成)

状态: **experimental** (开发中; 不推荐作为稳定版)
分支: `event-federation` · 检查点提交: `2bfd8b4d` · 日期: 2026-05-30

## 概述

CloudEdge Event Federation (ADR 0006) 的 Phase 1 和 Phase 1.5 清理已在 `event-federation` 上完成。这是 routerd 间 typed 事件总线的本地专用基础设施: observed-fact 信封、`EventGroup` Kind、SQLite 本地存储、用于事件 emit/list 的 CLI。**尚无跨节点传递** — 那是 Phase 2。

## 此检查点包含的内容

- `EventGroup` Kind (`federation.routerd.net/v1alpha1`) + 验证。
- `federation.Event` 信封 (observed fact; 既非配置也非命令),
  附带 `Normalize`/`Validate`/`IsExpired`。
- SQLite `federation_events` 表, 幂等的 `RecordFederationEvent`
  (`ON CONFLICT(id) DO NOTHING`), 带过滤的 `ListFederationEvents`
  (group 过滤 + 读取时过期过滤)。
- `routerctl federation event emit/list` (别名 `fed`)。
- 单元测试 + CLI 测试; ADR 0006 已更新至实现状态。

此处确定的语义 (后续 phase 不应回退): 存储的幂等性以事件 **`id`** 为键; **`dedupeKey`** 是 subscription 侧的分组键, 在 Phase 1 中不是 DB 的唯一约束。

## 下一步: Phase 2 — 仅传输

`routerd-eventd` + `EventPeer` 实现 overlay 上的推送传递 + HMAC +
`event_deliveries` + 保留期清理。Phase 2 **明确排除的范围**:
`EventSubscription`、plugin 触发、`DynamicConfigPart` 生成、
ARP/Clients observer 以及所有 provider mutation (这些属于 Phase 3 及以后)。

这是分支检查点笔记, **不是** 发布标签。
