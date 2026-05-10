---
title: 法務と再配布
---

# 法務と再配布

routerd 本体は BSD 3-Clause License で配布します。
全文は repository root の `LICENSE` にあります。

routerd の著作権表示は次の通りです。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

このページは、routerd release archive と routerd live ISO を再配布する際の
実務上の確認事項をまとめたものです。法的助言ではありません。

## routerd バイナリ

routerd バイナリは、この repository の Go source code から build します。
release 前に次を実行します。

```sh
make third-party-licenses
```

このコマンドは `THIRD_PARTY_LICENSES.md` を再生成します。
次の情報を一覧化します。

- routerd バイナリに link される Go module
- 検出した license text の種類
- license file 名
- module source URL
- live ISO で使う Alpine package
- Alpine package の license metadata と upstream URL

現在の audit path は、Go module の license file から GPL、LGPL、AGPL text を
検出します。link される Go module として見つかった場合は release を止めます。
その上で、routerd binary license の変更が必要か、依存を外すべきかを確認します。

ソースファイルには次のような SPDX 識別子を付けます。

```text
SPDX-License-Identifier: BSD-3-Clause
```

このヘッダーは routerd ソースのライセンスを示します。
同梱するツール、Alpine package、Go module、その他の第三者 component の
ライセンスを変更するものではありません。
それらは `THIRD_PARTY_LICENSES.md` に一覧化します。

## Release archive

release archive には次を入れます。

- routerd バイナリ
- installer script
- systemd または rc.d service template
- sample configuration
- `share/doc/LICENSE`
- `share/doc/THIRD_PARTY_LICENSES.md`

release archive を再配布する場合は、これらのファイルを一緒に配布します。

## Live ISO

live ISO は aggregate distribution です。
次を組み合わせています。

- routerd バイナリと script
- Alpine Linux base file
- dnsmasq、nftables、WireGuard tools、ppp、iproute2、chrony、
  tcpdump などの Alpine package

これらの Alpine package は、それぞれの upstream license を保ちます。
一部は GPL license です。
live ISO 全体を 1 つの GPL work として再ライセンスする扱いではありません。

live ISO では次の場所から routerd の通知を確認できます。

```text
/usr/share/licenses/routerd/LICENSE
/usr/share/licenses/routerd/THIRD_PARTY_LICENSES.txt
```

Alpine package の source 情報は、Alpine package repository、APKBUILD、
`THIRD_PARTY_LICENSES.md` にある upstream URL から確認できます。

## Release checklist

release 前に確認します。

1. `make third-party-licenses` を実行します。
2. Go module copyleft check で GPL、LGPL、AGPL module がないことを確認します。
3. GPL 系 license が、分離配布される Alpine package や外部 tool にだけ
   現れることを確認します。
4. 通常の test、schema、example、website、archive、live ISO check を実行します。
5. release archive に `share/doc/LICENSE` と
   `share/doc/THIRD_PARTY_LICENSES.md` があることを確認します。
6. live ISO に `/usr/share/licenses/routerd/` があることを確認します。

依存関係が大きく変わった場合は、tag を作る前にこのページと生成済み
license inventory を見直します。
