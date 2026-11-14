# DHCPv4Reservation

[各資源的設定](../resources.md#dhcpv4reservation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在已宣告的 DHCPv4Server 為一個 MAC 位址保留 IPv4 位址。

## 指定條件與參照關係

- 雖然結構描述將 server 標為選用，server、macAddress、ipAddress 在語意上必填。server 須為 interface 非空的 DHCPv4Server 裸名；一般驗證即使僅有一台也不允許省略。scope 會被拒絕。
- 目前在共用 dnsmasq 主機附檔寫入帶資源標籤的保留。server 關聯用於驗證、狀態及自訂選項產生，不代表獨立程序或主機檔案命名空間。Applied 不證明客戶端已取得租約。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 選填; string
    ipAddress: "<string>" # 必填; string
    leaseTime: "<string>" # 選填; string
    macAddress: "<string>" # 必填; string
    options: # 選填; array
      - # object
        code: 1 # 選填; integer; minimum=1; maximum=65535
        name: "<string>" # 選填; string
        value: "<string>" # 選填; string
    scope: "<string>" # 選填; string
    server: "<string>" # 選填; string
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
| `spec.scope` | 已移除欄位，非空值會被拒絕，請用 server。 |
| `spec.server` | 必填已設定 interface 的既有直接 DHCPv4Server metadata.name，此處不移除 Kind/name 前綴。 |
| `spec.macAddress` | 必填可由 net.ParseMAC 解析的 MAC 位址，以小寫輸出，無預設值。 |
| `spec.hostname` | 選用且不含空白或逗號的主機名稱，預設省略。可供已啟用 DNSZone 的 DHCP 衍生主機快照使用。 |
| `spec.ipAddress` | 必填純 IPv4 保留位址。目前驗證位址族，但此處不要求位於伺服器集區內，也不拒絕重複保留 IP/MAC。 |
| `spec.leaseTime` | 選用後端期間字串，預設空值且拒絕逗號。目前執行時保留主機行不輸出此欄位，因此不會在此路徑覆寫租約時間。 |
| `spec.options` | 以此保留標籤輸出的選用 DHCP 選項清單，每項須有 code/name 其中之一及非空 value，預設空。 |
| `spec.options[].code` | 數字選項識別碼，與 name 互斥，結構描述範圍 1–65535，無預設識別碼。 |
| `spec.options[].name` | dnsmasq 選項名稱，與 code 互斥，不可含空白或逗號。 |
| `spec.options[].value` | 必填非空後端格式值，不可含 CR/LF；原樣傳入，逗號可分隔多個選項值。 |
| `spec.when` | 選用共用狀態條件，預設無條件。不成立時排除保留並回報 Pending/WhenFalse，伺服器不可用時為 ServerPending。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)
