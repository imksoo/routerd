# LocalServiceRedirect

[各資源的設定](../resources.md#localserviceredirect) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

以 nftables prerouting NAT 將指定介面收到的符合流量重新導向路由器本機服務連接埠。

## 指定條件與參照關係

- interface 與非空 rules 必填。每條規則都需要 protocols、destinationSetRef 及兩個連接埠。規則內條件為 AND，協定選項為 OR。
- 比對接收介面、目的位址集合及目的連接埠。影響接收封包，不處理本機發起的 OUTPUT 流量。目標是本機服務，不是遠端後端；其他用途請用 PortForward/IngressService。
- 本機接聽服務與防火牆許可須另行設定。重新導向不啟動 DNSResolver 或其他程式，也不新增通用允許規則。依參照集合的位址家族產生 IPv4/IPv6 規則；DNS 集合由 IPAddressSet 更新。此實作產生 nftables 規則，不產生 FreeBSD PF 重新導向。
- 規則依資源 ID 再依名稱排序，不依 YAML 清單順序。省略名稱時使用從 0 開始的十進位索引字串，字典序可能使 10 在 2 前面。條件重疊時請明確命名以控制順序。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: "<name>"
  spec:
    interface: <string>
    rules:
      -
        destinationPort: <integer>
        destinationSetRef: <string>
        [ name: <string> ]
        protocols:
          - <string>
        redirectPort: <integer>
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
| `spec.interface` | **必填** | string | 必填，同一 Router 宣告的邏輯介面，解析為 OS 接收介面名稱。無預設值或狀態來源。 |
| `spec.rules` | **必填** | array | 必填且非空的重新導向清單。協定、位址集合及連接埠不從其他規則繼承。 |
| `spec.rules[].name` | *選填* | string | 選用的排序／註解標籤，預設為從 0 開始的十進位索引。不是資源參照，此處不拒絕重複名稱。 |
| `spec.rules[].protocols` | **必填** | array | 必填且非空的 tcp/udp 子集，拒絕重複或其他協定。不會預設同時啟用兩者。 |
| `spec.rules[].destinationSetRef` | **必填** | string | 必填，同一 Router 宣告的 IPAddressSet 裸名稱或 IPAddressSet/name。不支援欄位選擇或內嵌位址清單。 |
| `spec.rules[].destinationPort` | **必填** | integer; minimum=1; maximum=65535 | 必填的原目的連接埠 1–65535，無範圍語法或預設值。 |
| `spec.rules[].redirectPort` | **必填** | integer; minimum=1; maximum=65535 | 必填的本機服務連接埠 1–65535。重新導向後需有可連接的接聽服務，YAML 驗證不檢查該服務。 |
| `spec.when` | *選填* | object | 選用的共用啟用條件，預設無條件。state/all/any 與參照限制請見共用欄位。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
