---
title: LAN 側服務
sidebar_position: 5
---

# LAN 側服務

![處理 LAN address、DHCPv4/DHCPv6、router advertisement、local DNS、lease event 與 client option 的 LAN-side routerd services](/img/diagrams/tutorial-lan-side-services.png)

本頁介紹處理路由器 LAN 側的 routerd 資源。
LAN 側資源負責內側介面的位址、DHCPv4 / DHCPv6 分配、IPv6 Router Advertisement，以及本地 DNS 解析器等功能。

WAN 側（從上游取得位址）請參閱 [WAN 側服務](./wan-side-services.md)。

## 服務分工

routerd 將 LAN 側服務明確劃分給兩個常駐程式：

- **dnsmasq** 負責 DHCPv4、DHCPv6、DHCP relay 及 IPv6 Router Advertisement。
- **`routerd-dns-resolver`** 負責 DNS 區域、條件式轉送、快取及查詢記錄。

採用經過驗證的 dnsmasq 直接處理 DHCP，DNS 政策則以具型別的 routerd 資源（`DNSResolver`、`DNSZone`）表達，兩者各司其職。

## 一覽

| 功能 | 資源 | 負責常駐程式 |
| --- | --- | --- |
| LAN 介面位址 | `IPv4StaticAddress`、`IPv6DelegatedAddress` | （kernel） |
| DHCPv4 範圍 | `DHCPv4Server` | dnsmasq |
| DHCPv4 固定分配 | `DHCPv4Reservation` | dnsmasq |
| DHCPv6（stateless / stateful） | `DHCPv6Server` | dnsmasq |
| IPv6 Router Advertisement | `IPv6RouterAdvertisement` | dnsmasq（RA 模式） |
| LAN 側時間伺服器廣告 | `DHCPv4Server`、`DHCPv6Server` | dnsmasq |
| DNS 區域（本地權威） | `DNSZone` | `routerd-dns-resolver` |
| DNS 解析器監聽 | `DNSResolver` | `routerd-dns-resolver` |
| DHCP 租約事件中繼 | （內建） | `routerd-dhcp-event-relay` |

## DHCPv4 範圍

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 192.0.2.64
      end: 192.0.2.191
      leaseTime: 12h
    gatewayFrom:
      resource: IPv4StaticAddress/lan-base
      field: address
    dnsServerFrom:
      - resource: IPv4StaticAddress/lan-base
        field: address
    ntpServerFrom:
      - resource: IPv4StaticAddress/lan-base
        field: address
    domainFrom:
      resource: DNSZone/lan
      field: zone
    stickyHoldDays: 3
```

將自動分配的用戶端範圍與固定位址範圍分開，可使操作更清晰易讀。
`stickyHoldDays` 為選填項目。指定大於 0 的值後，routerd 會短期保留 DHCP 租約歷史，並在租約釋放或到期後，臨時產生 dnsmasq 的 `dhcp-host` hold 項目。相同 MAC 位址可在 hold 期間內重新取得相同位址，該位址不會立即分配給其他用戶端。

## DHCPv4 靜態預約

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: smart-meter
  spec:
    server: lan-dhcpv4
    macAddress: "02:00:00:00:00:01"
    hostname: smart-meter
    ipAddress: 192.0.2.10
```

`DHCPv4Reservation` 會展開為 dnsmasq 的 host reservation 項目。
在 Web 管理介面與事件記錄中，會以不依賴裝置當前 IP 的穩定資源名稱顯示。

FreeBSD 上，dnsmasq 的租約檔案存放於 `/var/db/routerd/dnsmasq` 目錄下。
若僅存放於 `/var/run`，重新啟動後租約將遺失。
rc.d 腳本會在啟動前建立執行時期目錄與租約目錄。
`routerctl apply` 會在重新啟動 dnsmasq 前先執行 `dnsmasq --test`。
同時也會自動產生 DHCP、DHCPv6、RA、DNS 所需的 pf 通道。

## IPv6 RA 與 DHCPv6

