---
title: 基本の NAT とファイアウォール方針
sidebar_position: 4
---

# 基本の NAT とファイアウォール方針

routerd では、IPv4 の外向き NAPT と状態を持つファイアウォールを実適用できます。
このページでは、NAT44 とファイアウォール Kind の基本を説明します。

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

routerd は nftables の `routerd_nat` テーブルを生成します。
Phase 1.5e では router05 で DS-Lite トンネルを実適用しました。
IPv4 既定経路、NAT44、IPv4 HTTP 通信も確認しています。

## conntrack 観測

routerd は conntrack の集計を観測できます。
環境によって `/proc/net/nf_conntrack` が存在しない場合は、sysctl 由来の集計へ縮退します。
この場合も routerd は失敗として止まらず、観測できる範囲だけを状態に出します。

## ファイアウォール Kind

`FirewallZone`、`FirewallPolicy`、`FirewallRule` で状態を持つフィルタを表します。
routerd は nftables の `inet routerd_filter` テーブルを生成します。
`untrust`、`trust`、`mgmt` の役割から既定の許可と拒否を決めます。
DHCP、DNS、DS-Lite などに必要な開口は routerd が内部で生成します。
