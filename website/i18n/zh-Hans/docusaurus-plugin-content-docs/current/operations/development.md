---
title: 开发检查
---

# 开发检查

routerd 将两种自动化路径分开。

- CI workflow 检查普通 branch push 与 pull request。
- release workflow 在 release tag push 后生成 release archive。

release workflow 会构建多个 OS 与 CPU architecture 的 archive，并发布 GitHub Release asset。
因此它和普通 CI 分离。

## CI workflow

`.github/workflows/ci.yaml` 会在 branch push 与 pull request 时执行。
它使用 Ubuntu runner，检查 review 前应保持成功的项目。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

CI workflow 不发布 release artifact。
release archive 只由 date-based tag 触发的 `Release` workflow 生成。

## pre-commit hook

repository 包含可选的 pre-commit hook script。

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

启用后，`git commit` 前会执行：

```sh
go test ./...
make check-schema
```

任一命令失败时，commit 会停止。
这可在 CI 前发现 schema drift 与 test failure。

紧急需要本地 commit 时，可指定下列环境变量。

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

只有在后续修正已明确时才使用。
branch push 后 CI 仍会执行。
