# FirewallPolicy

[各資源的設定](../resources.md#firewallpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

設定與區域及明確規則搭配的共用隱含防火牆選項。多個資源會彙總，不是獨立套用。

## 指定條件與參照關係

- 沒有必填 spec 欄位。logDeny 在多個資源間取 OR。產生器的 sameRoleAccept 從 true 開始且沒有設為 false 的路徑，因此目前填 false 也無法停用相同角色間轉送。
- Linux nftables 與 FreeBSD PF 的隱含/明確規則順序不同。PF 在已允許路徑的明確拒絕前產生廣泛隱含允許；Linux 在該區域配對的隱含動作前產生明確規則。不可假定明確拒絕能覆蓋所有隱含、控制或狀態例外。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    [ logDeny: <boolean> ]
    [ sameRoleAccept: <boolean> ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.logDeny` | *選填* | boolean | 預設 false，任一資源為 true 即啟用 Linux 隱含 input/forward 拒絕紀錄。明確 drop 仍需其 log 旗標。目前 PF 不讀取此選項。 |
| `spec.sameRoleAccept` | *選填* | boolean | 解碼預設 false，但產生器有效預設固定 true，false 無效。Linux 包含 untrust 對 untrust；PF 不產生其廣泛隱含轉送允許。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
