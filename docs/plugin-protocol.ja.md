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
