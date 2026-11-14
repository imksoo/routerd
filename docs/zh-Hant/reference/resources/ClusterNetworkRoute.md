# ClusterNetworkRoute

[各資源的設定](../resources.md#clusternetworkroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

透過明確指定的下一跳，設定前往 Kubernetes Pod 與 Service 網路的 IPv4 路由。不會自動探索叢集 CIDR 或節點。

## 指定條件與參照關係

- pods 與 services 合計至少需要一個 CIDR，via 至少需要一個項目。僅接受 IPv4，兩個清單之間也不允許重複或重疊的網段。
- 每個 CIDR 與 via 的組合會產生名為 <name>-pods-NN-via-NN 或 <name>-services-NN-via-NN 的 IPv4StaticRoute，編號從 1 開始。若已明確宣告相同 apiVersion、kind、name 的資源，則優先使用該資源並略過產生。
- weight 轉換為路由 metric，並非 ECMP 流量分配權重：省略或 0 為 100；1–999 為 1000 − weight。正值越大，metric 越低；0 與 900 的 metric 相同。
- 產生的路由亦受 IPv4StaticRoute 實作限制：FreeBSD 會輸出，但目前 Linux 流程不安裝產生的靜態路由。展開與驗證成功並不證明能正常轉送。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    [ pods:
        [ cidrs:
            - <string>
        ]
    ]
    [ services:
        [ cidrs:
            - <string>
        ]
    ]
    via:
      -
        interface: <string>
        nextHop: <string>
        [ weight: <integer> ]
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
| `spec.pods` | *選填* | object | 選用的 Pod 網路 CIDR 群組；不會自動補入 CIDR。 |
| `spec.pods.cidrs` | *選填* | array | Pod 網路的 IPv4 前綴。預設為空；services.cidrs 為空時必填。群組內及群組間皆不可重複或重疊。 |
| `spec.services` | *選填* | object | 選用的 Service 網路 CIDR 群組；不會自動補入 CIDR。 |
| `spec.services.cidrs` | *選填* | array | Service 網路的 IPv4 前綴。預設為空；pods.cidrs 為空時必填。兩個群組皆適用相同的重複與重疊限制。 |
| `spec.via` | **必填** | array | 必填且不可為空的下一跳清單。每個項目皆套用至所有 CIDR。相同 interface 與 nextHop 組合即使 weight 不同也會被拒絕。 |
| `spec.via[].interface` | **必填** | string | 必填，為同一 Router 中宣告的邏輯介面名稱。必須能在設定的介面索引中解析，並非狀態值來源。 |
| `spec.via[].nextHop` | **必填** | string | 必填，為不含 CIDR 的 IPv4 下一跳位址，不接受資源參照。沒有預設值或自動探索。 |
| `spec.via[].weight` | *選填* | integer; minimum=0; maximum=999 | 選用整數 0–999，預設為 0。依上述規則轉換為 metric，不是流量比例。 |
| `spec.when` | *選填* | object | 選用的共用狀態條件；省略表示無條件。state/all/any 的語意與結構描述限制請見共用欄位。先篩選條件，再展開產生的路由。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/cluster_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/api/cluster_routes.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/render/networkd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/networkd.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `pods`, `services`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)
