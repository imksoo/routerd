# DNSForwarder

[各資源的設定](../resources.md#dnsforwarder) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

依查詢名稱替一個 DNSResolver 選擇本機區域或上游 DNS 的規則。

## 指定條件與參照關係

- resolver 與非空 match 必填，且 zoneRefs 與 upstreams 恰有一個非空清單。所有參照皆須存在於同一 Router，可用裸名或對應 Kind/name。
- 接聽端點篩選後依 Router 資源順序評估，並非最長後綴優先。內部或特定規則應置於全域規則之前。已選上游失敗時回傳 SERVFAIL，不會轉往其他來源洩漏內部查詢。
- 多個上游參照合併為一個來源池。bootstrap 合併，第一個非空 sourceInterface 由整個池共用；不會分別遵循互相衝突的上游介面設定。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    [ dnssecValidate: <boolean> ]
    [ healthcheck:
        [ failThreshold: <integer> ]
        [ interval: <string> ]
        [ passThreshold: <integer> ]
        [ timeout: <string> ]
    ]
    match:
      - <string>
    [ queryTimeout: <string> ]
    resolver: <string>
    [ upstreams:
        - <string>
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
    [ zoneRefs:
        - <string>
    ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.resolver` | **必填** | string | 必填的既有 DNSResolver 名稱或 DNSResolver/name，無預設值。 |
| `spec.match` | **必填** | array | 必填非空網域後綴清單；忽略大小寫與尾端句點。example.net 匹配本身及子網域，"." 匹配全部。無隱含全域規則。 |
| `spec.zoneRefs` | **條件必填** | array | 供本機回答的既有 DNSZone 名稱或 DNSZone/name，與 upstreams 互斥；upstreams 為空時必填。 |
| `spec.upstreams` | **條件必填** | array | 既有 DNSUpstream 名稱或 DNSUpstream/name，不是直接 IP 或 URL。與 zoneRefs 互斥；各資源的伺服器組成一個備援池。 |
| `spec.dnssecValidate` | *選填* | boolean | 預設 false；true 設定上游查詢 DO 位元並要求回應 AD，不執行本機密碼學 DNSSEC 驗證。區域來源不使用此欄位。 |
| `spec.queryTimeout` | *選填* | string | 每次上游查詢嘗試的正 Go duration。省略時繼承正的 healthcheck.timeout，否則為 3s。不同於背景探測間隔，區域回應不使用。 |
| `spec.healthcheck` | *選填* | object | 上游背景探測選項，預設 interval 15s、timeout 3s、failThreshold 3、passThreshold 2；本機區域來源不使用。 |
| `spec.healthcheck.interval` | *選填* | string | Go duration；省略、空值或執行時非正值使用 15s。設定驗證僅檢查語法，不強制正值。 |
| `spec.healthcheck.timeout` | *選填* | string | 探測 Go duration；省略、空值或非正值使用 3s。正值亦作為省略 queryTimeout 的預設值。 |
| `spec.healthcheck.failThreshold` | *選填* | integer; minimum=1 | 標示上游異常的連續失敗次數，預設 3；執行時非正值亦為 3，結構描述下限 1。 |
| `spec.healthcheck.passThreshold` | *選填* | integer; minimum=1 | 恢復所需的連續成功次數，預設 2；執行時非正值亦為 2，結構描述下限 1。 |
| `spec.when` | *選填* | object | 選用共用狀態條件；省略表示無條件，可將此規則自有效解析器設定排除。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `resolver`, `upstreams`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
