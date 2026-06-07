---
title: 貢献する
---

# 貢献する

![Diagram showing the routerd contribution flow from host-facing change scope through local checks, schema checks, example validation, website build, shellcheck, and license agreement](/img/diagrams/contributing.png)

routerd はプレリリースのルーター制御プレーンです。
貢献は歓迎します。
ただし、ネットワーク、ファイアウォール、経路、インストーラー、
ホストサービス管理に関わる変更は慎重に確認します。

正式な貢献ガイドは repository root にあります。

- [`CONTRIBUTING.md`](https://github.com/imksoo/routerd/blob/main/CONTRIBUTING.md)

## ローカル確認

pull request を開く前に次を実行します。

```sh
make test
make check-schema
make validate-example
make website-build
```

shell script を変更した場合は、変更した script に対して `shellcheck` も
実行します。

## pre-commit フック

routerd には任意で使える pre-commit hook があります。

```sh
cp scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

この hook は commit の前に Go テストと schema 確認を実行します。

## ドキュメントの文体・用語

ドキュメントを書くときは次の文体に合わせます。ページごとに表現がぶれると読み手が混乱するためです。

- 文体は **です・ます調**で統一します。
- **一文一義**を心がけ、長い文は分割します。
- **受動態を避け**、能動態で書きます（例: 「設定が適用されます」→「routerd が設定を適用します」）。
- 「**〜することができます**」は「**〜できます**」に短くします。
- **英語語順の直訳を避けます**（主語・修飾の順序を日本語として自然に並べ替えます）。
- 専門用語は [用語集](concepts/glossary.md) に従い、**初出のみ「日本語（English）」**の形で英語を併記します。
- コマンド・フィールド名・`Kind` などの識別子は原文（英数字）のままコードスパンで書きます。

訳語の追加・変更が必要になったら、まず [用語集](concepts/glossary.md) を更新してから各ページに反映してください。

## ライセンス

routerd は BSD 3-Clause License で配布します。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

この repository へ貢献する場合、その貢献は、別の明記がない限り同じ
ライセンスで提供されるものとします。
