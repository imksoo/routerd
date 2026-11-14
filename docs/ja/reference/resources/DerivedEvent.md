# DerivedEvent

[リソース別の設定](../resources.md#derivedevent) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

リソースの現在状態の条件が変わると、成立・取り消しイベントを発行します。EventRuleのようなイベント列の判定ではありません。

## 指定条件と参照関係

- topicと空でないinputsが必須です。入力は旧来のfield・equals・not_empty・any_of構造であり、共通のresource・field参照、dependsOn、whenではありません。
- 現在の評価はKind/name.status.field（${...}で囲んでも可）でnet.routerd.net/v1alpha1の状態を読みます。.status.を含まない文字列は状態参照でなく固定値です。通常の状態値参照検証による、参照先存在・提供フィールドの確認は行いません。
- 起動時、自身以外のローカルrouterd.**イベント受信時、定期処理時（既定5秒）に評価します。遷移状態・待機タイマーはプロセス再起動で初期化され、保存済みstatusからは復元しません。
- retractWhenは検証されますが、現行の評価処理では読みません。取り消しはemitWhenの判定の反転に従い、retractWhenの変更は挙動を変えません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DerivedEvent
  metadata:
    name: "<name>"
  spec:
    emitInitial: true # 任意; boolean
    emitWhen: "all_true" # 任意; string; enum=["all_true", "any_true"]
    hysteresis: "<string>" # 任意; string
    inputs: # 必須; array
      - # object
        any_of: # 任意; array
          - # array
            - # object
              equals: "<string>" # 任意; string
              field: "<string>" # 任意; string
              not_empty: true # 任意; boolean
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        not_empty: true # 任意; boolean
    retractWhen: "any_false" # 任意; string; enum=["any_false", "all_false"]
    topic: "<string>" # 必須; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.topic` | 必須の基底トピックです。成立時に<topic>.asserted、不成立時に<topic>.retractedを発行し、基底名のみのイベントは発行しません。 |
| `spec.inputs` | 1件以上必要な条件一覧で、emitWhenで組み合わせます。空の条件オブジェクトはtrueになるため、条件を明示してください。 |
| `spec.inputs[].field` | 既定は空の文字列です。Kind/name.status.fieldはstatus直下の値を読み、参照先・項目がなければ空です。他の文字列は固定値として評価します。配列はJSON文字列になり、equalsは直列化した全体と比較します。 |
| `spec.inputs[].equals` | 大文字小文字を区別する完全一致です。空が既定で、一致比較をしません。fieldが空でなく、equals・not_emptyが未指定なら、暗黙に空でないことを要求します。 |
| `spec.inputs[].not_empty` | 既定はfalseです。trueは空・空白だけの値を拒否し、equalsも非空なら両条件が必要です。 |
| `spec.inputs[].any_of` | AND条件群のORリストです。空でなければ、その入力直下のfield・equals・not_emptyを使わずこちらを優先します。各群は1件以上の条件がすべて成立する必要があり、空群はfalseです。 |
| `spec.inputs[].any_of[][].field` | 群内の参照・固定値です。inputs[].fieldと同じ解決規則です。 |
| `spec.inputs[].any_of[][].equals` | 群内の一致条件で、空・既定値の意味はinputs[].equalsと同じです。 |
| `spec.inputs[].any_of[][].not_empty` | 群内の非空条件で、既定はfalseです。inputs[].not_emptyと同じ意味です。 |
| `spec.emitWhen` | 既定all_trueは全入力の成立、any_trueは1件以上の成立を要求します。この判定がfalseになると取り消しに遷移します。 |
| `spec.retractWhen` | any_false（既定）・all_falseを受け付けますが、検証後は使用しません。独立した取り消し条件としては使えません。 |
| `spec.hysteresis` | 任意のGo形式期間で、既定0は即時遷移です。新しい判定が待機満了時の再確認まで維持される必要があり、元に戻ると取り消します。初回状態は待機せず決めます。負数も検証を通り、即時に発火します。 |
| `spec.emitInitial` | 既定falseは初回状態だけを保存してイベントを出しません。trueはコントローラー状態の初期化時に結果を発行し、再起動後も対象です。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/derived/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/derived/engine.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `topic`
