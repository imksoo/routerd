# DynamicOverridePolicy

[各資源的設定](../resources.md#dynamicoverridepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

授權指定動態來源在動態部分有效期間遮蔽特定啟動設定資源，不改寫已儲存的啟動設定。

## 指定條件與參照關係

- 以來源、操作及目標apiVersion/kind/name完全相符判定。沒有匹配allow時拒絕遮蔽；同一目標的多個有效遮蔽合併至最晚到期時間。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow: # 必填; array
      - # object
        operations: # 必填; array
          - "<string>" # string
        source: "<string>" # 必填; string
        targets: # 必填; array
          - # object
            apiVersion: "<string>" # 必填; string
            kind: "<string>" # 必填; string
            name: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.allow` | 允許規則清單；空清單無效果且不授權。 |
| `spec.allow[].source` | 必填來源識別，須與DynamicConfigPart.spec.source完全一致；不一定是Plugin或DynamicConfigSource名稱，無萬用字元與預設值。 |
| `spec.allow[].operations` | 允許操作；只接受mask，空清單不允許任何操作。 |
| `spec.allow[].targets` | 可遮蔽的啟動設定資源完整識別清單；空清單不允許任何目標。 |
| `spec.allow[].targets[].apiVersion` | 目標API群組與版本；每項必填，無預設值。 |
| `spec.allow[].targets[].kind` | 目標資源種類，必填；不展開萬用字元。 |
| `spec.allow[].targets[].name` | 必填，與目標metadata.name完全相符；不展開萬用字元。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_config.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_config.go) · [pkg/dynamicconfig/merge.go](https://github.com/imksoo/routerd/blob/main/pkg/dynamicconfig/merge.go)
