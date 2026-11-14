# IPAddressSet

[各資源的設定](../resources.md#ipaddressset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在支援的 nftables NAT、路由及防火牆規則間共用固定與 DNS 解析的 IP 位址。不建立路由或啟動 DNS 服務。

## 指定條件與參照關係

- addresses 或 names 至少一筆，兩者可並用。位址為單一 IPv4/IPv6，不接受 CIDR。各設定清單內重複會被拒絕；解析結果則合併並去重。
- 使用端以裸名稱或 IPAddressSet/name 參照。支援的 nftables 使用端包含 LocalServiceRedirect、NAT44Rule/FlowDNATPinhole、EgressRoutePolicy 與 FirewallRule/FlowPinhole。未被參照時僅回報 Observed/referenced:false，不解析名稱或建立核心集合。
- 更新前須由使用端產生器建立 nftables 集合，否則為 Pending/SetMissing。名稱透過 /etc/resolv.conf 查詢 A/AAAA，不使用 DNSUpstream 參照。部分 DNS 失敗仍可套用其餘位址；全數失敗則為 Pending/ResolveFailed，該輪不替換現有集合。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: "<name>"
  spec:
    [ addresses:
        - <string>
    ]
    [ names:
        - <string>
    ]
    [ refreshInterval: <string> ]
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
| `spec.addresses` | **條件必填** | array | 選用的固定 IPv4/IPv6 清單，預設為空；names 為空時必填。IPv4 映射 IPv6 會轉為 IPv4，不展開子網路。 |
| `spec.names` | **條件必填** | array | 選用的 DNS 名稱，預設為空；addresses 為空時必填。拒絕空名稱與去除前後空白後的重複。A/AAAA 結果與固定值合併，不展開萬用名稱。 |
| `spec.refreshInterval` | *選填* | string | 選用 Go duration。預設為最小正 DNS TTL 的一半，最低 1m；無正 TTL 時視為 5m，因此為 2m30s。正設定值只能縮短週期，可低於 1m。0／負值可解析但不縮短。實際更新在控制器協調時執行。 |
| `spec.when` | *選填* | object | 選用的共用啟用條件，預設無條件。state/all/any 與使用端啟用行為請見共用欄位。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `updatedAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
