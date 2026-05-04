---
title: NTT フレッツ IPv6 設定
slug: /how-to/flets-ipv6-setup
---

# NTT フレッツ IPv6 設定

このページでは、NTT NGN の HGW 配下で DHCPv6-PD と DS-Lite を使う構成を説明します。
現在の routerd では、DHCPv6-PD は `routerd-dhcpv6-client` が担当します。

## 前提

- WAN インターフェースは HGW 側につながっています。
- DHCPv6-PD で委譲プレフィックスを受け取ります。
- AFTR は DHCPv6 情報要求では返らない環境があります。
- `gw.transix.jp` などの AFTR FQDN は、HGW が広告する DNS でだけ解決できる場合があります。

## DHCPv6-PD

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
```

リースは次のように保存されます。

```text
/var/lib/routerd/dhcpv6-client/wan-pd/lease.json
```

状態はデーモンの API で確認します。

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
```

## LAN への展開

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
```

RA では RDNSS も設定します。

```yaml
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

実際の DNS アドレスは、委譲プレフィックスから導出した LAN アドレスに合わせます。

## AFTR の条件付き DNS 転送

HGW 経由でだけ解決できる AFTR FQDN には、条件付き転送を使います。

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
      - name: transix-aftr
        kind: forward
        match: [transix.jp]
        upstreams:
          - udp://[2404:8e00::feed:101]:53
```

## DS-Lite

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

`localAddressSource: interface` は、WAN 側の RA/SLAAC で構成された
グローバル IPv6 アドレスを DS-Lite の local endpoint に使います。
PD から LAN アドレスを導出するより早く構成できます。

`aftrIPv6` を直接指定した場合は DNS 解決を省略します。
この環境では、DHCPv6 情報要求から AFTR が空であることは正常です。

## 確認

```bash
routerd apply --config router.yaml --once --dry-run
routerctl status
ip -6 tunnel show
ip route show default
nft list table ip routerd_nat
```

予行実行で問題がないことを確認してから実適用します。
