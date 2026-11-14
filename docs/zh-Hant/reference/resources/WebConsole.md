# WebConsole

[各資源的設定](../resources.md#webconsole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

由routerd提供唯讀Web主控台，HTTP監聽與可變更設定的ControlAPI不同。

## 指定條件與參照關係

- runtime選第一個WebConsole，不為每項各開監聽。listenAddressFrom解析值優先，listenAddress可作備用，兩者不互斥。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    basePath: "<string>" # 選填; string
    enabled: true # 選填; boolean
    links: # 選填; array
      - # object
        description: "<string>" # 選填; string
        label: "<string>" # 必填; string
        url: "<string>" # 必填; string
    listenAddress: "<string>" # 選填; string
    listenAddressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    port: 1 # 選填; integer; minimum=1; maximum=65535
    title: "<string>" # 選填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.enabled` | 存在WebConsole資源時預設true；false停用所選主控台，沒有資源則不開監聽。 |
| `spec.listenAddress` | 備用綁定位址，預設127.0.0.1；非空白值須為IP，listenAddressFrom解析值優先。 |
| `spec.listenAddressFrom` | 綁定IP來源，預設未指定；先讀狀態，再嘗試支援的設定介面/位址解析。須有resource與field；無法解析且無listenAddress時，即使optional=true仍無法啟動。 |
| `spec.port` | TCP埠，預設8080；解碼後0也選預設，非零值須為1–65535。 |
| `spec.basePath` | HTTP路徑前綴，預設/；非空值須以/開頭，不可含NUL或換行。 |
| `spec.title` | 主控台頁面標題，預設routerd，不會重新命名路由器或資源。 |
| `spec.links` | 額外主控台連結，預設空；label或URL空白時顯示階段略過。 |
| `spec.links[].label` | 連結文字，無預設值，空白不顯示。 |
| `spec.links[].url` | 連結目的URL，無預設值，不是Kind/名稱參照，會去除前後空白。 |
| `spec.links[].description` | 選填說明，預設空，顯示時去除前後空白。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/webconsole/console.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/console.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `listenAddress`, `port`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
