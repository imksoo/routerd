---
title: sysctl 設定檔
slug: /concepts/sysctl-profile
---

# sysctl 設定檔

![routerd 派生的 router sysctl、明確 Sysctl 與 SysctlProfile override、platform gate，以及 runtime 或 persistent write](/img/diagrams/concept-sysctl-profile.png)

routerd 會從路由器的資源中自動推導出適用於 Linux 路由器的 sysctl 設定。
在一般的家用路由器設定中，不需要羅列大量的 `SysctlProfile` 或 `Sysctl`。
routerd 會從 NAT、DS-Lite、BGP、IPv6 前綴委派（PD）、RA、LAN 服務等資源，
自動推導出 forwarding、redirect、reverse path filter、conntrack、TCP，以及各介面的 RA 設定。

`Sysctl` 和 `SysctlProfile` 僅作為有限的逃生出口，用來補充 routerd 尚無法自動推導的
硬體、核心或發行版特有設定。它們不是表達路由器需求的主要手段，
而是作為實作層面的覆寫選項。

`runtime: true` 會在控制器鏈 serve 執行期間，立即將設定反映至執行中的核心。
`persistent: true` 會將持久設定寫入 `/etc/sysctl.d/`。
`routerctl apply` 只會將明確指定的 `Sysctl` / `SysctlProfile` 套用至主機。
推導產生的 sysctl 屬於 plan / render 的對象，實際套用由 `routerd serve` 負責。

僅在需要使用明確設定檔作為逃生出口時，才透過 `overrides` 覆寫差異。

```yaml
spec:
  profile: router-linux
  overrides:
    net.netfilter.nf_conntrack_max: "524288"
```

routerd 在寫入前會先讀回確認現有值。
若目前的值已符合預期，則不執行寫入。
此情況下也不會發出套用事件。

部分 sysctl 的值會被核心向上取整。
對於這類值，請使用 `compare: atLeast`。
`value` 是寫入的值，`expectedValue` 是讀回時預期的值。
省略 `expectedValue` 時，以 `value` 作為預期值。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: socket-buffer
spec:
  key: net.core.rmem_max
  value: "16777216"
  expectedValue: "16777216"
  compare: atLeast
  runtime: true
```

## router-linux 的設定值

| 鍵 | 值 | 說明 |
| --- | --- | --- |
| `net.ipv4.ip_forward` | `1` | 啟用 IPv4 封包轉送。 |
| `net.ipv4.conf.all.forwarding` | `1` | 啟用各介面的 IPv4 轉送。 |
| `net.ipv4.conf.all.rp_filter` | `0` | 避免 reverse path filter 丟棄原則路由或 DS-Lite 通道的回程封包。 |
| `net.ipv4.conf.default.rp_filter` | `0` | 對之後建立的通道介面也停用 reverse path filter。 |
| `net.ipv4.conf.all.send_redirects` | `0` | 不從路由器發送 ICMP redirect。 |
| `net.ipv4.conf.default.send_redirects` | `0` | 對之後建立的介面套用相同設定。 |
| `net.ipv4.conf.all.src_valid_mark` | `1` | 讓使用 fwmark 的路由選擇在 reverse path 判斷時能夠考慮 mark 值。 |
| `net.ipv6.conf.all.forwarding` | `1` | 啟用 IPv6 封包轉送。 |
| `net.ipv6.conf.default.forwarding` | `1` | 對之後建立的介面也啟用 IPv6 轉送。 |
| `net.netfilter.nf_conntrack_acct` | `1` | 啟用 conntrack 的封包與位元組統計，用於 Web 管理介面的用戶端流量匯總。在未載入 conntrack 的環境中為選用。 |
| `net.netfilter.nf_conntrack_max` | `262144` | 避免大量裝置和應用程式同時連線時 conntrack 滿載。在未載入 conntrack 的環境中為選用。 |
| `net.netfilter.nf_conntrack_buckets` | `65536` | 建議設為 `nf_conntrack_max / 4`。因環境而異可能無法寫入，故為選用。 |
| `net.netfilter.nf_conntrack_tcp_timeout_established` | `86400` | 預設的 5 天對家用路由器而言過長，縮短為 24 小時。在未載入 conntrack 的環境中為選用。 |
| `net.netfilter.nf_conntrack_udp_timeout` | `30` | 縮短單次 UDP 的保留時間。在未載入 conntrack 的環境中為選用。 |
| `net.netfilter.nf_conntrack_udp_timeout_stream` | `180` | 將持續 UDP 的保留時間設為 3 分鐘。在未載入 conntrack 的環境中為選用。 |
| `net.core.rmem_max` | `16777216` | 將接收緩衝區上限設為 16 MiB。 |
| `net.core.wmem_max` | `16777216` | 將發送緩衝區上限設為 16 MiB。 |
| `net.ipv4.tcp_rmem` | `4096 87380 16777216` | 擴大 TCP 接收緩衝區的自動調整範圍。 |
| `net.ipv4.tcp_wmem` | `4096 65536 16777216` | 擴大 TCP 發送緩衝區的自動調整範圍。 |
| `net.core.netdev_max_backlog` | `5000` | 降低瞬間接收突發流量時發生丟棄的機率。 |
| `net.core.somaxconn` | `4096` | 明確指定 listen backlog 的上限。 |
| `net.ipv4.ip_local_port_range` | `1024 65535` | 擴大路由器本身使用的臨時連接埠範圍。 |
| `net.ipv4.tcp_fin_timeout` | `30` | 縮短 FIN-WAIT-2 的保留時間。 |
| `net.ipv4.tcp_mtu_probing` | `1` | 讓 TCP 在無法收到 Path MTU notification 的路徑上也能退回較小的 segment。 |
| `net.ipv4.tcp_tw_reuse` | `1` | 允許重複使用 TIME-WAIT socket。 |
| `net.ipv6.route.max_size` | `16384` | 提高 IPv6 路由快取的上限。 |

`net.ipv4.route.max_size` 在現行 Linux 的部分環境中效果有限，
routerd 的預設設定檔不予設定。
若有需要，請以個別 `Sysctl` 的形式（而非 `overrides`）新增，並在實機上確認該鍵是否存在。

`net.netfilter.nf_conntrack_udp_timeout` 的預設值為 `30` 秒，
與 Linux conntrack 對無回應 UDP 的預設值一致。
若需要稍長時間以便與防火牆拒絕或 DPI 觀測記錄相關聯，可覆寫為 `60` 秒。

```yaml
spec:
  profile: router-linux
  overrides:
    net.netfilter.nf_conntrack_udp_timeout: "60"
```

conntrack、NFLOG、WireGuard 等模組的載入，routerd 會從 NAT、防火牆記錄、
連線流量記錄、WireGuard 等資源自動推導。
`KernelModule` 不是使用者撰寫的設定 Kind。若有推導遺漏，
應視為實作端推導邏輯的錯誤加以修正。

## 與個別 Sysctl 的使用區別

個別 `Sysctl` 僅用於真正偏離 routerd 推導模型的值。
DS-Lite 通道的 `rp_filter=0`、WAN/LAN 的 `accept_ra=2`、LAN 的
`send_redirects=0` 等 routerd 能夠理解的介面設定，會從資源自動推導，
通常不需要在設定中手動撰寫。

範例：在驗證用核心上臨時提高 socket 緩衝區大小

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: lab-rmem-max
spec:
  key: net.core.rmem_max
  value: "33554432"
  compare: atLeast
  runtime: true
  persistent: true
```
