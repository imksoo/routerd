---
title: 最初のルーター
sidebar_position: 2
---

# 最初のルーター

このチュートリアルでは、最小の役立つルーター YAML を組み立てます。WAN を 1 本
(IPv4 を DHCP で取得)、LAN を 1 本 (静的 IPv4 アドレス) です。これでホストは
上流に話しかけられて、LAN の決まったアドレスでアクセスできるようになります。
LAN 側サービス (DHCP / DNS / RA) とファイアウォールは続きのチュートリアルで足します。

このページは [インストール](./install) を済ませ、`/usr/local/sbin/` に routerd
バイナリがある前提です。

## 1. インターフェースを確認する

ホストの物理構成から始めます。

```bash
ip link
```

小さなルーター VM では次のようになります。

- WAN: `ens18`
- LAN: `ens19`

このようなカーネル名を使うのは、`Interface` リソースの `spec.ifname` のところだけです。
それ以外では、リソースの `metadata.name` (`wan`、`lan` など) で参照します。
これにより、NIC を差し替えても他のリソースを書き換えずに済みます。

## 2. インターフェースを宣言する

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
```

`Interface` を 2 つ。`managed: true` で routerd がインターフェース設定の所有権を
持ちます。`adminUp: true` で routerd がリンクを上げます。

## 3. WAN に IPv4 アドレスをもらう

`DHCPv4Address` を追加します。

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Address
      metadata:
        name: wan-dhcpv4
      spec:
        interface: wan
        client: dhclient
        required: true
```

`spec.interface: wan` が `Interface` リソースを名前で参照しています。
`required: true` でリースが取れていないときに routerd が警告を出します。

## 4. LAN に静的 IPv4 アドレスを設定する

`IPv4StaticAddress` を追加します。

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-ipv4
      spec:
        interface: lan
        address: 192.168.10.1/24
        exclusive: true
```

`exclusive: true` で、このインターフェース上の routerd が知らないアドレスを
取り外します。他のツールが追加で割り当てる前提なら外してください。

## 5. 検証とドライラン

ファイルを保存して validate します。

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
```

通ったら計画を見ます。

```bash
sudo routerd apply \
  --config /usr/local/etc/routerd/router.yaml \
  --once --dry-run
```

Interface 2 件を管理し、DHCP リースを 1 件取得し、静的アドレスを 1 件入れる、
という計画になっているはずです。NAT もファイアウォールもまだ無いのは意図的です。

## 6. apply

```bash
sudo routerd apply \
  --config /usr/local/etc/routerd/router.yaml \
  --once
```

確認:

```bash
routerctl get
routerctl describe interface/wan
ip addr show ens18
ip addr show ens19
```

`ens18` に DHCP で割り当てられたアドレス、`ens19` に `192.168.10.1/24` が見えるはずです。

## まだやっていないこと

ホストは上流と通信でき、LAN から 192.168.10.1 で到達できるようになりましたが、
LAN クライアント向けのルーターとしてはまだ機能していません。次のものがありません。

- LAN 側 DHCP サーバー
- LAN の DNS サービス
- LAN クライアントが WAN に出る IPv4 NAT
- ファイアウォールルール

それぞれ別リソースです。続く 2 つのチュートリアルで段階的に足します。

- [LAN 側サービス](./lan-side-services) — LAN の DHCP/DNS のために dnsmasq を入れる
- [基本のファイアウォール](./basic-firewall) — NAT と既定拒否のホームルーター設定
