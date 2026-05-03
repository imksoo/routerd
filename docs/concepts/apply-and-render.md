---
title: 適用と生成
slug: /concepts/apply-and-render
sidebar_position: 4
---

# 適用と生成

routerd には、日常運用でよく使う操作がいくつかあります。
このページでは、文書内で使う言葉をそろえて説明します。

## 検証する

`routerd validate` は YAML の形を確認します。
Kind 名、必須フィールド、値の範囲、明らかな依存関係の誤りを見つけます。

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
```

## 計画を見る

`routerd plan` は、ホストに何をしようとしているかを表示します。
本番ルーターに向ける前に、管理用接続を消さないか、予期しない経路変更がないかを確認します。

```bash
routerd plan --config /usr/local/etc/routerd/router.yaml
```

## 予行実行する

`--dry-run` は、ホストを変更せずに適用結果だけを確認します。
routerd では、新しい制御器や実機検証の初期段階で予行実行を既定にします。

```bash
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

## 適用する

`routerd apply` は YAML の意図に合わせてホストを変更します。
一度だけ実行する場合は `--once` を付けます。
常駐させる場合は `routerd serve` を使います。

```bash
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
sudo routerd serve --config /usr/local/etc/routerd/router.yaml
```

## 生成する

文書内の「生成」は、routerd が dnsmasq 設定、nftables 設定、systemd ユニット、NixOS 設定などのホスト向けファイルを作ることを指します。
生成だけでホストが変わるとは限りません。
実際に反映するかどうかは、適用処理と予行実行の指定に従います。

Phase 2.0 以降、dnsmasq は DNS 応答を担当しません。
dnsmasq は DHCPv4、DHCPv6、中継、RA の設定だけを生成します。
DNS の待ち受け、ローカルゾーン、条件付き転送、暗号化 DNS は `DNSResolver` が扱います。
`DNSResolver` は `routerd-dns-resolver` の実行設定です。

## 調整する

常駐モードでは、routerd はイベントを受け取り、必要なリソースを再評価します。
この「意図と現在状態の差を縮める処理」を、この文書では調整と呼びます。
たとえば DHCPv6-PD の Renew 後にプレフィックスが変わると、LAN アドレス、RA、DNS 応答、DS-Lite 経路が順に調整されます。
