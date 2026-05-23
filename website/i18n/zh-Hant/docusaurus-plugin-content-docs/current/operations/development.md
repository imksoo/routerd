---
title: 開發時確認
---

# 開發時確認

routerd 將兩條自動化流程分開管理。

- CI workflow 負責確認一般的 push 與 pull request。
- release workflow 在推送 release tag 後產生發布封存檔。

release workflow 涵蓋多種 OS 與 CPU 架構，並公開 GitHub Release 的成果物。
因此與一般 CI 分開維護。

## CI workflow

`.github/workflows/ci.yaml` 在分支 push 與 pull request 時執行。
使用 Ubuntu runner，確認在進入程式碼審查前應保持綠燈的範圍。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

CI workflow 不公開發布成果物。
發布封存檔由日期格式的 tag 觸發 `Release` workflow 產生。

## pre-commit hook

儲存庫中附有可選用的 pre-commit hook。

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

啟用後，`git commit` 執行前會進行以下確認。

```sh
go test ./...
make check-schema
```

任一項失敗，commit 即中止。
可在 CI 之前提早發現 schema 差異或測試失敗。

若緊急情況下需要在本地 commit，可指定以下環境變數。

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

請僅在後續修正明確的情況下使用。
push 分支後，CI 仍會執行。
