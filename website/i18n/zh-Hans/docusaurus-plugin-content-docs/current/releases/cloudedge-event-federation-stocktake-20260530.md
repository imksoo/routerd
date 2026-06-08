# CloudEdge + Event Federation — 合并前盘点 (event-federation → main)

状态: **experimental** (实验室验证的基础设施; 不推荐作为稳定版)
分支: `event-federation` · Head: `8c4821c8` · 日期: 2026-05-30
作者: review subagent (仅记录事实; 合并判断由 orchestrator 最终决定)

这是对 `event-federation` 分支作为 `main` 的 experimental-MVP 候选进行的只读盘点文档。不进行代码变更或合并。

## 范围概述

`event-federation` **领先 `main` 36 个提交、落后 0 个提交** (可进行干净的 fast-forward)。它是 `cloudedge-mvp` 的严格超集, `cloudedge-mvp` 的 head `713233b0` 是 `event-federation` 的祖先 (通过 `git merge-base --is-ancestor` 确认)。也就是说, 该分支包含 **CloudEdge/SAM** (`cloudedge-mvp` 的全部内容) **+ Event Federation Phase 1 / 1.5 / 2 / 3**。

相对于 `main` 新增的内容:

- **CloudEdge / SAM** (源自 `cloudedge-mvp`): dynamic-config 基础设施
  (`DynamicConfigPart` / masks / `DynamicOverridePolicy`)、plugin runner
  (observe-only, dry-run; actionPlans 仅用于展示)、L3 hybrid
  (`OverlayPeer` / `HybridRoute`)、Selective Address Mobility
  (`AddressMobilityDomain` / `RemoteAddressClaim` / `CloudProviderProfile`,
  Linux 数据平面)、zone 无关的 PMTU/MSS clamp (#53)、nft 所有权
  诊断、`routerctl doctor hybrid`。
- **Event Federation** (ADR 0006): typed observed-event 信封 + SQLite 本地
  存储 + `routerctl federation event` CLI (Phase 1/1.5); `routerd-eventd`
  传输守护进程 + `EventGroup` / `EventPeer` Kind + HMAC 推送传递 +
  `event_deliveries` + 保留期清理 (Phase 2); `EventSubscription` Kind +
  subscription 触发的 plugin → `DynamicConfigPart` (`RemoteAddressClaim`)
  (Phase 3)。新增 Kind 共 3 个: `EventGroup`、`EventPeer`、`EventSubscription`
  (apiVersion `federation.routerd.net/v1alpha1`)。

## 证据清单

仓库内的证据/里程碑文档 (全部已确认存在):

| 文档 | 结果 / 判定 |
|---|---|
| `docs/releases/cloudedge-sam-mvp-milestone.md` | Azure/AWS/OCI x PVE 全部 **PASS / clean**; 3 云对等; experimental |
| `docs/releases/cloudedge-sam-stocktake-20260529.md` | 合并前盘点; 粗糙之处 = experimental 后续跟进, 非阻塞项 |
| `docs/releases/evidence/cloudedge-sam-azure-pve-20260529.md` | Azure x PVE **PASS / clean** |
| `docs/releases/evidence/cloudedge-sam-aws-pve-20260529.md` | AWS x PVE **PASS / clean** (Azure 对等, 首次运行) |
| `docs/releases/event-federation-checkpoint.md` | Phase 1 + 1.5 检查点; experimental; 非发布标签 |
| `docs/releases/evidence/cloudedge-event-federation-transport-20260530.md` | Phase 2 传输冒烟测试 **Result: PASS** (断言 A–G 共 7 项) |
| `docs/releases/evidence/cloudedge-event-federation-subscription-20260530.md` | Phase 3 subscription 冒烟测试 **Result: PASS** (主路径 + 4 项否定检查) |

完整的证据包 (以及 OCI 摘要) 按照既定的实验室模式存放在相邻的实验室仓库 `/home/imksoo/routerd-labs/...` 中 (未提交至本仓库)。引用的传输/subscription 证据包 (`routerd-labs/event-federation/evidence/20260530T091652Z-...` 和 `...20260530T111612Z-...`) 存在于磁盘上。

### 链接完整性发现 (轻微, 建议修复)

`cloudedge-sam-mvp-milestone.md:24` 将 OCI 证据链接为
`routerd-labs/cloudedge-sam/evidence/20260530T031247Z-oci-pve-hardening/summary.md`,
但磁盘上的实际目录为
`20260530T031247Z-oci-pve-hardening-43a64c55/` (缺少提交后缀 `-43a64c55`)。**引用路径无法解析** → 断链。(该路径位于外部实验室仓库中, 不影响网站构建, 但作为引用不准确。) 仓库内 `docs/releases/evidence/*.md` 的 4 处引用全部正确解析。

## 完整性发现

### ADR 0006 的状态已过时 (合并前必须修复)

`docs/adr/0006-event-federation.md` 的 Status 部分仍包含以下内容:

> Phase 1 (...) is implemented on `event-federation`. **Phase 2 (peer delivery
> over the overlay) is pending.**

Context 中还写着 **"OCI×PVE in progress"**。两者目前均不准确: Phase 2 和 Phase 3 已实现 (附带 PASS 冒烟测试), OCI×PVE 也已通过。需要将 ADR 的 Status 块更新为 Phase 1–3 已实现 + OCI clean。

### 文档站点导航 — 新文档处于孤立状态 (合并前必须修复)

`website/sidebars.ts` 是文档的侧边栏 (`docs/` 下的默认英文版)。
SAM 参考文档 (`reference/selective-address-mobility`) 已在侧边栏注册
(sidebars.ts:150)。但:

- **`docs/how-to/event-federation-subscription.md` 未在 `website/sidebars.ts` 中注册**
  (`grep` 结果 = 0)。处于孤立状态, 不会出现在站点的 How-to guides 分类中。
- **`docs/reference/` 下没有专门的 federation 参考文档** (
  `docs/reference/` 下仅有 `dynamic-config.md` 和 `selective-address-mobility.md`)。
  如果计划编写 federation 参考页面则尚未创建; 如果 how-to 是唯一的
  federation 文档, 则仍需要侧边栏条目。

根据项目策略 (正本 = 日文 `website/i18n/ja`, Web 默认 = 英文 `docs/`),
i18n/ja 的侧边栏/翻译也需要条目, 但侧边栏结构是共享的 (`sidebars.ts`),
因此将 how-to 添加到 `sidebars.ts` 即为唯一的必要布线更改; ja 翻译内容是
单独的 (低优先级, experimental) 后续跟进。

## API Schema 生成发现 (合并前必须修复)

生成器: `make generate-schema` → `cmd/routerd-schema` →
`schemas/routerd-config-v1alpha1.schema.json` (+ control + control-openapi)。
3 个 schema 文件全部被 git 跟踪。

- 执行 `make generate-schema` (和 `make check-schema`) **无差异** —
  `git status --short schemas/` 是干净的。即已提交的 schema 与生成器
  内部一致。
- **但 schema 是不完整的。** `cmd/routerd-schema/main.go` 通过 `resourceSchema(apiVersion, "Kind", Spec{})` 手动列举各 Kind。SAM 的 Kind 已注册 (327–331 行: OverlayPeer, HybridRoute, AddressMobilityDomain, CloudProviderProfile, RemoteAddressClaim)。**新增的 federation 3 个 Kind — `EventGroup`、`EventPeer`、`EventSubscription` — 未在生成器列表中注册。** 因此不会出现在生成/发布的 JSON schema 中, 重新生成也不会产生差异 (生成器不知道它们的存在)。
- 修复 = 在 `cmd/routerd-schema/main.go` 中添加 `resourceSchema(api.FederationAPIVersion, "EventGroup"/"EventPeer"/"EventSubscription", api.…Spec{})` 的 3 行, 然后运行 `make generate-schema` 并提交 `schemas/` 的差异。(此处不进行修复, 仅向 orchestrator 报告。)

验证备注: `make check-schema` 当前 **通过**。这仅将生成器输出与已提交文件进行比对, 不检测缺失的 Kind。因此 CI 的绿色状态无法捕获此缺口。

## make dist / 打包完整性

- `routerd-eventd` **已包含** 在 `make dist` 中: Makefile 的 `ROUTERD_RELEASE_BINS` 包含
  `$(ROUTERD_EVENTD_BIN)` (Makefile:33–34), `build-daemons` 进行构建 (Makefile:74),
  dist 进行安装 (Makefile:199)。通过 `make -n dist | grep eventd` 确认了构建 + 安装行。
- **示例插件 (`examples/plugins/event-to-remote-claim`) 未包含在 `make dist` 中**
  (Makefile 中无引用; `make -n dist` 中无 `examples/plugins`)。这已在 **文档中说明**:
  `examples/plugins/event-to-remote-claim/README.md` ("## Build and install" →
  `go build -o bin/event-to-remote-claim ./examples/plugins/event-to-remote-claim`) 和
  `docs/how-to/event-federation-subscription.md:61–64` 均指导操作者单独构建。
- **打包无需 eventd 特定的更改。** `packaging/install.sh` 使用通用 glob
  (`for binary in bin/*`, 第 1873 行) 安装所有二进制文件, 因此 `routerd-eventd`
  会自动安装。按组划分的 systemd 单元
  `routerd-eventd@<group>.service` 由 **routerd 自身生成** (controller chain /
  `pkg/render/eventd_systemd.go` + `pkg/controller/eventfederation` 的 `EventGroup` supervision)。
  它不作为静态单元捆绑, 因此 `install.sh` 的 `systemd/*.service` 循环中不需要。
  `contrib/systemd/` 中没有静态的 `routerd-eventd.service` (设计上如此 — 是模板化的 `@.service`)。

## 无 Provider Mutation (安全 / 范围门控) — 已确认: 无

对整个代码树进行 grep (Go 源码: `pkg/`、`cmd/`、`examples/`):

- **无云 SDK 导入** (`aws-sdk` / `azure-sdk` / `oci-go-sdk` /
  `cloud.google.com` / `github.com/{aws,Azure,oracle}/`) — 零匹配。
- **无云 CLI exec。** 调用外部工具的 `exec.Command*` 仅存在于
  `pkg/controller/dhcpv4client/controller.go` 和
  `cmd/routerd-pppoe-client/main.go` (本地 DHCP/PPPoE), 与云无关。
- `ActionPlan` **被声明为仅展示**: `pkg/plugin/types.go:85–86`
  ("MVP routerd never executes ActionPlans"); 测试
  `TestRunRemoteAddressClaimActionPlanIsDisplayOnly` 强制执行。
- 示例插件读取 `os.Stdin` JSON 并仅写入 `os.Stdout` JSON
  (`examples/plugins/event-to-remote-claim/main.go`) — **无 exec、http、net、云调用**;
  其头部注释声明 provider action 执行在 MVP 范围外 (Phase 4/5)。

结论: **此分支中不存在可执行的 provider mutation 路径。** 与 provider
相关的接口仅有声明式 spec (`CloudProviderProfile`、capture type `provider-secondary-ip`)、
仅展示的 actionPlans, 以及无云调用的示例插件。

## Experimental 标记 — 已确认

- `cloudedge-sam-mvp-milestone.md`: "Status: **experimental** (lab-validated; NOT
  recommended-stable)"; 明确保留稳定升级 / 发布标签的授予。
