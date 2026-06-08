# ADR 0014: 配置正本与 CLI verb

## 状态

提议 — 2026-06-07。

定义配置的持久化模型、candidate/commit 生命周期、`routerd` / `routerctl` 的
命令界面。替换 `routerd` 场景式的 verb 膨胀，
将删除、历史和回滚与现有 SQLite generations 对齐。

## 背景

routerd 将磁盘上的 `router.yaml` 同时用作操作员输入和启动时 reconcile 的
状态。这一混淆产生了具体缺陷：运行时删除资源后重启不会保持。

- `routerctl delete` 删除主机产物、所有权台账条目和对象状态，
  但**不编辑** `router.yaml`。
- `routerd serve` 启动时读取 `router.yaml` 并作为 desired state 进行 reconcile。
- apply/serve 的孤立 GC 与 `router.yaml` 中声明的资源比较，
  文件中仍存在的被视为"desired"并被重建。

因此，对启动配置中仍存在的资源执行 `delete`，会在下次启动或 apply 时恢复。

审视了两种业界模型：

- **DB 为正本（Cisco running-config、Kubernetes etcd）。** mutation 进入存储，
  文件是输入。这使命令式 delete 持久化，但对 routerd 而言，
  牺牲了产品核心的纯文本、带注释、可版本控制、可移植的
  配置（`cat` 审计、单文件拷贝灾难恢复、
  升级时的 schema 重写、无盘 USB 持久化）。
  还需要 startup-config/running-config 的分离。
- **文件为正本，candidate/commit（VyOS/Junos）。** 人类可读配置是
  持久化的真实来源。`set`/`delete`/`commit` 构建 candidate，
  `commit` 原子地验证和应用，内置历史/回滚。

纯 GitOps 作为目标被否决：Git 名义上是正本，但 apply 失败的
文件在 Git 上仍作为声明的状态存在，记录的真实与现实默默偏离。
采用的模型将正本定义为"最后成功 apply 的配置"，
通过事务性 commit 作为门控来修正这一点。

CLI 界面也是按实现而非意图增长的：

- `routerd` 有 11 个 verb（validate / check / observe / plan / adopt /
  render / apply / rollback / delete / serve / run）。"不 apply 只看"的 verb
  有 5 个重复，有未实现的 `run` 存根，`apply` 的必需 `--once` 看起来像可选的。
- `routerctl` 有约 28 个 verb。4 个重复的检查 verb
  （get / status / show / describe）仅在数据来源（配置文件 / status 套接字 /
  状态存储）上不同，6 个顶层运行时数据表 dump、
  2 个诊断 verb（doctor / diagnose）。

## 决策

### 1. 正本

单一正本是一个人类可读的规范 `router.yaml` 文件。routerd 不将
真实来源移入不透明数据库。

- 正本是**最后成功 apply 的**配置。验证或 reconcile
  失败的配置不成为正本。
- 注释和顺序通过注释保持的 YAML round-trip（yaml.v3 `Node`）在
  机器 mutation 中保留。
- 每次成功 apply 原子地写入规范文件（temp + fsync + rename），
  并生成 generation 快照。历史和回滚复用现有 SQLite generations。
  不引入新的历史机制。
- 启动时，`serve` 读取规范配置。如果验证失败，serve
  reconcile last-good 的已提交 generation，并给出大警告，
  而非拒绝启动或将损坏文件作为正本。

### 2. 二进制分离

- **`routerd` 是 daemon/引擎。** systemd unit 仅运行 `routerd serve`。
  `serve` 执行一次收敛并退出
  （启动测试、CI、漂移修复）。引导和恢复通过
  `routerd serve --config <initial.yaml>` 种子规范文件。
- **`routerctl` 是操作员 CLI**（kubectl 等效）。拥有配置生命周期和检查 verb。
  mutation verb 通过控制套接字与运行中的 daemon 通信。
  daemon 执行特权的规范写入、reconcile 和 generation 快照。

### 3. 配置生命周期 verb（在 `routerctl` 上）

- `validate [-f <file>]` — 静态 schema 合法性。无主机变更。
- `plan [-f <file>]` — 差分预览。无主机变更。
- `apply -f <file>` — mutation 规范文件并 reconcile。**输入必需。**
  - 默认为**部分 upsert**（输入中的资源被添加或更新。其他资源
    不变）。与部分 `delete` 对称。
  - `--replace` 将规范文件完全等于输入（不存在的资源被 prune）。
  - **没有 `add` verb**：添加需要 body，所以用片段 `apply`。
    仅 `delete` 需要独立 verb。因为缺失无法用文档表达。
  - `serve` 运行中时，apply 默认立即 reconcile。
    `--no-reconcile` 仅写入。serve 未运行时，`routerctl apply`
    报错并指向 `routerd serve`。
