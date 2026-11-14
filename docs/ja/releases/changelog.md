---
title: 変更履歴
---

# 変更履歴

routerd のリリース履歴です。形式は [Keep a Changelog](https://keepachangelog.com/ja/) に準拠します。
変更は「追加」「変更」「非推奨」「削除」「修正」「セキュリティ」に分類します。
ただし版番号は Semantic Versioning ではなく、日付と時刻に基づく `vYYYYMMDD.HHmm` 形式を使います。
ソフトウェアは v1alpha1 段階のため、リリース間で破壊的変更を含むことがあります。

## Unreleased

- ブラウザーのコンフィグ作成ツールを削除し、全リソース種類のYAML定義を含むMarkdown文書へ切り替えました。

## v20261114.0631

### Fixed

- IPIPのIPv4アドレス一覧が空の場合は未設定として扱い、初回起動時に設定を進められるよう修正しました。
- Live ISOに既存のcasper `noprompt` を指定し、再起動時のメディア取り外し確認による入力待ちをなくしました。

## v20261114.0607

### Changed

- 型付き設定参照、SQLite保存層、状態遷移、イベント処理、MTU選択、SAM入力復号、Linux IPIP観測を簡略化し、未使用の互換経路とテスト専用コードを削除しました。
- PDリースを単一の正本へ集約し、保存項目の定義を一本化しました。識別情報、期限、旧prefixは維持します。

### Fixed

- daemon再起動後に、一致する適用済み設定の世代表示を復元するよう修正しました。
- 設定変更時に旧EventRuleの保留タイマーを停止し、設定保存成功後に古いstatusを整理するよう修正しました。

[Release history](https://github.com/imksoo/routerd/releases)
