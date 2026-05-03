---
title: はじめに
---

# はじめに

このチュートリアルでは、routerd の基本的な流れを確認します。
最初はホストを変更せず、検証、計画、予行実行だけを行います。

## 1. インターフェースを確認します

```bash
ip link
```

例では WAN を `ens18`、LAN を `ens19` とします。
実機では必ず自分のホストに合わせて読み替えてください。

## 2. 最小 YAML を作ります

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

## 3. 検証します

```bash
routerd validate --config first-router.yaml
```

## 4. 計画を見ます

```bash
routerd plan --config first-router.yaml
```

## 5. 予行実行します

```bash
routerd apply --config first-router.yaml --once --dry-run
```

ここまででホストは変更されません。
次のページでは、WAN のアドレスと LAN のアドレスを足します。
