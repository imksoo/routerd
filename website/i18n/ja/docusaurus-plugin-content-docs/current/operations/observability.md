# 観測パイプライン

![Diagram showing Telemetry, LogSink, and ObservabilityPipeline resources feeding routerd OpenTelemetry SDK signals and routerd event exporter output to OTLP, syslog, stdout, or Loki sinks](/img/diagrams/operations-observability.png)

`Telemetry` は、routerd 自身のメトリクス・トレース・ログを OTLP へ出すための小さなリソースです。`LogSink` は、運用イベントや観測ログの転送経路を表します。OTLP の `LogSink` は、コレクターのエンドポイントを重複して書かず、`Telemetry` リソースを参照します。routerd のイベントログを Loki などパイプライン型のリモート sink に送りたい場合は、`ObservabilityPipeline` を使います。

`ObservabilityPipeline` は、同梱の `otelcol` プロセスではなく、routerd 内蔵のパイプラインです。OTLP のログ・メトリクス・トレースは通常の OpenTelemetry SDK を使い、設定したログ sink には軽量なイベントエクスポーターが送信します。

現在のログ sink は次のとおりです。

- `stdout`: JSON 形式のイベント行。サービスマネージャー配下で管理されるログ出力に適しています。
- `syslog`: 既存の `LogSink` と同じ syslog 形式。ローカルまたはリモートの syslog を使います。
- `loki`: `/loki/api/v1/push` への HTTP push。

`kafka` は、意図する外部パイプラインを設定に残すためのメタデータとして受け付けます。routerd はまだ Kafka へ直接 publish しません。

設定例:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: ObservabilityPipeline
metadata:
  name: remote-observability
spec:
  otlp:
    endpoint: http://otel-collector.lan:4317
    insecure: true
    headers:
      authorization: Bearer example-token
  serviceNamespace: routerd
  attributes:
    site: edge
  signals: [logs, metrics, traces]
  sampling:
    rate: 1
  logs:
    sinks:
      - name: loki
        type: loki
        minLevel: info
        loki:
          url: http://loki.lan:3100/loki/api/v1/push
          tenant: routerd
```

OTLP のフィールドは、routerd が管理するユニットの標準的な OpenTelemetry 環境変数に展開されます。ログ sink のエクスポーターはプロセス内バスの `routerd.**` イベントを購読するため、`journalctl` を scrape せずに、コントローラーの状態変化やデーモンのイベントを転送できます。

サンプリングはパイプラインごとに決定的で、sink への分配の前に適用されます。運用イベントログでは `1`（全件）のままにしてください。高頻度のソースを意図的にダウンサンプリングする場合にのみ変更します。

完全な例は `examples/observability-loki.yaml` にあります。

## イベント配送と復旧の契約

プロセス内バスの購読キューには上限があり、満杯になると通知を破棄します。SQLite に保存済みのイベントは削除しません。エクスポーターと EventRule エンジンはバス通知を履歴の再読込にだけ使い、通知がなくても既定で1秒ごとに再読込します。再読込失敗後の再試行間隔は1秒から1分まで増加します。これらの周期には処理時間、DB 障害、プロセス停止による遅延を含めません。

| consumer | 正本となる入力 | 復旧方法と限界 |
| --- | --- | --- |
| route などの再評価通知 consumer | 現在の設定、dynamic part、object status | 通知または周期 reconcile で最新入力を再読込します。IPv4 route controller の通常周期の上限は30秒です。古い通知や重複通知の payload で desired state を上書きしません。他の controller にはそれぞれの周期があります。 |
| DerivedEvent | 参照先の現在の status | 既定で5秒ごとに再評価します。途中の過去の遷移は復元できません。 |
| EventRule | commit 済みのイベント履歴 | count、sequence、window には履歴が必要です。新規 consumer は現在の cursor から開始し、それ以前の全履歴を再生しません。相関状態はプロセス内に保持します。 |
| ObservabilityPipeline | commit 済みのイベント履歴 | 保存済みイベントを再試行します。sampling と sink の severity filter は引き続き適用します。全 status 更新試行を欠落なく残す監査ログではありません。 |
| provider action executor | ActionExecution 履歴と安定した冪等キー | 成功を永続記録済みの操作は再試行・再起動後も再実行しません。外部成功後、ローカル結果記録前の障害には executor 側の冪等性または provider 状態の観測が必要です。 |

設定された EventRule の pattern が `routerd.resource.status.changed` に一致するとき、通常の SQLite status store は意味のある status 更新とそのイベントを同一 transaction で保存します。イベント保存失敗時は status 更新も取り消すため、producer の再試行で遷移を記録できます。commit 後に同じイベント ID でローカル配送し、その直前にプロセスが終了しても履歴の周期再読込で復旧します。status store とバスは同じ transactional store を使う必要があり、能力不足や接続先不一致は status 保存前にエラーにします。無関係な topic の rule や該当する rule のない設定には、この追加能力を要求しません。

鮮度の判定と遷移属性は、transaction 内の最新 status を基準とし、並行する merge が保持したフィールドも反映します。新しい設定世代で status が同値なら、observed-generation のメタデータだけ更新し、遷移イベントを追加しません。

履歴を必要とする該当 EventRule がない場合、status 通知の既存契約を維持します。status 保存後にイベント保存だけ失敗し、エラーを返しながらローカル配送を試みる場合があります。同じ status の再保存では欠落した履歴を補完しません。現在値の consumer は再走査で回復できますが、履歴に一度も保存されなかったイベントを exporter が復元することはできません。上の transaction 保証は status 遷移イベントに限定し、全 daemon や derived-event producer を対象とはしません。

履歴消費は at-least-once です。処理成功後に cursor 保存が失敗すると、同じイベント ID で callback を再実行します。EventRule の出力やログ配送には重複があり得ます。複数 sink への配送は原子的ではなく、cursor は外部 API の exactly-once を保証しません。[Event federation](../reference/event-federation.md) は別の配送履歴を持ち、ローカル status 通知キューを永続的な正本には使いません。
