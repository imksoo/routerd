# IPv6RAAddress

[各資源的設定](../resources.md#ipv6raaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

宣告主機接收IPv6 RA/SLAAC，屬於RA client，不是向LAN公告的IPv6RouterAdvertisement。

## 指定條件與參照關係

- managed控制主機RA設定。FreeBSD產生accept_rtadv/rtsold；Linux networkd接管保留/啟用RA，將DHCPv6移離networkd時也由drop-in抑制RA觸發DHCPv6。不保證收到可用上游RA。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RAAddress
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # 必填; string
    managed: true # 選填; boolean
    required: true # 選填; boolean
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.interface` | 必填Router介面索引中的邏輯名稱，不加Interface/，必須在宣告設定存在，無預設值。 |
| `spec.managed` | 預設true；false不要求主機RA設定，只選觀察意圖，與Interface的managed獨立。 |
| `spec.required` | 預設false，目前觀察/產生路徑不讀取，不要求apply返回前完成SLAAC位址取得。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)
