# EventRule

[リソース別の設定](../resources.md#eventrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

永続化されたローカルイベント履歴のパターンからイベントを生成します。相関キーごとに状態を分離できます。

## 指定条件と参照関係

- pattern.operatorとemit.topicが必須です。topic・topics・trigger・expectedのいずれかも必要ですが、この検証だけでは動作するパターンとは限りません。expectedだけでは入力一覧にならず、absenceにはtriggerまたはtopicが必要です。トピックのリソース存在確認は行いません。
- 入力順はtopics、topic、triggerです。all_of・sequenceはトピックの完全一致を使います。any_of・window・count・throttle・debounce・absenceはドット区切りの*（1区間）と**（0区間以上）に対応します。
- all_ofは受信済みトピックを発火後も保持し、すべて受信した後は該当イベントのたびに再発火します。sequenceは完了時、またはstrict指定時の一覧内順序違反でリセットします。windowは閾値到達後に時間窓を、countはカウンターをリセットします。
- カウンター・途中の順序・タイマーはメモリー上にあり、プロセス再起動後は空から開始します。イベント読取位置は永続化し、新規読取処理は履歴の現在の末尾から開始します。読取位置の保存失敗時は入力が再処理され得ます。routerd-eventruleが生成したイベントは、連鎖ループ防止のため全ルールから除外します。
- 相関状態はルールごとに最大4096キーで、最も古く観測したキーのカウンター・タイマーを破棄します。window・throttleは入力イベント時刻で比較し、absence・debounceのタイマーは処理中の経過時間を使います。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit: # 必須; object
      attributes: # 任意; object
        "<key>": "<string>" # 任意; string
      topic: "<string>" # 必須; string
    pattern: # 必須; object
      allow_missing_correlation: true # 任意; boolean
      correlate_by: "<string>" # 任意; string
      duration: "<string>" # 任意; string
      expected: "<string>" # 任意; string
      interval: "<string>" # 任意; string
      operator: "all_of" # 必須; string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"]
      quiet: "<string>" # 任意; string
      rate: 0 # 任意; integer; minimum=0
      strict: true # 任意; boolean
      threshold: 0 # 任意; integer; minimum=0
      topic: "<string>" # 任意; string
      topics: # 任意; array
        - "<string>" # string
      trigger: "<string>" # 任意; string
      window: "<string>" # 任意; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.pattern` | 必須のパターン定義です。選択した演算子で使わない項目も排他エラーにはなりません。 |
| `spec.pattern.operator` | all_of・any_of・sequence・window・absence・throttle・debounce・countのいずれかが必須で、既定値はありません。 |
| `spec.pattern.topic` | topicsの後に追加する単一入力トピックです。absenceではtriggerが空の場合の開始トピックになります。既定は空です。 |
| `spec.pattern.topics` | 順序付きの入力一覧で、既定は空です。sequenceでは順序に意味があり、all_ofでは一覧の全トピックの受信が必要です。 |
| `spec.pattern.trigger` | absenceのタイマーを開始・再開するトピックで、topicより優先します。他の演算子の入力一覧にもtopicの後で追加されます。既定は空です。 |
| `spec.pattern.expected` | absenceの取り消しトピックです。同じ相関キーの待機タイマーを取り消し、triggerの一致より先に判定します。通常の入力一覧には入りません。既定は空で、取り消しなしです。 |
| `spec.pattern.duration` | Go形式の期間です。window・throttle・absenceの既定は1m、debounceは1sです。個別のwindow・interval・quietを優先します。0・負数も検証を通るため、待機には正の値を使用してください。 |
| `spec.pattern.window` | windowの集計期間です。durationより優先し、両方省略時は1mです。開始境界以降を数え、閾値到達後は窓を空にします。他の演算子では使いません。 |
| `spec.pattern.quiet` | debounceの無入力待機期間です。durationより優先し、両方省略時は1sです。該当入力ごとにタイマーを再開し、最後の入力に基づき発火します。 |
| `spec.pattern.interval` | throttleの相関キーごとの最短発火間隔です。durationより優先し、両方省略時は1mです。最初の該当イベントは直ちに発火します。 |
| `spec.pattern.rate` | 0以上の整数で既定は0ですが、現行エンジンは読みません。throttleの制御にはintervalを使用してください。 |
| `spec.pattern.threshold` | window・countの必要イベント数です。0以上で、省略・0は1として扱います。他の演算子では使いません。 |
| `spec.pattern.correlate_by` | attributes.<key>、resource.name/kind/apiVersion、daemon.instance/kindで状態を分離します。空なら共通キーdefaultです。入力トピックは文字列で、Kind/name参照ではありません。 |
| `spec.pattern.allow_missing_correlation` | 既定はfalseで、相関値欠落の入力を飛ばしwarningCountを増やします。trueでは空キーにまとめます。ただしresourceオブジェクトが存在して選択メンバーだけ空の場合は、falseでも受け入れます。 |
| `spec.pattern.strict` | 既定はfalseです。sequenceで一覧内のトピックが順序違いで来ると進行を0に戻します。その入力を新しい先頭として再利用しません。一覧外のトピックは無視します。 |
| `spec.emit` | 必須の出力イベント定義です。重要度はinfoで、resourceは本EventRuleになります。 |
| `spec.emit.topic` | 空でない出力イベント型が必須です。既定値はなく、ルールエンジンは自身の生成イベントを入力にしません。 |
| `spec.emit.attributes` | 任意の文字列マップです。${event.type}・${correlation}・${count}・${resource.name/kind/apiVersion}・${attributes.<key>}を展開し、未解決の式は文字列のまま残ります。正のcountと空でないcorrelationは同名の出力キーを上書きします。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/eventrule/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/eventrule/engine.go) · [pkg/eventconsumer/consumer.go](https://github.com/imksoo/routerd/blob/main/pkg/eventconsumer/consumer.go) · [pkg/bus/bus.go](https://github.com/imksoo/routerd/blob/main/pkg/bus/bus.go)
