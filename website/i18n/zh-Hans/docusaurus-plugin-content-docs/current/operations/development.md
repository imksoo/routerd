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

## CI workflow

`.github/workflows/ci.yaml` 在分支 push 与 pull request 时执行。
使用 Ubuntu runner，确认在进入代码审查前应保持绿灯的范围。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

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
