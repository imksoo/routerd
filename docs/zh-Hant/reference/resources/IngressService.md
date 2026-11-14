# IngressService

[各資源的設定](../resources.md#ingressservice) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

以後端選擇與主動健康檢查公開 IPv4 TCP/UDP 端點，可搭配本機 DNS 與內部迴送 NAT。屬於第 4 層 NAT，不是 HTTP 反向代理或 TLS 終止。

## 指定條件與參照關係

- listen 與至少一個後端必填。每個後端需且僅提供 address/addressFrom 之一與 port；固定值可為 IPv4 或主機名。資料轉送仍為 IPv4。健康歷史依名稱索引，請使用穩定唯一名稱；省略使用 backend-0、backend-1 等。
- 不論 healthCheck.interval，通常每 5 秒依序檢查後端。未指定協定時預設 TCP，即使 listen 是 UDP。主機名使用字串排序第一個 IPv4；DNS 失敗可重用先前位址，但仍進行健康檢查。
- NoHealthyBackends 與 policy.onNoHealthyBackends 是狀態輸出，不保證產生 drop/reject。沒有有效後端時 NAT 控制器保留原後端定義，可能回到第一個設定的靜態後端。不可依賴此設定在全數異常時可靠封鎖。
- failover 依宣告順序選第一個健康後端。Linux sourceHash/random 可分散至所有健康且已解析後端，不使用權重。PF 僅產生第一個選定目標，不實作分散演算法。靜態產生不依健康資料庫選擇，可能使用第一個宣告後端。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends:
      -
        [ address: <string> ]
        [ addressFrom:
            field: <string>
            [ optional: <boolean> ]
            resource: <string>
        ]
        [ name: <string> ]
        port: <integer>
        [ weight: <integer> ]
    [ externalDNS: <boolean> ]
    [ hairpin:
        [ enabled: <boolean> ]
        [ interfaces:
            - <string>
        ]
        [ mode: ("" | "auto" | "manual" | "off") ]
    ]
    [ healthCheck:
        [ expectedBody: <string> ]
        [ expectedStatus:
            - <integer>
        ]
        [ healthyThreshold: <integer> ]
        [ host: <string> ]
        [ interval: <string> ]
        [ path: <string> ]
        [ protocol: ("" | "tcp" | "http" | "https") ]
        [ timeout: <string> ]
        [ tlsSkipVerify: <boolean> ]
        [ unhealthyThreshold: <integer> ]
    ]
    [ hostname: <string> ]
    listen:
      [ address: <string> ]
      [ addressFrom:
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
      ]
      interface: <string>
      port: <integer>
      protocol: ("tcp" | "udp")
    [ policy:
        [ onNoHealthyBackends: ("" | "drop" | "reject") ]
        [ selection: ("" | "failover" | "sourceHash" | "random") ]
    ]
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
| `spec.listen` | **必填** | object | 必填 IPv4 入口端點，建立 NAT/防火牆規則，不建立應用程式監聽或指派位址。 |
| `spec.listen.interface` | **必填** | string | 必填單純已宣告邏輯介面名稱，不是 Kind/name。須存在設定索引並可解析 OS ifname。 |
| `spec.listen.address` | *選填* | string | 選用純 IPv4 目的位址，與 addressFrom 互斥。省略時比對監聽介面上任意目的位址。 |
| `spec.listen.addressFrom` | *選填* | object | 選用共用 resource/field/optional 參照，與 address 互斥。NAT/靜態產生支援宣告的 IPv4StaticAddress.address 或 VirtualAddress.address，不讀任意執行期狀態。一般參照驗證通過不保證產生器支援。 |
| `spec.listen.protocol` | **必填** | string; enum=["tcp", "udp"] | 必填 tcp/udp，無預設值。選擇轉送流量，不決定健康檢查協定。 |
| `spec.listen.port` | **必填** | integer; minimum=1; maximum=65535 | 必填目的連接埠 1–65535，無範圍、清單或預設值。 |
| `spec.hairpin` | *選填* | object | 選用經公開位址返回內部的 NAT 設定，啟用時需可解析 listen 位址。PortForward 預設 off，IngressService 預設 auto。 |
| `spec.hairpin.enabled` | *選填* | boolean | 預設 false。mode 省略時 true 讓 PortForward 使用 manual，IngressService 仍預設 auto。明確 off 即使 enabled=true 也停用。 |
| `spec.hairpin.mode` | *選填* | string; enum=["", "auto", "manual", "off"] | auto/manual/off。auto 在 listen 與後端同屬宣告 IPv4 子網或相同私有 /24 時選 listen 介面，不探索任意 LAN 路由。manual 需 enabled=true 與 interfaces 才產生規則。 |
| `spec.hairpin.interfaces` | *選填* | array | 選用單純宣告邏輯介面名，不可重複，啟用 manual 時需非空。auto 可另加入 listen 介面。IPv4 DNAT 加 masquerade/PF SNAT 讓回程經路由器。 |
| `spec.when` | *選填* | object | 選用共用啟用條件，預設無條件。不成立時從有效產生排除；條件與結構描述限制請見共用欄位。 |
| `spec.hostname` | *選填* | string | 選用 FQDN，用於本機 DNS 發布與狀態，預設空。不作 HTTP Host 路由或健康檢查 Host/SNI。發布需有 DNSResolver 服務的適當 DNSZone。 |
| `spec.externalDNS` | *選填* | boolean | 預設 false。true 略過 routerd 自動本機名稱紀錄與 DNS 範圍警告，不更新外部 DNS 供應商。 |
| `spec.backends` | **必填** | array | 必填非空後端清單，順序為 failover 優先順。設定接受主機名，但靜態 NAT 產生需已解析 IPv4，不自行查 DNS。 |
| `spec.backends[].name` | *選填* | string | 選用穩定健康歷史鍵，預設 backend-<從零起算索引>。此驗證不拒絕重複名稱，可能混合歷史，請用唯一名稱。 |
| `spec.backends[].address` | *選填* | string | 純 IPv4 或主機名，與 addressFrom 必須擇一。主機名僅解析 IPv4，DNS 失敗時有先前位址則保留。 |
| `spec.backends[].addressFrom` | *選填* | object | 與 address 互斥的共用來源。主動檢查讀執行狀態，optional 不會讓未解析後端變健康。靜態產生僅支援上述宣告位址欄位。 |
| `spec.backends[].port` | **必填** | integer; minimum=1; maximum=65535 | 必填後端連接埠 1–65535，用於資料轉換與健康檢查連線。 |
| `spec.backends[].weight` | *選填* | integer; minimum=0 | 接受非負整數，預設 0；目前健康選擇與 Linux/PF NAT 產生器不讀取。不提供加權分散。 |
| `spec.healthCheck` | *選填* | object | 選用主動健康檢查設定，省略仍啟用 TCP 檢查，不是停用。 |
| `spec.healthCheck.protocol` | *選填* | string; enum=["", "tcp", "http", "https"] | tcp（預設）、http 或 https，獨立於 listen.protocol。HTTP(S) 直接向已解析後端位址與連接埠發 GET。 |
| `spec.healthCheck.interval` | *選填* | string | 驗證接受 Go 時間長度，但控制器不讀取。實際使用鏈的 5 秒週期與協調事件。 |
| `spec.healthCheck.timeout` | *選填* | string | 正 Go 時間長度，有效預設 1s。省略/零/負值即使通過語法驗證也使用 1s。適用每後端探測，不是整輪或 DNS 查詢期限。 |
| `spec.healthCheck.path` | *選填* | string | HTTP(S) URL 路徑，預設 /；非空須以 / 開頭，TCP 不使用。 |
| `spec.healthCheck.host` | *選填* | string | 選用 HTTP Host 與 HTTPS TLS ServerName/SNI 覆寫，預設空，URL 使用解析 IP。不可含空白或控制字元。 |
| `spec.healthCheck.expectedStatus` | *選填* | array | 選用完整 HTTP 狀態允許清單 100–599。省略/空接受 200–399；HTTP 用戶端預設先跟隨重新導向再評估回應。 |
| `spec.healthCheck.tlsSkipVerify` | *選填* | boolean | 預設 false。true 僅停用 HTTPS 健康探測的憑證/主機名驗證，不是資料 TLS 處理。 |
| `spec.healthCheck.expectedBody` | *選填* | string | HTTP 回應本文前 1 MiB 中區分大小寫的子字串。預設空停用本文比對，不是正規表示式。 |
| `spec.healthCheck.healthyThreshold` | *選填* | integer; minimum=1 | 升為健康的連續成功次數，省略/0 為 1。健康與計數從後端狀態恢復。結構描述最小 1 與接受零預設不同。 |
| `spec.healthCheck.unhealthyThreshold` | *選填* | integer; minimum=1 | 移除已健康後端的連續失敗次數，省略/0 為 1。初始未解析後端為不健康。 |
| `spec.policy` | *選填* | object | 選用選擇設定，預設 failover/drop，但目前無健康動作僅報告。 |
| `spec.policy.selection` | *選填* | string; enum=["", "failover", "sourceHash", "random"] | failover（預設）、sourceHash 或 random。Linux sourceHash 雜湊用戶端 IPv4 來源，random 以等權後端數取模。單一健康後端退回 failover；PF 一律單一目標。 |
| `spec.policy.onNoHealthyBackends` | *選填* | string; enum=["", "drop", "reject"] | 接受 drop（預設）/reject，但僅寫入 status.conditions.reason，不產生明確 drop/reject；請見上述無健康後端限制。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/ingressservice/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/ingressservice/controller.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `hostname`, `listenAddress`, `activeBackend`, `activeBackends`, `healthyBackends`, `totalBackends`, `backends`, `observedAt`, `dryRun`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)
