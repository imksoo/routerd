# DNSResolver

[各資源的設定](../resources.md#dnsresolver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

具有 UDP 與 TCP 接聽端點、連接 DNSForwarder 規則的本機 DNS 服務。

## 指定條件與參照關係

- 至少一個 DNSForwarder 必須參照此解析器。請以獨立資源設定轉送規則，舊式內嵌 sources 並非支援的 YAML 輸入。
- 省略 listen 或使用空清單會正規化為 127.0.0.1:5053。每個端點皆提供 UDP 與 TCP。允許遞迴查詢的端點應限於可信任網路。
- 固定與動態接聽位址會合併去重。必要動態值未解析時會記錄等待相依，但仍使用其他有效位址；若無任何可用端點則維持 Pending。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    cache: # 選填; object
      enabled: true # 選填; boolean
      maxEntries: 0 # 選填; integer; minimum=0
      maxTTL: "<string>" # 選填; string
      minTTL: "<string>" # 選填; string
      negativeTTL: "<string>" # 選填; string
    listen: # 必填; array
      - # object
        addressFrom: # 選填; array
          - # object
            field: "<string>" # 選填; string
            optional: true # 選填; boolean
            resource: "<string>" # 必填; string
        addresses: # 選填; array
          - "<string>" # string
        name: "<string>" # 選填; string
        port: 1 # 選填; integer; minimum=1; maximum=65535
        sources: # 選填; array
          - "<string>" # string
    metrics: # 選填; object
      perUpstream: true # 選填; boolean
    queryLog: # 選填; object
      enabled: true # 選填; boolean
      path: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.listen` | 接聽端點清單；為空時補入回送端點。 |
| `spec.listen[].name` | 選用標籤，依清單順序預設 listen-0、listen-1……。 |
| `spec.listen[].addresses` | 不含連接埠或 CIDR 的 IPv4/IPv6 位址；與 addressFrom 同時為空時預設 [127.0.0.1]。 |
| `spec.listen[].addressFrom` | 選用的共用 resource/field/optional 來源清單。須有已宣告資源與明確的提供欄位。IPv4StaticAddress.address 可在狀態建立前讀取宣告值，CIDR 取位址部分。optional 預設 false；選用值未解析則略過。 |
| `spec.listen[].port` | 選用連接埠 1–65535，省略或 0 會正規化為 5053。 |
| `spec.listen[].sources` | 此解析器所屬 DNSForwarder 名稱清單；空值選取全部。請用裸 metadata.name：驗證雖接受帶類型前綴的名稱，但常駐程序不移除前綴。清單順序不會重排轉送規則。 |
| `spec.cache` | 選用的回應快取設定。僅快取上游回應，不快取本機區域回應；預設停用。 |
| `spec.cache.enabled` | 預設 false；true 啟用記憶體回應快取。 |
| `spec.cache.maxEntries` | 省略或 0 時為 10000；結構描述要求非負值。限制項目數而非位元組數。 |
| `spec.cache.minTTL` | 選用的 Go duration 快取時間下限；空值不限制。不合法期間在此路徑會被忽略，不會改寫回應中的 TTL。 |
| `spec.cache.maxTTL` | 選用的 Go duration 上限，於 minTTL 之後套用；空值不限制，不合法期間忽略。 |
| `spec.cache.negativeTTL` | 選用的 Go duration，於 min/max 之後覆寫空的成功回應或 NXDOMAIN 保留時間。預設空值使用套用上下限前的 60 秒；不快取 SERVFAIL 等暫時失敗。 |
| `spec.metrics` | 指標選項；目前執行程式未使用 perUpstream。 |
| `spec.metrics.perUpstream` | 解碼為布林值，預設 false；目前不切換個別上游指標。 |
| `spec.queryLog` | 選用的 SQLite 查詢記錄，預設停用。 |
| `spec.queryLog.enabled` | 預設 false；true 依 queryLog.path 記錄查詢。 |
| `spec.queryLog.path` | 啟用時省略或空白預設 /var/lib/routerd/dns-queries.db，為資料庫路徑而非文字記錄。 |
| `spec.when` | 選用共用狀態條件，預設無條件。state/all/any 與結構描述限制請見共用欄位。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `listeners`, `listenAddresses`, `sources`, `updatedAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
