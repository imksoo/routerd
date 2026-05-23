---
title: 具備健康檢查的多 WAN 切換
---

# 具備健康檢查的多 WAN 切換

## 適用情境

路由器有多條對外路徑，希望 routerd 能夠：

- 從可用路徑中自動選擇最佳路徑。
- 主線路異常時自動切換至備援線路。
- 切換時不中斷現有連線，平滑完成轉移。

常見情境如下：

- 家庭路由器以 DS-Lite 隧道為主線路，以上游 HGW NAT 為備援。
- SOHO 路由器以兩條 ISP 線路（光纖 + LTE 等）進行冗餘。
- 分支辦公室路由器優先使用企業 VPN 線路，僅在無法連線時切換至公共網際網路。

## routerd 的解決方式

使用 `EgressRoutePolicy` 宣告候選路徑與選擇方式。
routerd 會持續選取「ready（上游資源已穩定）且 healthy（`HealthCheck` 通過）」的候選中，
weight 最高的路徑。切換時更新 OS 路由表，並重新套用追蹤策略的 `NAT44Rule`，
但刻意不清除 conntrack。現有連線繼續維持，只有新連線使用新路徑。

這樣的設計讓 weight 較低的備援路徑從啟動之初即可使用，之後等主線路確認可用後再平滑切回。

測試用 PPPoE 等會消耗 session 額度的備援路徑，可保留在 YAML 中同時設為停用。
在 `PPPoESession`、對應的 `HealthCheck` 及 `EgressRoutePolicy` 的候選中指定 `disabled: true`，
正常套用時即會停止並停用已產生的服務，需要時可手動進行測試。

## 最小配置

配置由三個元件組成：各候選路徑的 `HealthCheck`、統整這些路徑的 `EgressRoutePolicy`，
以及追蹤策略的 `NAT44Rule`。

### 健康檢查

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-via-primary
spec:
  target: 1.1.1.1
  protocol: tcp
  port: 443
  interval: 30s
  timeout: 3s
```

每個檢查從對應的 `EgressRoutePolicy` 候選中參照。routerd 從該參照推導探針的來源綁定與 socket mark，
因此配置中無需寫入主機特定的設定。
ICMP 容易被中途的過濾器丟棄，建議對穩定目的地（如 1.1.1.1）使用 TCP/443 較為可靠。

### Egress 策略

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
  hysteresis: 30s
  candidates:
    - name: ds-lite-primary
      source: DSLiteTunnel/ds-lite-primary
      deviceFrom:
        resource: DSLiteTunnel/ds-lite-primary
        field: interface
      gatewaySource: none
      weight: 90
      healthCheck: internet-via-primary

    - name: hgw-fallback
      source: Interface/wan
      deviceFrom:
        resource: Interface/wan
        field: ifname
      gatewaySource: static
      gateway: 192.0.2.1
      weight: 50
      healthCheck: internet-via-hgw
```

`hysteresis` 用於防止頻繁切換（chatter）。候選路徑變為 unhealthy 後，在此時間內不會降級。

### 追蹤策略的 NAT

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-egress
spec:
  type: masquerade
  egressPolicyRef: ipv4-default
  sourceRanges:
    - 192.0.2.0/24
```

masquerade 的來源位址取自 routerd 當時所選候選路徑的介面位址。
切換後，下一個封包即以新路徑的位址進行 NAT。

## 不對 RFC1918 目的地執行 NAT

當上游閘道擁有返回 LAN 的路由時，可對公共網際網路執行 NAT，同時避免對其他內部網路目的地執行 NAT。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-wan-hgw
spec:
  type: masquerade
  egressInterface: wan
  sourceRanges:
    - 192.0.2.0/24
  excludeDestinationCIDRs:
    - 192.168.0.0/16
    - 172.16.0.0/12
    - 10.0.0.0/8

---

apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: hgw-lan
spec:
  destination: 192.168.0.0/16
  device: wan
```

這樣可讓 RFC1918 目的地透過路由分配，公共網際網路流量則經由所選的 egress 路徑。

## 操作建議

- 請務必確保帶外管理路徑（mgmt 介面、控制台、專用 SSH NIC 等）。透過 WAN 側 SSH 修改路由或防火牆設定有一定風險。
- 一個 `HealthCheck` 建議只由一個候選路徑參照。這樣 routerd 能推導出單一探針路徑，「探針失敗 = 該路徑故障」的解讀更清晰。
- 切換時請勿清除 conntrack。routerd 刻意不清除，讓已完成交握的 TCP 連線自然結束。
- 當前選用的候選路徑可透過 `routerctl describe EgressRoutePolicy/<name>` 的 `status.selectedCandidate` 確認。

## 相關項目

- [Path MTU 與 MSS clamping](../concepts/path-mtu.md)
- [防火牆規則基礎](./firewall-rule.md)
- [DS-Lite 設定](./flets-ipv6-setup.md)
