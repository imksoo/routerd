---
title: 開発時の確認
---

# 開発時の確認

routerd は 2 つの自動化経路を分けています。

- CI workflow は通常の push と pull request を確認します。
- release workflow は release tag を push した後にリリースアーカイブを生成します。

release workflow は複数の OS と CPU アーキテクチャーを対象にします。
また、GitHub Release の成果物も公開します。
そのため、通常の CI とは分けています。

## CI workflow

`.github/workflows/ci.yaml` は branch push と pull request で動きます。
Ubuntu runner を使い、レビュー前に緑に保つべき範囲を確認します。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

CI workflow はリリース成果物を公開しません。
リリースアーカイブは、日付ベースの tag で `Release` workflow が生成します。

## pre-commit hook

リポジトリには任意で使える pre-commit hook を含めています。

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

有効にすると、`git commit` の前に次の確認を実行します。

```sh
go test ./...
make check-schema
```

どちらかが失敗すると commit は止まります。
schema の差分やテスト失敗を CI の前に検出できます。

緊急でローカル commit が必要な場合は、次の環境変数を指定します。

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

後続の修正が明確な場合だけ使ってください。
branch を push した後は CI が実行されます。
