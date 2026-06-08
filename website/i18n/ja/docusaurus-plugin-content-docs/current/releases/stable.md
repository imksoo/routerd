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
| バージョン | **v20260608.0642** |
| 位置づけ | 推奨安定版（v20260528.2308 を置き換え。ADR 0014 による CLI 体系の再設計 — `routerd` をデーモン専任、`routerctl` を管理 CLI に分離。OpenRC 管理の信頼性向上、DNS リゾルバの VRRP VIP 待ち受け対応、forcefrag の prerouting 移動、BGP peer watch の安定化） |
| 稼働実績 | lab 環境（router06/router07/k8s-rt-01/k8s-rt-02）と本番ルーター（homert02）で検証済み。Cloud VM テスト（lab + k8s）全 PASS。12 の issue を解消し 12 の PR をマージ |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）、CI と Release ワークフローをすべて通過 |

## v20260608.0642 を推奨する理由

この版は v20260528.2308 のすべての本番安全特性を継承した上で、**CLI 体系の再設計**（ADR 0014）と **OpenRC / init スクリプトの信頼性向上**を中心に 40 コミットの改善を加えています。

### ADR 0014 — CLI 体系の再設計

routerd の CLI を「デーモン」と「管理ツール」に明確に分離しました。

- **`routerd`** はデーモン専任。唯一のサブコマンドは `routerd serve`。
- **`routerctl`** が管理 CLI。`validate` / `plan` / `apply` / `doctor` / `get` / `describe` / `status` / `ledger` / `dns-queries` / `traffic-flows` などのすべての管理操作を担います。
- 旧来の `routerd apply` / `routerd validate` / `routerd run` などは廃止。`--once` フラグも撤廃しました。
- ドキュメントとスクリプトのコマンド参照をすべて新しい verb 体系に更新しました（#254–#262）。

### OpenRC / init スクリプトの信頼性向上

FreeBSD および OpenRC 環境での init スクリプト管理に 6 件の修正を適用しました。

- **OpenRC DNS リゾルバの二重管理を解消**（#306）— 以前は `routerd serve` と OpenRC の両方が DNS リゾルバを管理しようとし、二重起動が発生していました。
- **OpenRC アップグレード時に古い `routerd serve` を停止**（#311, #313）— アップグレード中に旧プロセスが残存する問題を修正。
- **OpenRC で managed helper を再起動時に掃除**（#315）— orphan helper プロセスの蓄積を防止。
- **DNS リゾルバの helper supervision**（#283）— OpenRC が DNS リゾルバの helper プロセスを正しく監視・起動するようにしました。
- **残留 helper の更新**（#280）と **OpenRC 再起動の nodeps 化**（#278）— アップグレード時のサービス依存関係の問題を解消。

### ネットワーク機能の改善

- **DNS リゾルバが VRRP VIP で待ち受け可能に**（#319）— `IP_FREEBIND` / `IPV6_FREEBIND` ソケットオプションにより、まだ割り当てられていない VIP アドレスでもリスナーを開始できます。VRRP backup ノードで DNS サービスを事前に起動できるようになりました。
- **forcefrag の DF クリアを prerouting フックに移動**（#328）— 従来 forward フックで `oifname` を使っていましたが、prerouting では出力インターフェースが未確定のため `fib daddr oifname` でルーティングテーブルを参照する方式に変更。MSS clamp が正しく動作しない場合がありましたが、これで解消しました。
- **BGP peer watch の不要更新を抑止**（#329）— `desiredPeerMatches()` が `reflect.DeepEqual` を使っていたため、`dynamicExportPrefixes` の変化や GracefulRestart の書式差異（`"2m"` vs `"120s"`）で毎回 `UpdatePeer` が走っていました。安定比較関数 `stableDesiredPeerEqual` を導入し、semantically 同一の設定では更新を抑止します。
- **`routerd serve` が起動時に loopback を有効化**（#321）— Live ISO やコンテナ環境で `lo` が down のままの場合に自動で `ip link set lo up` を実行します。

### インストーラーの改善

- **bootstrap installer が一時ディレクトリを確実にクリーンアップ**（#324）— `exec sh ./install.sh` だと EXIT トラップが発火しない問題を修正。
- **installer の apply state 警告を修正**（#327）— `routerctl get status` の出力形式を `-o json` に変更し、`lastApplyTime` の判定を正確にしました。
- **BGP peer state の watch による status 即時更新**（#304）— BGP セッション状態の変化を即時に status に反映。
- **VRRP が inactive の keepalived を再起動**（#299）— VRRP のフェイルオーバーが正しく動作しないケースに対応。

### ドキュメント

- **日本語正本翻訳 37 記事、中国語翻訳 80 記事を追加**（#322）— ADR / explainer / how-to / ops / reference / releases / evidence / slides の全カテゴリで日本語を正本として整備し、zh-Hans / zh-Hant に翻訳しました。
- **全ドキュメントダイアグラムを gpt-image-2 で再生成**（#261）— 統一的なビジュアルスタイルに更新。

### v20260528.2308 からの継承事項

v20260528.2308 の本番安全特性はすべて継承しています。

- fd 漏洩修正（#39 SQLite ledger、#40 Unix socket / BGP gobgp client）
- heap 漏洩修正（OTel instrument singleton、bounded reverse DNS cache）
- `routerctl doctor runtime` によるリソース継続監視
- BGP セッションの routerd アップグレード跨ぎ
- `doctor dslite` の selectedSource 整合
- ゲートウェイ健全性の独立画面
- `install.sh` の即時失敗（ペイロード欠落検出）
- シークレット伏字化
- `ManagementAccess` 適用ガード
- 機械可読 `routerctl doctor`（`-o json`）

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
