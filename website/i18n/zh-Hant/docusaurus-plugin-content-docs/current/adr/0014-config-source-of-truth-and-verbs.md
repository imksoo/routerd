# ADR 0014: 組態正本與 CLI verb

## 狀態

提議 — 2026-06-07。

定義組態的持久化模型、candidate/commit 生命週期、`routerd` / `routerctl` 的
指令介面。替換 `routerd` 場景式的 verb 膨脹，
將刪除、歷史和回滾與現有 SQLite generations 對齊。

## 背景

routerd 將磁碟上的 `router.yaml` 同時用作操作員輸入和啟動時 reconcile 的
狀態。這一混淆產生了具體缺陷：執行時刪除資源後重新啟動不會保持。

- `routerctl delete` 刪除主機產物、所有權台帳條目和物件狀態，
  但**不編輯** `router.yaml`。
- `routerd serve` 啟動時讀取 `router.yaml` 並作為 desired state 進行 reconcile。
- apply/serve 的孤立 GC 與 `router.yaml` 中宣告的資源比較，
  檔案中仍存在的被視為「desired」並被重建。

因此，對啟動組態中仍存在的資源執行 `delete`，會在下次啟動或 apply 時恢復。

審視了兩種業界模型：

- **DB 為正本（Cisco running-config、Kubernetes etcd）。** mutation 進入儲存，
  檔案是輸入。這使命令式 delete 持久化，但對 routerd 而言，
  犧牲了產品核心的純文字、帶註解、可版本控制、可攜的
  組態（`cat` 稽核、單檔複製災難復原、
  升級時的 schema 重寫、無磁碟 USB 持久化）。
  還需要 startup-config/running-config 的分離。
- **檔案為正本，candidate/commit（VyOS/Junos）。** 人類可讀組態是
  持久化的真實來源。`set`/`delete`/`commit` 建構 candidate，
  `commit` 原子地驗證和套用，內建歷史/回滾。

純 GitOps 作為目標被否決：Git 名義上是正本，但 apply 失敗的
檔案在 Git 上仍作為宣告的狀態存在，記錄的真實與現實默默偏離。
採用的模型將正本定義為「最後成功 apply 的組態」，
透過交易式 commit 作為閘門來修正這一點。

CLI 介面也是按實作而非意圖增長的：

- `routerd` 有 11 個 verb（validate / check / observe / plan / adopt /
  render / apply / rollback / delete / serve / run）。「不 apply 只看」的 verb
  有 5 個重複，有未實作的 `run` 存根，`apply` 的必需 `--once` 看起來像可選的。
- `routerctl` 有約 28 個 verb。4 個重複的檢查 verb
  （get / status / show / describe）僅在資料來源（組態檔 / status 套接字 /
  狀態儲存）上不同，6 個頂層執行時資料表 dump、
  2 個診斷 verb（doctor / diagnose）。

## 決策

### 1. 正本

單一正本是一個人類可讀的規範 `router.yaml` 檔案。routerd 不將
真實來源移入不透明資料庫。

- 正本是**最後成功 apply 的**組態。驗證或 reconcile
  失敗的組態不成為正本。
- 註解和順序透過註解保持的 YAML round-trip（yaml.v3 `Node`）在
  機器 mutation 中保留。
- 每次成功 apply 原子地寫入規範檔案（temp + fsync + rename），
  並產生 generation 快照。歷史和回滾複用現有 SQLite generations。
  不引入新的歷史機制。
- 啟動時，`serve` 讀取規範組態。如果驗證失敗，serve
  reconcile last-good 的已提交 generation，並給出大警告，
  而非拒絕啟動或將損壞檔案作為正本。

### 2. 二進位分離

- **`routerd` 是 daemon/引擎。** systemd unit 僅執行 `routerd serve`。
  `serve` 執行一次收斂並結束
  （啟動測試、CI、漂移修復）。引導和復原透過
  `routerd serve --config <initial.yaml>` 種子規範檔案。
- **`routerctl` 是操作員 CLI**（kubectl 等效）。擁有組態生命週期和檢查 verb。
  mutation verb 透過控制套接字與執行中的 daemon 通訊。
  daemon 執行特權的規範寫入、reconcile 和 generation 快照。

### 3. 組態生命週期 verb（在 `routerctl` 上）

- `validate [-f <file>]` — 靜態 schema 合法性。無主機變更。
- `plan [-f <file>]` — 差分預覽。無主機變更。
- `apply -f <file>` — mutation 規範檔案並 reconcile。**輸入必需。**
  - 預設為**部分 upsert**（輸入中的資源被新增或更新。其他資源
    不變）。與部分 `delete` 對稱。
  - `--replace` 將規範檔案完全等於輸入（不存在的資源被 prune）。
  - **沒有 `add` verb**：新增需要 body，所以用片段 `apply`。
    僅 `delete` 需要獨立 verb。因為缺失無法用文件表達。
  - `serve` 執行中時，apply 預設立即 reconcile。
    `--no-reconcile` 僅寫入。serve 未執行時，`routerctl apply`
    報錯並指向 `routerd serve`。
