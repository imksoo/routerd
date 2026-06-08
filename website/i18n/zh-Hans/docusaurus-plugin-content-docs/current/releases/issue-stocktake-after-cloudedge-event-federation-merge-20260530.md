# Issue 盘点 — CloudEdge SAM + Event Federation 合并后 (2026-05-30)

状态: 只读盘点。未进行 issue 的关闭、评论、标记或创建。
以下所有操作均为供 orchestrator/用户应用的 **建议**。

## 概述

PR #54 (`event-federation → main`) **已合并** (合并提交 `baeaff16`)。作为 `cloudedge-mvp` 的严格超集, 将 CloudEdge/SAM + Event Federation Phase 1 / 1.5 / 2 / 3 以 **experimental** 身份落地 — 无发布标签, 不推荐作为稳定版。PR #49 (`cloudedge-mvp → main`) 已作为被 #54 **取代而关闭**。

4 个 issue 保持开放 (#50、#51、#52、#53), 全部属于 CloudEdge SAM, 且都带有现已过时的 `branch cloudedge-mvp` 标签。其中:

- **#53 和 #50 事实上已解决** — 由已合并的 zone 无关 PMTU/MSS clamp (提交 `3c540656`) 完成。两个 codex OCI x PVE 重测评论确认 PASS (routerd_mss 存在, MSS 1300, doctor hybrid PASS, ping/SSH/scp 全部通过)。可以关闭。
- **#52 部分已解决** — 已合并的 `doctor hybrid` 可 *检测/警告* reject-all FORWARD/INPUT 主机防火墙, 但文档成果物 (将 OCI 镜像防火墙引导记入 SAM how-to) 是剩余的开放部分。作为文档后续跟进保持开放, 或如判断 doctor 警告已足够则附注关闭。
- **#51 (wizard OCI provider) 不受合并影响** — wizard 是实验室原型, 非核心; OCI provider 生成未添加。保持开放; Phase 4.1 的自然候选。

开放或已关闭的 issue 均不阻塞 Phase 4.0 最小权限 plugin 上下文框架 (Plugin 上下文 allowlist + secret 脱敏)。该工作是全新领域, 应作为 **新** issue 提交。

## 已合并基线

- main = `baeaff16` (PR #54 合并提交)。
- PR #54 合并 2026-05-30T12:20 — "Experimental: CloudEdge SAM + Event Federation Phase 1–3"。
- PR #49 关闭 (被 #54 取代)。
- Experimental: **无发布标签**, 未升级为稳定版。
- 相关已合并提交:
  - `3c540656` — SAM 转发路径的 zone 无关 PMTU/MSS clamp (#53) + doctor hybrid PMTU/防火墙检查 (#52)。影响 `pkg/render/mtu.go`、`cmd/routerctl/doctor.go`、golden SAM fixture、`docs/adr/0006-event-federation.md`。
  - `713233b0` — OCI x PVE SAM 干净冒烟测试 + 3 云对等记录。
  - Event Federation Phase 1→3 块 (`9c785db8` … `515fe7e8`), Phase 2/3 冒烟测试证据
    (`docs/releases/evidence/cloudedge-event-federation-{transport,subscription}-20260530.md`)。
- 已关闭的上下文 issue: #41–#48 (以及更早的 #2–#40) 在 SAM/先前工作期间关闭。特别是
  #12 ("MSS clamp can raise lower MSS / ignores source iface MTU") 和 #9 ("routerd_mss reported
  as orphan") 是 #50/#53 背后的历史 MSS 谱系; #42 ("forwarded /32 dropped by
  FORWARD policy — doctor visualize") 和 #48 ("doctor hybrid classify FORWARD skip reasons") 是
  #52 doctor 工作的已关闭前身。

## Issue 分类表

| # | 标题 | 当前状态 | #54 合并的影响 | 建议操作 | 可关闭? |
|---|---------|-----------------|-----------------|---------------|--------------|
| 53 | SAM OCI: TCP/SSH stalls after ping without MSS handling | OPEN, 无标签; codex 重测评论 = PASS | 由 `3c540656` **解决** (zone 无关 clamp → MSS 1300; OCI 重测 PASS) | **关闭** (done-by-main-merge) | **是** |
| 50 | SAM: surface/derive PMTU/MSS for wg-hybrid delivery paths | OPEN, `enhancement` + `branch cloudedge-mvp`; codex 重测 = PASS | 由 `3c540656` **解决** (clamp 导出 + `doctor hybrid` PMTU/MSS 警告); OCI 重测 PASS | **关闭** (done-by-main-merge); 关闭前/时变更标签 | **是** |
| 52 | SAM OCI: Ubuntu image iptables rejects WireGuard/FORWARD | OPEN, `documentation` + `branch cloudedge-mvp`; codex 重测 = doctor 警告 + 实验室引导 | **部分**: doctor 警告 (`3c540656`); **文档 how-to** 部分仍开放 | **保持** (文档后续跟进) 或附注关闭; 变更标签 | 否 (文档部分) |
| 51 | cloudedge-sam wizard: add OCI provider support | OPEN, `enhancement` + `branch cloudedge-mvp` | **无影响**: wizard 是实验室原型, 无核心变更; OCI provider 生成未添加 | **保持** (仍然相关 / Phase 4.1 候选); 变更标签 | 否 |

分类映射:
- **done-by-main-merge**: #53、#50
- **docs-i18n / 文档后续跟进**: #52 (剩余文档部分)
- **still-relevant / phase4.1-follow-up**: #51, 以及 #52 的 doctor-FORWARD-pattern 扩展
- **phase4.0-blocker**: 无
- **superseded-by-#54**: PR #49 (已关闭); issue 无
- **obsolete-duplicate**: 无

## 建议关闭 (草案评论)

### #53 — 作为 done-by-main-merge 关闭
> 由 `3c540656` 解决 (PR #54 → main `baeaff16` 合并)。PMTU/MSS clamp 被 FirewallZone 门控; SAM 作为无 zone 的转发平面因此未导出 clamp, 在 OCI 的低 PMTU underlay 下 ICMP 通过但 TCP 黑洞。修复使 FirewallZone 无关、接口类型无关的 MSS clamp 针对 RemoteAddressClaim 传递路径使用有效 overlay MTU (约 1392 inner → MSS 1300) 导出。OCI x PVE 重测 PASS: `routerd_mss` 两侧存在 (MSS 1300), `doctor hybrid` PASS, 双向 ping/SSH (源地址保留) 和 100MiB scp x3 全部通过; 3 云干净对等。关闭。(Experimental — 无发布标签。)

### #50 — 作为 done-by-main-merge 关闭
> 由 `3c540656` 解决 (PR #54 → main `baeaff16`)。SAM 传递路径现在导出有范围的 TCP MSS clamp, `doctor hybrid` 显示 PMTU/MSS 态势 (SAM 传递路径无 clamp 时警告) — 正是此处要求的 2 个行为。先前 `routerd_mss` 不存在的 OCI 重测输出 MSS 1300, 双向 ping/SSH/scp 通过。关闭。(标签变更备注: `branch cloudedge-mvp` 已过时 — 分支已合并, PR #49 已关闭。)

### #52 — 作为文档后续跟进保持开放 (或附注关闭)
> 由 `3c540656` (PR #54 → main `baeaff16`) 部分处理: `doctor hybrid` 现在检测并警告阻塞 wg/overlay 转发的 reject-all FORWARD/INPUT 主机防火墙, 不自动修改, 显示所需的主机配置。重测确认: 引导前 doctor 警告, 应用范围限定的实验室 allow 规则后 PASS。**仍开放**: 将 OCI Ubuntu 镜像防火墙引导前提条件 (UDP/51820 INPUT, FORWARD `<vnic> <-> wg-hybrid`) 记入 CloudEdge SAM how-to。作为文档任务保持开放; 标签变更为仅 `documentation`。

### #51 — 保持开放 (Phase 4.1 候选)
> PR #54 未处理 — cloudedge-sam wizard 是实验室原型, 合并中未向核心添加 OCI provider 生成。保持开放。自然归入 Phase 4.1 provider actionPlan plugin 工作 (aws/azure/oci provider profile 生成)。标签变更: 移除 `branch cloudedge-mvp`。

## 建议标签变更 — `branch cloudedge-mvp` 已过时

4 个开放 issue (#50、#51、#52、#53) 全部带有 `branch cloudedge-mvp`。该分支已通过 PR #54 合并至 main, 对应的 PR #49 已关闭, 因此标签不再指向实际存在的分支。

建议 (此处 **不** 应用):
- #50、#53: 关闭时移除 `branch cloudedge-mvp`。
- #51: 移除 `branch cloudedge-mvp` → 保留 `enhancement` (未来如引入 Phase 4.1 / cloudedge 跟踪标签则添加)。
- #52: 移除 `branch cloudedge-mvp`, 保留 `documentation`。

建议引入稳定的 `cloudedge` 或 `event-federation` 标签以替代分支范围的标签用于未来跟踪。

## 建议的新/后续跟进 issue (草案 — 未创建)

1. **i18n: Event Federation how-to + 参考的 ja/zh 翻译**
   按照文档区域设置策略, 将 event-federation-subscription how-to 和 federation 参考页面翻译为 ja (正本) 和 zh-Hans/zh-Hant。Phase 3 合并后目前仅有英文版。(新 issue; 无现有匹配。)

2. **FreeBSD rc.d 的 `routerd-eventd` supervision**
   Phase 2 通过 controller/systemd 添加了 EventGroup 自动 supervision (`1791cd5a`)。为 FreeBSD 路由器 (router04 对等) 添加 FreeBSD rc.d 对应物使 `routerd-eventd` 被 supervised。(新 issue; 无现有匹配。)

3. **EventSubscription batchWindow / debounce 精确定时器**
   Phase 3 的 EventSubscriptionController 为 poll + dedup; 添加精确的 debounce/batchWindow 定时器使突发事件在 plugin 调用前确定性合并 (与 ADR 0006 的迟滞 / 防抖不变量相关)。(新 issue。)

4. **Observer 自捕获不变量 (Phase 4 循环防止)**
   强制 ADR 0006 的不变量: 路由器不重新 emit 自身捕获地址的事件。在 provider plugin 开始修改云状态之前, 在 observe→federate 路径上添加回归测试/守卫。(新 issue; Phase 4 前提条件。)

5. **实验室清理: router03 / router05 保留 `515fe7e8` 二进制文件**
   Phase 3 实验室冒烟测试的二进制文件仍部署在 router03/router05 上。重新部署为已合并 main 的工件 (或推荐稳定构建), 跟踪以确保实验室路由器不滞留在 experimental 提交上。(新 issue; 实验室清理。)

6. **Phase 4.1: Provider actionPlan plugin (aws/azure/oci) — dry-run**
   实现将 RemoteAddressClaim 转换为 provider API 调用 (AWS/Azure/OCI 辅助 IP 分配) 的 provider actionPlan plugin, 从 dry-run/observe-only 开始。**包含 #51** (wizard OCI provider 生成) — 不重复提交, 将 #51 作为 OCI 切片链接。(新 issue; Phase 4.1。)

7. **Phase 4.0: Plugin 上下文 allowlist + secret 脱敏**
   最小权限 plugin 上下文框架。脱敏策略 A: 脱敏内联 secret, 省略 secret 文件路径, 省略 `SecretValueSourceSpec`, 不暴露完整 `router.yaml`, 不暴露 provider 凭据, 上下文层不进行 provider mutation。这是所有 provider mutation plugin 的 **Phase 4.0 阻塞项**。(新 issue; 无现有匹配 — 全新领域。)

## Phase 4.0 阻塞项 (显式)

**4 个开放 issue (#50–#53) 均不阻塞 Phase 4.0** (最小权限 plugin 上下文 allowlist + secret 脱敏; 防止意外 provider mutation / 凭据泄露)。对已关闭 issue (#2–#48) 的扫描也 **未发现** 关于 plugin 上下文、secret 泄露、凭据脱敏的 issue。Phase 4.0 框架是全新工作, 应在 provider actionPlan plugin (Phase 4.1) 被允许执行 mutation 之前以上述草案 #7 提交。

## Phase 4.1 候选

- **#51** — wizard OCI provider 支持 → 输入 provider profile 生成; 包含在 Phase 4.1 provider actionPlan plugin issue (草案 #6) 中。
- **#50 / #52 / #53** — 已解决, 但其 PMTU/防火墙 *provider 上下文* 知识 (有效 overlay MTU, 主机 FORWARD 态势) 为 Phase 4.1 provider plugin 需要通过 Phase 4.0 上下文 allowlist 呈现的数据提供了启示。无需重新打开; 作为设计输入引用。

## 无稳定升级 / 无发布标签

CloudEdge SAM + Event Federation 工作通过 PR #54 **仅以 experimental** 身份落地到 main。**无发布标签**, 未 **升级** 为稳定版。发布标签由用户判断, 不在此盘点范围内。
