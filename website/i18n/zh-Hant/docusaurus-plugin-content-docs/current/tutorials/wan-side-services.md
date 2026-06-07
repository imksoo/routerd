---
title: WAN 側服務
sidebar_position: 4
---

# WAN 側服務

![處理 DHCPv4、DHCPv6-PD、PPPoE、DS-Lite、health check、egress selection、NAT44 與 downstream status input 的 WAN-side routerd services](/img/diagrams/tutorial-wan-side-services.png)

本頁介紹處理路由器 WAN 側的 routerd 資源。
WAN 側資源負責建立上游鏈路、從 ISP 取得 IP 位址與前綴、終結隧道，以及向控制器鏈提供多條上游路由等功能。

LAN 側（路由器向內側提供的服務）請參閱 [LAN 側服務](./lan-side-services.md)。

## 一覽

| 功能 | 資源 | 負責常駐程式 |
| --- | --- | --- |
| 實體 / 虛擬介面 | `Interface`、`IPv4StaticAddress` | （kernel） |
| 透過 DHCP 從 ISP 取得 IPv4 | `DHCPv4Client` | `routerd-dhcpv4-client` |
| 從 ISP 取得 IPv6 前綴 | `DHCPv6PrefixDelegation`、`IPv6DelegatedAddress` | `routerd-dhcpv6-client` |
| 其他 DHCPv6 選項（DNS、AFTR 等） | `DHCPv6Information` | `routerd-dhcpv6-client` |
| 上游時間伺服器 | `NTPClient` | `systemd-timesyncd` 或 `ntpd` |
| PPPoE 會話 | `PPPoESession` | `routerd-pppoe-client` |
| IPv6 上的 IPv4（DS-Lite） | `DSLiteTunnel` | （kernel `ip6tnl`） |
| WAN 路由選擇 | `EgressRoutePolicy`、`HealthCheck` | `routerd-healthcheck@<name>` |
| IPv4 NAT（masquerade） | `NAT44Rule` | （nftables） |
| 靜態 IPv4 路由 | `IPv4Route` | （kernel） |

請根據 ISP 的提供形態，選擇所需資源的組合。

## 模式 A：原生雙堆疊（IPv4 + IPv6）

ISP 在同一 WAN 介面上同時發送 IPv4（DHCPv4）與 IPv6 前綴（DHCPv6-PD）的構成。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata: {name: wan}
  spec:
    ifname: ens18
    role: untrust

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata: {name: wan-v4}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata: {name: wan-pd}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata: {name: lan-base}
  spec:
    pdRef: wan-pd
    interface: lan
    suffix: ::1/64

- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata: {name: lan-to-wan}
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.0.2.0/24
```

`DHCPv4Client` 啟動 `routerd-dhcpv4-client`，並將租約內容寫入 `lease.json`。位址本身由 kernel 持有，routerd 向下游資源發出事件。

`DHCPv6PrefixDelegation` 使用 `routerd-dhcpv6-client` 取得 IA_PD。`IPv6DelegatedAddress` 從取得的前綴中切出分配給 LAN 側的 `/64`（或其他長度）。

## 上游 NTP / SNTP

`NTPClient` 可從 DHCPv4 option 42 或 DHCPv6 option 31 中擷取時間伺服器。
若上游不發送時間伺服器，則將指定的公共 NTP 伺服器設定至 OS 的 NTP 用戶端。
Linux / NixOS 使用 `systemd-timesyncd`，FreeBSD 使用 `ntpd`。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata: {name: system-time}
  spec:
    provider: systemd-timesyncd
    managed: true
    source: auto
    serverFrom:
      - resource: DHCPv4Client/wan-v4
        field: ntpServers
      - resource: DHCPv6Information/wan-info
        field: sntpServers
    fallbackServers:
      - ntp.jst.mfeed.ad.jp
      - ntp.nict.jp
```

若要將路由器本身作為 LAN 用戶端的時間參考來源，請併用 LAN 側的 `ntpServerFrom` 與 `sntpServerFrom`。

## 模式 B：PPPoE（IPv4）+ DHCPv6-PD（IPv6）

舊式 xDSL 系構成，IPv4 透過 PPPoE 取得，IPv6 透過相同實體鏈路的原生 DHCPv6-PD 取得。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata: {name: wan-pppoe}
  spec:
    interface: wan
    user: "user@isp.example"
    passwordFromSecret: pppoe-password
    mtu: 1454
    mru: 1454

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata: {name: wan-pd}
  spec:
    interface: wan
```

`PPPoESession` 啟動 `routerd-pppoe-client`，在 Linux 上包裝 `pppd`/`rp-pppoe`，在 FreeBSD 上包裝 `ppp(8)`。PPPoE 會話的介面（通常為 `ppp0`）可作為路由或 `NAT44Rule` 的參照對象。

## 模式 C：DS-Lite（在僅 IPv6 的接入網路上隧道 IPv4）

ISP 不提供原生 IPv4，僅提供 IPv6 的構成。IPv4 透過連往 AFTR（Address Family Transition Router）的 DS-Lite 隧道實現。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata: {name: wan-pd}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata: {name: wan-info}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata: {name: ds-lite-primary}
  spec:
    sourceInterface: wan
    aftrFQDN: gw.transix.jp
    aftrFQDNResolverFromResource:
      resource: DHCPv6Information/wan-info
      field: dnsServers
    mtu: 1454
```

`DSLiteTunnel` 在解析到 AFTR 位址後，以 kernel 的 `ip6tnl` 裝置建立。
AFTR 記錄通常只能透過接入網路內的 DNS 解析，因此請使用 `aftrFQDNResolverFromResource` 指定 ISP 的 DNS。

## 模式 D：多 WAN（主線路 + 備援）

有多條路由時，請將 `EgressRoutePolicy` 與 `HealthCheck` 組合至 WAN 取得資源中使用。詳細請參閱[多 WAN 切換](../how-to/multi-wan.md)。

## 狀態確認

各 WAN 資源的狀況可透過 `routerctl describe <kind>/<name>` 確認。範例：

```sh
routerctl describe DHCPv6PrefixDelegation/wan-pd      # phase: Bound, prefix: 2001:db8:1::/56
routerctl describe DSLiteTunnel/ds-lite-primary       # phase: Up, aftr: 2001:db8:cafe::1
routerctl describe EgressRoutePolicy/ipv4-default     # selectedCandidate: ds-lite-primary
```

亦可從 Web 管理介面的「Overview」「Resources」分頁確認相同資訊。「Connections」分頁顯示各 WAN 路由的實際 conntrack/pf 狀態。

## 相關項目

- [LAN 側服務](./lan-side-services.md)
- [多 WAN 切換](../how-to/multi-wan.md)
- [NTT NGN 的 DS-Lite](../how-to/flets-ipv6-setup.md)
- [Path MTU 與 MSS clamping](../concepts/path-mtu.md)
