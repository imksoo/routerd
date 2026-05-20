---
title: 最初のルーターを上げる
sidebar_position: 2
---

# 最初のルーターを上げる

このチュートリアルでは、最小の routerd 構成 — 「DHCPv4 で IPv4 を取る WAN 1 本、静的 IPv4 アドレスの LAN 1 本」を上げます。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata:
        name: wan
      spec:
        interface: wan

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-address
      spec:
        interface: lan
        address: 192.0.2.1/24
```

`DHCPv4Client` は `routerd-dhcpv4-client` が所有します。
routerd は OS 付属クライアントへ委譲しません。daemon は他の routerd daemon と同じ contract (`/v1/status`、`lease.json`、`events.jsonl`) で状態を公開します。

本番 apply の前に、validate と plan で確認してください：

```bash
routerd validate --config first-router.yaml
routerd plan     --config first-router.yaml
routerd apply    --config first-router.yaml --once --dry-run
```

管理経路 (LAN SSH、コンソール、ハイパーバイザーコンソール) が変更を生き残ることを確認してから、`--dry-run` なしで apply します。

## 次に

- [WAN 側サービス](./wan-side-services.md) — DHCPv6-PD、PPPoE、DS-Lite
- [LAN 側サービス](./lan-side-services.md) — DHCP、RA、DNS、ローカルゾーン
- [基本の NAT と firewall ポリシー](./basic-firewall.md)
