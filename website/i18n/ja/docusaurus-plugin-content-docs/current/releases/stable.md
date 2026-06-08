---
title: 安定版マイルストーン
sidebar_label: 安定版マイルストーン
sidebar_position: 0
---

# 安定版マイルストーン

routerd は `vYYYYMMDD.HHmm` 形式で頻繁にリリースしますが、その中から**本番運用に推奨できる版**を「安定版マイルストーン」として節目ごとに選びます。新しく導入するときは、このページで案内する版を使ってください。

## 現在の推奨版

| 項目 | 内容 |
| --- | --- |
| バージョン | **v20260608.1354** |
| 位置づけ | 推奨安定版（v20260608.0642 を置き換え。pair-stable SAM transport addressing — `addressingMode: pair-stable` によるコンパクトな leaf-spine 設定記述） |
| 稼働実績 | lab 環境（7 compact config 検証済み）、k8s クラスタ（10 ノード: 2 RR + 8 leaf、全 BGP Established、FIB 正常、connectivity pass）、本番ルーター（homert02、影響なし）で検証済み。不具合 0 件 |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）、CI と Release ワークフローをすべて通過 |

## v20260608.1354 を推奨する理由

この版は v20260608.0642 のすべての特性を継承した上で、**pair-stable SAM transport addressing** を追加しています。

### Pair-stable addressing（#330, #331）

`SAMTransportProfile` に `spec.addressingMode: pair-stable` が追加されました。inner prefix と canonical peer key の fnv64a ハッシュを用いた決定的な /31 スロット割り当てアルゴリズムです。

- **コンパクトな設定記述。** leaf ノードで `topologyNodeRefs` が不要になり、ノードごとのトポロジ宣言の繰り返しを排除。svnet1 設定で約 100 行削減。
- **トポロジ変更に対する安定性。** ノードの追加・削除で既存ピアのアドレスが再割り当てされません（ソート順に依存する `edge-index` との違い）。
- **後方互換。** 既存の `edge-index`（デフォルト）設定はそのまま動作。
- **衝突検出。** `routerd validate` / `routerctl validate` で /31 スロットのハッシュ衝突を設定時に検出。

### v20260608.0642 からの継承事項

v20260608.0642 の全特性を継承: ADR 0014 CLI 再設計、OpenRC 信頼性向上、DNS VRRP VIP 対応、forcefrag prerouting 修正、BGP peer watch 安定化、およびそれ以前の全本番安全修正。

## 既知の観測（リリースを止めない事項）

- **`install.sh` 後に `routerd-bgp` が旧 inode のままで動く場合がある。** これは意図どおりです。`install.sh` はアップグレード時に `routerd-bgp` を自動再起動しないので、確立済みの BGP セッションと ECMP が routerd バイナリの更新をまたいで残ります。運用者が Graceful Restart のタイミングで `systemctl restart routerd-bgp` を実行するまで、プロセスは旧 inode のバイナリを掴み続けます。
- **`ManagementAccess` 未宣言の構成では `routerctl doctor mgmt` が SKIP になる。** これは稼働中の設定側の選択であり、リリースの欠陥ではありません。

:::warning アップグレード時の注意
- **v20260528.2308 から上げる場合:** ADR 0014 により CLI の verb 体系が変わりました。`routerd apply` → `routerctl apply`、`routerd validate` → `routerctl validate` など。サービスユニットやスクリプトで旧コマンドを使っている場合は書き直してください。`install.sh` が新しいサービスユニットを自動配置するため、systemd 管理下のユニットは自動で更新されます。
- **`install.sh` は必ず展開したリリースディレクトリに `cd` してから実行してください。**
- **v20260523.1542 以前から上げる場合:** `disabled:` フィールド（`enabled: false` を使用してください）と `--controller-chain*` / `--observe-interval` フラグは削除済みです。
- **DNS リゾルバのサービスユニット化:** リゾルバは `routerd-dns-resolver@<name>.service` として動きます。初回アップグレード時だけ短い DNS 瞬断が出ます。
:::

## 「安定版」の意味と注意点

:::warning API はまだ v1alpha1 です
「安定版マイルストーン」は、**この版が本番運用に堪える品質である**ことを示すもので、**API（リソーススキーマ）の後方互換を約束するものではありません**。
:::

- routerd のリソース API は現在 **v1alpha1** です。リリース間で**破壊的変更が入ることがあります**。
- バージョンを上げるときは、後方互換に頼らず、**新しいスキーマに合わせて設定（YAML）を書き直す**前提で進めてください。
- 移行用の互換コードは持たない方針です。各版の変更点は [変更履歴](./changelog.md) を確認してください。

## 導入とアップグレード

導入手順は [導入とアップグレード](../install-and-upgrade.md) を参照してください。アップグレードは、推奨マイルストーン版を起点に行うことを勧めます。
