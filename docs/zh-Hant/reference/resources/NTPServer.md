# NTPServer

[各資源的設定](../resources.md#ntpserver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

使用chrony或FreeBSD ntpd設定時間伺服器與上游。

## 指定條件與參照關係

- managed預設false；true須宣告上游來源，static另須servers。接聽與允許來源清單會合併，上游替代來源則依source順序選擇。
- ntpd產生器不輸出allowCIDRs限制，不能假設宣告此清單就會對ntpd套用用戶端ACL。
- 動態來源模式下，上游值未解析本身不會停止產生伺服器設定，可能沒有上游。Applied表示設定/服務處理，不證明時鐘已同步。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    [ allowCIDRFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ allowCIDRs:
        - <string>
    ]
    [ fallbackServers:
        - <string>
    ]
    [ listenAddressFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ listenAddresses:
        - <string>
    ]
    [ managed: <boolean> ]
    [ provider: ("chrony" | "ntpd") ]
    [ serverFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ servers:
        - <string>
    ]
    [ source: ("static" | "auto" | "dhcp" | "dhcpv6") ]
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
| `spec.managed` | *選填* | boolean | 預設false，只記錄Observed，不設定或啟動時間服務；管理檔案與服務時設true。 |
| `spec.source` | *選填* | string; enum=["static", "auto", "dhcp", "dhcpv6"] | 預設static，使用servers。auto/dhcp/dhcpv6皆依serverFrom解析值、fallbackServers、servers的順序選取，不會自動探索DHCP來源。 |
| `spec.servers` | **條件必填** | array | 上游主機名稱或IP，預設空；managed=true且source省略或static時必填。各項須為非空白單一值，不是資源參照。 |
| `spec.serverFrom` | *選填* | array | 明確值來源清單，預設空。例如DHCPv4Client/wan的ntpServers或DHCPv6PrefixDelegation/wan-pd的sntpServers。只用於auto/dhcp/dhcpv6，每個resource與field須存在。 此解析器不使用optional旗標，缺值即選下一種替代來源。 |
| `spec.when` | *選填* | object | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |
| `spec.provider` | *選填* | string; enum=["chrony", "ntpd"] | 伺服器實作，通常預設chrony，FreeBSD預設ntpd；ntpd僅支援FreeBSD。 |
| `spec.fallbackServers` | *選填* | array | 備用上游清單，預設空；auto/dhcp/dhcpv6的serverFrom無值時，優先於servers採用，不產生獨立備用指令。 |
| `spec.listenAddresses` | *選填* | array | 靜態接聽IP，預設空；CIDR取其主機IP。解析結果為空時不產生bindaddress/interface listen指令，使用後端預設行為。 |
| `spec.listenAddressFrom` | *選填* | array | 額外接聽位址來源，預設空，與listenAddresses合併。每項須有resource與field。已宣告接聽設定但合併結果為空時，為Pending/NoListenAddresses。 |
| `spec.allowCIDRs` | *選填* | array | 允許用戶端CIDR清單，預設空。chrony產生各allow，ntpd產生設定不使用此清單。靜態值在驗證時只檢查非空白，不檢查CIDR語法。 |
| `spec.allowCIDRFrom` | *選填* | array | 與allowCIDRs合併的CIDR來源，預設空。非optional來源缺值時為Pending/AllowCIDRFromPending；衍生CIDR錯誤時為Pending/AllowCIDRFromInvalid。不接受沒有前綴長度的裸IP。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `servers`, `listenAddresses`, `allowCIDRs`, `source`, `updatedAt`
