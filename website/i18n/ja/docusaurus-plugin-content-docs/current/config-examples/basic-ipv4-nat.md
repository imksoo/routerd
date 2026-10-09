---
title: 基本的な IPv4 NAT ルーター
sidebar_position: 10
---

# 基本的な IPv4 NAT ルーター

![DHCP WAN、LAN アドレス、DHCPv4 サーバー、NAT44 を持つ小さな IPv4 ルーター](/img/diagrams/config-example-basic-ipv4-nat.png)

これは、テスト用 LAN の端末がルーターから DHCP（アドレスを自動で配る仕組み）で
private IPv4 アドレスを受け取り、ルーターが WAN 側の IPv4 へ NAT（アドレスの書き換え）して
外へ出す例です。最初は **隔離した Ubuntu Server VM** と VM コンソールで試してください。

完全な YAML は `examples/example-basic-ipv4-nat.yaml` にあります。

:::caution 本番用の安全設計ではありません

この例にはファイアウォール用リソースも含まれますが、routerd の
ファイアウォール機能は現在も土台を整えている段階です。
インターネットに公開するルーターや、家・学校・職場の唯一の防御としては
使わないでください。

:::

## 構成

```text
テスト用上流ネットワーク
          |
        ens18 (WAN, DHCPv4)
          |
      [ routerd VM ]
          |
        ens19 (LAN, 192.168.10.1/24)
          |
     テスト用クライアント
```

| 場所 | 役割 | 主なリソース |
| --- | --- | --- |
| WAN | 上流から IPv4 を DHCP で受ける | `Interface/wan`、`DHCPv4Client/wan-dhcpv4` |
| LAN | ルーター自身の IPv4 と DHCP の範囲 | `Interface/lan`、`IPv4StaticAddress/lan-base`、`DHCPv4Server/lan-dhcpv4` |
| NAT | LAN の IPv4 が WAN に出るときだけ書き換える | `NAT44Rule/lan-to-wan` |

VM の NIC 名、LAN の番号、DHCP の範囲は例です。管理用 NIC と重ならない値に
変えてください。

## 要点となる設定

WAN は、最初は外部のネットワーク設定が所有します。LAN は routerd が管理します。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: wan
  spec:
    ifname: ens18
    adminUp: true
    managed: false
    owner: external

- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: lan
  spec:
    ifname: ens19
    adminUp: true
    managed: true
    owner: routerd

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: lan-base
  spec:
    interface: lan
    address: 192.168.10.1/24
```

LAN の DHCP サーバーは、LAN のアドレスをゲートウェイとしてクライアントへ配ります。

```yaml
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
    # この学習用の例では、routerd 自身ではなく外部 DNS を配る。
    dnsServers:
      - 1.1.1.1
      - 1.0.0.1
```

### NAT44Rule の形

NAT の項目名は `type`、`egressInterface`、`sourceRanges` です。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: lan-to-wan
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.168.10.0/24
    excludeDestinationCIDRs:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
```

`sourceRanges` は NAT してよい送信元だけを示します。
`excludeDestinationCIDRs` は、ほかのプライベートネットワークへの通信を
うっかり NAT しないための例です。実際の上流や VPN と重なる範囲は、
自分の構成に合わせて見直します。

:::note DNS は何を配るか確認する

この例の DHCP は、外部 DNS (`1.1.1.1` と `1.0.0.1`) をクライアントへ配ります。
利用を許可された DNS を自分のネットワークに合わせて選んでください。routerd 自身を
DNS サーバーとして配りたいときは、先に `DNSResolver` などの DNS サービスを追加します。

:::

## この設定例を実行する

[最初のルーター](../tutorials/first-router.md)に、導入版に合うファイルの取得、
インターフェース名の変更、検証、隔離 dry-run、反映、サービス起動、端末の確認を
まとめています。リリースアーカイブにはリポジトリの `examples/` ディレクトリは
含まれません。上の抜粋だけでなく、取得した完全なファイルを使ってください。

## 関連項目

- [NAT44 とファイアウォールの準備](../tutorials/basic-firewall.md)
- [LAN 側サービス](../tutorials/lan-side-services.md)
- [適用と生成](../concepts/apply-and-render.md)
