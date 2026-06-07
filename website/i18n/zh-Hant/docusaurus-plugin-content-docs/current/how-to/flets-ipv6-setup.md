---
title: DHCPv6-PD 上的 DS-Lite（僅 IPv6 接入網路）
slug: /how-to/flets-ipv6-setup
---

# DHCPv6-PD 上的 DS-Lite（僅 IPv6 接入網路）

![IPv6-only access 中 DHCPv6-PD、delegated LAN IPv6、AFTR DNS forwarding、DS-Lite tunnel egress 與安全 RA 的構成流程](/img/diagrams/how-to-flets-ipv6-setup.png)

## 適用情境

ISP 提供僅 IPv6 的接入網路，IPv4 連線透過 AFTR（Address Family Transition Router）的 DS-Lite 隧道實現。在這種配置中，路由器負責以下工作：

- 透過 DHCPv6-PD 取得 IPv6 前綴，並分配給 LAN。
- 建立通往 AFTR 的 DS-Lite（IPv4-in-IPv6 / `ip6tnl`）隧道。
- AFTR 的 FQDN 有時只有接入網路的 DNS 才能解析，因此使用條件式轉送。
- 在 IPv6 RA 中加入 RDNSS，讓 SLAAC 用戶端（包含 Android）自動取得 DNS 設定。

此模式在日本 FLET'S 系列線路（NTT NGN + `gw.transix.jp` 等）中最為典型，但同樣適用於類似的 DS-Lite 部署。

## 前提條件

- WAN 介面已透過 HGW 或 ONU 連接至僅 IPv6 的接入網路。
- 該介面可使用 DHCPv6-PD。
- AFTR 的 DNS 是否會透過 DHCPv6 information-request 回傳，因 ISP 或 HGW 而異，請針對兩種情況做好準備。

## DHCPv6-PD

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
```

租約儲存於：

```text
/var/lib/routerd/dhcpv6-client/wan-pd/lease.json
```

可透過 Unix socket 確認常駐程式狀態：

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
```

## LAN 位址推導與 RA

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: lan-from-pd
  spec:
    interface: lan
    prefixDelegation: wan-pd
    dependsOn:
      - resource: DHCPv6PrefixDelegation/wan-pd
        phase: Bound
    addressSuffix: "::1"

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: lan-ra
  spec:
    interface: lan
    prefixFrom:
      resource: IPv6DelegatedAddress/lan-from-pd
      field: address
    oFlag: true
    rdnssFrom:
      - resource: IPv6DelegatedAddress/lan-from-pd
        field: address
```

RA 廣播的 RDNSS 使用從委派前綴推導出的 LAN 側位址。
SLAAC 用戶端會自動取得此解析器位址。

## AFTR 的條件式 DNS 轉送

AFTR 的 FQDN 通常只有 ISP 接入網路的 DNS 才能解析。
只將該網域轉送至接入網路的解析器，其餘流量交由一般上游處理。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: resolver
  spec:
    listen:
      - name: local
        addresses: [127.0.0.1]
        port: 53
    sources:
      - name: aftr
        kind: forward
        match: [transix.jp]
        upstreams:
          - udp://[2404:8e00::feed:101]:53
      - name: default
        kind: upstream
        match: ["."]
        upstreams:
          - udp://1.1.1.1:53
```

請將 `transix.jp` 及上游 IPv6 位址替換為 ISP 公告的實際值。

## DS-Lite 隧道

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: ds-lite
  spec:
    interface: wan
    tunnelName: ds-routerd
    localAddressSource: interface
    aftrFQDN: gw.transix.jp
    dependsOn:
      - resource: DNSResolver/resolver
        phase: Applied
```

`localAddressSource: interface` 使用 WAN 側透過 SLAAC/RA 取得的 IPv6 位址作為隧道的本地端點。
此位址比 LAN 側推導更早取得，因此隧道可更快建立。

若 ISP 公告了穩定的 AFTR 位址且希望省略 DNS 解析，可直接指定 `aftrIPv6`：

```yaml
spec:
  aftrIPv6: 2001:db8:cafe::1
```

在 NTT NGN 的 HGW 等不透過 DHCPv6 information-request 回傳 AFTR 的環境中，靜態指定 `aftrFQDN` 或 `aftrIPv6` 是正確的備援方式。

隧道內側的 IPv4 位址通常使用 RFC 6333 的 B4-AFTR 連結範圍 `192.0.0.0/29`。
若要使用從 LAN 範圍切出的位址，請以 `IPv4StaticAddress` 資源定義，
並從 `DSLiteTunnel.localAddressFrom` 與 `NAT44Rule.snatAddressFrom` 參照該值。
自訂範例請參閱 `examples/dslite-lan-range-snat.yaml`。

## 驗證

```bash
routerctl apply --config router.yaml --dry-run
routerctl status

ip -6 tunnel show
ip route show default
nft list table ip routerd_nat

# 確認可透過隧道取得 IPv4 連線
curl --interface ds-routerd https://1.1.1.1/
```

請先以 dry-run 確認計畫無誤、且已備妥回滾路徑後，再正式套用。

## 相關項目

- [WAN 側服務](../tutorials/wan-side-services.md)
- [多 WAN 切換](./multi-wan.md)
- [Path MTU 與 MSS clamping](../concepts/path-mtu.md)
