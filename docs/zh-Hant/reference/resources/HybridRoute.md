# HybridRoute

[各資源的設定](../resources.md#hybridroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

將明確列出的 IPv4 目的網路展開為經 OverlayPeer 的主路由表 IPv4Route。

## 指定條件與參照關係

- 僅支援非預設 IPv4 CIDR，拒絕 default、0.0.0.0/0、IPv6 及非主表。目的前綴遮罩化後產生 <資源名>-<CIDR非英數連續字元改連字號>，例如 branch-10-20-0-0-16。
- 產生名稱須唯一，不可與明確 IPv4Route 名稱或目的衝突。同一 HybridRoute 內重複正規化目的也會衝突。HealthCheck 異常僅改變狀態，不阻擋安裝、撤回路由或改選對端。
- 產生 IPv4Route 使用 peer.underlay.interface 作 device，僅 ipsec 可將 IPv4 remote.address 作 gateway，其他為裝置路由。須另行提供隧道/VPN 與實際可達性。Linux 使用正 metric，FreeBSD 底層 IPv4Route 不套用 metric。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # 必填; array
      - "<string>" # string
    healthCheckRef: "<string>" # 選填; string
    install: # 選填; object
      metric: 0 # 選填; integer; minimum=0
      table: "" # 選填; string; enum=["", "main"]
    peerRef: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.destinationCIDRs` | 必填非空且不含預設路由的 IPv4 CIDR 清單，不自動補遠端網路或探索。 |
| `spec.peerRef` | 必填既有 OverlayPeer metadata.name 裸名；展開雖可正規化 OverlayPeer/name，一般跨資源驗證仍要求裸名。 |
| `spec.install` | 選用路由安裝設定，預設主表、metric 0，不提供策略路由或自動 HA 選擇。 |
| `spec.install.table` | 僅空值或 main，兩者均展開為主表 IPv4Route，拒絕其他表名或編號。 |
| `spec.install.metric` | 非負整數，預設 0，複製至產生 IPv4Route；Linux 僅輸出正值，FreeBSD 路徑忽略。 |
| `spec.healthCheckRef` | 選用既有 HealthCheck 裸名，空值不附加健康狀態。unhealthy/failed/error 標示 Degraded 但不變更路由，此函式不將缺少或 unknown 狀態視為異常。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/controller/chain/dynamic_effective.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/dynamic_effective.go) · [pkg/controller/chain/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hybrid.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `routes`, `peerRef`, `defaultRouteUntouched`, `estimatedMTU`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
