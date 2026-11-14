# FederationSLO

[リソース別の設定](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

routerctl doctorが共有イベント配送・購読を診断するときのグループ別閾値です。配送再試行やPlugin実行方針自体は変更しません。

## 指定条件と参照関係

- groupRefは必須で、既存のEventGroup名が必要です。同じグループへのFederationSLOは最大1件です。数値はすべて0以上で、0の既定値適用後もlagWarnSeconds<lagFailSecondsが必要です。
- 遅延は閾値以上でWARN・FAIL、購読件数は許容最大を超えた場合にWARN・FAILです。期限接近は残TTLが閾値未満のときで、未配送・失敗があるとWARNからFAILになる場合があります。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # 任意; object
      expiresSoonSeconds: 0 # 任意; integer; minimum=0
      lagFailSeconds: 0 # 任意; integer; minimum=0
      lagWarnSeconds: 0 # 任意; integer; minimum=0
    groupRef: "<string>" # 必須; string
    subscription: # 任意; object
      maxFailedRuns: 0 # 任意; integer; minimum=0
      maxPendingRuns: 0 # 任意; integer; minimum=0
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.groupRef` | EventGroup/nameでなく裸のEventGroup metadata.nameが必須です。存在とグループごとの一意性を検証します。 |
| `spec.delivery` | 任意の配送診断閾値です。省略項目は以下の既定値になり、通信タイムアウト・再試行設定ではありません。 |
| `spec.delivery.lagWarnSeconds` | 0以上の秒数です。省略・0は60で、配送遅延の最大測定値がこの値以上なら、FAIL条件でない限りWARNです。 |
| `spec.delivery.lagFailSeconds` | 0以上の秒数で、省略・0は180です。配送遅延の最大測定値がこの値以上ならFAILです。実効WARN閾値より大きくする必要があります。 |
| `spec.delivery.expiresSoonSeconds` | 0以上の秒数で、省略・0は120です。イベントの残TTLがこの値未満なら期限接近です。0で無効化はできず、期限のないイベントは別扱いです。 |
| `spec.subscription` | 任意の購読実行診断の許容件数です。既定はpending・failedとも許容0件で、処理キュー・再試行回数の制限ではありません。 |
| `spec.subscription.maxPendingRuns` | 0以上の件数で既定0です。pendingがこの値を超えるとWARNですが、failed側がFAILならそちらを優先します。 |
| `spec.subscription.maxFailedRuns` | 0以上の件数で既定0です。失敗件数がこの値を超えるとFAILになります。購読の最大3回の試行方針は変わりません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [cmd/routerctl/doctor_federation.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor_federation.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。
