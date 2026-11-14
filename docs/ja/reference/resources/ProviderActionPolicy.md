# ProviderActionPolicy

[リソース別の設定](../resources.md#provideractionpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

実行用Pluginに渡すプロバイダー操作の許可条件です。ポリシーの宣言だけでは操作やクラウドへのログインは行いません。

## 指定条件と参照関係

- 最初のProviderActionPolicyだけを使います。省略時はenabled:false・dryRunOnly:true・requireApproval:true・プロバイダーと操作の許可なし・maxActionsPerRun:0です。各許可条件は組み合わせて適用し、適用対象の条件はdry-runの事前確認にも必要です。
- 自動実行にはenabled:true・dryRunOnly:false・requireApproval:false・正の上限に加え、許可リストへの適合とexecute.providerAction能力を持つ適切な実行Pluginが必要です。外部認証情報と実際のプロバイダー権限は実行Plugin側で扱います。
- executionWindowは現行実装でどの文字列も許可し、実行時間を制限しません。allowUndoは必要なundo情報がある場合のベストエフォートな巻き戻しを許可するだけで、原子的なトランザクションを保証しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    [ allowUndo: <boolean> ]
    [ allowedActions:
        - <string>
    ]
    [ allowedCIDRs:
        - <string>
    ]
    [ allowedProviderRefs:
        - <string>
    ]
    [ allowedProviders:
        - <string>
    ]
    [ dryRunOnly: <boolean> ]
    [ enabled: <boolean> ]
    [ executionWindow: <string> ]
    [ maxActionsPerRun: <integer> ]
    [ requireApproval: <boolean> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.enabled` | *任意* | boolean | 既定はfalseです。操作実行・事前確認の許可にはtrueが必要です。 |
| `spec.dryRunOnly` | *任意* | boolean | 既定はtrueです。falseでも他の条件をすべて満たす必要があります。trueの事前確認にもenabled・許可リストが必要です。 |
| `spec.requireApproval` | *任意* | boolean | 既定はtrueです。falseによる自動承認はenabled:true・dryRunOnly:falseの場合だけです。許可リストやPlugin選択を回避しません。 |
| `spec.allowedProviders` | *任意* | array | aws・azure・oci・gcpの完全一致許可リストです。既定の空はすべて拒否し、前後空白のない標準表記を使います。 |
| `spec.allowedProviderRefs` | *任意* | array | 操作のproviderRefに対する任意の完全一致リストです。空なら参照名の制限なしです。状態値参照として解決せず、この検証ではプロファイルの存在も確認しません。実際の操作のproviderRefと表記を合わせます。 |
| `spec.allowedActions` | *任意* | array | assign-secondary-ip・unassign-secondary-ip・assign-route-table-route・unassign-route-table-route・ensure-forwarding-enabled・ensure-forwarding-disabledの完全一致許可リストです。空ならすべて拒否します。 |
| `spec.allowedCIDRs` | *任意* | array | 任意のIPv4・IPv6 CIDRです。空ならCIDR制限なし、指定時はtarget.addressの存在と、IP部分がいずれかの範囲内であることを要求します。CIDR形式の対象でもホスト部で判定し、Pluginが変更する全アドレスを検査するものではありません。 |
| `spec.maxActionsPerRun` | *任意* | integer | 0以上の整数で、既定の0は実行を拒否します。正の値は自動コントローラーの1回の候補選択件数を制限し、単一操作のエンジン呼び出しは正であることだけを確認します。累計件数や課金額の上限ではありません。 |
| `spec.allowUndo` | *任意* | boolean | 既定はfalseです。trueはundo情報とポリシー条件を満たす場合のベストエフォートな巻き戻しを許可します。 |
| `spec.executionWindow` | *任意* | string | 任意の自由形式文字列で、既定は空です。現行処理ではスケジュールとして解析せず実行時にも制約しないため、時間制限にはなりません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go) · [pkg/controller/provideraction/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/provideraction/controller.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。
