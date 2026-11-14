# DSLiteTunnel

[各資源的設定](../resources.md#dslitetunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

向 AFTR 以 IPv6 封裝 IPv4。通道設定、IPv4 預設路由、NAT 與連通檢查是不同事項。

## 指定條件與參照關係

- 停用時仍需 interface 及 aftrFrom/aftrFQDN/aftrIPv6/remoteAddress 至少一項。來源可並用，依此順序嘗試至成功。
- localAddress 是外層 IPv6 來源，但目前 localAddressFrom 選擇內層 IPv4 來源，兩者不可互換。內層來源預設 192.0.0.2，遠端 192.0.0.1。無 IPv4 來源參照時使用預設。
- AFTR／本機／相依值缺失時為 Pending。Up 僅表示通道與連結設定，不保證 AFTR 或網際網路可達。啟用條件為假或停用時會移除未使用通道；僅相依失敗會略過協調，不保證立即刪除。
- Linux 使用 ip6tnl/ipip6，FreeBSD 使用 gif。非 gif 名稱會確定性映射至 gif100–gif999，實際名稱發布於狀態。已移除的 aftrSource/ready_when 會被拒絕，請用 aftrFrom/dependsOn。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    [ aftrAddressOrdinal: <integer> ]
    [ aftrAddressSelection: ("ordinal" | "ordinalModulo") ]
    [ aftrDNSServers:
        - <string>
    ]
    [ aftrFQDN: <string> ]
    [ aftrFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ aftrIPv6: <string> ]
    [ defaultRoute: <boolean> ]
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    [ enabled: <boolean> ]
    [ encapsulationLimit: <string> ]
    interface: <string>
    [ localAddress: <string> ]
    [ localAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ localAddressSource: ("interface" | "static" | "delegatedAddress") ]
    [ localAddressSuffix: <string> ]
    [ localDelegatedAddress: <string> ]
    [ mtu: <integer> ]
    [ remoteAddress: <string> ]
    [ routeMetric: <integer> ]
    [ tunnelName: <string> ]
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
| `spec.interface` | **必填** | string | 必填且已宣告的邏輯底層介面，正常參照驗證檢查其存在。 |
| `spec.tunnelName` | *選填* | string | 選用裝置名稱，預設 metadata.name；FreeBSD 可能使用上述不同的 gif 名稱。 |
| `spec.enabled` | *選填* | boolean | 預設 true。false 移除未使用設定通道並回報 Disabled，仍須通過驗證。 |
| `spec.aftrFQDN` | **條件必填** | string | 選用 AFTR 主機名稱，不可有空白或斜線。在 aftrFrom 之後、固定地址之前嘗試。 |
| `spec.aftrIPv6` | **條件必填** | string | 選用純 AFTR IPv6 位址，在 aftrFQDN 之後嘗試，無預設值。 |
| `spec.remoteAddress` | **條件必填** | string | 選用 IPv6 AFTR 替代值，最後嘗試，不是內層 IPv4 目的地。 |
| `spec.aftrFrom` | **條件必填** | object | 選用共用 resource/field 來源，通常為 DHCPv6 提供的 AFTR 名稱。需存在且為提供欄位。執行時另讀同一資源的 dnsServers 並與 aftrDNSServers 合併。 |
| `spec.aftrDNSServers` | *選填* | array | 選用純 IPv4/IPv6 DNS 伺服器，查詢埠 53。無明確或來源伺服器時，執行時預設 127.0.0.1:1053。此宣告不啟動解析器，請提供可達伺服器。 |
| `spec.aftrAddressOrdinal` | *選填* | integer; minimum=1 | AAAA 選擇索引從 1 開始，預設 1。明確指定索引或模式時先按 IPv6 字串字典序排序；兩者省略則保留解析器順序。 |
| `spec.aftrAddressSelection` | *選填* | string; enum=["ordinal", "ordinalModulo"] | 預設 ordinal，超出範圍會失敗。ordinalModulo 將索引減 1 後依筆數循環，不在 AFTR 間分配流量。 |
| `spec.localAddress` | **條件必填** | string | 選用固定外層 IPv6 來源；localAddressSource:static 時必填，不是內層 IPv4 來源。 |
| `spec.localAddressFrom` | *選填* | object | 選用的內層 IPv4 共用狀態來源，先讀狀態，再讀宣告的 IPv4StaticAddress.address。必要來源缺值為 Pending；optional:true 使用 192.0.0.2。結果須為可用單播 IPv4。 |
| `spec.localAddressSource` | *選填* | string; enum=["interface", "static", "delegatedAddress"] | 預設 interface 在無 localAddress 時尋找可用全域 IPv6。static 需 localAddress。delegatedAddress 從 localDelegatedAddress 推導，可能先配置 /128 並等待 DAD。 |
| `spec.localDelegatedAddress` | **條件必填** | string | 既有 IPv6DelegatedAddress 裸名稱，delegatedAddress 模式必填。其他模式指定時也檢查其存在。 |
| `spec.localAddressSuffix` | *選填* | string | 選用 IPv6 後綴。委派模式優先順：此值→參照的 addressSuffix→::1。interface 模式也可用此後綴推導來源。 |
| `spec.defaultRoute` | *選填* | boolean | 預設 false。FreeBSD 執行時可變更／新增經 192.0.0.1 的預設路由。Linux 通道控制器不依此安裝 IPv4 預設，請用 IPv4Route/EgressRoutePolicy。 |
| `spec.routeMetric` | *選填* | integer; minimum=0 | 選用非負整數，解碼預設 0。目前 DS-Lite 執行流程不將它傳入預設路由設定。 |
| `spec.mtu` | *選填* | integer; minimum=1280; maximum=65535 | 選用 1280–65535，執行時預設 1460。套用至通道連結，不保證底層路徑支援此大小。 |
| `spec.encapsulationLimit` | *選填* | string | Linux encaplimit 引數，預設 none。此處無詳細值驗證，不合法值可使 ip 命令失敗。FreeBSD 執行流程不使用。 |
| `spec.when` | *選填* | object | 選用共用啟用條件，預設無條件；為假時移除未使用設定通道。 |
| `spec.dependsOn` | *選填* | array | 選用共用相依條件 AND 清單，預設空。不滿足時為 Pending/DependsOnFalse 並略過更新。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `interface`, `device`, `tunnelName`, `localIPv6`, `innerLocalIPv4`, `innerRemoteIPv4`, `localInterface`, `aftrName`, `aftrIPv6`, `mtu`, `dryRun`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
