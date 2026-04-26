---
title: Plugin Protocol
slug: /reference/plugin-protocol
---

# Plugin Protocol

routerd の plugin は、信頼済みのローカル実行ファイルです。

routerd は plugin を次の形式で呼び出します。

- stdin: JSON input
- stdout: JSON output
- stderr: 人間向け log
- environment variables: action と resource metadata

## Actions

- `validate`
- `observe`
- `plan`
- `ensure`
- `delete`

## Environment Variables

- `ROUTERD_ACTION`
- `ROUTERD_RESOURCE_API_VERSION`
- `ROUTERD_RESOURCE_KIND`
- `ROUTERD_RESOURCE_NAME`
- `ROUTERD_GENERATION`
- `ROUTERD_RUN_DIR`
- `ROUTERD_STATE_DIR`
- `ROUTERD_DRY_RUN`

plugin runner の実装が進むにつれて、入出力 JSON の詳細をこの文書へ追加します。

## Log Sink Plugins

`spec.type: plugin` の `LogSink` は一方向のイベント出力先です。routerd はイベントごとに、設定された信頼済みローカル実行ファイルを1回起動します。

- stdin: 1つの JSON event object と末尾改行
- stdout: 無視
- stderr: 人間向け診断
- environment variables:
  - `ROUTERD_LOG_LEVEL`
  - `ROUTERD_LOG_ROUTER`
  - `ROUTERD_LOG_COMMAND`

Event JSON:

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
