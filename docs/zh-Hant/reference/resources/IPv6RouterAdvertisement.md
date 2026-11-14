# IPv6RouterAdvertisement

[各資源的設定](../resources.md#ipv6routeradvertisement) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

設定 LAN 的 IPv6 路由器公告選項，並棄用先前委派的前綴。

## 指定條件與參照關係

- interface 及 prefix 或 prefixFrom 必填。靜態轉譯與即時寫入不同：目前即時產生 interface/enable-ra/選項，但不由 prefix/prefixFrom 衍生一般 dhcp-range。須搭配可用 DHCPv6Server/位址設定並確認實際線上 PIO，結構描述或 Applied 無法證明目前前綴公告。
- dnssl 與 dnsslFrom 互斥，prefix/prefixFrom、rdnss/rdnssFrom 可並用。即使 optional true，共用參照仍須有既有資源與明確提供欄位。已移除 prefixSource/ready_when 會被拒絕。
- prefixFrom 指向 IPv6DelegatedAddress 時，仍有效的舊 /64 以前綴偏好期 0、剩餘有效期的 PIO 直接發送，使用實際鏈路本地來源與跳數限制 255。一般目前前綴服務仍走共用 dnsmasq。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: "<name>"
  spec:
    configPath: "<string>" # 選填; string
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    dnssl: # 選填; array
      - "<string>" # string
    dnsslFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    mFlag: true # 選填; boolean
    mtu: 1280 # 選填; integer; minimum=1280; maximum=65535
    oFlag: true # 選填; boolean
    pidFile: "<string>" # 選填; string
    preferredLifetime: "<string>" # 選填; string
    prefix: "<string>" # 選填; string
    prefixFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    prfPreference: "" # 選填; string; enum=["", "low", "medium", "high"]
    rdnss: # 選填; array
      - "<string>" # string
    rdnssFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    validLifetime: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.interface` | 必填邏輯介面。不同於許多位址資源，RA 此欄位未由全域參照驗證確認存在；執行時先解析別名，失敗則原樣作 OS 名稱。請宣告 Interface 並使用其名稱。 |
| `spec.prefix` | 固定 IPv6 前綴，未提供 prefixFrom.resource 時必填，可作 prefixFrom 狀態替代值。目前即時寫入不以此產生一般前綴範圍，不能假設僅宣告即可公告。 |
| `spec.prefixFrom` | 選用共用 resource/field/optional 來源，須有既有資源與明確提供欄位。用於回報前綴，參照 IPv6DelegatedAddress 時也找出待棄用的舊 /64；單純缺值不阻擋一般設定產生。 |
| `spec.rdnss` | 選用固定 IPv6 DNS 清單，預設空，與 rdnssFrom 合併。於共用 dnsmasq 輸出無標籤 option6:dns-server，此寫入器不依 RA 介面隔離。 |
| `spec.rdnssFrom` | 選用共用狀態來源清單，無自動來源。目前 RA 路徑忽略未解析來源的等待原因，僅使用已有值；optional 在此不提供服務就緒閘門。 |
| `spec.dnssl` | 選用搜尋網域清單，預設空；每值為不含空白、斜線或逗號的單一網域，與 dnsslFrom 互斥，輸出共用無標籤 option6:domain-search。 |
| `spec.dnsslFrom` | 選用來源清單，限 DNSZone/<name>、field: zone，資源須存在；可先讀宣告值，與 dnssl 互斥。目前一般 RA 產生忽略來源缺值的等待原因。 |
| `spec.mFlag` | 布林值預設 false，供直接棄用舊委派前綴的 RA 使用；目前一般 dnsmasq RA 產生不映射至 M。 |
| `spec.oFlag` | 布林值預設 false，使用路徑同 mFlag 的直接棄用公告，不是一般 dnsmasq O 位元開關。 |
| `spec.mtu` | 選用 1280–65535，省略或 0 不輸出明確 MTU，除非有優先的 PathMTU 衍生值。經 dnsmasq ra-param mtu:<值> 傳入。 |
| `spec.prfPreference` | 空值或 medium 使用一般偏好，low/high 輸出 dnsmasq 路由器偏好並影響舊前綴直接公告。 |
| `spec.preferredLifetime` | 選用字串，預設空，目前一般即時寫入不使用。舊前綴直接 PIO 一律使用 preferred lifetime 0，不受此值影響。 |
| `spec.validLifetime` | 一般即時 dnsmasq 轉譯將此值作 ra-param 路由器有效期，不是目前前綴 PIO 有效期。請用十進位秒，省略保留 dnsmasq 預設。舊前綴直接 RA 空值或無效值為 1800、上限 65535，舊 PIO 則用租約剩餘期間。 |
| `spec.configPath` | 選用解碼字串，預設空；目前即時控制器使用共用 dnsmasq ConfigPath/PIDFile 選項，不使用此資源覆寫。 |
| `spec.pidFile` | 選用解碼字串，預設空；目前即時控制器使用共用 dnsmasq ConfigPath/PIDFile 選項，不使用此資源覆寫。 |
| `spec.when` | 選用共用狀態條件，預設無條件；為假時自有效設定排除並為 Pending/WhenFalse。 |
| `spec.dependsOn` | 選用共用 AND 相依清單，預設空。不成立回報 Pending/DependsOnFalse 並略過舊前綴直接公告，但一般共用 dnsmasq 行寫入器不檢查此閘門，不能完全抑制一般服務。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/routeradvertisement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/routeradvertisement.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
