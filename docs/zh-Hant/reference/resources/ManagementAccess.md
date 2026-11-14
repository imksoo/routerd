# ManagementAccess

[各資源的設定](../resources.md#managementaccess) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

宣告套用前管理連線中斷檢查所檢查的介面。此資源不建立管理接聽端或防火牆允許規則。

## 指定條件與參照關係

- 套用前與 routerctl doctor 會執行檢查。fail 結果會阻止實際套用，除非明確指定 --allow-mgmt-lockout；dry-run 僅報告。檢查的是設定，不是實際管理連線。
- 每個介面必須宣告為 Interface。只要存在任何 FirewallZone，每個管理介面就必須屬於 role 為 trust 或 mgmt 的區域。僅有同名 Bridge/WireGuard 資源無法滿足此檢查。
- 未宣告 ManagementAccess 時，此檢查不產生結果。requireWebConsoleBound 檢查明確的萬用接聽位址，不要求 WebConsole 存在，也不證明其位址屬於所列管理介面。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ManagementAccess
  metadata:
    name: "<name>"
  spec:
    allowSourceCIDRs: # 選填; array
      - "<string>" # string
    interfaces: # 必填; array
      - "<string>" # string
    requireWebConsoleBound: true # 選填; boolean
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.interfaces` | 必填且不可為空的 Interface 名稱或 Interface/name 清單。正規化後重複會被拒絕。指的是已宣告資源，不是任意 OS 名稱。 |
| `spec.allowSourceCIDRs` | 選用的 IPv4/IPv6 CIDR 清單，預設為空。會驗證語法，但目前管理檢查與防火牆產生器不使用此欄位，不會強制來源位址存取限制。 |
| `spec.requireWebConsoleBound` | 預設 true。啟用的 WebConsole 明確接聽 0.0.0.0 或 :: 時產生 fail；false 則降為 warn。多個 ManagementAccess 中任一省略或 true 都會啟用要求。動態接聽位址僅警告；未設定來源且省略 listenAddress 時，此處不產生結果。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/management.go](https://github.com/imksoo/routerd/blob/main/pkg/config/management.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go) · [cmd/routerctl/doctor.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `interfaces`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
