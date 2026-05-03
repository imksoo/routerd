---
title: 基本の NAT とファイアウォール方針
sidebar_position: 4
---

# 基本の NAT とファイアウォール方針

現在の routerd では、IPv4 の外向き NAPT は実適用できます。
一方で、状態を持つフィルタとしてのファイアウォールはまだ棚上げです。
このページでは、実装済みの NAT44 と、ファイアウォール Kind の現在位置を説明します。

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

`Zone`、`FirewallPolicy`、`ExposeService` は API の土台として存在します。
ただし、状態を持つフィルタの本番向け実適用はまだ完了していません。
利用者向けの本番防御は、現時点ではホスト側の既存方針と併用してください。
