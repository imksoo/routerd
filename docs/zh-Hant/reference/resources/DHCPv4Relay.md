# DHCPv4Relay

[各資源的設定](../resources.md#dhcpv4relay) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

透過 dnsmasq 將 DHCPv4 請求轉送至明確指定的一個 IPv4 伺服器。

## 指定條件與參照關係

- 至少需要一個已宣告介面與純 IPv4 上游位址，不提供自動探索、上游清單、個別介面中繼來源位址或條件欄位。
- Linux 與 FreeBSD 皆在共用 dnsmasq 程序為各介面產生 dhcp-relay=0.0.0.0,<upstream>,<OS名稱>。主機路由須能到達上游。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces: # 必填; array
      - "<string>" # string
    upstream: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.interfaces` | 必填且不可為空的邏輯介面清單，各名稱須存在於同一 Router 的介面索引。執行時解析 Interface.ifname 與支援的產生介面別名，無預設值。 |
| `spec.upstream` | 必填的 DHCP 伺服器純 IPv4 位址，不含連接埠、CIDR 或資源參照，無預設值。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)
