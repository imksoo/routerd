# CloudProviderProfile

[各資源的設定](../resources.md#cloudproviderprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

描述規劃供應商操作所用的身分及目標中繼資料預設值。此宣告本身不進行雲端驗證或建立資源。

## 指定條件與參照關係

- provider、非空 capabilities、auth.mode 與 auth.command 必填。驗證會檢查宣告，但目前執行流程不執行 auth.command。供應商變更由受 ProviderActionPolicy 控制的獨立執行 Plugin 處理。
- Mobility capture 等使用端以 metadata.name 選擇設定檔，不是共用狀態值來源。subscriptionID/resourceGroup 只補足未設定的操作目標值，不覆蓋明確值。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth:
      command: <string>
      mode: ("external-command")
    capabilities:
      - <string>
    provider: ("azure" | "aws" | "oci" | "gcp")
    [ resourceGroup: <string> ]
    [ subscriptionID: <string> ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.provider` | **必填** | string; enum=["azure", "aws", "oci", "gcp"] | 必填 azure/aws/oci/gcp，無預設值。選擇規劃／執行器標籤，不會自動探索憑證。 |
| `spec.subscriptionID` | *選填* | string | 選用的目標訂閱 ID，預設空。僅在目標缺少 subscriptionId 時補入，不檢查存在或登入。 |
| `spec.resourceGroup` | *選填* | string | 選用的操作目標 resourceGroup 預設值，預設空。不建立或驗證資源群組。 |
| `spec.capabilities` | **必填** | array | 必填非空字串清單，不可含空項目。目前驗證不限制為已知能力詞彙，執行政策也不將它視為操作允許清單。 |
| `spec.auth` | **必填** | object | 必填的驗證描述。目前實作僅驗證，不呼叫命令。 |
| `spec.auth.mode` | **必填** | string; enum=["external-command"] | 必填 external-command，唯一接受的模式。不會將 auth.command 啟動為服務。 |
| `spec.auth.command` | **必填** | string | 必填非空宣告，無預設值。目前執行流程不執行，也不傳遞由此取得的憑證。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/planner.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `provider`, `capabilities`
