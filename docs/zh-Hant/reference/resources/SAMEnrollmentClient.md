# SAMEnrollmentClient

[各資源的設定](../resources.md#samenrollmentclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

將本機註冊要求提交至啟動 RR ControlAPI，取得允許的拓撲，儲存為動態 SAMRRSet 及選用 direct 對等群組供既有控制器使用，不建立雲端基礎設施。

## 指定條件與參照關係

- claimRef 須解析為本機 SAMEnrollmentClaim，執行時該要求的 rrSetRef 須為 SAMRRSet/name。leaf 要求可參照遠端原則，不需在本機複製 SAMEnrollmentPolicy。
- 至少需一個 bootstrapEndpoints 或 rrSocket，兩者不互斥。先使用 Unix 通訊端，再依序使用 HTTP(S) 端點。一般接納可使用成功的 RR 回應；direct 拓撲要求設定端點間一致，無法安全接納時保留 RR 路徑。
- 停用 direct-mesh 時，會先移除快取 direct 拓撲，再聯絡可能無法連線的 RR。用戶端重試/更新狀態會保存，拓撲更新不代表隧道或 BGP 已運作。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    bootstrapEndpoints: # 選填; array
      - "<string>" # string
    claimRef: "<string>" # 必填; string
    controlAPITLS: # 選填; object
      caFile: "<string>" # 選填; string
      certFile: "<string>" # 選填; string
      insecureSkipVerify: true # 選填; boolean
      keyFile: "<string>" # 選填; string
      serverName: "<string>" # 選填; string
    controlAPITokenFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
    retryBackoff: # 選填; object
      max: "<string>" # 選填; string
      min: "<string>" # 選填; string
    rrSocket: "<string>" # 選填; string
    stateTTLRefreshBefore: "<string>" # 選填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.claimRef` | 必填同一 Router 宣告的 SAMEnrollmentClaim/name，不使用狀態欄位選擇器。 |
| `spec.bootstrapEndpoints` | 選用且有順序的絕對 HTTP(S) ControlAPI URL，須含主機。未提供 rrSocket 時至少一個，不自動探索。每個註冊要求目前使用 10 秒要求期限。 |
| `spec.controlAPITokenFrom` | 僅供 HTTP(S) 用戶端使用的選用 Bearer 權杖來源。省略不傳 Bearer 標頭，提供時 file/env 只能指定一項，與要求的 join HMAC 憑證不同。 |
| `spec.controlAPITokenFrom.file` | 路由器本機原始權杖檔，與 env 互斥。會移除首尾空白；設定 HTTP 端點時，缺少、無法讀取或為空會失敗。 |
| `spec.controlAPITokenFrom.env` | 存放權杖的 routerd 程序環境變數名稱，與 file 互斥，不是環境檔路徑。 |
| `spec.controlAPITokenFrom.base64` | 預設 false，true 執行標準 Base64 解碼並移除解碼後首尾空白，不是加密。 |
| `spec.controlAPITLS` | 選用 HTTPS 用戶端設定，不用於 Unix 通訊端，不會將 http URL 自動轉成 https。 |
| `spec.controlAPITLS.caFile` | 選用 PEM CA 檔，提供時以檔案內憑證取代用戶端根憑證集；省略使用一般信任設定。 |
| `spec.controlAPITLS.certFile` | 選用用戶端憑證檔，須與 keyFile 同時設定，沒有預設憑證或自動產生的憑證。 |
| `spec.controlAPITLS.keyFile` | 選用用戶端私密金鑰檔，須與 certFile 同時設定。 |
| `spec.controlAPITLS.serverName` | 選用 TLS SNI/驗證名稱覆寫，空值依 URL 主機行為，不變更端點路由。 |
| `spec.controlAPITLS.insecureSkipVerify` | 預設 false。true 停用伺服器憑證鏈及主機名稱驗證，對純 HTTP URL 無效。 |
| `spec.rrSocket` | 選用本機 RR Unix 通訊端絕對路徑，可與 HTTP(S) 端點並用。Unix 用戶端不使用 Bearer 權杖或 TLS。 |
| `spec.stateTTLRefreshBefore` | 提前更新的 Go duration，預設 10 分鐘。若大於等於觀察到的正 RR 租約長度，執行時改用一半租約以避免立即重複更新。驗證僅檢查可解析性，不強制正值，請用正值。 |
| `spec.retryBackoff` | 選用指數重試設定，失敗時由 min 倍增至 max。direct 拓撲恢復另有較短的收斂重試路徑。 |
| `spec.retryBackoff.min` | Go duration，預設 10 秒。僅驗證可解析性，接受零或負值；請用正值避免立即或排在過去的重試。 |
| `spec.retryBackoff.max` | Go duration，預設 15 分鐘。min/max 都明確指定時要求 max>=min，請用正值。驗證比較不納入省略欄位的預設值。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go) · [pkg/controller/mobility/core_helpers.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/core_helpers.go) · [pkg/controlapi/client.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/client.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
