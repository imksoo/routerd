# ProviderActionPolicy

[各資源的設定](../resources.md#provideractionpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

控制交給執行 Plugin 的供應商操作。宣告政策本身不會執行操作或登入雲端。

## 指定條件與參照關係

- 僅使用第一個 ProviderActionPolicy。省略時 enabled=false、dryRunOnly=true、requireApproval=true，無供應商／操作允許項目且 maxActionsPerRun=0。所有條件共同生效，適用條件也約束 dry-run 預覽。
- 自動執行需 enabled=true、dryRunOnly=false、requireApproval=false、正上限，並符合允許清單及具 execute.providerAction 能力的合適 Plugin。外部憑證與實際供應商權限由執行器處理。
- 目前 executionWindow 接受所有字串，不限制執行時間。allowUndo 僅在有合適 undo 資料時允許盡力回復，不保證原子交易。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # 選填; boolean
    allowedActions: # 選填; array
      - "<string>" # string
    allowedCIDRs: # 選填; array
      - "<string>" # string
    allowedProviderRefs: # 選填; array
      - "<string>" # string
    allowedProviders: # 選填; array
      - "<string>" # string
    dryRunOnly: true # 選填; boolean
    enabled: true # 選填; boolean
    executionWindow: "<string>" # 選填; string
    maxActionsPerRun: 0 # 選填; integer
    requireApproval: true # 選填; boolean
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.enabled` | 預設 false。允許執行或預覽操作需為 true。 |
| `spec.dryRunOnly` | 預設 true。false 僅在其他條件全數通過時允許實際變更；true 的預覽仍需 enabled 與允許清單。 |
| `spec.requireApproval` | 預設 true。false 僅在 enabled=true 且 dryRunOnly=false 時允許自動核准，不略過允許清單或執行器選擇。 |
| `spec.allowedProviders` | 精確比對 aws/azure/oci/gcp 的允許清單，預設空表示全部拒絕。使用無前後空白的標準拼寫。 |
| `spec.allowedProviderRefs` | 選用的操作 providerRef 精確允許清單，空表示不限制參照。不解析為狀態來源，此驗證也不檢查設定檔存在；須符合實際操作的 providerRef 拼寫。 |
| `spec.allowedActions` | 精確允許標準操作：assign-secondary-ip、unassign-secondary-ip、assign-route-table-route、unassign-route-table-route、ensure-forwarding-enabled、ensure-forwarding-disabled。空表示全拒絕。 |
| `spec.allowedCIDRs` | 選用 IPv4/IPv6 CIDR。空不加限制；非空要求 target.address 存在，且其 IP（含 CIDR 輸入的主機部分）落於任一前綴。不檢查執行器可能變更的所有位址。 |
| `spec.maxActionsPerRun` | 非負整數，預設 0 拒絕執行。正值限制自動控制器每輪候選數；單一操作引擎呼叫僅檢查為正值。不是累計或費用預算。 |
| `spec.allowUndo` | 預設 false；true 允許在具備 undo 資料並符合政策檢查時盡力回復。 |
| `spec.executionWindow` | 選用的自由字串，預設空。目前不解析為排程，也不在執行時強制限制，因此不限制時間。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go) · [pkg/controller/provideraction/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/provideraction/controller.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

此 Kind 未登錄一般狀態參照的允許欄位。

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
