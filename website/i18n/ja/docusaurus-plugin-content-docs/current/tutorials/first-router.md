---
title: 最初のルーターを上げる
sidebar_position: 2
---

# 最初のルーターを立ち上げる

![DHCPv4 WAN、static LAN address、最小 Interface resource、validate-plan-dry-run apply loop を示す first router tutorial](/img/diagrams/tutorial-first-router.png)

このチュートリアルでは、最小の routerd 構成を立ち上げます。構成は「DHCPv4 で IPv4 を取得する WAN 1 本」と「固定 IPv4 アドレスの LAN 1 本」です。

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
routerd は OS 付属のクライアントには任せません。このデーモンは、他の routerd デーモンと同じ取り決め（`/v1/status`、`lease.json`、`events.jsonl`）で状態を公開します。

本番に適用する前に、validate と plan で確認してください。

```bash
routerd validate --config first-router.yaml
routerd plan     --config first-router.yaml
routerd apply    --config first-router.yaml --once --dry-run
```

管理経路（LAN 経由の SSH、コンソール、ハイパーバイザーのコンソール）が変更後も残ることを確認してから、`--dry-run` を付けずに適用します。

## 次に読むもの

- [WAN 側サービス](./wan-side-services.md) — DHCPv6-PD、PPPoE、DS-Lite
- [LAN 側サービス](./lan-side-services.md) — DHCP、RA、DNS、ローカルゾーン
- [基本の NAT とファイアウォールポリシー](./basic-firewall.md)
