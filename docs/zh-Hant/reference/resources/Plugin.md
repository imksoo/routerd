# Plugin

[各資源的設定](../resources.md#plugin) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

註冊實作routerd外掛協定的執行檔，由明確操作或EventSubscription等使用端呼叫。

## 指定條件與參照關係

- executable必須是絕對路徑。執行時須為存在的一般檔案，且至少有一個執行權限位元。
- 目前實作不會僅因宣告triggers而啟動週期或事件排程；它會驗證並在plugin list顯示，仍須由使用端呼叫。
- context使用apiVersion、kind、name三項識別資源。目標不存在不會造成設定驗證錯誤，而是在執行時省略該項；秘密資訊會遮蔽。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: Plugin
  metadata:
    name: "<name>"
  spec:
    capabilities: # 選填; array
      - "observe.cloud" # string; enum=["observe.cloud", "observe.providerPrivateIPs", "propose.dynamicConfig", "propose.providerAction", "execute.providerAction"]
    context: # 選填; object
      resources: # 選填; array
        - # object
          apiVersion: "<string>" # 必填; string
          kind: "<string>" # 必填; string
          name: "<string>" # 必填; string
    env: # 選填; object
      "<key>": "<string>" # 選填; string
    executable: "<string>" # 必填; string
    timeout: "<string>" # 選填; string
    triggers: # 選填; array
      - # object
        every: "<string>" # 選填; string
        topic: "<string>" # 選填; string
        type: "interval" # 必填; string; enum=["interval", "event"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.executable` | 外部程式的絕對路徑；必填，無預設值。 |
| `spec.timeout` | 單次執行時間上限。預設10s；明確指定時須為正的Go duration，例如30s。 |
| `spec.env` | 以變數名稱為鍵的額外環境變數，預設空。runner先設定自身PATH，缺少時使用內建路徑，再套用這些值；不繼承其他父程序變數。忽略空鍵及含=的鍵。 |
| `spec.capabilities` | 能力宣告，預設空。接受observe.cloud、observe.providerPrivateIPs、propose.dynamicConfig、propose.providerAction、execute.providerAction。清查執行須有observe.providerPrivateIPs；供應商操作執行須有execute.providerAction；此清單本身不會啟動執行。 |
| `spec.context` | 控制透過標準輸入傳入哪些設定資源的允許清單；預設空，不傳入設定。 |
| `spec.context.resources` | 允許的資源識別清單，預設空；不展開萬用字元。 |
| `spec.context.resources[].apiVersion` | 參照設定資源的API群組與版本；每項必填。 |
| `spec.context.resources[].kind` | 參照設定資源的種類；每項必填。 |
| `spec.context.resources[].name` | 參照設定資源的metadata.name；每項必填。 |
| `spec.triggers` | 觸發宣告清單，預設空；目前runtime僅驗證與顯示，請參照上方限制。 |
| `spec.triggers[].type` | 觸發類型；必須指定interval或event，無預設值。 |
| `spec.triggers[].every` | interval時必填，須為正的Go duration；event時此欄位不產生週期排程，無預設值。 |
| `spec.triggers[].topic` | 事件主題宣告；選填，預設空，event時驗證器也不強制要求。這不是資源名稱。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/runner.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/runner.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go) · [pkg/providerinventory/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/providerinventory/executor.go) · [pkg/provideraction/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/executor.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

此 Kind 未登錄一般狀態參照的允許欄位。

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
