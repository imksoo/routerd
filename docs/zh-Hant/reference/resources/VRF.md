# VRF

[各資源的設定](../resources.md#vrf) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

宣告Linux VRF裝置、路由表與成員意圖。目前產生計畫/artifact識別並保護路由表，但沒有建立VRF/連接成員的controller；計畫輸出不證明VRF已建立。

## 指定條件與參照關係

- members須為Router已宣告的邏輯介面，拒絕自身成員。routeTable也用於策略路由清理時保留該表，BGP的VRF設定亦可參照。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 選填; string
    members: # 選填; array
      - "<string>" # string
    routeTable: 1 # 必填; integer; minimum=1; maximum=4294967295
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.ifname` | 核心VRF名稱，預設metadata.name；最多15字元，不可含空白或/。 |
| `spec.routeTable` | 必填正路由表ID，無預設值。Schema為1–4294967295，目前Go驗證檢查下限。 |
| `spec.members` | 邏輯介面名稱清單，預設空，不加Kind/；每項須存在且不可為本VRF的metadata.name，實際連接未實作。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/apply/artifacts.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/artifacts.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)
