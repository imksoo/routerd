---
title: 开发时确认
---

# 开发时确认

![Diagram showing development checks split between local pre-commit tests, CI pull request validation, and release workflow archive publishing](/img/diagrams/operations-development.png)

routerd 将两条自动化流程分开管理。

- CI workflow 负责确认一般的 push 与 pull request。
- release workflow 在推送 release tag 后生成发布归档文件。

release workflow 涵盖多种 OS 与 CPU 架构，并公开 GitHub Release 的产物。
因此与一般 CI 分开维护。


## 测试与 harness 设计的强制规范

适用于单元测试、离线 fixture、runtime smoke、release qualification，以及可复用的旧 helper、模板和复制来源，包括目前没有 caller 的代码。修改时必须说明产品要求、必要直接证据和观测限制。

- 保留观测与诊断日志，与产品合否分离。没有具体产品要求，不得将 journal、进展计数器、latency 统计或整机诊断变成合否条件。
- PASS／产品 FAIL 必须依据已完成且可归属到目标的直接证据。API／SSH 波动、采集失败、缺失或损坏输入、guest command 未完成和 timeout 应保持为观测或基础设施结果，不得转成零、无违规、成功拒绝或实测产品违规。
- 减少无依据的速度断言、重复 probe 和 gate。保留契约期限和停止挂起工作的有界 watchdog，并与产品性能测量区分。
- 删除临时数据前保留失败的原始尝试、来源身份、exit、时间和 cleanup 结果。仅清理本次拥有的资源，不得隐藏失败或删除产品证据。
- 不得未经原因调查重复失败的实机试验。达到现有 retry、时间或费用预算时停止，报告失败、不确定性、剩余预算和修复方案后再继续。仅对有依据的失败或缺失观测进行有界重采集，并保留每次尝试。
- 审计实际入口、选择的来源路径和 SHA、依赖及最终结果传播。候选或 mock 验证只证明其范围，不能证明运行入口已选择候选或真实转发成功。
- 区分实现、准备候选、运行代码、保存原本重放、刻意修改的 fixture 和未验证行为。保留原本负例，不得把历史失败改写为 PASS。
- 复用旧代码或尚未连接的代码时也必须遵守。封存原本保持不变，修复可复用来源；不得以当前没有 caller 为由忽略缺陷。

这些规范不放宽真实功能要求、安全与所有权检查或费用限制。缺少必要证据不能建立 PASS。保留正常通信和直接违规的证明，只删除与诊断之间无依据的耦合。

正确例：curl exit 0／HTTP 200 且具备必要 body 与路径证据时，可将可选 latency 缺失记为诊断并 PASS。错误例：WireGuard 读取失败证明禁止 AllowedIP 不存在；invalid apply 的 timeout／强制终止计为成功拒绝；同一网络栈 ping 成功证明隧道转发。

## CI workflow

`.github/workflows/ci.yaml` 在分支 push 与 pull request 时执行。
使用 Ubuntu runner，确认在进入代码审查前应保持绿灯的范围。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

当变更涉及 `webconsole/`、已签入的静态资源、共用 quality workflow 或 Makefile 时，CI 还会
执行 Web Console 专用关卡：`npm ci`、全部依赖及生产依赖的 high severity audit、TypeScript
typecheck、生产构建，以及生成资源的差异检查。

CI workflow 不公开发布产物。
发布归档文件由日期格式的 tag 触发 `Release` workflow 生成。

## pre-commit hook

仓库中附有可选用的 pre-commit hook。

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

启用后，`git commit` 执行前会进行以下确认。

```sh
go test ./...
make check-schema
```

任一项失败，commit 即中止。
可在 CI 之前提早发现 schema 差异或测试失败。

若紧急情况下需要在本地 commit，可指定以下环境变量。

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

请仅在后续修正明确的情况下使用。
push 分支后，CI 仍会执行。
