# FirewallZone

[各資源的設定](../resources.md#firewallzone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

將已設定介面歸入具名信任角色，用於產生防火牆規則。不建立 OS 介面或獨立路由、橋接網域。

## 指定條件與參照關係

- role 與至少一個介面必填，同一區域內重複的介面字串會被拒絕。FirewallRule 使用區域 metadata.name；self 是路由器本機的特殊端點，不是一般介面區域。
- 隱含規則允許 mgmt 前往所有角色與 self，trust 前往 self 及 mgmt 以外的角色。untrust 原則拒絕，但目前共用規則允許相同角色間轉送。既有連線、相關流量與控制協定例外另行評估。OS 順序限制請見 FirewallPolicy/FirewallRule。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces:
      - <string>
    role: ("untrust" | "trust" | "mgmt")
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.role` | **必填** | string; enum=["untrust", "trust", "mgmt"] | 必填 untrust、trust 或 mgmt，無預設值。選擇隱含防火牆行為，不是介面所有權或驗證設定。 |
| `spec.interfaces` | **必填** | array | 必填且非空的邏輯介面參照。單純名稱預設為 Interface；接受 Interface/name、PPPoESession/name、WireGuardInterface/name、DSLiteTunnel/name。必須可在設定介面/通道索引中解析，且產生規則時有非空核心介面名稱。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `interfaces`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
