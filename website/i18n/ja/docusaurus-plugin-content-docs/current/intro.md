---
title: ドキュメント
slug: /
sidebar_position: 0
sidebar_label: 概要
---

# routerd ドキュメント

routerd は型付き YAML リソースから、Linux / NixOS / FreeBSD 上で動作する観測可能なルーターを作ります。目的に合うセクションから読んでください。

## 目的別

| やりたいこと | 出発点 |
| --- | --- |
| routerd とは何か、なぜあるのかを知る | [概要 → routerd とは](./concepts/what-is-routerd.md) |
| 初めてルーターを立てる | [チュートリアル → はじめに](./tutorials/getting-started.md) |
| 特定の配置課題を解く | [How-to ガイド](./how-to/multi-wan.md) |
| リソース種別やフィールドを引く | [リファレンス → リソース API](./reference/api-v1alpha1.md) |
| 稼働中のルーターを運用する | [運用 → Reconcile](./operations/reconcile.md) |
| 難しい事例の背景メモを読む | [ナレッジベース](./knowledge-base/dhcpv6-pd-clients.md) |
| 何が変わったか追う | [リリース → 変更履歴](./releases/changelog.md) |

## セクション一覧

- **概要 (Concepts)** — ビジョン、設計思想、リソースモデル、所有のセマンティクス
- **チュートリアル (Tutorials)** — インストール、最初のルーター、WAN/LAN サービス、基本ファイアウォール、NixOS クイックスタート
- **How-to** — マルチ WAN、フレッツ初期設定、PVE オーバーレイ、OpenTelemetry 送信、トラブルシューティング
- **ナレッジベース (Knowledge base)** — 実環境からの現場メモ (DHCPv6-PD クライアント、NTT NGN PD 取得)
- **リファレンス (Reference)** — リソース API、制御 API、プラグインプロトコル、対応プラットフォーム、所有ルール
- **運用 (Operations)** — Reconcile と削除、状態データベース、ホストインベントリ
- **設計ノート (Design notes)** — アーキテクチャ上の未解決事項と設計の根拠
- **リリース (Releases)** — 変更履歴
