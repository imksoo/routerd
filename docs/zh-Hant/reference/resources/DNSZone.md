# DNSZone

[各資源的設定](../resources.md#dnszone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

透過 DNSForwarder.zoneRefs 提供本機 A/AAAA 記錄及自動 PTR 回應；不是支援任意記錄類型、區域傳送或簽署的通用權威 DNS。

## 指定條件與參照關係

- 須由 DNSForwarder 參照，且 match 須涵蓋查詢的正向或反向網域。僅宣告 DNSZone 不會開啟 DNS 連接埠。符合區域的 VirtualAddress/IngressService hostname 亦會匯入，除非其指定 externalDNS。
- 固定 ipv4/ipv6 可與動態 ipv4From/ipv6From 並用。已解析動態值取代同位址族固定值，未解析時保留固定值。必要來源缺值記錄於個別記錄的等待資訊，不阻擋整個解析器；選用缺值不記錄等待。位址族錯誤或無效值會報錯。
- dnssec.enabled 僅在本機回應設定 AD，不產生 RRSIG/DNSKEY 或驗證簽章。dnssec.mode、dhcpDerived.hostnameSuffix、dhcpDerived.ddns、reverseZones 目前不改變回應行為，不能視為可用的簽署、DDNS 或反向區域存取控制。
- 僅回答 A/AAAA 與自動 PTR。名稱存在但無所查位址族時為 NOERROR/NODATA，區域內名稱不存在時為 NXDOMAIN。支援萬用字元。重複固定 hostname 採最後記錄，不累加各項位址。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    [ dhcpDerived:
        [ ddns: <boolean> ]
        [ hostnameSuffix: <string> ]
        [ leaseFile: <string> ]
        [ sources:
            - <string>
        ]
        [ ttl: <integer> ]
    ]
    [ dnssec:
        [ enabled: <boolean> ]
        [ mode: ("" | "validate" | "sign") ]
    ]
    [ records:
        -
          hostname: <string>
          [ ipv4: <string> ]
          [ ipv4From:
              field: <string>
              [ optional: <boolean> ]
              resource: <string>
          ]
          [ ipv6: <string> ]
          [ ipv6From:
              field: <string>
              [ optional: <boolean> ]
              resource: <string>
          ]
          [ ttl: <integer> ]
    ]
    [ reverseZones:
        -
          name: <string>
    ]
    [ ttl: <integer> ]
    zone: <string>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.zone` | **必填** | string | 必填區域網域，例如 home.example.net，執行時補尾端句點，無預設值。目前僅驗證非空，請使用一般小寫 DNS 名稱。 |
| `spec.ttl` | *選填* | integer; minimum=0 | 區域預設 TTL 秒數；省略或 0 為 300，結構描述要求非負值。用於未指定 TTL 的固定記錄及自動 PTR。 |
| `spec.dnssec` | *選填* | object | 選用的本機回應 AD 旗標設定，不提供密碼學簽署或驗證。 |
| `spec.dnssec.enabled` | *選填* | boolean | 預設 false；true 不產生簽章，連本機否定或 NODATA 回應也設定 AD。 |
| `spec.dnssec.mode` | *選填* | string; enum=["", "validate", "sign"] | 結構描述接受空值、validate 或 sign，預設空值。會解碼但目前本機區域回應邏輯不使用。 |
| `spec.records` | *選填* | array | 選用固定 A/AAAA 記錄清單，預設空。無位址 hostname 也可通過驗證，但不提供 A/AAAA 回應。 |
| `spec.records[].hostname` | **條件必填** | string | 必填且不可含空白或逗號。相對名稱附加 zone；已在區域內的完整名稱維持原樣，區域外名稱也會附加 zone，不會另行提供外部區域。萬用字元可用 *.subdomain。 |
| `spec.records[].ipv4` | *選填* | string | 選用的純 IPv4 位址，預設無；可與 ipv4From 並用，來源未解析時保留此值。 |
| `spec.records[].ipv4From` | *選填* | object | 選用共用狀態來源，須明確指定並驗證 resource/field。第一個有效值須為 IPv4，CIDR 取主機位址。執行時僅讀狀態，不像部分其他使用者會以 IPv4StaticAddress 宣告值補足。 |
| `spec.records[].ipv6` | *選填* | string | 選用的純 IPv6 位址，預設無；與 ipv6From 並用及缺值處理方式同 IPv4。 |
| `spec.records[].ipv6From` | *選填* | object | 選用共用 resource/field/optional 來源，第一個值須為 IPv6。參照、CIDR 與缺值規則同 ipv4From；optional 預設 false。 |
| `spec.records[].ttl` | *選填* | integer; minimum=0 | 記錄 TTL 秒數；省略或 0 繼承區域 TTL。結構描述要求非負，不影響 DHCP 衍生 TTL。 |
| `spec.dhcpDerived` | *選填* | object | 選用 DHCP 租約衍生記錄設定，啟用區域可由執行時租約事件更新，並非 RFC 2136 DDNS。 |
| `spec.dhcpDerived.sources` | *選填* | array | 預設空值停用即時租約事件與保存主機快照匯入。任意非空清單會啟用，但目前不依字串篩選來源或驗證 DHCP 資源參照；所有收到的租約事件皆提供給各啟用區域。 |
| `spec.dhcpDerived.hostnameSuffix` | *選填* | string | 解碼的選用字串，預設空值；目前租約記錄產生不讀取，後綴使用 zone。 |
| `spec.dhcpDerived.ddns` | *選填* | boolean | 解碼布林值，預設 false；目前不以此旗標控制事件更新，也不據此實作 DNS UPDATE。 |
| `spec.dhcpDerived.ttl` | *選填* | integer; minimum=0 | DHCP 衍生正向記錄 TTL 秒數；省略或 0 為 60，與區域 TTL 無關。PTR 回應使用區域 TTL。 |
| `spec.dhcpDerived.leaseFile` | *選填* | string | 建構或重新載入區域時讀取的選用 dnsmasq 租約檔案。空值不匯入，與即時事件啟用分開，並非持續監看檔案。 |
| `spec.reverseZones` | *選填* | array | 選用反向區域中繼資料清單，預設空。常駐程序保存名稱但不在 PTR 回應時查閱；PTR 依記錄產生，不受此清單控制。 |
| `spec.reverseZones[].name` | **條件必填** | string | 每項內結構描述要求的字串，保存為完整反向區域名稱，但目前不影響回應選擇。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `zone`, `records`, `pendingRecords`, `updatedAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
