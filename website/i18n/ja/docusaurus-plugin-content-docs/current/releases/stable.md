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
| バージョン | **v20260608.2325** |
| 位置づけ | 推奨安定版（v20260608.1354 を置き換え。peersFrom/membersFrom による動的配布でゼロタッチの leaf 設定を実現） |
| 稼働実績 | k8s クラスター（10 ノード: 2 RR + 8 leaf、peersFrom + membersFrom + peer-group-sync すべて緑、フル verify 通過）、lab 環境（FreeBSD router01/04 アップグレード検証済み）、本番ルーター（homert02、validate pass）で検証済み。不具合 0 件 |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）、CI と Release ワークフローをすべて通過 |

## v20260608.2325 を推奨する理由

この版は v20260608.1354 のすべての特性を継承した上で、**peersFrom**、**membersFrom**、および **peer-group-sync** を追加し、SAM ファブリックのゼロタッチ leaf 設定を実現しています。

### peersFrom + SAMPeerGroup（#332, #333）

`SAMTransportProfile` に `spec.peersFrom` が追加され、`SAMPeerGroup` リソースを参照できるようになりました。union セマンティクス: `peersFrom` のメンバーを先に読み込み、静的な `peers` が `nodeRef` 単位で上書きします。RR ノードで `publishPeerGroup: true` を設定すると、`SAMPeerGroup` の `DynamicConfigPart` を自動生成します。

### ピアグループ同期（#334, #336）

WireGuard 内部ネットワーク上のポート 19652 で動作する軽量 HTTP サービスです。RR が `GET /v1/peer-groups` を提供し、leaf は WireGuard ピアを検出して一致するグループを自動取得します。手動で `SAMPeerGroup` を配布する必要はありません。

### MobilityMemberSet + membersFrom（#339, #340）

`MobilityMemberSet` Kind は、共有の識別情報のみのプールメンバー（`nodeRef`、`site`、`role`）を保持します。`MobilityPool.spec.membersFrom` でこれらを取り込むことで、leaf は自身の捕捉/検出の詳細だけをインラインに残し、O(N^2) の設定重複を削減します。`publishMemberSet: true` を設定すると、`GET /v1/member-sets` 経由でメンバーセットを生成・配布します。svnet1 設定で 78 行削減（2624 → 2546）。

### FreeBSD 旧フラグ互換（#337, #338）

廃止された `routerd serve` フラグ（`--observe-interval`、`--controller-chain*`）が `/etc/rc.conf` に残っていても、警告付きで受理・無視されるようになり、アップグレード失敗を防ぎます。

### v20260608.1354 からの継承事項

v20260608.1354 の全特性を継承: pair-stable アドレッシング、ADR 0014 CLI 再設計、およびそれ以前の全本番安全修正。

## 既知の観測（リリースを止めない事項）

- **`install.sh` 後に `routerd-bgp` が旧実行ファイルの inode のままで動く場合がある。** これは意図どおりです。`install.sh` はアップグレード時に `routerd-bgp` を自動再起動しないので、確立済みの BGP セッションと ECMP が routerd バイナリの更新をまたいで残ります。
- **`ManagementAccess` 未宣言の構成では `routerctl doctor mgmt` が SKIP になる。** これは稼働中の設定側の選択であり、リリースの欠陥ではありません。

:::warning アップグレード時の注意
- **v20260528.2308 から上げる場合:** ADR 0014 により CLI の verb 体系が変わりました。`routerd apply` → `routerctl apply`、`routerd validate` → `routerctl validate` など。サービスユニットやスクリプトで旧コマンドを使っている場合は書き直してください。`install.sh` が新しいサービスユニットを自動配置するため、systemd 管理下のユニットは自動で更新されます。
- **`install.sh` は必ず展開したリリースディレクトリに `cd` してから実行してください。**
- **v20260523.1542 以前から上げる場合:** `disabled:` フィールド（`enabled: false` を使用してください）と `--controller-chain*` / `--observe-interval` フラグは削除済みです。
- **DNS リゾルバーのサービスユニット化:** リゾルバーは `routerd-dns-resolver@<name>.service` として動きます。初回アップグレード時だけ短い DNS 瞬断が出ます。
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
