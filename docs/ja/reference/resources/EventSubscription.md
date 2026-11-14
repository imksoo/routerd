# EventSubscription

[リソース別の設定](../resources.md#eventsubscription) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

期限内の共有イベントを永続DBから選び、ローカルのPluginを実行して結果をDynamicConfigPartとして保存します。

## 指定条件と参照関係

- groupRef・1件以上のmatch.types・trigger.pluginRefが必須です。グループ・Pluginの参照はKind/nameでなく裸のmetadata名です。この検証では存在を確認せず、Pluginがなければ実行時Pending、該当保存イベントがないグループはIdleになります。
- 大文字小文字を区別します。types・subject前方一致・payload各項目・sourceNodesの条件はAND、各一覧の選択肢はORです。イベント型は完全一致で、ローカルトピックのワイルドカードではありません。
- 通常5秒ごとに保存イベントを確認します。成功済みの購読・イベント組は飛ばし、失敗は最大3回、処理中断のpendingは再処理できます。少なくとも1回の処理であり、Pluginの外部操作を厳密に1回だけにする保証ではありません。
- 両待機設定が正でなければ、その時点の処理対象をまとめて1回実行し、1イベントごととは限りません。待機タイマーはメモリー上、イベント・実行履歴は永続化します。待機中は1024件または対象データ約4MiBで早期実行します。
- Pluginには該当イベントと、Plugin.context.resourcesで明示許可した設定だけを渡し、秘密値は伏せます。TTL・生成リソースは本購読でなくPluginResultが指定します。異なるイベント群は別のDynamicConfigPart取得元として共存します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: <string>
    match:
      [ payload:
          [ <name>: <string> ]
      ]
      [ sourceNodes:
          - <string>
      ]
      [ subjectPrefixes:
          - <string>
      ]
      types:
        - <string>
    trigger:
      [ batchWindow: <string> ]
      [ debounce: <string> ]
      pluginRef: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.groupRef` | **必須** | string | 保存イベントのgroup_nameを選ぶ裸のEventGroup名が必須です。既定値・グループの自動作成・起動はありません。 |
| `spec.match` | **必須** | object | 必須の選別条件です。typesは1件以上必要で、他の条件が空ならその制限なしです。 |
| `spec.match.types` | **必須** | array | 空でないイベント型を1件以上指定します。いずれかへの完全一致が必要で、ワイルドカードも単なる文字列です。 |
| `spec.match.subjectPrefixes` | *任意* | array | subjectの前方一致一覧です。既定の空は制限なしで、パス区間やワイルドカードでなく単純な文字列前方一致です。 |
| `spec.match.payload` | *任意* | object | 任意のキー・文字列の完全一致マップで、全項目が必要です。空キーは禁止です。欠落キーは空として読むため、期待値が空ならキー欠落にも一致します。 |
| `spec.match.sourceNodes` | *任意* | array | sourceNodeの完全一致許可一覧です。既定の空はすべて許可し、自ノードのイベントも自動除外しません。必要な発信元を明示してください。 |
| `spec.trigger` | **必須** | object | 必須のローカルPlugin起動設定です。Plugin.triggersとは別に、購読コントローラーが実行します。 |
| `spec.trigger.pluginRef` | **必須** | string | Plugin/nameでなく裸のPlugin metadata.nameが必須です。なければPendingです。そのPluginの実行ファイル・タイムアウト・環境・context制限を使います。 |
| `spec.trigger.batchWindow` | *任意* | string | 最初の対象イベントから測るGo形式の待機上限です。既定の空・0は上限なしですが、debounceが正なら既定上限1mです。正のbatchWindowでdebounce延長を制限します。負数も検証を通り、非正値として扱います。 |
| `spec.trigger.debounce` | *任意* | string | 新たに対象となった最後のイベントからの待機期間です。既定の空・0は無効で、同じイベントの再読取では延長しません。debounceとbatchWindowの早い方で実行し、正のbatchWindowがなければ上限1mです。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventsubscription/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventsubscription/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。
