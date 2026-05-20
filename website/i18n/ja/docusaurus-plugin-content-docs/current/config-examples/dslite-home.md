---
title: DS-Lite ホームルーター
sidebar_position: 30
---

# DS-Lite ホームルーター

IPv6 を主回線として使う access line の例です。router は Router Advertisement と
DHCPv6-PD で IPv6 を受け取り、LAN prefix を派生させ、IPv4 traffic は DS-Lite
tunnel に通します。

完全な検証済み YAML は `examples/example-dslite-home.yaml` にあります。

## 構成図

```mermaid
flowchart LR
  internet((Internet))
  aftr["[1] AFTR<br/>gw.transix.jp"]
  wan["[2] wan<br/>IPv6 RA + DHCPv6-PD"]
  router["[3] routerd host"]
  dslite["[4] DS-Lite tunnel<br/>ds-transix"]
  lan["[5] lan<br/>IPv4 + delegated IPv6"]
  clients["[6] LAN clients"]

  internet --- aftr --- dslite --- router
  wan --- router --- lan --- clients
```

## 図の対応表

| 番号 | 意味 | 主な resource |
| --- | --- | --- |
| [1] | DS-Lite tunnel の接続先になる ISP 側 AFTR。 | `DSLiteTunnel/transix` |
| [2] | IPv6 RA と DHCPv6-PD を受ける WAN interface。 | `IPv6RAAddress/wan-ra`, `DHCPv6PrefixDelegation/wan-pd` |
| [3] | tunnel と LAN service を作り、必要な sysctl を導出する routerd host。 | Derived host runtime |
| [4] | IPv4 egress に使う DS-Lite `ip6tnl` device。 | `DSLiteTunnel/transix`, `NAT44Rule/lan-to-dslite` |
| [5] | IPv4 address と delegated IPv6 address を持つ LAN interface。 | `IPv4StaticAddress/lan-ipv4`, `IPv6DelegatedAddress/lan-ipv6` |
| [6] | DHCPv4、RA、RDNSS、DNSSL を受ける LAN client。 | `DHCPv4Server/lan-dhcpv4`, `IPv6RouterAdvertisement/lan-ra` |

## この例で管理するもの

| 領域 | routerd resource |
| --- | --- |
| WAN IPv6 | `IPv6RAAddress/wan-ra` |
| Prefix delegation | `DHCPv6PrefixDelegation/wan-pd`, `IPv6DelegatedAddress/lan-ipv6` |
| DS-Lite | `DSLiteTunnel/transix` |
| LAN IPv4 と DHCPv4 | `IPv4StaticAddress/lan-ipv4`, `DHCPv4Server/lan-dhcpv4` |
| LAN IPv6 advertisement | `IPv6RouterAdvertisement/lan-ra` |
| DNS | `DNSZone/home`, `DNSResolver/lan-resolver` |
| IPv4 egress | `NAT44Rule/lan-to-dslite` |
| MTU/MSS | `DSLiteTunnel/transix` と firewall zone から自動導出 |

この例では Transix に近い AFTR 値を placeholder として使っています。実回線に合わせて、
AFTR FQDN、DNS server、DHCPv6 client profile を置き換えてください。

## 設定の要点

```yaml
# [2] WAN から IPv6 prefix delegation を取得する。
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
    client: dhcp6c
    profile: ntt-hgw-lan-pd

# [5] delegated prefix から LAN IPv6 address を派生させる。
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: lan-ipv6
  spec:
    prefixDelegation: wan-pd
    interface: lan
    subnetID: "0"
    addressSuffix: "::1"

# [1] + [4] ISP AFTR に向けた DS-Lite tunnel を作る。
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: transix
  spec:
    interface: wan
    tunnelName: ds-transix
    aftrFQDN: gw.transix.jp
    aftrDNSServers:
      - 2404:1a8:7f01:a::3
      - 2404:1a8:7f01:b::3
    localAddressSource: delegatedAddress
    localDelegatedAddress: lan-ipv6
    localAddressSuffix: "::100"
    defaultRoute: true
    mtu: 1454
```

この DS-Lite tunnel は delegated IPv6 address を local endpoint として使います。
回線側が WAN RA address を endpoint として期待する場合は、`localAddressSource` を
`interface` に変えてください。

## LAN 側サービス

この例では delegated prefix を RA で広告し、client には router を DNS として配ります。

```yaml
# [6] delegated LAN prefix と local DNS 情報を RA で広告する。
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: lan-ra
  spec:
    interface: lan
    prefixFrom:
      resource: IPv6DelegatedAddress/lan-ipv6
      field: address
    rdnssFrom:
      - resource: IPv6DelegatedAddress/lan-ipv6
        field: address
    dnsslFrom:
      - resource: DNSZone/home
        field: zone
    oFlag: true
    mtu: 1454
```

`DNSResolver` には AFTR 名向けの conditional forwarder を入れています。AFTR record が
access network 側の resolver でだけ意味を持つ構成では、この指定が重要です。

## 適用手順

```bash
cp examples/example-dslite-home.yaml router.yaml
routerd validate --config router.yaml
routerd plan --config router.yaml
routerd apply --config router.yaml --once --dry-run
```

plan では次を確認します。

- WAN / LAN interface 名が正しい。
- 管理 access を誤って消さない。
- AFTR FQDN と resolver address が意図した値になっている。
- NAT の出口が物理 WAN ではなく DS-Lite tunnel になっている。

問題なければ実適用します。

```bash
routerd apply --config router.yaml --once
```

## 確認

```bash
routerctl status
routerctl describe DHCPv6PrefixDelegation/wan-pd
routerctl describe IPv6DelegatedAddress/lan-ipv6
routerctl describe DSLiteTunnel/transix
routerctl describe NAT44Rule/lan-to-dslite
ip -6 tunnel show
ip route show default
```

LAN client 側では次を確認します。

```bash
ip -6 addr
ip route
curl https://1.1.1.1/
dig router.home.example
```

## よく変える場所

- platform に合わせて `client` と `profile` を変更する。
- Transix 以外では `gw.transix.jp` と AFTR resolver address を置き換える。
- DS-Lite tunnel を WAN RA address から張る必要がある場合は `localAddressSource: interface` を使う。
- DS-Lite では MSS clamp が必要になりやすい。routerd は tunnel MTU と LAN/WAN firewall zone から自動導出する。
