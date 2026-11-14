# NTPClient

[各資源的設定](../resources.md#ntpclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

管理或觀察向上游同步時間的用戶端。

## 指定條件與參照關係

- managed=true須宣告servers/serverFrom/fallbackServers至少一項，static還要求servers本身。無法解析上游時用戶端為Pending/NoServers；管理chrony時停用系統systemd-timesyncd。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    [ fallbackServers:
        - <string>
    ]
    [ interface: <string> ]
    [ managed: <boolean> ]
    [ provider: ("systemd-timesyncd" | "chrony" | "ntpd") ]
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
| `spec.provider` | *選填* | string; enum=["systemd-timesyncd", "chrony", "ntpd"] | 用戶端實作，通常預設systemd-timesyncd，FreeBSD預設ntpd，也接受chrony。ntpd需要FreeBSD rc.d，systemd-timesyncd需要平台支援。 |
| `spec.managed` | *選填* | boolean | 預設false，只記錄Observed，不設定或啟動時間服務；管理檔案與服務時設true。 |
| `spec.source` | *選填* | string; enum=["static", "auto", "dhcp", "dhcpv6"] | 預設static，使用servers。auto/dhcp/dhcpv6皆依serverFrom解析值、fallbackServers、servers的順序選取，不會自動探索DHCP來源。 |
| `spec.interface` | *選填* | string | 選填邏輯介面名稱，預設空；指定時Router整體驗證要求存在。NTP controller不用它選來源或綁定服務。 |
| `spec.servers` | **條件必填** | array | 上游主機名稱或IP，預設空；managed=true且source省略或static時必填。各項須為非空白單一值，不是資源參照。 |
| `spec.serverFrom` | *選填* | array | 明確值來源清單，預設空。例如DHCPv4Client/wan的ntpServers或DHCPv6PrefixDelegation/wan-pd的sntpServers。只用於auto/dhcp/dhcpv6，每個resource與field須存在。 此解析器不使用optional旗標，缺值即選下一種替代來源。 |
| `spec.fallbackServers` | *選填* | array | 備用上游，預設空；動態來源的serverFrom無值時採用。systemd-timesyncd還會產生FallbackNTP，chrony與ntpd不產生獨立備用指令。 |
| `spec.when` | *選填* | object | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `servers`, `source`, `updatedAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)
