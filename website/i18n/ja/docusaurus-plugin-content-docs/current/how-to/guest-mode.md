---
title: MAC アドレスでゲスト端末を隔離する
---

# MAC アドレスでゲスト端末を隔離する

## 想定するシーン

1 つの LAN セグメントに、信頼済み端末とゲスト端末が混在しています。
ゲスト端末には DHCP リースを渡し、ルーターの DNS や NTP は使わせます。
ただし、管理網、実験網、自宅サーバーなどのプライベートネットワークには届かないようにします。

## 固定リースを定義する

端末のアドレスと名前を安定させたい場合は `DHCPv4Reservation` を使います。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: aiseg2
  spec:
    server: lan-v4
    macAddress: "18:ec:e7:33:12:6c"
    hostname: aiseg2
    ipAddress: 172.18.0.150
```

## include mode

include mode では、一覧に書いた MAC アドレスだけを guest として扱います。
新しい端末は分類するまで trusted 側に残るため、家庭向けの安全な既定値です。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    interfaces:
      - Interface/lan
    guestServices:
      - dns
      - dhcp
      - ntp
    classification:
      - macAddress: "18:ec:e7:33:12:6c"
        as: guest
        name: aiseg2
        ipv4Reservation: aiseg2
```

## exclude mode

exclude mode では、対象インターフェース上の端末を既定で guest として扱います。
一覧に書いた MAC アドレスだけを trusted として扱います。
BYOD や集合住宅向けの構成で使いやすい方式です。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: byod-default-guest
  spec:
    mode: exclude
    interfaces:
      - Interface/lan
    classification:
      - macAddress: "bc:24:11:e0:8e:3a"
        as: trusted
        name: admin-laptop
```

## プライベートネットワーク拒否リスト

`guestEgressDeny` を省略すると、routerd はゲスト端末から次の宛先への転送を拒否します。

- `10.0.0.0/8`
- `172.16.0.0/12`
- `192.168.0.0/16`
- `fc00::/7`

例外を作る場合は `guestEgressAllow` を指定します。
許可規則は拒否規則より先に生成されます。

## 対応プラットフォーム

`ClientPolicy` は Linux nftables が必要です。
FreeBSD pf は routed filter path で同じ MAC ベース分類を持たないため、routerd は FreeBSD でこのリソースを未対応として報告します。

完全な例は [examples/guest-mode.yaml](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml) を参照してください。
