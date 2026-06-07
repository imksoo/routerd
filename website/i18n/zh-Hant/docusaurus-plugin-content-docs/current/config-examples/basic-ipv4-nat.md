---
title: 基本 IPv4 NAT 路由器
sidebar_position: 10
---

# 基本 IPv4 NAT 路由器

![由 DHCP WAN、routerd 管理的 LAN address、DHCPv4 server、NAT44 與 firewall zone 組成的基本 IPv4 gateway](/img/diagrams/config-example-basic-ipv4-nat.png)

這是一個接近最小設定的家用路由器範例，讓 LAN 用戶端透過 DHCP 取得的 WAN 端 IPv4 位址連上網際網路。

完整的已驗證 YAML 位於 `examples/example-basic-ipv4-nat.yaml`。

## 架構圖

```mermaid
flowchart LR
  internet((Internet))
  upstream["[1] ISP / upstream router"]
  wan["[2] wan<br/>DHCPv4 client"]
  router["[3] routerd host"]
  lan["[4] lan<br/>192.168.10.1/24"]
  clients["[5] LAN clients<br/>192.168.10.100-199"]

  internet --- upstream --- wan --- router --- lan --- clients
```

## 圖示對應表

| 編號 | 說明 | 主要資源 |
| --- | --- | --- |
| [1] | 分配 WAN 端 IPv4 租約的上游網路。 | routerd 管理範圍外 |
| [2] | 實體 WAN 介面，在此執行 DHCPv4 用戶端。 | `Interface/wan`、`DHCPv4Client/wan-dhcpv4` |
| [3] | 套用推導出的 forwarding sysctl 和 nftables 規則的 Linux 主機。 | Derived host runtime |
| [4] | routerd 持有的 LAN 閘道位址。 | `Interface/lan`、`IPv4StaticAddress/lan-base` |
| [5] | 將路由器作為閘道 / DNS 使用的 LAN 用戶端。 | `DHCPv4Server/lan-dhcpv4` |

## 此範例管理的項目

| 領域 | routerd 資源 |
| --- | --- |
| WAN 位址 | `Interface/wan`、`DHCPv4Client/wan-dhcpv4` |
| LAN 位址 | `Interface/lan`、`IPv4StaticAddress/lan-base` |
| LAN DHCPv4 | `DHCPv4Server/lan-dhcpv4` |
| IPv4 網際網路連線 | `NAT44Rule/lan-to-wan` |
| 基本過濾器 | `FirewallZone/wan`、`FirewallZone/lan`、`FirewallPolicy/home` |

此範例盡量簡化 DNS。向 DHCPv4 用戶端分發路由器的 LAN 位址作為 DNS 伺服器。在基本路由運作後，可視需要再新增 `DNSResolver` 和 `DNSZone`。

## 設定重點

```yaml
# [2] WAN 位址從上游網路透過 DHCPv4 取得。
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: wan-dhcpv4
  spec:
    interface: wan

# [4] routerd 持有 LAN 閘道位址。
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: lan-base
  spec:
    interface: lan
    address: 192.168.10.1/24

# [5] 向 LAN 用戶端分發位址、閘道、DNS、搜尋網域。
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 192.168.10.100
      end: 192.168.10.199
      leaseTime: 12h
    gatewayFrom:
      resource: IPv4StaticAddress/lan-base
      field: address
    dnsServerFrom:
      - resource: IPv4StaticAddress/lan-base
        field: address

# [2] -> [5] LAN IPv4 流量在出往 WAN 時進行 masquerade。
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: lan-to-wan
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.168.10.0/24
```

`NAT44Rule` 會反映至 routerd 的 nftables NAT 表格。在防火牆資源中，
將 WAN 介面加入 `untrust` 區域，LAN 介面加入 `trust` 區域。

## 套用步驟

```bash
cp examples/example-basic-ipv4-nat.yaml router.yaml
routerctl validate --config router.yaml
routerctl plan --config router.yaml
routerctl apply --config router.yaml --dry-run
```

確認管理存取並非依賴即將變更位址的 LAN 介面，或已具備主控台存取權限後再執行套用。

```bash
routerctl apply --config router.yaml
```

## 確認

```bash
routerctl status
routerctl describe DHCPv4Client/wan-dhcpv4
routerctl describe IPv4StaticAddress/lan-base
routerctl describe NAT44Rule/lan-to-wan
nft list table ip routerd_nat
nft list table inet routerd_filter
```

在 LAN 用戶端端確認以下項目。

```bash
ip route
ping 192.168.10.1
curl https://1.1.1.1/
```

## 常見的修改點

- 將 `ens18` 和 `ens19` 改為實際的介面名稱。
- 若與上游、VPN 或管理網路重疊，請變更 `192.168.10.0/24`。
- 在分發路由器作為 DNS 伺服器之前，視需要先新增 `DNSResolver`。