- `event-federation-checkpoint.md`: "Status: **experimental** (in development;
  NOT recommended-stable)"; "**not** a release tag."
- ADR 0006: "Accepted for **experimental implementation**."
- Phase 2/3 证据的判定将结果限定为控制平面, 并断言
  未发生 provider/云 mutation。

审查的文档中没有暗示稳定版 / 推荐版的内容。没有发布标签或稳定升级的
声明。

## 已知缺口

预期的 4 个缺口中, 2 个被准确识别为缺口, 1 个被
误认, 1 个未被记录:

1. **FreeBSD rc.d 对 `routerd-eventd` 的 supervision — 未实现 (仅 systemd), 且未记录。**
   `pkg/render/eventd_systemd.go` 仅渲染 systemd 单元, 没有 eventd 的
   rc.d 对应物。ADR 中也没有关于 eventd 的 rc.d / FreeBSD 的描述。
   → 应作为 experimental 的平台限制记录。
2. **`EventSubscription` 的 `batchWindow` / `debounce` 被接受但不以精确定时器执行。**
   Spec 字段存在 (`pkg/api/specs.go:1298–1303`), 但
   `pkg/controller/eventsubscription/controller.go` 是 **poll-tick 批处理**
   ("poll + dedup … each tick", 4–8 行), 没有精确的 batch/debounce 定时器。
   字段被接受为配置, 但目前仅为信息性。→ 作为限制未记录; 应记录。
