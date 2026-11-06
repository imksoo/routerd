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
| EventRule | commit 済みのイベント履歴とルールごとの永続 checkpoint | 新規または変更済みのルールは、設定準備時に記録した履歴位置から開始します。変更していないルールは、再起動後も count、sequence、window、throttle と待機中の timer を再開します。未読の入力履歴が保持期限で失われた場合はエラーで停止し、イベントの不在とは判定しません。 |
| ObservabilityPipeline | commit 済みのイベント履歴 | 保存済みイベントを再試行します。sampling と sink の severity filter は引き続き適用します。全 status 更新試行を欠落なく残す監査ログではありません。 |
| provider action executor | ActionExecution 履歴と安定した冪等キー | 成功を永続記録済みの操作は再試行・再起動後も再実行しません。外部成功後、ローカル結果記録前の障害には executor 側の冪等性または provider 状態の観測が必要です。 |

設定された EventRule の pattern が `routerd.resource.status.changed` に一致するとき、通常の SQLite status store は意味のある status 更新とそのイベントを同一 transaction で保存します。イベント保存失敗時は status 更新も取り消すため、producer の再試行で遷移を記録できます。commit 後に同じイベント ID でローカル配送し、その直前にプロセスが終了しても履歴の周期再読込で復旧します。status store とバスは同じ transactional store を使う必要があり、能力不足や接続先不一致は status 保存前にエラーにします。無関係な topic の rule や該当する rule のない設定には、この追加能力を要求しません。

鮮度の判定と遷移属性は、transaction 内の最新 status を基準とし、並行する merge が保持したフィールドも反映します。新しい設定世代で status が同値なら、observed-generation のメタデータだけ更新し、遷移イベントを追加しません。

履歴を必要とする該当 EventRule がない場合、status 通知の既存契約を維持します。status 保存後にイベント保存だけ失敗し、エラーを返しながらローカル配送を試みる場合があります。同じ status の再保存では欠落した履歴を補完しません。現在値の consumer は再走査で回復できますが、履歴に一度も保存されなかったイベントを exporter が復元することはできません。上の transaction 保証は status 遷移イベントに限定し、全 daemon や derived-event producer を対象とはしません。

各 EventRule は、相関状態、唯一のルール別入力 cursor、公開 status と、その結果として生成するローカルイベントを、1つの SQLite transaction で保存します。transaction が失敗した場合は、どれも進めません。commit の応答を失っても checkpoint を再読込し、保存済みの結果を重複生成しません。バス通知は commit 後に行い、通知を逃した consumer は履歴から復旧します。この保証はローカル履歴を対象とします。外部 API やログ sink への配送は別です。ログ sink は配送成功後の cursor 保存に失敗すると同じイベントを再送するため、at-least-once です。複数 sink への配送も原子的ではありません。

EventRule は `all_of`、`any_of`、`sequence`、`window`、`absence`、`throttle`、`debounce`、`count` をサポートします。throttle の時間には `interval` を指定してください。意味が実装されていない非ゼロの `rate` は拒否します。相関状態の上限はルールごとに4096キー、シリアライズ後の checkpoint は8 MiBです。どちらかを超える場合、最後に一致した入力の時刻が古いキーから、上限に収まるまで削除します。削除したキーにまたがるパターンは完了できません。削除ごとに `status.warningCount` を増やし、checkpoint と同じ transaction で公開します。設定の選択時に有効なルールを初期化し、削除したルールの checkpoint を廃止します。選択していない候補設定の準備だけでは消費状態を変更しません。

[Event federation](../reference/event-federation.md) は別の配送履歴を持ち、ローカル status 通知キューを永続的な正本には使いません。


### Controllerログの配送

OTLPのlogsを有効にすると、`routerd serve`はcontrollerのreconcileログを
OTLP exporterへ送り、従来のローカル出力も維持します。logs無効時もローカル出力は残ります。
`LogSink`の運用イベントと`ObservabilityPipeline`のjournal配送は別のストリームです。

Linuxの`TestServeTelemetryDeliversReconcileLogs`はsandboxで実際の`run`/`serve`を
子プロセス起動し、gRPC受信、ローカル出力、SIGTERM終了を確認します。
実行コマンドは`go test ./cmd/routerd -run '^TestServeTelemetryDeliversReconcileLogs$' -count=1`です。
パッケージ導入や外部collectorの停止・復旧をこの結果に含めません。


### パッケージのLinux組合せ試験

本体とnative addonの版が一致する隔離ルーター2台と、独立したcollector・UDP syslog受信側を使います。
設定適用前にcollector設定・待受・空き容量・ネットワークを確認します。
Telemetryの3 signal、namespace、ルーター識別属性を維持し、通常起動後90秒以内に
両ルーターの`service.name=routerd`について、ログ本文`controller reconcile completed`、
metric `routerd.controller.reconcile`、span `controller.reconcile`を受信側で照合します。
helperの記録だけでは合格にしません。syslogは両送信元のtagを照合します。
同じ構成でパッケージ整合性・クライアント通信・DNS/NTP転送・native分類を確認し、
empty適用後のHealthyとhelper・アドレス・経路・NATの実撤去を要求します。
終了後はVMを停止し、一時collector実行ファイルを削除します。生ログの恒久保管は不要です。

