# DHCPv4Relay

[各資源的設定](../resources.md#dhcpv4relay) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

透過 dnsmasq 將 DHCPv4 請求轉送至明確指定的一個 IPv4 伺服器。

## 指定條件與參照關係

- 至少需要一個已宣告介面與純 IPv4 上游位址，不提供自動探索、上游清單、個別介面中繼來源位址或條件欄位。
- Linux 與 FreeBSD 皆在共用 dnsmasq 程序為各介面產生 dhcp-relay=0.0.0.0,<upstream>,<OS名稱>。主機路由須能到達上游。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces:
      - <string>
    upstream: <string>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.interfaces` | **必填** | array | 必填且不可為空的邏輯介面清單，各名稱須存在於同一 Router 的介面索引。執行時解析 Interface.ifname 與支援的產生介面別名，無預設值。 |
| `spec.upstream` | **必填** | string | 必填的 DHCP 伺服器純 IPv4 位址，不含連接埠、CIDR 或資源參照，無預設值。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`
