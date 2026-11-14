# DNSUpstream

[各資源的設定](../resources.md#dnsupstream) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

供 DNSForwarder 參照的 DNS 伺服器端點；僅宣告不會啟動接聽或轉送查詢。

## 指定條件與參照關係

- protocol 必填，即使內部 URL 產生器有 udp 預設。address 或 addressFrom 至少提供一個，可同時使用並合併端點。動態 addressFrom 目前僅支援 UDP。
- 必要 addressFrom 未解析時，即使有固定端點，仍暫停包含它的整個轉送來源；選用值則略過。bootstrap 不同：固定清單可補足必要動態 bootstrap 的缺值。
- sourceInterface 原樣傳入通訊端綁定，不解析邏輯名稱或 Kind/name。Linux 請宣告 metadata.name 與 OS ifname 相同的介面。FreeBSD 通訊端路徑拒絕直接 ifname 綁定，而一般設定驗證也未提供可用的 fib:<n> 替代方式。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    [ address: <string> ]
    [ addressFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ bootstrap:
        - <string>
    ]
    [ bootstrapFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ path: <string> ]
    [ port: <integer> ]
    protocol: ("udp" | "tcp" | "dot" | "doh")
    [ sourceInterface: <string> ]
    [ tlsName: <string> ]
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
| `spec.protocol` | **必填** | string; enum=["udp", "tcp", "dot", "doh"] | 必填 udp、tcp、dot 或 doh，請用標準小寫。UDP/TCP 為明文 DNS，DoT 使用 TLS，DoH 使用 HTTPS。 |
| `spec.address` | **條件必填** | string | 不含協定、連接埠或路徑的伺服器 IP 或主機名稱；addressFrom 為空時必填。無隱含伺服器，若提供 bootstrap 則用於名稱解析。 |
| `spec.addressFrom` | **條件必填** | array | 選用且僅限 UDP 的狀態值來源清單，每項須有已宣告 resource 與明確提供欄位；optional 預設 false。動態純位址會獨立於此資源 port 正規化為 UDP 53，可傳回多個伺服器。 |
| `spec.port` | *選填* | integer; minimum=1; maximum=65535 | 選用 1–65535；省略或 0 時 udp/tcp 為 53、dot 為 853、doh 為 443。用於固定 address，不套用至 addressFrom 傳回的純位址。 |
| `spec.path` | *選填* | string | DoH URL 路徑，預設 /dns-query；doh 提供此值時須以 / 開頭。其他協定不使用。 |
| `spec.tlsName` | *選填* | string | DoT 憑證驗證與 SNI 伺服器名稱，省略使用端點主機名稱。DoH 明確設定會被驗證拒絕；UDP/TCP 雖可通過驗證但會忽略。 |
| `spec.bootstrap` | *選填* | array | 選用的上游主機名稱解析 DNS 清單。與 bootstrapFrom 皆空或省略且無必要相依等待時，使用 OS 解析器。同一轉送規則的上游共用合併後設定。 |
| `spec.bootstrapFrom` | *選填* | array | 選用且 field 必填的來源清單；resource/field 須非空，optional 預設 false。請使用既有 Kind/name 及其提供的狀態欄位；但此型別不同於一般 StatusValueSourceSpec，目前驗證不檢查目標存在或提供欄位成員資格。動態與固定 bootstrap 合併，固定清單可補足必要動態值的缺值。 |
| `spec.sourceInterface` | *選填* | string | 選用的已宣告 Interface 或 WireGuardInterface 名稱；預設空值由 OS 選路。綁定由轉送池共用，須注意上述原樣名稱及 FreeBSD 限制。 |
| `spec.when` | *選填* | object | 選用共用狀態條件，預設無條件。篩除此上游可能使轉送規則沒有可用端點。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go) · [cmd/routerd-dns-resolver/upstream_bind_linux.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_linux.go) · [cmd/routerd-dns-resolver/upstream_bind_freebsd.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_freebsd.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `address`, `url`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