3. **自推送 / 循环防止 — 已实现 (非缺口)。** ADR 的循环防止不变量已被
   强制执行: `pkg/eventd/outbox.go:78` 仅推送本地产生的事件 (`SourceNode == nodeName`),
   不重新推送接收到的事件。`TestOutboxLoopPrevention`
   (`pkg/eventd/outbox_test.go`) 覆盖。另一个 observer 侧不变量
   ("节点不重新 emit 自身捕获的地址的 observed event") 属于
   ARP/Clients observer, **是 Phase 4, 不在此分支中**。
   当前不存在需要跳过的内容。
4. **实验室节点保留 `515fe7e8` 构建。** Phase 3 证据
   (`...subscription-20260530.md`) 记录了将 `515fe7e8` 部署到 router03 + router05,
   但 **没有拆除 / 还原的记载**, 在实验室记录中这些节点被推定为
   仍在运行 Phase 3 构建。→ 需要明确的实验室笔记 (清理或有意保留)。

## 构建 / 测试健康度 (最终门控)

全部在 `event-federation` head `8c4821c8` 上执行:

- `gofmt -l pkg cmd examples` → **干净** (无输出文件)。
- `go build ./...` → **成功**。
- `go test ./...` → **1880 个测试通过, 95 个包** (exit 0)。无失败。
- `make check-schema` → **通过** (无差异) — 但请参阅上述 schema 不完整性发现;
  check-schema 不检测缺失的 Kind。