`1.5.0~20261104040833+g261e6675`はUbuntu 24.04、Collector contrib 0.151.0、
78リソース構成で合格しました。74通信・10 TCP single accepts、DNS転送、native TLS SNI、
DNS名の誤SNI表示修正、NTP応答、両empty撤去を確認しました。
全105リソース、collector停止・復旧、新たなHA電源断試験の合格を意味しません。

`LogRetention` の宣言を削除すると、以前の保持期間警告を含む status も撤去します。撤去済みポリシーがルーターを Degraded にし続けることはありません。宣言が残る非稼働ポリシーの前回実行状態は保持し、dry-run では削除しません。イベントジャーナル独自の安全上限は引き続き適用します。

### 実パッケージでのアーカイブ・期限切れ削除試験

空設定の隔離 Linux VM と独立 syslog 受信側を使います。実運用ルーターの DB に試験行を追加してはいけません。daemon 停止中に `plugin_runs` へ完了済みの試験行を 2 件用意し、8 日前の `rd15-native-retention-old` と現在時刻の `rd15-native-retention-fresh` を確認します。`LogRetention` は `retention: 7d`、`schedule: daily`、`signals: [pluginRuns]` とし、info レベルのアーカイブを許可する syslog `LogSink` を明示的に参照します。起動前に受信側への到達を確認し、受信処理を 2 分・8 MiB 以内に制限します。

正式パッケージの daemon を起動し、`routerctl get LogRetention/archive -o json` が `Applied`、`deleted: 1` になることを確認します。別途、受信側で古い行のマーカーを確認し、DB の読み取り専用検査で新しい行だけが残っていることを確認します。件数表示だけで合格にしません。最後に `routerctl apply --timeout 90s -f empty.yaml --replace` で撤去し、予約済み自己再起動の完了後に retention status が消え、`Healthy` になることを確認します。90 秒は daemon の 1 分の適用期限と応答処理を含む待機時間であり、処理期限や最終判定の緩和ではありません。

この経路は `76464310` の正式 Linux パッケージで通過しました。plugin 履歴の期限切れ削除と実際の UDP 受信の確認であり、パケット損失時の回復、受信側の永続保存、全ログテーブル、完全なイベント監査履歴の保証ではありません。


### conntrack 観測が取得できない場合

conntrack のスナップショットを取得できない場合、observer は `Unavailable`、
有効な `TrafficFlowLog` は `Degraded / ConntrackUnavailable` を報告します。
空のテーブルと見なして履歴を終了・削除せず、保存済みフローとカウンターを保持します。
観測が復旧し、履歴への書き込みが成功すると `Observed` に戻ります。

### 実パッケージでの Collector 停止・復旧

正式な本体・addon の版が一致する隔離 Linux ルーターと、別 VM の Collector contrib
を使います。試験前にパッケージ整合性、空設定の Healthy、Collector 設定・gRPC 待受・
ルーターからの到達性を確認します。受信時間と出力サイズを制限し、単独 exporter
ではなく通常の routerd サービスを使います。

Telemetry の 3 signal と試験識別属性を宣言し、DNSResolver/DNSZone/DNSForwarder
で既知の A レコードを peer に提供します。Telemetry 環境変数反映に伴う通常の
再起動が完了した後、Healthy と独立クライアントの UDP/TCP 応答を確認します。
150 秒以内に、上記組合せ試験と同じ main daemon のログ・metric・span を受信側で照合します。

Collector を停止し、待受消失とルーターからの新規接続失敗を確認します。
`routerctl apply --timeout 90s -f candidate.yaml --replace` で DNS レコードを変更し、
peer から UDP 20 回・TCP 20 回の新しい応答、Healthy、同一 MainPID を確認します。
Collector 停止中に routerd を再起動し、30 秒以内のサービス再起動と、90 秒以内の
DNS・Healthy 復旧を要求します。

同じ Collector を復帰させ、受信件数を記録して再度 DNS レコードを変更します。
新しい DNS 応答と、150 秒以内の全 3 signal の新規受信を確認します。
追加の daemon 再起動で送信を復旧させてはいけません。最後に empty を適用し、
Healthy と DNS 待受の実撤去を確認します。サービス停止、fixture アドレス・一時 Collector・
出力の削除、パッケージ整合性確認まで行います。
これは欠落なしの配送を保証しません。SDK の有限バッファや再起動で未配送データが
失われる場合があります。長時間停止、バッファ満杯、TLS/mTLS、受信側の永続保存、
実環境の全構成は別の検証範囲です。

正式製品 `db41e03e`、パッケージ `1.5.0~20261106222627+gdb41e03e` は、
Ubuntu 24.04 / Linux 6.8.0-147 と独立した Collector contrib 0.151.0 で通過しました。
停止中の DNS 変更後 40 問合せは同一 MainPID で成功し、再起動と DNS・Healthy 復旧は
14.41 秒でした。Collector 復帰後は追加再起動なしに main daemon の全 3 signal の
受信件数が増加しました。empty 撤去、パッケージ整合性、一時受信側の後片付けも成功し、
専用 VM を停止しました。以前の 2 ルーター組合せ試験を再走した結果ではありません。
