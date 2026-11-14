# VRF

[各資源的設定](../resources.md#vrf) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

宣告Linux VRF裝置、路由表與成員意圖。目前產生計畫/artifact識別並保護路由表，但沒有建立VRF/連接成員的controller；計畫輸出不證明VRF已建立。

## 指定條件與參照關係

- members須為Router已宣告的邏輯介面，拒絕自身成員。routeTable也用於策略路由清理時保留該表，BGP的VRF設定亦可參照。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    [ ifname: <string> ]
    [ members:
        - <string>
    ]
    routeTable: <integer>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.ifname` | *選填* | string | 核心VRF名稱，預設metadata.name；最多15字元，不可含空白或/。 |
| `spec.routeTable` | **必填** | integer; minimum=1; maximum=4294967295 | 必填正路由表ID，無預設值。Schema為1–4294967295，目前Go驗證檢查下限。 |
| `spec.members` | *選填* | array | 邏輯介面名稱清單，預設空，不加Kind/；每項須存在且不可為本VRF的metadata.name，實際連接未實作。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/apply/artifacts.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/artifacts.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `ifname`, `routeTable`, `members`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)
