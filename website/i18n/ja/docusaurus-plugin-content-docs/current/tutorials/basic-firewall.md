---
title: 基本の NAT と firewall ポリシー
sidebar_position: 6
---

# 基本の NAT と firewall ポリシー

routerd は Linux ルーター上で IPv4 NAPT (NAT44) と stateful firewall を適用します。
このチュートリアルでは、初期構成のホストに両方を入れる最小手順を示します。

## 想定するシーン

ルーターには次の構成があります：

- IPv4 が乗っている上流 interface (`wan`) — 形式は native dual-stack / PPPoE / DS-Lite いずれでも可。
- LAN 内クライアントに private アドレスを配る LAN interface (`lan`)。
- 任意で管理 interface (`mgmt`)。

目的は：

- LAN からの外向き IPv4 を masquerade する。
- 健全な firewall ポスチャを既定で適用する (WAN は LAN に届かない、LAN は WAN に届く、management はルーター自身に届く)。

## NAT44

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: lan-to-wan
  spec:
    outboundInterface: wan
    sourceCIDRs:
      - 192.0.2.0/24
    masquerade: true
```

routerd は `routerd_nat` nftables テーブルに rule を生成します。
DHCP 取得回線、PPPoE 仮想 interface、DS-Lite tunnel のいずれでも形は同じで、`outboundInterface` だけ変えます。

## conntrack 観測

routerd は conntrack を読み、Web Console と `routerctl connections` でライブフローを表示します。
`/proc/net/nf_conntrack` がない環境では sysctl 由来のサマリに縮退します。失敗にはせず、観測できる範囲だけを出します。

## Firewall Kind

`FirewallZone`、`FirewallPolicy`、`FirewallRule` が stateful filter を表現します。
routerd は `inet routerd_filter` nftables テーブルに生成します。

役割 (`untrust`、`trust`、`mgmt`) が暗黙の accept / drop マトリクスを提供します。
DHCP / DNS / DS-Lite 制御のような管理対象サービスに必要な穴は routerd が自動で開けます。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata: {name: wan}
  spec:
    role: untrust
    interfaces:
      - Interface/wan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata: {name: lan}
  spec:
    role: trust
    interfaces:
      - Interface/lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata: {name: default}
  spec: {}
```

例外を入れたいときは [firewall ルール guide](../how-to/firewall-rule.md) を参照。

## 確認

```sh
routerctl describe NAT44Rule/lan-to-wan
routerctl firewall test from=wan to=lan proto=tcp dport=22
nft list table inet routerd_filter
nft list table ip routerd_nat
```

## 関連項目

- [Firewall ゾーンを定義する](../how-to/firewall-zone.md)
- [Firewall 例外を追加する](../how-to/firewall-rule.md)
- [Firewall コンセプト](../concepts/firewall.md)