- `delete <kind>/<name>` — 從規範檔案原子部分刪除後 reconcile。

輸入慣例：`-f <file>` 讀取檔案，`-f -` 讀取 stdin，省略 `-f`
以目前規範檔案為目標（`validate`/`plan` 在活的正本上操作）。
`apply` 要求顯式輸入。`validate` 和 `plan` 非特權（唯讀）。
`apply` 和 `delete` 特權，透過控制套接字存取閘門控制，
由特權 daemon 執行。

### 4. 檢查和執行時 verb（在 `routerctl` 上）

- `get` / `status` / `show` / `describe` 合併為 2 個：
  - `get [kind[/name]] [-o yaml|json|table]` — 機器可讀。按 subject
    合併 spec 和 status。
  - `describe <kind>/<name>` — 人類可讀詳情（spec、status、conditions、
    最近事件、關聯執行時資料）。
  - `status` 和 `show` 刪除。其檢視合併到 `get`/`describe`。
  - 所有檢查查詢執行中 daemon 的控制 API，停止按 verb 切換資料來源
    （舊有混亂的根源）。
- 6 個執行時資料表 dump（`events`、`ledger`、`dns-queries`、
  `connections`、`traffic-flows`、`firewall-logs`）合併為 `get <subject>`。
- 診斷合併為 `doctor`。主動探測移到 `doctor --probe <subject>`
  （吸收 `diagnose`）。
- 領域子樹（`firewall`、`dynamic`、`mobility`、`plugin`、`action`、
  `federation`）保留，使用 `get`/`describe` 風格的子 verb。
  `wireguard` 和 `tailscale` 移到 `vpn` 子樹。`firewall-logs` 變為
  `get firewall-logs`。
- 執行時控制：`drain`/`undrain` 移到 `ingress` 下。
  `restart-dns-resolver` 泛化為 `restart <daemon>`。`set-log-level` 變為
  `log-level`。
- `version` 和 `help` 不變。

### 5. 從 `routerd` 刪除或移動

`check`、`observe`、`render`、`adopt`、未實作的 `run` 被刪除或合併
（`check`/`observe`/`render` 合入 `plan`，`adopt` 移到 `routerctl`）。
`apply` 失去必需的 `--once`。`rollback` 移到 `routerctl`。

### 6. 權限

規範 `router.yaml` world-readable，但寫入僅限 root/`routerd`
（秘密透過 `SecretValueSource` 外部保持）。控制套接字 `0660 root:routerd`，
讀取 verb 任意使用者可用，mutation verb 透過套接字成員身分閘門控制，
由特權 daemon 執行。

## 結論

- `delete` 和 `apply` 透過 commit 重寫規範正本，因此結構性地
  跨重新啟動持久化。
- apply 失敗的組態不成為執行中的正本。啟動時回退到 last-good。
- verb 介面縮減，按資料來源的重複被消除。
- 需要在控制 API 中新增 apply/plan/delete/validate mutation —
  主要實作成本。
- 破壞性變更可接受（1 名使用者、無向後相容 shim、遵循專案策略）。
  組態按新模型重寫。

## 實作計畫（目標）

- **Phase 1 — commit 核心。** daemon 內的規範寫入器：yaml.v3 round-trip
  （註解/順序保持）、原子寫入、成功 apply 時的 generation 快照、
  `serve` 的 last-good 啟動回退。
- **Phase 2 — 控制 API mutation。** 控制套接字 API 新增
  apply/plan/delete/validate。帶套接字權限模型。
- **Phase 3 — verb 遷移。** `routerctl` 取得 validate/plan/apply/delete
  （經由 daemon）。upsert 預設/`--replace`/輸入必需。`serve`。
  `routerd` 裁減為 serve 專用（check/observe/render/adopt/run 的刪除/移動、
  必需 `--once` 的刪除、rollback 移到 routerctl）。
- **Phase 4 — 檢查合併。** get/status/show/describe 在控制 API 上合併為
  `get`+`describe`。6 個資料表 dump 合併為 `get <subject>`。
  `diagnose` 吸收到 `doctor --probe`。
- **Phase 5 — 領域與控制整理。** wireguard/tailscale 的 `vpn` 子樹、
  `restart <daemon>`、`ingress drain/undrain`、`log-level`。
- **Phase 6 — 文件與遷移。** 教學/how-to/參考和
  範例組態更新到新介面。非推薦 verb 的刪除。
