# Web Console Design Rules

> 2026-05-12 確定。Claude が Web Console 全面 redesign を直接実装した際に積み上げたデザインルール。今後 codex が Web Console を改修する際は本書を参照すること。

## 設計フレームワーク

### 1. Device split (媒体別 layout)

| device | breakpoint | 情報密度 | 推奨 layout |
|---|---|---|---|
| **Mobile** | < 860px | 低、collapsed default | 縦 1 列、card 型、tap で expand |
| **Desktop** | >= 860px | 高、full table + sidebar | full table、horizontal data |

**重要**: 全 page で **mobile breakpoint = 860px** に統一すること。`760px` / `900px` / `640px` 等の混在は禁止 (一度発生したが全面統一済)。 `main` element 内 `640px` のみは例外 (small phone 用 padding 調整)。

### 2. 利用用途別 (page 役割の明確化)

| 用途 | 主 page | 補助 |
|---|---|---|
| **障害切り分け** | Overview (L1 status + Active alerts) | Events、Firewall |
| **運用観察** | Connections / Clients / VPN | Firewall log、Overview top protocols |
| **設定確認** | Config (read-only YAML) | Generations (history + diff) |
| **性能 tuning** | Firewall Tuning Suggestions | Controllers、OTel |

### 3. 情報 hierarchy (3 階層)

- **L1 (at-a-glance)**: page top の Metric grid、4–6 個まで、`auto-fit minmax(170px, 1fr)` で responsive
- **L2 (list/table)**: sortable / filterable、stable sort default
- **L3 (detail)**: expand or modal、collapsed default

#### L1 Metric grid 統一 (重要)

全 list/dashboard page の top に L1 Metric grid を置く。本書時点で **7 page で適用済**:

| page | metrics |
|---|---|
| Overview | phase / generation / resources / conntrack / families |
| Connections | IPv4 / IPv6 / Showing / Groups |
| Resources | total / healthy / warning / danger / dry-run kinds |
| Firewall | total denies (24h) / peak per bucket / unique sources / DPI identified / orphan returns / filter matched |
| Events | total / filter matched / severities / kinds |
| Generations | total / showing / diffable / latest phase |
| Controllers | total / live / dry-run / other modes |
| VPN | TailscalePanel 内に integrated (重複追加禁止) |

新 page を増やす場合も同様に **L1 Metric grid を必須** とする。

## 画面要素の規律 (必読)

| 要素 | 規律 |
|---|---|
| **pill / badge** | 状態 (color) か 1 つ重要 label のみ。**同 row に 3 個以上並列 NG** |
| **重複表示禁止** | Top labels と group header card で同 grouping を 2 度表示は **禁止** (Connections の sectionBar 非表示でこの規律を守る) |
| **empty button 禁止** | text="" の button は混乱を招く、必ず label or 意味ある content。代わりに **Select dropdown** で group jump 等の機能化 |
| **stable sort** | list の default sort は **stable key** (hostname / id)、値ベースは user 明示時のみ。SSE 受信時に reorder trigger しない |
| **scroll stability** | new row は **bottom append**、再 sort は user 操作 trigger のみ、row 高さ固定化、transition off |
| **time 表示** | `RelativeTime` component 統一 (`(2m ago)` 表示 + tooltip absolute) |
| **table mobile redesign** | mobile では table を card 縦並びに切替 (`resourceDesktopTable` / `resourceMobileList` pattern を流用) |

## Mobile vs Desktop split パターン

### Table → Card 切替 (Resources / Generations 等で実装済)

CSS で `display: none` 切替、JSX で両方 render する pattern:

```ts
// styles
resourceDesktopTable: {
  "@media (max-width: 860px)": {
    display: "none",
  },
},
resourceMobileList: {
  display: "none",
  "@media (max-width: 860px)": {
    display: "grid",
    gap: "10px",
  },
},
resourceMobileCard: {
  display: "grid",
  gap: "6px",
  padding: "10px 12px",
  border: "1px solid " + token,
  borderRadius: tokens.borderRadiusMedium,
},
```

```tsx
<div className={`${styles.tableWrap} ${styles.resourceDesktopTable}`}>
  <Table>...</Table>
</div>
<div className={styles.resourceMobileList}>
  {filtered.map(...).map(row => <div className={styles.resourceMobileCard}>...</div>)}
</div>
```

### Card 表示 (Clients / Connections で expandable card)

