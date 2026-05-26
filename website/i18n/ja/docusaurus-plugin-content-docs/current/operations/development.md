---
title: 開発時の確認
---

# 開発時の確認

routerd は 2 つの自動化経路を分けています。

- CI ワークフローは通常の push と pull request を検証します。
- Release ワークフローはリリースタグを push した後にリリースアーカイブを生成します。

Release ワークフローは複数の OS と CPU アーキテクチャーを対象にします。
また、GitHub Release の成果物も公開します。
そのため、通常の CI とは分けています。

## CI ワークフロー

`.github/workflows/ci.yaml` はブランチへの push と pull request で動きます。
Ubuntu のランナーを使い、レビュー前に緑に保つべき範囲を確認します。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

CI ワークフローはリリース成果物を公開しません。
リリースアーカイブは、日付ベースのタグで `Release` ワークフローが生成します。

## pre-commit フック

リポジトリには任意で使える pre-commit フックを含めています。

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

有効にすると、`git commit` の前に次の確認を実行します。

```sh
go test ./...
make check-schema
```

どちらかが失敗するとコミットは止まります。
スキーマの差分やテスト失敗を CI の前に検出できます。

緊急でローカルコミットが必要な場合は、次の環境変数を指定します。

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

後続の修正が明確な場合だけ使ってください。
ブランチを push した後は CI が実行されます。
