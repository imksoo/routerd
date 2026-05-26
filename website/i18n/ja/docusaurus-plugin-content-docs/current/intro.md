---
title: ドキュメント
slug: /
sidebar_position: 0
sidebar_label: 概要
---

# routerd ドキュメント

routerd は、型付きの YAML で書いた望ましい状態から、Linux / NixOS / FreeBSD 上で動くルーターを組み立てる宣言型のルーターです。設定を手続きで積み上げるのではなく、欲しい状態を宣言すると、routerd が実機をその状態へ近づけます。

目的に合うところから読んでください。

:::tip 推奨の安定版
新規に導入するなら、推奨の安定版マイルストーン **v20260526.1607** から始めてください。詳細は [安定版マイルストーン](./releases/stable.md) を参照してください。
:::

## 目的から探す

| やりたいこと | 出発点 |
| --- | --- |
| routerd を導入・更新する | [導入 → インストールとアップグレード](./install-and-upgrade.md) |
| routerd とは何か、なぜあるのかを知る | [はじめに → routerd とは](./concepts/what-is-routerd.md) |
| 他製品・他方式に対する位置づけを知る | [はじめに → 位置づけ](./concepts/positioning.md) |
| 初めてルーターを立てる | [導入 → クイックスタート](./tutorials/getting-started.md) |
| ディスクレス mini PC をルーターにする | [導入 → ディスクレス mini PC](./tutorials/diskless-minipc-walkthrough.md) |
| 宣言型モデル（リソース・適用・調整）を理解する | [機能解説 → リソースモデル](./concepts/resource-model.md) |
| 検証済みの構成例から設定を組む | [設定例集](./config-examples/index.md) |
| 特定の配置課題を解く | [How-to ガイド](./how-to/multi-wan.md) |
| リソースの種別やフィールドを引く | [リファレンス → リソース API](/docs/reference/api-v1alpha1) |
| 稼働中のルーターを運用する | [機能解説 → 調整（リコンサイル）](/docs/operations/reconcile) |
| 何が変わったかを追う | [リリースと安定版 → 変更履歴](./releases/changelog.md) |
| 難しい事例の背景を知る | [ナレッジベース](./knowledge-base/dhcpv6-pd-clients.md) |

## セクション一覧

- **はじめに** — routerd とは何か、位置づけ、設計思想
- **導入（クイックスタート）** — インストールとアップグレード、最初のルーター、OS 別の入門（NixOS / FreeBSD）、ディスクレス mini PC
- **機能解説（宣言型モデル）** — 用語集、リソースモデル、適用と生成、状態と所有、調整（リコンサイル）、Web 管理画面
- **設定リファレンス（機能別）** — DNS リゾルバ、ファイアウォール、Egress・マルチ WAN、BGP、Tailscale、OpenTelemetry など、機能ごとの設定方法
- **設定例集（シナリオ別）** — NAT、LAN の DHCP/DNS、DS-Lite、PPPoE、ポート転送、ゲスト分離、マルチ WAN フェイルオーバーなどの検証済み構成例
- **How-to ガイド** — フレッツ初期設定、IPv6 デュアルスタック、ゲストモード、OS ブートストラップ、PVE オーバーレイ、トラブルシューティング
- **ナレッジベース（実環境知見）** — 実環境で得た現場メモ（DHCPv6-PD クライアント、NTT NGN の PD 取得）
- **運用** — 状態データベース、インベントリ、USB 永続化、Alpine 配備、シークレット、観測、冗長化 など
- **リファレンス（API・プロトコル・対応環境）** — リソース API、制御 API、プラグインプロトコル、対応プラットフォーム、ハードウェア
- **リリースと安定版** — 安定版マイルストーン、変更履歴、リリース手順
- **設計ノート** — アーキテクチャ上の論点と設計の根拠
- **プロジェクト** — 貢献方法、ライセンスと法務
