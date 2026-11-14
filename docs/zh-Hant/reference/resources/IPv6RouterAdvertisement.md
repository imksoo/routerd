# IPv6RouterAdvertisement

[各資源的設定](../resources.md#ipv6routeradvertisement) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

設定 LAN 的 IPv6 路由器公告選項，並棄用先前委派的前綴。

## 指定條件與參照關係

- interface 及 prefix 或 prefixFrom 必填。靜態轉譯與即時寫入不同：目前即時產生 interface/enable-ra/選項，但不由 prefix/prefixFrom 衍生一般 dhcp-range。須搭配可用 DHCPv6Server/位址設定並確認實際線上 PIO，結構描述或 Applied 無法證明目前前綴公告。
- dnssl 與 dnsslFrom 互斥，prefix/prefixFrom、rdnss/rdnssFrom 可並用。即使 optional true，共用參照仍須有既有資源與明確提供欄位。已移除 prefixSource/ready_when 會被拒絕。
- prefixFrom 指向 IPv6DelegatedAddress 時，仍有效的舊 /64 以前綴偏好期 0、剩餘有效期的 PIO 直接發送，使用實際鏈路本地來源與跳數限制 255。一般目前前綴服務仍走共用 dnsmasq。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: "<name>"
  spec:
    [ configPath: <string> ]
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    [ dnssl:
        - <string>
    ]
    [ dnsslFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    interface: <string>
    [ mFlag: <boolean> ]
    [ mtu: <integer> ]
    [ oFlag: <boolean> ]
    [ pidFile: <string> ]
    [ preferredLifetime: <string> ]
    [ prefix: <string> ]
    [ prefixFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ prfPreference: ("" | "low" | "medium" | "high") ]
    [ rdnss:
        - <string>
    ]
    [ rdnssFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ validLifetime: <string> ]
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
| `spec.interface` | **必填** | string | 必填邏輯介面。不同於許多位址資源，RA 此欄位未由全域參照驗證確認存在；執行時先解析別名，失敗則原樣作 OS 名稱。請宣告 Interface 並使用其名稱。 |
| `spec.prefix` | **條件必填** | string | 固定 IPv6 前綴，未提供 prefixFrom.resource 時必填，可作 prefixFrom 狀態替代值。目前即時寫入不以此產生一般前綴範圍，不能假設僅宣告即可公告。 |
| `spec.prefixFrom` | **條件必填** | object | 選用共用 resource/field/optional 來源，須有既有資源與明確提供欄位。用於回報前綴，參照 IPv6DelegatedAddress 時也找出待棄用的舊 /64；單純缺值不阻擋一般設定產生。 |
| `spec.rdnss` | *選填* | array | 選用固定 IPv6 DNS 清單，預設空，與 rdnssFrom 合併。於共用 dnsmasq 輸出無標籤 option6:dns-server，此寫入器不依 RA 介面隔離。 |
| `spec.rdnssFrom` | *選填* | array | 選用共用狀態來源清單，無自動來源。目前 RA 路徑忽略未解析來源的等待原因，僅使用已有值；optional 在此不提供服務就緒閘門。 |
| `spec.dnssl` | *選填* | array | 選用搜尋網域清單，預設空；每值為不含空白、斜線或逗號的單一網域，與 dnsslFrom 互斥，輸出共用無標籤 option6:domain-search。 |
| `spec.dnsslFrom` | *選填* | array | 選用來源清單，限 DNSZone/<name>、field: zone，資源須存在；可先讀宣告值，與 dnssl 互斥。目前一般 RA 產生忽略來源缺值的等待原因。 |
| `spec.mFlag` | *選填* | boolean | 布林值預設 false，供直接棄用舊委派前綴的 RA 使用；目前一般 dnsmasq RA 產生不映射至 M。 |
| `spec.oFlag` | *選填* | boolean | 布林值預設 false，使用路徑同 mFlag 的直接棄用公告，不是一般 dnsmasq O 位元開關。 |
| `spec.mtu` | *選填* | integer; minimum=1280; maximum=65535 | 選用 1280–65535，省略或 0 不輸出明確 MTU，除非有優先的 PathMTU 衍生值。經 dnsmasq ra-param mtu:<值> 傳入。 |
| `spec.prfPreference` | *選填* | string; enum=["", "low", "medium", "high"] | 空值或 medium 使用一般偏好，low/high 輸出 dnsmasq 路由器偏好並影響舊前綴直接公告。 |
| `spec.preferredLifetime` | *選填* | string | 選用字串，預設空，目前一般即時寫入不使用。舊前綴直接 PIO 一律使用 preferred lifetime 0，不受此值影響。 |
| `spec.validLifetime` | *選填* | string | 一般即時 dnsmasq 轉譯將此值作 ra-param 路由器有效期，不是目前前綴 PIO 有效期。請用十進位秒，省略保留 dnsmasq 預設。舊前綴直接 RA 空值或無效值為 1800、上限 65535，舊 PIO 則用租約剩餘期間。 |
| `spec.configPath` | *選填* | string | 選用解碼字串，預設空；目前即時控制器使用共用 dnsmasq ConfigPath/PIDFile 選項，不使用此資源覆寫。 |
| `spec.pidFile` | *選填* | string | 選用解碼字串，預設空；目前即時控制器使用共用 dnsmasq ConfigPath/PIDFile 選項，不使用此資源覆寫。 |
| `spec.when` | *選填* | object | 選用共用狀態條件，預設無條件；為假時自有效設定排除並為 Pending/WhenFalse。 |
| `spec.dependsOn` | *選填* | array | 選用共用 AND 相依清單，預設空。不成立回報 Pending/DependsOnFalse 並略過舊前綴直接公告，但一般共用 dnsmasq 行寫入器不檢查此閘門，不能完全抑制一般服務。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/routeradvertisement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/routeradvertisement.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `interface`, `prefix`, `rdnss`, `configPath`, `dryRun`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
