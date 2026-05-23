---
title: DHCPv6-PD 上の DS-Lite (IPv6 のみアクセス網)
slug: /how-to/flets-ipv6-setup
---

# DHCPv6-PD 上の DS-Lite (IPv6 のみアクセス網)

## 想定するシーン

ISP が IPv6 のみのアクセス網を提供し、IPv4 接続は AFTR (Address Family Transition Router) への DS-Lite トンネルで実現する構成です。
このとき、ルーターは次を担います。

- DHCPv6-PD で IPv6 プレフィックスを取得し、LAN へ配ります。
- AFTR への DS-Lite (IPv4-in-IPv6 / `ip6tnl`) トンネルを確立します。
- AFTR の FQDN はアクセス網の DNS でしか解けない場合があるため、条件付き転送を使います。
- IPv6 RA に RDNSS を含めて、SLAAC クライアント (Android を含む) へ DNS を伝えます。

このパターンは、日本国内のフレッツ系回線 (NTT NGN + `gw.transix.jp` など) で典型的ですが、同様の DS-Lite 配備全般に適用できます。

## 前提

- WAN インターフェースが、HGW か ONU を経由して IPv6 のみのアクセス網につながっています。
- そのインターフェースで DHCPv6-PD を利用できます。
- AFTR の DNS が DHCPv6 の information-request で返るかどうかは ISP や HGW 次第です。どちらの場合にも備えてください。

## DHCPv6-PD

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
```

リースは次の場所に保存されます。

```text
/var/lib/routerd/dhcpv6-client/wan-pd/lease.json
```

デーモンの状態は Unix ソケットで確認できます。

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
```

## LAN アドレス導出と RA

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

RA で広告する RDNSS には、委任されたプレフィックスから導出した LAN 側アドレスを使います。
SLAAC クライアントは、このリゾルバを自動で取得します。

## AFTR の条件付き DNS 転送

AFTR の FQDN は、通常 ISP のアクセス網 DNS でしか解けません。
そのドメインだけをアクセス網のリゾルバへ転送し、それ以外は通常の上流に流します。

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

`transix.jp` と上流の IPv6 アドレスは、ISP が公開している値に置き換えてください。

## DS-Lite トンネル

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

`localAddressSource: interface` は、SLAAC/RA で WAN 側に付いた IPv6 アドレスを、トンネルのローカルエンドポイントとして使います。
このアドレスは LAN 側の導出より早く取れるため、トンネルが早く立ち上がります。

ISP が安定した AFTR アドレスを公開していて、DNS 解決を省きたい場合は、`aftrIPv6` を直接指定します。

```yaml
spec:
  aftrIPv6: 2001:db8:cafe::1
```

NTT NGN の HGW のように、DHCPv6 の information-request で AFTR が返らない環境では、`aftrFQDN` または `aftrIPv6` の静的な指定が正しいフォールバックです。

トンネル内側の IPv4 アドレスには、通常 RFC 6333 の B4-AFTR リンク範囲 `192.0.0.0/29` を使います。
LAN 範囲から切り出したアドレスを使いたい場合は、`IPv4StaticAddress` リソースで定義します。
その値を `DSLiteTunnel.localAddressFrom` と `NAT44Rule.snatAddressFrom` から参照します。
任意指定の例は `examples/dslite-lan-range-snat.yaml` にあります。

## 動作確認

```bash
routerd apply --config router.yaml --once --dry-run
routerctl status

ip -6 tunnel show
ip route show default
nft list table ip routerd_nat

# トンネル経由で IPv4 の疎通を確認
curl --interface ds-routerd https://1.1.1.1/
```

まず dry-run で確認し、計画が妥当でロールバック経路もある状態で本適用してください。

## 関連項目

- [WAN 側サービス](../tutorials/wan-side-services.md)
- [マルチ WAN 切替](./multi-wan.md)
- [Path MTU と MSS clamping](../concepts/path-mtu.md)
