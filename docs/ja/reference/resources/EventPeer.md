# EventPeer

[リソース別の設定](../resources.md#eventpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

EventGroupの相手側配送先を1件宣言します。通信認証と自ノード識別名はグループ側で指定します。

## 指定条件と参照関係

- groupRef・nodeNameは空でない値、endpointは必須です。groupRefはEventGroup/nameでなくEventGroupのmetadata.nameと照合します。EventPeerの検証は存在を確認せず、一致グループがなければ配送設定に入りません。
- 静的配送先はnodeName単位で取得済み配送先を上書きし、同じnodeNameの静的定義は後が優先します。typesとsubjectPrefixesは両方の条件が必要で、各一覧内はORです。大文字小文字を区別し、typesは完全一致でトピックのワイルドカードではありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    direction: "push" # 任意; string; enum=["push"]
    endpoint: "<string>" # 任意; string
    groupRef: "<string>" # 必須; string
    nodeName: "<string>" # 必須; string
    subjectPrefixes: # 任意; array
      - "<string>" # string
    types: # 任意; array
      - "<string>" # string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.groupRef` | 必須の裸のEventGroup名です。配送には対象グループのauth.secretFileが必要です。既定値はありません。 |
| `spec.nodeName` | 必須の相手識別名で、配送先重複排除・配送履歴のキーです。YAMLの名前が違ってもnodeNameが同じなら独立した相手にはなりません。 |
| `spec.endpoint` | 例http://192.0.2.10:8787のような基底URLが必須です。送信側で末尾の/を除き/v1/eventsを付けるため、そのパスは含めません。URL・スキームの利用可否や疎通はローカルYAML検証では確認しません。 |
| `spec.direction` | pushまたは空のみ受け付け、既定はpushです。常に送信先を作り、pullクライアントにはなりません。 |
| `spec.types` | 任意のイベント型完全一致の許可一覧です。既定の空は全型許可で、指定項目の空文字は禁止です。 |
| `spec.subjectPrefixes` | 任意のsubject前方一致一覧で、大文字小文字を区別します。既定の空は全subjectを許可し、一覧のいずれかへの前方一致で成立します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/eventd/push.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/push.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