`ConnectionCard` のような component:
- collapsed default = 1 行 summary (badge + endpoint + state + DPI app + age)
- tap で expand、detail list 表示 (state / source / destination / DPI / flow)
- `useState(expanded)` で個別管理、`aria-expanded` 適切

## Touch 挙動 (mobile)

- table-wrapping 要素に `overscroll-behavior-x: contain` で **横スクロール時の browser back/forward 抑止**
- touch direction detection で vertical-dominant swipe → page scroll、horizontal-dominant swipe → table scroll に分離
- mobile fixed row height (Clients = 68px)、`transition: none` で flashy 動き抑制

## Theme tokens

- **Fluent UI v9 `tokens`** を最大限活用 (hardcoded "4px" 等は禁止、`tokens.borderRadiusMedium` 使用)
- color は `webDarkTheme` のみ前提、token 引用優先
- icon color は `style={{ color: tokens.colorPaletteXxx }}` で個別指定 (例: `clientOSIconColor()` で Apple/Windows/Linux/Nintendo 等を vendor 別 brand color に)

## Component 採用方針

| 用途 | 採用 component |
|---|---|
| Layout | Card / CardHeader (Fluent UI v9) |
| Metric | 自作 `Metric` component (`<Metric label={...} value={...} />`) |
| Search | 自作 `SearchControl` (全 page 共通) |
| Time | 自作 `RelativeTime` (相対 + tooltip absolute) |
| Filter | `Select` (Fluent UI v9)、Phase / Kind / Severity 等で統一 |
| Dropdown | `Select` で意味ある jump (Connections の `Jump to group` 等) |
| Detail expansion | `<button> + useState(expanded)` + `aria-expanded` |
| Sidebar nav | `Button appearance="subtle"` + icon + label、active 状態は class 切替 |
| Notification badge | Sidebar nav item に conditional `<Badge appearance="tint" color="warning">` (例: Resources の dry-run / alert count) |

## State persistence (localStorage)

UX を保つために以下を localStorage に persist:

| key | 内容 |
|---|---|
| `routerd:nav:collapsed` | sidebar collapse 状態 (`"1"` / `"0"`) |
| `routerd:config:initialQuery` | Resources YAML jump 時の one-shot search query (consume + delete) |
| (Clients section 折りたたみ) | 既存 client section 用 |

## Document title

`useEffect` で `document.title = "${page label} - ${cfg.title || "routerd"}"` を更新。tab 識別性向上。

## Footer

- 簡潔に `Powered by routerd.`
- `routerd` は GitHub repo へ link (`target="_blank"` + `rel="noopener noreferrer"`)
- 著作権 + license 詳細は `docs/legal.md` / About page に集約 (footer に冗長な copyright 表記禁止、user 2026-05-12 直指示)

## 重複情報の排除

過去の anti-pattern (実装時に発見・修正済):
- ❌ Connections の **anchorDotBar (空 dot button 列)** = 削除、`Jump to group` Select dropdown に置換
- ❌ Connections の **ConnectionSummaryCharts (group header card と重複)** = 削除
- ❌ Connections / blade header sub-section nav = 削除 (group header で完結)
- ❌ Identified badge 4 個並列 → Metric grid (DPI / Port guess / Identifying / Unclassified)
- ❌ Interface card の **managed/adopted badge** → muted text

## Connections page 個別ルール

- ConnectionCard list (table 廃止)
- Top に L1 Metric grid (4 metric)
- ClassificationSummary (Classification ratio meter + Identified breakdown)
- `Jump to group` Select で group anchor jump
- Filter 7 種 (query / family / protocol / state / app / sort / direction)
- Group section 内に各 connection を ConnectionCard で list、collapsed → expand pattern
- DPI 識別済 vs Port guess vs Identifying… を **明確に区別表示** (confidence source、視覚 weak / strong)

## Clients page 個別ルール

- ClientInventory + ClientTraffic + DHCPLeaseTable の 3 view、sub-nav で切替
- ClientInventory:
  - top の 4 Metric (devices / online / addresses / activity types)
  - OS family section (Apple / Windows / Linux / Nintendo / etc) で grouping、section header collapsible (state persistent)
  - 各 device row = mobile collapsed 68px、icon + hostname + status + IP + family + count + last seen
  - tap で expand: nested addresses (IPv4/IPv6/privacy ext) + primary activity + protocol mix + DPI fingerprint
- `ClientSectionIcon` は vendor 別 brand color (`clientOSIconColor()`)

## Firewall page 個別ルール

