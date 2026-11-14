# ClusterNetworkRoute

[各資源的設定](../resources.md#clusternetworkroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

透過明確指定的下一跳，設定前往 Kubernetes Pod 與 Service 網路的 IPv4 路由。不會自動探索叢集 CIDR 或節點。

## 指定條件與參照關係

- pods 與 services 合計至少需要一個 CIDR，via 至少需要一個項目。僅接受 IPv4，兩個清單之間也不允許重複或重疊的網段。
- 每個 CIDR 與 via 的組合會產生名為 <name>-pods-NN-via-NN 或 <name>-services-NN-via-NN 的 IPv4StaticRoute，編號從 1 開始。若已明確宣告相同 apiVersion、kind、name 的資源，則優先使用該資源並略過產生。
- weight 轉換為路由 metric，並非 ECMP 流量分配權重：省略或 0 為 100；1–999 為 1000 − weight。正值越大，metric 越低；0 與 900 的 metric 相同。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    pods: # 選填; object
      cidrs: # 選填; array
        - "<string>" # string
    services: # 選填; object
      cidrs: # 選填; array
        - "<string>" # string
    via: # 必填; array
      - # object
        interface: "<string>" # 必填; string
        nextHop: "<string>" # 必填; string
        weight: 0 # 選填; integer; minimum=0; maximum=999
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.pods` | 選用的 Pod 網路 CIDR 群組；不會自動補入 CIDR。 |
| `spec.pods.cidrs` | Pod 網路的 IPv4 前綴。預設為空；services.cidrs 為空時必填。群組內及群組間皆不可重複或重疊。 |
| `spec.services` | 選用的 Service 網路 CIDR 群組；不會自動補入 CIDR。 |
| `spec.services.cidrs` | Service 網路的 IPv4 前綴。預設為空；pods.cidrs 為空時必填。兩個群組皆適用相同的重複與重疊限制。 |
| `spec.via` | 必填且不可為空的下一跳清單。每個項目皆套用至所有 CIDR。相同 interface 與 nextHop 組合即使 weight 不同也會被拒絕。 |
| `spec.via[].interface` | 必填，為同一 Router 中宣告的邏輯介面名稱。必須能在設定的介面索引中解析，並非狀態值來源。 |
| `spec.via[].nextHop` | 必填，為不含 CIDR 的 IPv4 下一跳位址，不接受資源參照。沒有預設值或自動探索。 |
| `spec.via[].weight` | 選用整數 0–999，預設為 0。依上述規則轉換為 metric，不是流量比例。 |
| `spec.when` | 選用的共用狀態條件；省略表示無條件。state/all/any 的語意與結構描述限制請見共用欄位。先篩選條件，再展開產生的路由。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/cluster_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/api/cluster_routes.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/render/networkd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/networkd.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)
