# EgressRoutePolicy

![EgressRoutePolicy 從 health status 選擇 outbound candidate 並發布 advisory 或已套用 routing state 的流程](/img/diagrams/concept-egress-route.png)

`EgressRoutePolicy` 選擇對外通訊所使用的路由，
取代先前的 WAN 路由策略。
不接受舊 Kind 名稱。

此策略觀察候選資源與 HealthCheck，並將所選候選儲存於 status。
其他資源可參照該 status。

`spec.mode` 的不同會影響 status 的擁有者。省略 `mode` 時，
egress-route 選擇器僅輸出含選擇結果的 status，以及附有 `role: advisory` / `advisory: true`
的 `routerd.lan.route.changed` 事件。此 status 是執行中控制器的輸出，並非套用的模擬執行結果。
`mode: priority`、`mark`、`hash` 時，policy 路由控制器成為
實際套用的路由與 NAT mark 狀態的擁有者。相依的控制器改為監聽 `routerd.resource.status.changed`，而非舊式的 route-changed 事件。

`mode: priority` 同樣使用 `selection: highest-weight-ready`。
從準備就緒的候選中選出 weight 最高的那一個，`priority` 作為平局決勝與
policy 路由規則的優先度。`priority` 不是選擇策略的替代品。`weighted-ecmp` 是實作前的保留值，不會靜默忽略，而是回報為 `UnsupportedSelection`。`enabled: false` 的候選不列入選擇對象，也不擁有所產生的 policy 路由規則與路由表。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: EgressRoutePolicy
metadata:
  name: ipv4-default
spec:
  family: ipv4
  destinationCIDRs:
    - 0.0.0.0/0
  selection: highest-weight-ready
  candidates:
    - name: ds-lite
      source: DSLiteTunnel/ds-lite
      deviceFrom:
        resource: DSLiteTunnel/ds-lite
        field: interface
      gatewaySource: none
      weight: 80
      healthCheck: internet-tcp443
    - name: ix2215
      source: Interface/ix2215
      deviceFrom:
        resource: Interface/ix2215
        field: ifname
      gatewaySource: static
      gateway: 172.17.0.1
      weight: 50
```

`destinationCIDRs` 是策略的目的地範圍。
省略時，IPv4 預設使用 `0.0.0.0/0`。
IPv6 預設使用 `::/0`。

`gatewaySource` 決定閘道的選取方式。

- `none`：用於 DS-Lite 等點對點裝置。
- `static`：在 `gateway` 中填寫 next hop 位址。
- `dhcpv4` 與 `dhcpv6`：用於來自 DHCP 用戶端的閘道。

選擇結果寫入以下 status：

- `status.selectedCandidate`
- `status.selectedDevice`
- `status.selectedGateway`
- `status.selectedWeight`
- `status.selectedTargets`
- `status.destinationCIDRs`

啟動後，首先選擇準備就緒的候選，不會無限期等待 weight 最高的路由。
若之後 weight 較高的候選進入就緒狀態，
routerd 會發出 `routerd.lan.route.changed`，
進而更新 `IPv4Route` 與 `NAT44Rule`。
此時不會清除 conntrack。
現有通訊依核心持有的狀態繼續，
新通訊則使用新路由與新 NAT 方向。

`IPv4Route` 可參照這些 status：

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: default-route
spec:
  destination: 0.0.0.0/0
  deviceFrom:
    resource: EgressRoutePolicy/ipv4-default
    field: selectedDevice
  gatewayFrom:
    resource: EgressRoutePolicy/ipv4-default
    field: selectedGateway
```

不應經由 DS-Lite（或任何隧道）的內部目的地，以一般路由方式表達。
上游閘道側的私有子網路指向 WAN 側，內部的 `10.0.0.0/8` 或 `172.16.0.0/12` 使用專屬路由，需要捨棄的範圍使用 `type: blackhole`。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: private-10-blackhole
spec:
  type: blackhole
  destination: 10.0.0.0/8
```

## HealthCheck

`HealthCheck` 宣告探測的意圖。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-tcp443
spec:
  target: 1.1.1.1
  protocol: tcp
  port: 443
```

`HealthCheck` 被 `EgressRoutePolicy` 的候選或 target 參照時，
routerd 自動從該路由 target 導出 health-check 常駐程式、socket mark 及來源綁定。設定中只需描述探測的意圖，各平台的 socket 機制則封閉在控制器與產生器內部。