- Deny activity Card top に L1 Metric grid (6 metric: total denies / peak / unique sources / DPI identified / orphan returns / filter matched)
- DenyRateChart (24h aggregate endpoint `/api/v1/firewall/deny-timeline`)
- Source IP top-N に **Filter icon button** で source IP auto-filter 適用
- Deny ranking + Deny timeline は responsive grid layout (mobile では各 cell に label 付き card 化)

## Resources page 個別ルール

- Card top に L1 Metric grid (5 metric)
- Filter 3 種 (search / kind / phase)
- desktop = table、mobile = card 切替
- 各 row に `<Button icon={<DocumentTextRegular />}>YAML</Button>` で Config page jump
  - jump 時に `localStorage.setItem("routerd:config:initialQuery", resource.name)` で one-shot transfer
  - Config page 起動時に consume + delete + search query auto-set

## Sidebar 個別ルール

- Sticky position (desktop)、mobile では horizontal scroll bar 風 (`overscroll-behavior-x: contain`)
- nav item に conditional badge (Resources の dry-run / alert count 等)
- collapse 状態を localStorage で persist
- print 時に非表示 (`@media print { display: none }`)

## Print stylesheet

- sidebar / header 非表示
- main content padding 0、gap 12px
- 運用報告などで content だけ印刷可能

## 過去の重要 user feedback (絶対遵守)

1. 「キューに入れるのは後回しで良いものだけ、優先させたいなら割り込ませないとダメ」 — codex 制御の規律 (memory `feedback_codex_message_must_use_tab` 参照)
2. 「スマホで Clients カードのアドレスが横幅足りなくて潰れて縦長になって見づらい」 → mobile redesign 完了 (Phase 3.15 + 直近の Primary IP 修正)
3. 「mobile で楕円がたくさん並んでて意味不明」→ anchorDotBar / empty button 削除完了
4. 「mobile で table を触ると左右だけ動いて上下スクロールできない」→ touch direction detection で page scroll 優先
5. 「不意にスクロールされて見失う」→ stable sort + scroll snapshot restore + transition off
6. 「Pending と Disabled/Standby を区別したい」→ phase 4 種に拡張 (Pending / Disabled / Standby / NotApplicable)
7. 「Connections の Top jump dot とカードが重複」→ Jump to group Select に置換、ConnectionSummaryCharts 削除
8. 「Sony 共通 DNS で BRAVIA を PS と誤認識」→ peers DNS 精密 pattern + MAC OUI + nDPI fingerprint で総合判定
9. 「nDPI 入れた割に便利になってない」→ `acceptSampleRate=1` (全 packet) + DPI source / port-guess 区別表示
10. 「テーブルじゃ情報保持しきれない、デザイナーとしてこだわりを見せろ」→ Connections expandable card 全面 redesign

## 今後の改修指針 (codex 向け)

- **重複情報 / pill 並列 / empty button** を検出したら即座に削除 or 統合
- 新 page を増やす時は **L1 Metric grid + filter + responsive (table/card)** の 3 点セットから始める
- mobile-first で考え、desktop は extension とする (mobile が動作しない page は merge しない)
- visible improvement は **commit + push** (release tag は phase 集約で daily)
- 大規模変更は **複数 sub-phase に分けて段階的 release**
- 設計判断で迷ったら user に escalate、独断で大規模変更を進めない
- Fluent UI v9 + tokens-driven、hardcoded color/spacing を増やさない

## 参考 commit (本日の design redesign 関連)

- ConnectionCard expandable: `webconsole/src/main.tsx` 内 `function ConnectionCard`
- Resources mobile cards: `resourceDesktopTable` / `resourceMobileList` style + JSX 両方 render
- Generations mobile cards: 同 style 再利用
- L1 Metric grid 拡張: 各 page の Card top に `<div className={styles.grid}>` で `<Metric />` 4-6 個
- Sidebar notification badge: `navTextHeader` style + conditional `<Badge>`
- localStorage persist: `routerd:nav:collapsed`, `routerd:config:initialQuery`
- Footer GitHub link: `consoleLegalLink` style
- Print stylesheet: `@media print` で sidebar/header hide、main padding 0
- Mobile breakpoint 統一: 全 35 箇所 = `@media (max-width: 860px)` のみ

---

**更新責任**: 本ファイルは Web Console design rule の単一の真実。大きな design 変更を加える際は本書を更新すること。conflict / 矛盾が生じたら user に escalate。
