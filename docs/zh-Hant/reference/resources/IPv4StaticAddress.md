# IPv4StaticAddress

[各資源的設定](../resources.md#ipv4staticaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在邏輯介面宣告靜態IPv4位址，由controller協調設定，不是DHCP租約。

## 指定條件與參照關係

- interface參照與address/prefix必填。同邏輯介面及遮罩後網路前綴的兩個靜態資源，即使主機位址不同也拒絕；與相同組合的VirtualAddress重疊也拒絕。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: <string>
    [ allowOverlap: <boolean> ]
    [ allowOverlapReason: <string> ]
    [ exclusive: <boolean> ]
    interface: <string>
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
| `spec.interface` | **必填** | string | 必填Router介面索引中的邏輯名稱，不加Interface/，必須在宣告設定存在，無預設值。 |
| `spec.address` | **必填** | string | 必填含前綴長度的IPv4主機位址，例如192.0.2.1/24，無預設值或隱含/32。解析器檢查前綴語法，執行期使用IPv4操作。 |
| `spec.exclusive` | *選填* | boolean | 預設false。目前觀察器警告exclusive清理未實作，執行期位址controller也不讀取此旗標。額外位址的整體協調獨立，false不保證保留手動位址。 |
| `spec.allowOverlap` | *選填* | boolean | 預設false，保留供解碼但目前驗證與位址協調不使用，不會略過重複/衝突檢查。 |
| `spec.allowOverlapReason` | *選填* | string | 預設空，僅保留宣告，不授權重疊或改變allowOverlap效果。 |
| `spec.when` | *選填* | object | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `interface`, `ifname`, `address`, `dryRun`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)
