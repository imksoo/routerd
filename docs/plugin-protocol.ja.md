---
title: プラグインプロトコル
slug: /reference/plugin-protocol
---

# プラグインプロトコル

routerd のプラグインは、信頼済みのローカル実行ファイルです。リソース固有の振る舞い（観測、計画、反映、削除）を本体に取り込まずに拡張するためのものです。リモートのレジストリやリモートインストールは用意していません。プラグインは必ずルータ自身のファイルシステム上にある、事前に検証されたファイルです。

プラグインは、マニフェストと実行ファイルからなる小さなディレクトリで配置します。

```text
/usr/local/libexec/routerd/plugins/net.interface/0.1.0/
├── plugin.yaml
└── plugin.sh
```

マニフェストには、対応する routerd リソースと、実装している動作を宣言します。

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: Plugin
metadata:
  name: net.interface
  version: 0.1.0
spec:
  resource:
    apiVersion: net.routerd.net/v1alpha1
    kind: Interface
  runtime:
    executable: plugin.sh
  actions:
    validate: true
    observe: true
    plan: true
    ensure: true
    delete: true
  requirements:
    commands:
      - ip
      - jq
```

## routerd からの呼び出し

routerd がプラグインに処理を依頼するときは、実行ファイルを次の形式で起動します。

- 標準入力: 対象リソースと文脈情報を含む JSON。
- 標準出力: プラグインからの結果 JSON。
- 標準エラー: 人間向けの診断メッセージ。
- 環境変数: 動作種別とリソースメタデータ。

プラグインは一度に 1 件のリソースを担当します。どのプラグインを呼ぶかは、マニフェストの `spec.resource` と対象リソースの `apiVersion` / `kind` を突き合わせて決まります。

## 動作種別

それぞれが反映処理の 1 段階に対応します。

- `validate`: リソースの構造と意味の検証。
- `observe`: そのリソースに関わるホスト状態の読み取り。
- `plan`: 望む状態と観測状態の差分の算出。
- `ensure`: ホストを望む状態に近づける。
- `delete`: そのリソースが所有するホスト状態の削除。

マニフェストでは、実装している動作だけを true にします。

## 環境変数

動作種別やリソースメタデータは、プラグイン側で標準入力 JSON から取り出さなくても済むよう、環境変数として渡します。

- `ROUTERD_ACTION`
- `ROUTERD_RESOURCE_API_VERSION`
- `ROUTERD_RESOURCE_KIND`
- `ROUTERD_RESOURCE_NAME`
- `ROUTERD_GENERATION`
- `ROUTERD_RUN_DIR`
- `ROUTERD_STATE_DIR`
- `ROUTERD_DRY_RUN`

標準入出力で受け渡す JSON の詳細は、プラグインランナーの実装が整うのに合わせて、本ドキュメントに動作種別ごとに追記していきます。

## ログ送出プラグイン

`spec.type: plugin` の `LogSink` は、これとは別のもっと単純な形式のプラグインです。一方向のイベント送出先で、routerd はイベントごとに、設定された信頼済みのローカル実行ファイルを 1 回起動します。

- 標準入力: 1 件の JSON イベントオブジェクトに改行を付けたもの。
- 標準出力: 読み捨て。
- 標準エラー: 人間向けの診断メッセージ。
- 環境変数:
  - `ROUTERD_LOG_LEVEL`
  - `ROUTERD_LOG_ROUTER`
  - `ROUTERD_LOG_COMMAND`

イベントの中身はこのような形です。

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

routerd は実行ファイルが終了するまで待ってからイベントを送出済みとみなします。`spec.plugin.timeout` で待ち時間の上限を決めているため、応答が遅い、あるいは止まっている送出先が反映処理を巻き込んで止めることはありません。
