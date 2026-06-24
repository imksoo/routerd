---
title: はじめに
---

# はじめに

![インターフェースの確認と小さな YAML 設定から validate、plan、dry-run、serve、routerctl status へ進む安全な最初の routerd ループ](/img/diagrams/tutorial-getting-started.png)

このチュートリアルでは、まず安全な進め方を確認します。

1. 小さなルーターリソースファイルを書きます。
2. 検証します。
3. 計画を確認します。
4. 予行実行します。
5. 安全を確かめてからデーモンを起動します。

最初の確認では、ホストのネットワークを変更しません。
先にリリースアーカイブと `install.sh` で routerd を導入してください。
OS 別の手順は [インストールとアップグレード](../install-and-upgrade.md) を参照してください。

## 1. インターフェース名の確認

```bash
ip link
```

ここでは WAN を `ens18`、LAN を `ens19`、管理用を `ens20` とします。
実機では必ず自分のホストに合わせて読み替えてください。

管理経路は、変更するインターフェースと分けてください。
routerd が引き継ぐ予定のインターフェースだけで最初の検証をすると危険です。

## 2. インターフェースとホスト準備の記述

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: Package
      metadata:
        name: router-host-tools
      spec:
        packages:
          - os: ubuntu
            names: [dnsmasq, nftables, conntrack, iproute2]

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

ルーター機能に必要なホスト側の実行時設定は、宣言したリソースから routerd が導き出します。
`Package`、`Sysctl`、`SysctlProfile` は、まだ自動で導けないパッケージやカーネル設定を補うための、
限定的な逃げ道としてのみ使います。

## 3. 検証

```bash
routerctl validate -f first-router.yaml --replace
```

検証では、routerd がホストに触れる前にリソースの形を確かめます。

## 4. 計画の確認

```bash
routerctl plan -f first-router.yaml --replace
```

計画では、インターフェース名の間違い、依存関係の不足、作成されるホスト成果物を確認します。

## 5. 予行実行

```bash
routerctl plan -f first-router.yaml --replace
```

予行実行では、リソースの読み込み、依存の順序、生成内容を確かめます。
ネットワークの変更は確定しません。

## 6. 計画が安全ならデーモンを起動

```bash
sudo routerd serve --config first-router.yaml
```

本番では、同梱のサービスマネージャー用ファイルを使って routerd を導入してください。
こうすると、起動時に `routerd serve` が自動的に開始されます。

## 7. 状態の確認

```bash
routerctl status
routerctl events --limit 20
routerctl connections --limit 50
```

次のチュートリアルでは、LAN の DHCP、RA、DNS、経路ポリシー、NAT44、DS-Lite を追加します。

## 次に読むもの

- [WAN 側サービス](./wan-side-services.md) — DHCPv6-PD、PPPoE、DS-Lite、DHCPv4 WAN を設定する
- [LAN 側サービス](./lan-side-services.md) — DHCPv4 スコープ、RA、DNS、NTP を追加する
- [基本のファイアウォール](./basic-firewall.md) — 3 ロール構成のファイアウォールゾーンを有効にする
- [routerctl doctor](../operations/routerctl-doctor.md) — 適用後のルーターの健全性を確認する