在 IPv6 LAN 中，請在 Router Advertisement 中包含 RDNSS 一起發送。
Android 不會透過 DHCPv6 取得 DNS，因此 RDNSS 是必要的。
Windows 用戶端還需要額外提供 DHCPv6 stateless 伺服器。

Router Advertisement 沒有標準的 NTP 伺服器廣告。
若要將路由器本身設為 LAN 的時間參考來源，請使用 DHCPv4 option 42 與 DHCPv6 option 31（SNTP）。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: lan-ra
  spec:
    interface: lan
    prefixFrom:
      resource: IPv6DelegatedAddress/lan-base
      field: address
    mFlag: false
    oFlag: true
    rdnssFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    dnsslFrom:
      - resource: DNSZone/lan
        field: zone
    mtu: 1454

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: lan-dhcpv6
  spec:
    interface: lan
    mode: stateless
    dnsServerFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    sntpServerFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    domainSearchFrom:
      - resource: DNSZone/lan
        field: zone
```

若要透過 DHCPv6 同時分配位址，請使用 `mode: stateful` 或 `mode: both`。
若要讓 LAN 的 DNS suffix 與 `DNSZone` 一致，請使用 `domainFrom`、`dnsslFrom`、`domainSearchFrom`。
DHCPv4 的 domain-name、RA 的 DNSSL、DHCPv6 的 domain-search 均參照相同的本地區域，因此無需重複撰寫網域字串。

## 本地 DNS 區域

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: lan
  spec:
    zone: lan.example.org
    ttl: 300
    records:
      - hostname: router
        ipv4From:
          resource: IPv4StaticAddress/lan-base
          field: address
        ipv6From:
          resource: IPv6DelegatedAddress/lan-base
          field: address
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
        - DHCPv6Server/lan-dhcpv6
      ddns: true
      ttl: 60
```

固定記錄寫在 `records:` 中，DHCP 租約衍生的記錄寫在 `dhcpDerived.sources` 中。
兩者在查詢時會合併。
若 DHCP 衍生的 hostname 為相對名稱，會發布於 DNSZone 本身之下，通常無需撰寫 `hostnameSuffix`。

## DNS 解析器監聽

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: lan-resolver
  spec:
    listen:
      - name: lan
        addressFrom:
          - resource: IPv4StaticAddress/lan-base
            field: address
          - resource: IPv6DelegatedAddress/lan-base
            field: address
        port: 53
        sources: [local-zone, default]
    sources:
      - name: local-zone
        kind: zone
        match:
          - lan.example.org
        zoneRef:
          - DNSZone/lan
      - name: default
        kind: upstream
        match:
          - "."
        upstreams:
          - https://dns.example.net/dns-query
          - udp://1.1.1.1:53
    cache:
      enabled: true
      maxEntries: 10000
```

解析器會在參照資源的 status 中取得的所有位址上監聽。
即使因 PD 更新等原因新增 IPv6 位址，也無需重新啟動即可自動跟進。

## 動作確認

```sh
# 確認 LAN 介面已載入 v4 / v6
routerctl describe Interface/lan

# 即時追蹤 DHCP 事件
routerctl events --topic 'routerd.dhcp.lease.**' --resource DHCPv4Server/lan-dhcpv4

# 以本地解析器進行名稱解析
dig @<lan-ip> router.lan.example.org
dig @<lan-ip> example.com
```

## 操作提示

- 請先從 `routerctl plan` 與 `--dry-run` 開始。在確保管理路徑與已知的回滾路徑後，再啟用生產環境的 LAN 監聽。
- 若手動修改了 dnsmasq 的租約檔案，請重新啟動 `routerd-dhcp-event-relay` 以使記憶體內狀態同步。租約的變更請盡量透過 routerd 進行。
- 請保留公共 DNS 作為備援。`routerd-dns-resolver` 會降低健康檢查失敗的轉送器優先度，但僅在沒有其他健全替代方案時才會如此。

## 相關項目

- [WAN 側服務](./wan-side-services.md)
- [本地 DNS 區域](../how-to/dns-local-zone.md)
- [專用 DNS 上游](../how-to/dns-private-upstream.md)
