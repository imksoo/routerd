---
title: DS-Lite ホームルーター
sidebar_position: 30
---

# DS-Lite ホームルーター

![IPv6 WAN、DS-Lite トンネル、LAN IPv4 と委任 IPv6 サービスの構成](/img/diagrams/config-example-dslite-home.png)

DS-Lite は、IPv6 が中心の回線で IPv4 パケットを ISP のトンネル経由で外へ出す方式です。
これは最初のルーター用ではなく、ISP ごとの情報が必要な上級例です。WAN、トンネル、DNS を
間違えると接続が切れるため、コンソールまたは独立した管理経路があるラボで試します。

完全な検証済み YAML は
[`examples/example-dslite-home.yaml`](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
にあります。中の Transix に近い AFTR 値は例なので、自分の回線の値に置き換えます。

## この例が作るもの

| 役割 | YAML にある実際のリソース名 |
| --- | --- |
| WAN から IPv6 の委任プレフィックスを受ける | `DHCPv6PrefixDelegation/wan-pd` |
| LAN 用 IPv6 アドレスを導出する | `IPv6DelegatedAddress/lan-v6` |
| LAN で DNS に答える | `DNSResolver/lan`、`DNSZone/home` |
| IPv4 を IPv6 に載せるトンネルを作る | `DSLiteTunnel/transix` |
| LAN へ IPv4、DNS、IPv6 ルーター情報を配る | `DHCPv4Server/lan`、`IPv6RouterAdvertisement/lan` |

`lan-v4`、`lan-v6`、`lan`、`transix` はこのファイルが選んだ名前です。どの routerd
設定でも必ず同じ名前になるわけではありません。

## 設定のつながり

WAN のプレフィックス委任と、そこから作る LAN IPv6 アドレスは名前で結びます。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
    profile: ntt-hgw-lan-pd

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: lan-v6
  spec:
    prefixDelegation: wan-pd
    interface: lan
    subnetID: "0"
    addressSuffix: "::1"
    announce: true
```

DS-Lite トンネルは、その `lan-v6` をローカル側のアドレスとして使います。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: transix
  spec:
    interface: wan
    tunnelName: ds-transix
    aftrFQDN: gw.transix.jp
    aftrDNSServers: [2404:1a8:7f01:a::3, 2404:1a8:7f01:b::3]
    localAddressSource: delegatedAddress
    localDelegatedAddress: lan-v6
    localAddressSuffix: "::100"
    defaultRoute: true
```

回線が WAN の Router Advertisement アドレスをトンネル元に要求する場合は、ここを
そのままコピーせず、回線事業者の指定に従った `localAddressSource` を使います。

DNS、DHCPv4、RA も同じローカル名を使います。

```yaml
- kind: DNSResolver
  metadata:
    name: lan
  # IPv4StaticAddress/lan-v4 と IPv6DelegatedAddress/lan-v6 で待ち受ける。

- kind: DHCPv4Server
  metadata:
    name: lan
  # IPv4StaticAddress/lan-v4 を gateway と DNS として配る。

- kind: IPv6RouterAdvertisement
  metadata:
    name: lan
  # IPv6DelegatedAddress/lan-v6 と DNSZone/home を広告する。
```

短い抜粋は名前のつながりを示すものです。必要な `spec` は完全な YAML を読みます。

## daemon の前に確認する

ファイルをコピーし、ISP 固有の値をすべて置き換えてから単体確認を行います。
サービスの起動やネットワーク変更は行いません。

```sh
cp examples/example-dslite-home.yaml router.yaml
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config router.yaml
sudo routerd apply --config router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
```

WAN/LAN の名前、AFTR FQDN、リゾルバーのアドレス、管理経路が自分のものか確認します。
違うものが一つでもあれば先へ進みません。

## 適用後に見るもの

コンソールまたは独立した管理経路からだけ live 操作を行い、サービス起動後に確認します。

```sh
sudo routerctl get status
sudo routerctl describe DHCPv6PrefixDelegation/wan-pd
sudo routerctl describe IPv6DelegatedAddress/lan-v6
sudo routerctl describe DSLiteTunnel/transix
sudo routerctl describe FirewallZone/wan
sudo ip -6 tunnel show
sudo ip route show default
```

LAN クライアントでは、IPv6、経路、ローカル DNS を確認します。

```sh
sudo ip -6 addr
sudo ip route
curl https://1.1.1.1/
dig router.home.example
```

## 関連項目

- [WAN 側サービス](../tutorials/wan-side-services.md)
- [LAN 側サービス](../tutorials/lan-side-services.md)
- [基本的な IPv4 NAT ルーター](./basic-ipv4-nat.md)
