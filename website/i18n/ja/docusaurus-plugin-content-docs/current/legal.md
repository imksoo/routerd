---
title: 法務と再配布
---

# 法務と再配布

![Diagram showing legal and redistribution checks from third-party license inventory through release archives, live ISO aggregate licensing, SPDX source headers, copyleft review, and release checklist](/img/diagrams/legal-redistribution.png)

routerd 本体は BSD 3-Clause License で配布します。
全文はリポジトリルートの `LICENSE` にあります。

routerd の著作権表示は次の通りです。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

このページは、routerd のリリースアーカイブとライブ ISO を再配布する際の、
実務上の確認事項をまとめたものです。法的助言ではありません。

## routerd バイナリ

routerd バイナリは、このリポジトリの Go ソースコードからビルドします。
リリース前に次を実行します。

```sh
make third-party-licenses
```

このコマンドは `THIRD_PARTY_LICENSES.md` を再生成し、次の情報を一覧化します。

- routerd バイナリにリンクする Go モジュール
- 検出したライセンステキストの種類
- ライセンスファイル名
- モジュールのソース URL

現在の監査処理は、Go モジュールのライセンスファイルから GPL、LGPL、AGPL の
テキストを検出します。リンクする Go モジュールとして見つかった場合は、リリースを
止めます。その上で、routerd バイナリのライセンスを変える必要があるか、依存を外す
べきかを確認します。

ソースファイルには次のような SPDX 識別子を付けます。

```text
SPDX-License-Identifier: BSD-3-Clause
```

このヘッダーは routerd ソースのライセンスを示します。
同梱するツール、Go モジュール、その他の第三者コンポーネントの
ライセンスを変えるものではありません。
それらは `THIRD_PARTY_LICENSES.md` に一覧化します。

## リリースアーカイブ

リリースアーカイブには次を入れます。

- routerd バイナリ
- インストーラースクリプト
- systemd または rc.d のサービステンプレート
- サンプル設定
- `share/doc/LICENSE`
- `share/doc/THIRD_PARTY_LICENSES.md`

リリースアーカイブを再配布する場合は、これらのファイルを一緒に配布します。

## ライブ ISO

ライブ ISO は集合的な配布物で、次を組み合わせています。

- routerd バイナリとスクリプト
- Ubuntu のベースファイル
- dnsmasq、nftables、WireGuard tools、ppp、iproute2、chrony、
  tcpdump などのパッケージ

これらのパッケージは、それぞれの upstream ライセンスを保ちます。
一部は GPL ライセンスです。
ライブ ISO 全体を 1 つの GPL 著作物として再ライセンスする扱いではありません。

ライブ ISO では、次の場所から routerd の通知を確認できます。

```text
/usr/share/licenses/routerd/LICENSE
/usr/share/licenses/routerd/THIRD_PARTY_LICENSES.txt
```

## リリースチェックリスト

リリース前に確認します。

1. `make third-party-licenses` を実行します。
2. Go モジュールの copyleft チェックで、GPL、LGPL、AGPL のモジュールがないことを確認します。
3. GPL 系ライセンスが、分離して配布するパッケージや外部ツールにだけ
   現れることを確認します。
4. 通常のテスト、スキーマ、example、website、アーカイブ、ライブ ISO のチェックを実行します。
5. リリースアーカイブに `share/doc/LICENSE` と
   `share/doc/THIRD_PARTY_LICENSES.md` があることを確認します。
6. ライブ ISO に `/usr/share/licenses/routerd/` があることを確認します。

依存関係が大きく変わった場合は、タグを作る前にこのページと、生成済みの
ライセンス一覧を見直します。
