# DynamicOverridePolicy

[各資源的設定](../resources.md#dynamicoverridepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

授權指定動態來源在動態部分有效期間遮蔽特定啟動設定資源，不改寫已儲存的啟動設定。

## 指定條件與參照關係

- 以來源、操作及目標apiVersion/kind/name完全相符判定。沒有匹配allow時拒絕遮蔽；同一目標的多個有效遮蔽合併至最晚到期時間。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow:
      -
        operations:
          - <string>
        source: <string>
        targets:
          -
            apiVersion: <string>
            kind: <string>
            name: <string>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.allow` | **必填** | array | 允許規則清單；空清單無效果且不授權。 |
| `spec.allow[].source` | **必填** | string | 必填來源識別，須與DynamicConfigPart.spec.source完全一致；不一定是Plugin或DynamicConfigSource名稱，無萬用字元與預設值。 |
| `spec.allow[].operations` | **必填** | array | 允許操作；只接受mask，空清單不允許任何操作。 |
| `spec.allow[].targets` | **必填** | array | 可遮蔽的啟動設定資源完整識別清單；空清單不允許任何目標。 |
| `spec.allow[].targets[].apiVersion` | **必填** | string | 目標API群組與版本；每項必填，無預設值。 |
| `spec.allow[].targets[].kind` | **必填** | string | 目標資源種類，必填；不展開萬用字元。 |
| `spec.allow[].targets[].name` | **必填** | string | 必填，與目標metadata.name完全相符；不展開萬用字元。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_config.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_config.go) · [pkg/dynamicconfig/merge.go](https://github.com/imksoo/routerd/blob/main/pkg/dynamicconfig/merge.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

此 Kind 未登錄一般狀態參照的允許欄位。
