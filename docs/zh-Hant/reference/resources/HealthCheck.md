# HealthCheck

[各資源的設定](../resources.md#healthcheck) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

探測 IP 目標並發布 Healthy/Unhealthy 狀態供路由選擇。成功僅代表設定的協定與路徑通過。

## 指定條件與參照關係

- 運作時請明確指定 target。targetSource 與 interface 影響驗證及計畫顯示，但目前產生器直接傳入 target，不會自動解析預設閘道或 DS-Lite 遠端目標；輔助程式拒絕空目標。
- 來源綁定與 socket mark 從參照此檢查的 EgressRoutePolicy 候選／目標推導，不能以 HealthCheck YAML 直接設定。推導值衝突時無法選出唯一路徑。DS-Lite 關聯探測需要可用的綁定。
- TCP 僅檢查連線建立。DNS 透過目標伺服器解析 example.com 的 A 記錄。HTTP 對 http://<target>:<port>/ 發送 GET，將含 4xx 的 200–499 視為成功，不檢查特定應用 URL 或回應內容。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    [ addressFamily: ("ipv4" | "ipv6") ]
    [ enabled: <boolean> ]
    [ healthyThreshold: <integer> ]
    [ interface: <string> ]
    [ interval: <string> ]
    [ port: <integer> ]
    [ protocol: ("" | "icmp" | "tcp" | "dns" | "http") ]
    [ role: ("link" | "next-hop" | "internet" | "service" | "policy") ]
    [ target: <string> ]
    [ targetSource: ("auto" | "static" | "defaultGateway" | "dsliteRemote") ]
    [ timeout: <string> ]
    [ type: ("ping") ]
    [ unhealthyThreshold: <integer> ]
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
| `spec.enabled` | *選填* | boolean | 預設 true；false 停用探測。 |
| `spec.type` | *選填* | string; enum=["ping"] | 預設 ping，僅接受 ping。TCP/DNS/HTTP 由 protocol 選擇。 |
| `spec.role` | *選填* | string; enum=["link", "next-hop", "internet", "service", "policy"] | 選用分類 link/next-hop/internet/service/policy；驗證與計畫預設 next-hop。不會在執行時選擇目標或協定。 |
| `spec.addressFamily` | *選填* | string; enum=["ipv4", "ipv6"] | 選用 ipv4/ipv6。目標驗證將省略視為 ipv4，IPv6 目標須明確指定 ipv6。target 必須是 IP 常值，不是主機名稱解析設定。 |
| `spec.target` | **條件必填** | string | 符合 addressFamily 的純 IP 位址。targetSource 為 static 時驗證必填，目前輔助程式在所有模式下實際都需要目標；無可用執行時預設值。 |
| `spec.targetSource` | *選填* | string; enum=["auto", "static", "defaultGateway", "dsliteRemote"] | 驗證預設 auto，也接受 static/defaultGateway/dsliteRemote。目前產生器不實作自動目標查詢，請見上述運作限制。 |
| `spec.interface` | *選填* | string | 選用的邏輯介面／PPPoESession／DSLiteTunnel 名稱，指定時必須存在於共用介面／通道索引。此欄位本身不會轉為目前輔助程式的 --source-interface。 |
| `spec.protocol` | *選填* | string; enum=["", "icmp", "tcp", "dns", "http"] | 預設 icmp；tcp/dns/http 選擇上述檢查。HTTP 為明文，不是 HTTPS。 |
| `spec.port` | *選填* | integer; minimum=0; maximum=65535 | 0–65535，預設 0 使用協定預設：TCP 443、DNS 53、HTTP 80。ICMP 不使用連接埠。 |
| `spec.interval` | *選填* | string | 正值 Go duration；輔助程式／執行時預設 30s。目前計畫觀察器省略時顯示 60s，並非輔助程式預設。 |
| `spec.timeout` | *選填* | string | 每次探測的正值 Go duration，預設 3s。這是探測逾時，不是路由器重新啟動期限。 |
| `spec.healthyThreshold` | *選填* | integer; minimum=1 | 轉為 Healthy 所需連續成功次數，預設 1。失敗會重設成功計數。請使用正整數；明確的 0 不符合結構描述最小值。 |
| `spec.unhealthyThreshold` | *選填* | integer; minimum=1 | 轉為 Unhealthy 所需連續失敗次數，預設 3。成功會重設失敗計數；未達門檻時為 Failing。 |
| `spec.when` | *選填* | object | 選用的共用啟用條件，省略表示無條件。巢狀 state/all/any 及條件請見共用欄位。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/healthcheck/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/engine.go) · [pkg/healthcheck/dslite_binding.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/dslite_binding.go) · [pkg/render/healthcheck_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/healthcheck_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-healthcheck/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-healthcheck/main.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `role`, `target`, `sourceAddress`, `sourceInterface`, `protocol`, `consecutiveFailed`, `lastCheckedAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
