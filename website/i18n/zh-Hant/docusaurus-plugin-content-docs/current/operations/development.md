---
title: 開發檢查
---

# 開發檢查

routerd 將兩種自動化路徑分開。

- CI workflow 檢查一般 branch push 與 pull request。
- release workflow 在 release tag push 後產生 release archive。

release workflow 會建置多個 OS 與 CPU architecture 的 archive，並發布 GitHub Release asset。
因此它和一般 CI 分離。

## CI workflow

`.github/workflows/ci.yaml` 會在 branch push 與 pull request 時執行。
它使用 Ubuntu runner，檢查 review 前應保持成功的項目。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

CI workflow 不發布 release artifact。
release archive 只由 date-based tag 觸發的 `Release` workflow 產生。

## pre-commit hook

repository 包含可選的 pre-commit hook script。

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

啟用後，`git commit` 前會執行：

```sh
go test ./...
make check-schema
```

任一命令失敗時，commit 會停止。
這可在 CI 前偵測 schema drift 與 test failure。

緊急需要本機 commit 時，可指定下列環境變數。

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

只有在後續修正已明確時才使用。
branch push 後 CI 仍會執行。
