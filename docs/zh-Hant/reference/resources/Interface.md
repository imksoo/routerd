# Interface

[各資源的設定](../resources.md#interface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

為既有OS介面提供metadata.name邏輯名稱，供其他資源參照，不建立實體介面。

## 指定條件與參照關係

- 相依資源的interface使用metadata.name邏輯名稱，ifname填實際裝置名。managed=false不會抑制已要求的LinkController啟用/MTU變更。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    [ adminUp: <boolean> ]
    ifname: <string>
    managed: <boolean>
    [ mtu: <integer> ]
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
| `spec.ifname` | **必填** | string | 必填既有核心裝置名稱，例如ens18，無預設值；不存在時不建立，而記為Down/InterfaceNotFound。 |
| `spec.managed` | **必填** | boolean | 管理宣告，解碼預設false，但JSON Schema要求明確填寫。影響管理/觀察資訊及周邊處理，不保證adminUp/MTU只觀察不變更。 |
| `spec.adminUp` | *選填* | boolean | 預設false，不要求啟用；true會啟用已存在但關閉的link。false不表示強制關閉。 |
| `spec.mtu` | *選填* | integer; minimum=576; maximum=9216 | 預設0保留目前MTU；非零值須為576–9216，不同時套用。 |
| `spec.when` | *選填* | object | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `ifname`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `primaryIPv4`, `primaryIPv6`, `macAddress`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
