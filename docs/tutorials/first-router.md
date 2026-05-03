---
title: 最初のルーター
sidebar_position: 2
---

# 最初のルーター

このページでは、WAN と LAN を持つ最小のルーターを作ります。
WAN は DHCPv4、LAN は静的 IPv4 アドレスから始めます。

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
      kind: DHCPv4Lease
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

`DHCPv4Lease` は `routerd-dhcpv4-client` が管理するリースです。
従来の OS DHCP クライアントを直接選びません。
routerd のデーモン契約に合わせて状態を公開します。

実機へ向ける前に次を実行します。

```bash
routerd validate --config first-router.yaml
routerd plan --config first-router.yaml
routerd apply --config first-router.yaml --once --dry-run
```

管理用接続が消えないことを確認してから、予行実行なしで適用します。
