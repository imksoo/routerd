# ProviderActionPolicy

[リソース別の設定](../resources.md#provideractionpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

実行用Pluginに渡すプロバイダー操作の許可条件です。ポリシーの宣言だけでは操作やクラウドへのログインは行いません。

## 指定条件と参照関係

- 最初のProviderActionPolicyだけを使います。省略時はenabled:false・dryRunOnly:true・requireApproval:true・プロバイダーと操作の許可なし・maxActionsPerRun:0です。各許可条件は組み合わせて適用し、適用対象の条件はdry-runの事前確認にも必要です。
- 自動実行にはenabled:true・dryRunOnly:false・requireApproval:false・正の上限に加え、許可リストへの適合とexecute.providerAction能力を持つ適切な実行Pluginが必要です。外部認証情報と実際のプロバイダー権限は実行Plugin側で扱います。
- executionWindowは現行実装でどの文字列も許可し、実行時間を制限しません。allowUndoは必要なundo情報がある場合のベストエフォートな巻き戻しを許可するだけで、原子的なトランザクションを保証しません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # 任意; boolean
    allowedActions: # 任意; array
      - "<string>" # string
    allowedCIDRs: # 任意; array
      - "<string>" # string
    allowedProviderRefs: # 任意; array
      - "<string>" # string
    allowedProviders: # 任意; array
      - "<string>" # string
    dryRunOnly: true # 任意; boolean
    enabled: true # 任意; boolean
    executionWindow: "<string>" # 任意; string
    maxActionsPerRun: 0 # 任意; integer
    requireApproval: true # 任意; boolean
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.enabled` | 既定はfalseです。操作実行・事前確認の許可にはtrueが必要です。 |
| `spec.dryRunOnly` | 既定はtrueです。falseでも他の条件をすべて満たす必要があります。trueの事前確認にもenabled・許可リストが必要です。 |
| `spec.requireApproval` | 既定はtrueです。falseによる自動承認はenabled:true・dryRunOnly:falseの場合だけです。許可リストやPlugin選択を回避しません。 |
| `spec.allowedProviders` | aws・azure・oci・gcpの完全一致許可リストです。既定の空はすべて拒否し、前後空白のない標準表記を使います。 |
| `spec.allowedProviderRefs` | 操作のproviderRefに対する任意の完全一致リストです。空なら参照名の制限なしです。状態値参照として解決せず、この検証ではプロファイルの存在も確認しません。実際の操作のproviderRefと表記を合わせます。 |
| `spec.allowedActions` | assign-secondary-ip・unassign-secondary-ip・assign-route-table-route・unassign-route-table-route・ensure-forwarding-enabled・ensure-forwarding-disabledの完全一致許可リストです。空ならすべて拒否します。 |
| `spec.allowedCIDRs` | 任意のIPv4・IPv6 CIDRです。空ならCIDR制限なし、指定時はtarget.addressの存在と、IP部分がいずれかの範囲内であることを要求します。CIDR形式の対象でもホスト部で判定し、Pluginが変更する全アドレスを検査するものではありません。 |
| `spec.maxActionsPerRun` | 0以上の整数で、既定の0は実行を拒否します。正の値は自動コントローラーの1回の候補選択件数を制限し、単一操作のエンジン呼び出しは正であることだけを確認します。累計件数や課金額の上限ではありません。 |
| `spec.allowUndo` | 既定はfalseです。trueはundo情報とポリシー条件を満たす場合のベストエフォートな巻き戻しを許可します。 |
| `spec.executionWindow` | 任意の自由形式文字列で、既定は空です。現行処理ではスケジュールとして解析せず実行時にも制約しないため、時間制限にはなりません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go) · [pkg/controller/provideraction/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/provideraction/controller.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
