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
| バージョン | **v20260525.0112** |
| 位置づけ | 推奨安定版（v20260523.1542 を置き換え） |
| 稼働実績 | 本番ルーター（homert02）で稼働中。BGP の 2-way ECMP を維持し、Graceful Restart により無瞬断でアップグレードできます |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）、CI と Release ワークフローを通過 |

## v20260525.0112 を推奨する理由

- **起動直後の DNS 不通がありません。** `DNSResolver` は依存がすべて解決するのを待たず部分起動するようになりました。すでに解決済みの listen アドレスと forward ソースで応答を開始し、残りが保留の間は `phase: Degraded` と `waiting` リストを報告し、DHCPv6 のプレフィックス委任が届くと `Applied` に収束します。以前の版は PD 待ちの起動直後に DNS を拒否していました。
- **BGP 制御プレーンの成果をすべて備えています。** FRR を使わず routerd 自前の `routerd-bgp` デーモンで eBGP peer を保持し、next-hop 書き換えの修正（#26）により第三者 next-hop を広告する上流でも 2-way ECMP を維持し、Alpine/OpenRC の live ISO でも `routerd-bgp` が OpenRC 下で起動します（#28）。
- **アップグレードが BGP を乱しません。** `install.sh` はバイナリ更新時に `routerd-bgp` を自動再起動しなくなり、routerd 更新をまたいで eBGP セッションと ECMP を維持します。
- **運用が容易になりました。** `routerd rollback --list` / `--to <generation>` で保存済みの設定世代を再適用でき、`routerctl set-log-level` でログ詳細度を実行時に変更でき、`routerctl describe` が Phase・Reason・Message と是正ヒントを表示します。
- **非 root での status 取得。** 読み取り専用の status ソケットは `root:routerd`・モード `0o660` で作成されるため、`routerd` グループに属する運用者は sudo なしで `routerctl status` を実行できます。
- **本番ルーター（homert02）で稼働**し、静的バイナリ（`CGO_ENABLED=0`）で配布、CI と Release ワークフローを通過しています。

:::warning v20260523.1542 以前からのアップグレード
このマイルストーンでは `disabled:` フィールド（`enabled: false` を使用）と、no-op の `--controller-chain*` / `--observe-interval` フラグを削除しました。`disabled:` を使っていた設定は書き直し、削除済みフラグを渡しているホストの service unit はアップグレード前に更新してください。
:::

## 「安定版」の意味と注意点

:::warning API はまだ v1alpha1 です
「安定版マイルストーン」は、**この版が本番運用に堪える品質である**ことを示すもので、**API（リソーススキーマ）の後方互換を約束するものではありません**。
:::

- routerd のリソース API は現在 **v1alpha1** です。リリース間で**破壊的変更が入ることがあります**。
- バージョンを上げるときは、後方互換に頼らず、**新しいスキーマに合わせて設定（YAML）を書き直す**前提で進めてください。
- マイグレーション用の互換コードは持たない方針です。各版の変更点は [変更履歴（Changelog）](./changelog.md) を確認してください。

## 導入とアップグレード

導入手順は [導入とアップグレード](../install-and-upgrade.md) を参照してください。アップグレードは、推奨マイルストーン版を起点に行うことを勧めます。
