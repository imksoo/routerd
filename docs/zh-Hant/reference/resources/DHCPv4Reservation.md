# DHCPv4Reservation

[各資源的設定](../resources.md#dhcpv4reservation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在已宣告的 DHCPv4Server 為一個 MAC 位址保留 IPv4 位址。

## 指定條件與參照關係

- 雖然結構描述將 server 標為選用，server、macAddress、ipAddress 在語意上必填。server 須為 interface 非空的 DHCPv4Server 裸名；一般驗證即使僅有一台也不允許省略。scope 會被拒絕。
- 目前在共用 dnsmasq 主機附檔寫入帶資源標籤的保留。server 關聯用於驗證、狀態及自訂選項產生，不代表獨立程序或主機檔案命名空間。Applied 不證明客戶端已取得租約。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    [ hostname: <string> ]
    ipAddress: <string>
    [ leaseTime: <string> ]
    macAddress: <string>
    [ options:
        -
          [ code: <integer> ]
          [ name: <string> ]
          [ value: <string> ]
    ]
    [ scope: <string> ]
    server: <string>
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.scope` | *選填* | string | 已移除欄位，非空值會被拒絕，請用 server。 |
| `spec.server` | **必填** | string | 必填已設定 interface 的既有直接 DHCPv4Server metadata.name，此處不移除 Kind/name 前綴。 |
| `spec.macAddress` | **必填** | string | 必填可由 net.ParseMAC 解析的 MAC 位址，以小寫輸出，無預設值。 |
| `spec.hostname` | *選填* | string | 選用且不含空白或逗號的主機名稱，預設省略。可供已啟用 DNSZone 的 DHCP 衍生主機快照使用。 |
| `spec.ipAddress` | **必填** | string | 必填純 IPv4 保留位址。目前驗證位址族，但此處不要求位於伺服器集區內，也不拒絕重複保留 IP/MAC。 |
| `spec.leaseTime` | *選填* | string | 選用後端期間字串，預設空值且拒絕逗號。目前執行時保留主機行不輸出此欄位，因此不會在此路徑覆寫租約時間。 |
| `spec.options` | *選填* | array | 以此保留標籤輸出的選用 DHCP 選項清單，每項須有 code/name 其中之一及非空 value，預設空。 |
| `spec.options[].code` | **條件必填** | integer; minimum=1; maximum=65535 | 數字選項識別碼，與 name 互斥，結構描述範圍 1–65535，無預設識別碼。 |
| `spec.options[].name` | **條件必填** | string | dnsmasq 選項名稱，與 code 互斥，不可含空白或逗號。 |
| `spec.options[].value` | *選填* | string | 必填非空後端格式值，不可含 CR/LF；原樣傳入，逗號可分隔多個選項值。 |
| `spec.when` | *選填* | object | 選用共用狀態條件，預設無條件。不成立時排除保留並回報 Pending/WhenFalse，伺服器不可用時為 ServerPending。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `address`, `hostname`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)
