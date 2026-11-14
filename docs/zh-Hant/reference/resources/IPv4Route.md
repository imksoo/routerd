# IPv4Route

[各資源的設定](../resources.md#ipv4route) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

安裝 IPv4 單播或黑洞路由，可從資源狀態取得裝置與閘道。

## 指定條件與參照關係

- blackhole 不可搭配 device、deviceFrom、gateway、gatewayFrom 或 preferredSource。unicast 執行時必須能解析裝置；直接連接路由可省略閘道。
- 動態值優先於 device/gateway 固定值；無法解析時使用固定值。沒有可用值時，來源的 optional 不會略過 DeviceMissing 或 GatewayMissing。參照仍須指向已宣告的資源，並明確指定該資源提供的欄位。
- Linux 使用 ip route replace；preferredSource 對應 src，僅正數 metric 會傳入。FreeBSD 使用 route change/add 與 -ifa，此路徑不套用 metric。Installed 不代表端到端連線成功。
- ready_when 與 device/gateway 中的 ${...} 運算式已移除，驗證會拒絕。請使用 dependsOn 與 deviceFrom/gatewayFrom。preferredSource 也不支援狀態運算式。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    destination: "<string>" # 必填; string
    device: "<string>" # 選填; string
    deviceFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    gateway: "<string>" # 選填; string
    gatewayFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    metric: 0 # 選填; integer; minimum=0
    preferredSource: "<string>" # 選填; string
    type: "unicast" # 選填; string; enum=["unicast", "blackhole"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.destination` | 必填的目的 IPv4 CIDR。預設路由請使用 0.0.0.0/0，而非 default。目前驗證僅解析前綴，未在此拒絕 IPv6，但此資源與控制器供 IPv4 使用。 |
| `spec.type` | 預設 unicast。blackhole 丟棄流量，不可指定裝置、閘道或來源。 |
| `spec.device` | 選用的邏輯介面或 OS 裝置名稱，deviceFrom 無值時使用。unicast 為空時為 Pending/DeviceMissing；OS 裝置不存在時為 DeviceNotReady。blackhole 禁用。 |
| `spec.deviceFrom` | 選用的裝置狀態值來源；resource 與 field 遵循共用參照規則。正規化後第一個值優先於 device。預設無來源，blackhole 禁用。 |
| `spec.gateway` | 選用的純 IPv4 下一跳，預設為空，表示透過裝置且無閘道的路由。也是 gatewayFrom 無法解析時的替代值。blackhole 禁用。 |
| `spec.gatewayFrom` | 選用的狀態來源，遵循共用 resource/field 規則。第一個正規化值優先於 gateway。設有來源但來源與固定值皆無值時，維持 Pending/GatewayMissing。blackhole 禁用。 |
| `spec.preferredSource` | 本機發起流量的選用優先 IPv4 來源，預設不指定。套用時必須已是本機位址，否則為 Pending/PreferredSourceNotLocal。不會指派位址或執行 SNAT。blackhole 禁用。 |
| `spec.metric` | 選用的非負整數，預設 0。Linux 僅傳入正值；FreeBSD 套用時忽略。其他條件相同時通常優先使用較低 metric。 |
| `spec.dependsOn` | 選用的共用資源相依條件 AND 清單，預設為空。不滿足時以 DependsOnFalse 略過安裝。resource/field/phase/equals/notEmpty/optional 請見共用欄位。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)
