---
title: 貢献する
---

# 貢献する

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

## pre-commit hook

routerd には任意で使える pre-commit hook があります。

```sh
cp scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

この hook は commit の前に Go テストと schema 確認を実行します。

## ライセンス

routerd は BSD 3-Clause License で配布します。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

この repository へ貢献する場合、その貢献は、別の明記がない限り同じ
ライセンスで提供されるものとします。
