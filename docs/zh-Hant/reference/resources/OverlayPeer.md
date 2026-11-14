# OverlayPeer

[各資源的設定](../resources.md#overlaypeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

描述供 HybridRoute 與路徑 MTU 策略衍生使用的既有傳輸；本資源不自行建立 WireGuard、Tailscale、IPsec 或隧道。

## 指定條件與參照關係

- role、nodeID、underlay.type 必填。一般驗證在 wireguard/ipip/gre/fou/gue 要求 underlay.interface，HybridRoute 展開時則所有類型均需要，含 tailscale/ipsec/route。
- ipip/gre/fou/gue 的 underlay.interface 裸名須參照已宣告 TunnelInterface，但不驗證 mode 與 underlay.type 相符。WireGuard 可使用外部管理裝置，僅警告不拒絕；其他裝置亦須事先可用。
- nodeID/remote.nodeID/role 為描述資訊，不提供探索、認證、HA 選舉或自動拓撲匹配；無 when/dependsOn。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # 必填; string
    pathMTU: # 選填; object
      forceFragmentIPv4: true # 選填; boolean
    remote: # 選填; object
      address: "<string>" # 選填; string
      nodeID: "<string>" # 選填; string
    role: "onprem" # 必填; string; enum=["onprem", "cloud"]
    underlay: # 必填; object
      address: "<string>" # 選填; string
      interface: "<string>" # 選填; string
      type: "wireguard" # 必填; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.role` | 必填 onprem 或 cloud，無預設值，不建立雲端資源或選擇業者。 |
| `spec.nodeID` | 必填非空本機節點身分標籤，不解析為主機或外部帳戶。 |
| `spec.underlay` | 必填傳輸描述，實際網路傳輸須另行設定。 |
| `spec.underlay.type` | 必填 wireguard/tailscale/ipsec/route/ipip/gre/fou/gue，選擇路由展開與 MTU 開銷估算，不建立常駐程式。 |
| `spec.underlay.interface` | 產生 IPv4Route 所用裝置或邏輯介面，路由展開時必填。隧道模式使用 TunnelInterface 裸名，外部管理裝置使用實際 OS 名稱，不自動探索。 |
| `spec.underlay.address` | 選用純 IPv4/IPv6 描述，驗證為 IP。目前 HybridRoute 目標衍生不將其用作閘道或本機綁定，也不指派到介面。 |
| `spec.remote` | 選用遠端描述，預設空，不註冊或協商對端。 |
| `spec.remote.nodeID` | 選用遠端身分標籤，預設空，不用於即時探索或授權。 |
| `spec.remote.address` | 選用 IP 字串。僅 ipsec 路由展開會以有效 IPv4 作 gateway，無效/IPv6/空值為裝置路由。其他類型不以此作閘道，OverlayPeer 驗證亦未單獨檢查其為 IP。 |
| `spec.pathMTU` | 選用轉送路徑 MTU 選項，預設不要求強制分片，不會加密底層。 |
| `spec.pathMTU.forceFragmentIPv4` | 預設 false，true 僅限 wireguard/ipip/gre/fou/gue。透過 Linux nftables 對符合衍生轉送路徑的過大 IPv4 清除 DF，不是全域隧道旗標或 IPv6 分片。FreeBSD 略過此控制器，此路徑無同等 DF 清除動作。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/render/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/render/mtu.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/mtu.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
