# ControlAPI

[各資源的設定](../resources.md#controlapi) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

啟用可讀狀態及變更設定的HTTP Control API，與Unix socket控制及WebConsole分開。

## 指定條件與參照關係

- 只支援一個ControlAPI。明確HTTP CLI旗標優先於相應資源設定；沒有資源或啟用旗標時停用HTTP控制。
- tls.certFile與tls.keyFile須一起指定；clientCAFile另須兩者，並要求及驗證用戶端憑證。tokenFrom.file與env互斥。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # 選填; array
      - "<string>" # string
    enabled: true # 選填; boolean
    listenAddress: "<string>" # 選填; string
    port: 1 # 選填; integer; minimum=1; maximum=65535
    tls: # 選填; object
      certFile: "<string>" # 選填; string
      clientCAFile: "<string>" # 選填; string
      keyFile: "<string>" # 選填; string
    tokenFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.enabled` | 資源存在時預設true，不存在時停用；明確HTTP監聽CLI旗標會啟用。 |
| `spec.listenAddress` | 綁定IP，預設127.0.0.1；非空白值須為IP，不會自行變更來源CIDR允許清單。 |
| `spec.port` | TCP埠，預設65432；解碼值0也採預設，其餘須為1–65535。 |
| `spec.allowCIDRs` | 來源IP允許清單，預設127.0.0.1/32與::1/128，非空清單會取代預設。須為有效CIDR，拒絕0.0.0.0/0與::/0，與防火牆規則不同。 |
| `spec.tokenFrom` | 選填Bearer token來源；省略不檢查token，但仍限制來源CIDR。使用時只指定file或env其中一項，不是routerd資源參照。 |
| `spec.tokenFrom.file` | token檔案路徑，預設空，與env互斥；HTTP API初始化時由routerd讀取，無法讀取則初始化失敗。 |
| `spec.tokenFrom.env` | routerd程序環境變數名稱，預設空，與file互斥；變數不存在則HTTP API初始化失敗。 |
| `spec.tokenFrom.base64` | 預設false。true時先修剪來源，再用標準Base64解碼並再次修剪；格式錯誤或最終空token使初始化失敗。 |
| `spec.tls` | 選填伺服器TLS設定，未指定憑證/金鑰時監聽為明文HTTP。 |
| `spec.tls.certFile` | 伺服器憑證檔，預設未指定，須搭配keyFile。啟動時載入X.509金鑰對，最低TLS 1.2。 |
| `spec.tls.keyFile` | 伺服器私鑰檔，預設未指定，須搭配certFile；為本機檔案路徑，不是資源參照。 |
| `spec.tls.clientCAFile` | 選填PEM用戶端CA集合，須搭配certFile/keyFile並強制驗證用戶端憑證；檔案缺少或無效則啟動失敗。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/controlapi/server.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/server.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)
