---
title: プラグインプロトコル
slug: /reference/plugin-protocol
---

# プラグインプロトコル

routerd のプラグインは、信頼済みのローカル実行ファイルです。

routerd はプラグインを次の形式で呼び出します。

- 標準入力: JSON 入力
- 標準出力: JSON 出力
- 標準エラー: 人間向けログ
- 環境変数: 操作とリソースメタデータ

## 操作

- `validate`
- `observe`
- `plan`
- `ensure`
- `delete`

## 環境変数

- `ROUTERD_ACTION`
- `ROUTERD_RESOURCE_API_VERSION`
- `ROUTERD_RESOURCE_KIND`
- `ROUTERD_RESOURCE_NAME`
- `ROUTERD_GENERATION`
- `ROUTERD_RUN_DIR`
- `ROUTERD_STATE_DIR`
- `ROUTERD_DRY_RUN`

プラグイン実行処理の実装が進むにつれて、入出力 JSON の詳細をこの文書へ追加します。

## ログ出力先プラグイン

`spec.type: plugin` の `LogSink` は一方向のイベント出力先です。routerd はイベントごとに、設定された信頼済みローカル実行ファイルを1回起動します。

- 標準入力: 1つの JSON イベントオブジェクトと末尾改行
- 標準出力: 無視
- 標準エラー: 人間向け診断
- 環境変数:
  - `ROUTERD_LOG_LEVEL`
  - `ROUTERD_LOG_ROUTER`
  - `ROUTERD_LOG_COMMAND`

イベント JSON:

```json
{
  "timestamp": "2026-04-26T00:00:00Z",
  "level": "info",
  "message": "routerd command completed",
  "router": "lab-router",
  "command": "reconcile",
  "fields": {
    "phase": "Healthy"
  }
}
```