- `delete <kind>/<name>` — 从规范文件原子部分删除后 reconcile。

输入约定：`-f <file>` 读取文件，`-f -` 读取 stdin，省略 `-f`
以当前规范文件为目标（`validate`/`plan` 在活的正本上操作）。
`apply` 要求显式输入。`validate` 和 `plan` 非特权（只读）。
`apply` 和 `delete` 特权，通过控制套接字访问门控，
由特权 daemon 执行。

### 4. 检查和运行时 verb（在 `routerctl` 上）

- `get` / `status` / `show` / `describe` 合并为 2 个：
  - `get [kind[/name]] [-o yaml|json|table]` — 机器可读。按 subject
    合并 spec 和 status。
  - `describe <kind>/<name>` — 人类可读详情（spec、status、conditions、
    最近事件、关联运行时数据）。
  - `status` 和 `show` 删除。其视图合并到 `get`/`describe`。
  - 所有检查查询运行中 daemon 的控制 API，停止按 verb 切换数据来源
    （旧有混乱的根源）。
- 6 个运行时数据表 dump（`events`、`ledger`、`dns-queries`、
  `connections`、`traffic-flows`、`firewall-logs`）合并为 `get <subject>`。
- 诊断合并为 `doctor`。主动探测移到 `doctor --probe <subject>`
  （吸收 `diagnose`）。
- 领域子树（`firewall`、`dynamic`、`mobility`、`plugin`、`action`、
  `federation`）保留，使用 `get`/`describe` 风格的子 verb。
  `wireguard` 和 `tailscale` 移到 `vpn` 子树。`firewall-logs` 变为
  `get firewall-logs`。
- 运行时控制：`drain`/`undrain` 移到 `ingress` 下。
  `restart-dns-resolver` 泛化为 `restart <daemon>`。`set-log-level` 变为
  `log-level`。
- `version` 和 `help` 不变。

### 5. 从 `routerd` 删除或移动

`check`、`observe`、`render`、`adopt`、未实现的 `run` 被删除或合并
（`check`/`observe`/`render` 合入 `plan`，`adopt` 移到 `routerctl`）。
`apply` 失去必需的 `--once`。`rollback` 移到 `routerctl`。

### 6. 权限

规范 `router.yaml` world-readable，但写入仅限 root/`routerd`
（秘密通过 `SecretValueSource` 外部保持）。控制套接字 `0660 root:routerd`，
读取 verb 任意用户可用，mutation verb 通过套接字成员身份门控，
由特权 daemon 执行。

## 结论

- `delete` 和 `apply` 通过 commit 重写规范正本，因此结构性地
  跨重启持久化。
- apply 失败的配置不成为运行中的正本。启动时回退到 last-good。
- verb 界面缩减，按数据来源的重复被消除。
- 需要在控制 API 中添加 apply/plan/delete/validate mutation —
  主要实现成本。
- 破坏性变更可接受（1 名用户、无后向兼容 shim、遵循项目策略）。
  配置按新模型重写。

## 实现计划（目标）

- **Phase 1 — commit 核心。** daemon 内的规范写入器：yaml.v3 round-trip
  （注释/顺序保持）、原子写入、成功 apply 时的 generation 快照、
  `serve` 的 last-good 启动回退。
- **Phase 2 — 控制 API mutation。** 控制套接字 API 添加
  apply/plan/delete/validate。带套接字权限模型。
- **Phase 3 — verb 迁移。** `routerctl` 获得 validate/plan/apply/delete
  （经由 daemon）。upsert 默认/`--replace`/输入必需。`serve`。
  `routerd` 裁减为 serve 专用（check/observe/render/adopt/run 的删除/移动、
  必需 `--once` 的删除、rollback 移到 routerctl）。
- **Phase 4 — 检查合并。** get/status/show/describe 在控制 API 上合并为
  `get`+`describe`。6 个数据表 dump 合并为 `get <subject>`。
  `diagnose` 吸收到 `doctor --probe`。
- **Phase 5 — 领域与控制整理。** wireguard/tailscale 的 `vpn` 子树、
  `restart <daemon>`、`ingress drain/undrain`、`log-level`。
- **Phase 6 — 文档与迁移。** 教程/how-to/参考和
  示例配置更新到新界面。非推荐 verb 的删除。
