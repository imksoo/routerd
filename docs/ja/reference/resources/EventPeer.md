# EventPeer

[リソース別の設定](../resources.md#eventpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

EventGroupの相手側配送先を1件宣言します。通信認証と自ノード識別名はグループ側で指定します。

## 指定条件と参照関係

- groupRef・nodeNameは空でない値、endpointは必須です。groupRefはEventGroup/nameでなくEventGroupのmetadata.nameと照合します。EventPeerの検証は存在を確認せず、一致グループがなければ配送設定に入りません。
- 静的配送先はnodeName単位で取得済み配送先を上書きし、同じnodeNameの静的定義は後が優先します。typesとsubjectPrefixesは両方の条件が必要で、各一覧内はORです。大文字小文字を区別し、typesは完全一致でトピックのワイルドカードではありません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    [ direction: ("push") ]
    endpoint: <string>
    groupRef: <string>
    nodeName: <string>
    [ subjectPrefixes:
        - <string>
    ]
    [ types:
        - <string>
    ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.groupRef` | **必須** | string | 必須の裸のEventGroup名です。配送には対象グループのauth.secretFileが必要です。既定値はありません。 |
| `spec.nodeName` | **必須** | string | 必須の相手識別名で、配送先重複排除・配送履歴のキーです。YAMLの名前が違ってもnodeNameが同じなら独立した相手にはなりません。 |
| `spec.endpoint` | **必須** | string | 例http://192.0.2.10:8787のような基底URLが必須です。送信側で末尾の/を除き/v1/eventsを付けるため、そのパスは含めません。URL・スキームの利用可否や疎通はローカルYAML検証では確認しません。 |
| `spec.direction` | *任意* | string; enum=["push"] | pushまたは空のみ受け付け、既定はpushです。常に送信先を作り、pullクライアントにはなりません。 |
| `spec.types` | *任意* | array | 任意のイベント型完全一致の許可一覧です。既定の空は全型許可で、指定項目の空文字は禁止です。 |
| `spec.subjectPrefixes` | *任意* | array | 任意のsubject前方一致一覧で、大文字小文字を区別します。既定の空は全subjectを許可し、一覧のいずれかへの前方一致で成立します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/eventd/push.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/push.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
