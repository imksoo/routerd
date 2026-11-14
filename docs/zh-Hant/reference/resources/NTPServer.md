# NTPServer

[各資源的設定](../resources.md#ntpserver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

使用chrony或FreeBSD ntpd設定時間伺服器與上游。

## 指定條件與參照關係

- managed預設false；true須宣告上游來源，static另須servers。接聽與允許來源清單會合併，上游替代來源則依source順序選擇。
- ntpd產生器不輸出allowCIDRs限制，不能假設宣告此清單就會對ntpd套用用戶端ACL。
- 動態來源模式下，上游值未解析本身不會停止產生伺服器設定，可能沒有上游。Applied表示設定/服務處理，不證明時鐘已同步。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    allowCIDRFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    allowCIDRs: # 選填; array
      - "<string>" # string
    fallbackServers: # 選填; array
      - "<string>" # string
    listenAddressFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    listenAddresses: # 選填; array
      - "<string>" # string
    managed: true # 選填; boolean
    provider: "chrony" # 選填; string; enum=["chrony", "ntpd"]
    serverFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    servers: # 選填; array
      - "<string>" # string
    source: "static" # 選填; string; enum=["static", "auto", "dhcp", "dhcpv6"]
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
| `spec.managed` | 預設false，只記錄Observed，不設定或啟動時間服務；管理檔案與服務時設true。 |
| `spec.source` | 預設static，使用servers。auto/dhcp/dhcpv6皆依serverFrom解析值、fallbackServers、servers的順序選取，不會自動探索DHCP來源。 |
| `spec.servers` | 上游主機名稱或IP，預設空；managed=true且source省略或static時必填。各項須為非空白單一值，不是資源參照。 |
| `spec.serverFrom` | 明確值來源清單，預設空。例如DHCPv4Client/wan的ntpServers或DHCPv6PrefixDelegation/wan-pd的sntpServers。只用於auto/dhcp/dhcpv6，每個resource與field須存在。 此解析器不使用optional旗標，缺值即選下一種替代來源。 |
| `spec.when` | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |
| `spec.provider` | 伺服器實作，通常預設chrony，FreeBSD預設ntpd；ntpd僅支援FreeBSD。 |
| `spec.fallbackServers` | 備用上游清單，預設空；auto/dhcp/dhcpv6的serverFrom無值時，優先於servers採用，不產生獨立備用指令。 |
| `spec.listenAddresses` | 靜態接聽IP，預設空；CIDR取其主機IP。解析結果為空時不產生bindaddress/interface listen指令，使用後端預設行為。 |
| `spec.listenAddressFrom` | 額外接聽位址來源，預設空，與listenAddresses合併。每項須有resource與field。已宣告接聽設定但合併結果為空時，為Pending/NoListenAddresses。 |
| `spec.allowCIDRs` | 允許用戶端CIDR清單，預設空。chrony產生各allow，ntpd產生設定不使用此清單。靜態值在驗證時只檢查非空白，不檢查CIDR語法。 |
| `spec.allowCIDRFrom` | 與allowCIDRs合併的CIDR來源，預設空。非optional來源缺值時為Pending/AllowCIDRFromPending；衍生CIDR錯誤時為Pending/AllowCIDRFromInvalid。不接受沒有前綴長度的裸IP。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go)