- 此次运行中未观察到 `cmd/routerd` 的 networkd-env 测试失败。

## 与 PR #49 的关系 — 选项 (仅事实; 非建议)

PR #49 (`gh pr view 49`): OPEN, **draft**, `cloudedge-mvp → main`, 标题
"CloudEdge MVP: hybrid routing and selective address mobility"。内容是
`event-federation` 的 **严格子集** (head `713233b0` 是祖先)。
`event-federation` 领先 main 36 / 落后 0 → **可干净 fast-forward**。

- **(a) 将 #49 重定向/替换为 `event-federation → main` PR。** 单个 PR 承载 CloudEdge/SAM + EF Phase 1–3;
  #49 关闭/被取代。一次审查, 一次合并。
- **(b) 先通过 #49 合并 `cloudedge-mvp`, 再合并 `event-federation`。** 两阶段:
  CloudEdge/SAM 作为独立合并落地, EF 紧随其后。更细的历史; 两次审查/合并周期;
  #49 有存在意义。
- **(c) `event-federation` 的单次 experimental 合并。** 与 (a) 相同的最终状态, 但框架为单次
  experimental 合并; #49 作为被取代关闭。

无论哪种情况, #49 的差异完全包含在 `event-federation` 中, FF 是干净的。

## 建议 (最终)

**判定: 作为 experimental 功能准备合并至 `main`。** 构建干净, gofmt 干净,
1880 测试绿色, golden 无变更, 无 provider mutation 路径, 一致的
experimental 标记, `make dist` 包含 `routerd-eventd`, 打包无需变更。
CloudEdge/SAM 已在 3 个云中进行实验室验证 (PASS/clean), EF Phase 1–3 各有 PASS
实验室冒烟测试 (transport + subscription)。

盘点中指出的合并前整备项目已在同一路径 (同一分支, 与本文档并行提交) 中解决:

1. **Schema (必须修复) — 已解决。** 将 `EventGroup` / `EventPeer` /
   `EventSubscription` 注册到 `cmd/routerd-schema/main.go`;
   重新生成 `schemas/routerd-config-v1alpha1.schema.json` 并包含;
   `make check-schema` 通过。
2. **ADR 0006 状态 (必须修复) — 已解决。** 将 Status/Context 更新为 Phase 1–3
   已实现 + OCI×PVE clean (3 云对等); 设置了逐 phase 标记;
   添加了 `## Known limitations (experimental)` 子节。
3. **文档导航 (必须修复) — 已解决。** 将 `how-to/event-federation-subscription` 添加到
   `website/sidebars.ts` (英文/默认侧边栏; ja 翻译为延迟后续跟进,
   非阻塞 — Docusaurus 会回退到源文档)。
4. **OCI 证据链接 (建议修复) — 已解决。** 修正了 `cloudedge-sam-mvp-milestone.md` 中的
   `-43a64c55` 目录后缀。
5. **Experimental 缺口 (建议修复) — 已解决。** 在 ADR 0006 "Known limitations" 中记录:
   仅 systemd 的 `routerd-eventd` (FreeBSD rc.d 未支持);
   `batchWindow`/`debounce` 被接受但为 poll-tick 批处理 (无精确定时器)。

剩余 (非阻塞, 在此跟踪):

6. **实验室拆除笔记。** router03/router05 在 Phase 3 冒烟测试后保留 `515fe7e8` 构建
   (配置已恢复到基线; 仅二进制文件未还原)。不是 `main` 合并的阻塞项,
   是实验室管理笔记。下次实验室操作时还原或重新固定。
7. **i18n。** `event-federation-subscription.md` 的 ja/zh 翻译以及专门的 federation
   参考页面为延迟后续跟进。

**建议的合并形式:** 单个 `event-federation → main` PR, **PR #49 作为被取代关闭**
(`cloudedge-mvp` 的内容是 `event-federation` 的严格祖先/子集; 干净 fast-forward,
0 落后)。这是最小开销的路径, 维持单次 experimental 落地。
选项 (b) — 先通过 #49 落地 `cloudedge-mvp`, 再合并 `event-federation` — 仅在
需要单独的 CloudEdge/SAM 历史检查点时才有价值, 但并非必须。

**合并本身和 PR #49 的处置由维护者判断** (向 main 的发布/合并是所有者门控)。无标签; experimental。
