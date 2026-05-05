---
title: はじめに
---

# はじめに

このチュートリアルでは、最初に安全な流れを確認します。

1. 小さなルーターリソースファイルを書きます。
2. 検証します。
3. 計画を確認します。
4. 予行実行します。
5. 安全を確認してからデーモンを起動します。

最初の確認では、ホストのネットワークを変更しません。

## 1. インターフェース名を確認します

```bash
ip link
```

例では WAN を `ens18`、LAN を `ens19`、管理用を `ens20` とします。
実機では必ず自分のホストに合わせて読み替えてください。

管理経路は、変更対象のインターフェースと分けます。
routerd が引き継ぐ予定のインターフェースだけを使って、最初の検証をしないでください。

## 2. インターフェースとホスト準備から始めます

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

    - apiVersion: system.routerd.net/v1alpha1
      kind: SysctlProfile
      metadata:
        name: router-linux
      spec:
        profile: router-linux

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

`Package` と `SysctlProfile` により、ホスト準備もルーターの意図に含めます。
ルーター機能は OS のツールや転送設定に依存します。
そのため、プロトコルリソースより前に明示しておくと安全です。

## 3. 検証します

```bash
routerd validate --config first-router.yaml
```

検証では、routerd がホストに触れる前にリソースの形を確認します。

## 4. 計画を確認します

```bash
routerd plan --config first-router.yaml
```

計画では、インターフェース名の間違い、依存関係の不足、作成されるホスト成果物を確認します。

## 5. 予行実行します

```bash
routerd apply --config first-router.yaml --once --dry-run
```

予行実行では、リソース読み込み、依存順序、生成内容を確認します。
ネットワーク変更は確定しません。

## 6. 計画が安全ならデーモンを起動します

```bash
sudo routerd serve --config first-router.yaml
```

本番では、`SystemdUnit` リソースまたは systemd ユニットファイルを使います。
これにより、`routerd serve` を起動時に開始できます。

## 7. 状態を確認します

```bash
routerctl status
routerctl events --limit 20
routerctl connections --limit 50
```

次のチュートリアルでは、LAN DHCP、RA、DNS、経路ポリシー、NAT44、DS-Lite を足します。
