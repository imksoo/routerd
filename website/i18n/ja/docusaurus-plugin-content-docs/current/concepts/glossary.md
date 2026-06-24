---
title: 用語集
sidebar_label: 用語集
sidebar_position: 1
---

# 用語集

![routerd の用語を宣言リソース、runtime evidence、host artifact、networking behavior に整理した対応図](/img/diagrams/concept-glossary.png)

routerd のドキュメントで使う主な用語と、その日本語訳をまとめます。訳語は次の方針で選んでいます。

- **ネットワーク用語**は、国内の商用ルーター（NEC IX / ヤマハ RTX）のマニュアル表記に合わせています。日本のネットワーク技術者が読み慣れた言葉を優先します。
- **宣言型モデルの用語**（リソース、調整ループなど）は、Kubernetes / Terraform 日本語ドキュメントの慣習に合わせています。

## ネットワーク用語

| 英語 | 表記（本ドキュメント） | 備考 |
| --- | --- | --- |
| interface | インターフェース | 末尾は長音「ー」。ヤマハ表記に合わせる |
| route / routing | 経路 / ルーティング | 経路情報・経路表のように使う |
| gateway | ゲートウェイ | |
| NAT | NAT | |
| NAPT | NAPT | 動的な多対一変換。商用機の「IPマスカレード」に相当 |
| firewall | ファイアウォール | routerd のゾーン型ステートフル機能 |
| filter / rule | フィルター / ルール | 個々の許可・拒否規則 |
| prefix delegation (PD) | プレフィックス委任（PD） | 「委譲」は使わない。NTT / ヤマハ / RFC の標準語に統一 |
| upstream | 上流 | DNS / 経路の上位側 |
| egress | egress（送出側） | 初出のみ注記し、以降は英字のまま |
| ingress | ingress（受信側） | 初出のみ注記し、以降は英字のまま |

## 宣言型モデルの用語

| 英語 | 表記（本ドキュメント） | 備考 |
| --- | --- | --- |
| declarative | 宣言型 | 「宣言的」も可だが「宣言型」に統一 |
| resource | リソース | |
| Kind | Kind（種別） | 大文字 Kind を保持 |
| spec | spec（仕様） | 望ましい状態を書く側 |
| status | status（状態） | 実際の状態を表す側 |
| apply | 適用 | `routerctl apply` の動作 |
| reconcile / reconcile loop | 調整（リコンサイル）/ 調整ループ | 実状態を望ましい状態へ近づける処理 |
| controller | コントローラー | |
| render / rendering | 生成（レンダリング） | 設定ファイル等を組み立てる処理 |
| daemon | デーモン | |
| generation | 世代（generation） | SQLite の世代番号 |
| owned / ownership | 所有 / 所有権 | |
| bootstrap | ブートストラップ | |
| appliance | アプライアンス | |
| Tier (H/S/C/E) | Tier H / Tier S … | 機能段階の固有名詞として保持 |

## その他の表記

- **Web Console**（routerd の Web UI）は「**Web 管理画面**」と表記します。ただし `WebConsole`（その UI を有効にする Kind 名）はコード識別子なので、原文のまま `WebConsole` と書きます。
