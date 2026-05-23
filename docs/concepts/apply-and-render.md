---
title: 適用と生成
slug: /concepts/apply-and-render
sidebar_position: 4
---

# 適用と生成

routerd には、日常の運用でよく使う操作がいくつかあります。
このページでは、ドキュメント内で使う言葉をそろえて説明します。

## 検証する

`routerd validate` は YAML の形を確認します。
Kind 名、必須フィールド、値の範囲、明らかな依存関係の誤りを検出します。

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
```

## 計画を見る

`routerd plan` は、ホストに対して何をしようとしているかを表示します。
本番ルーターへ適用する前に、管理用の接続が切れないか、予期しない経路変更がないかを確認できます。

```bash
routerd plan --config /usr/local/etc/routerd/router.yaml
```

## 予行実行する

`--dry-run` は、ホストを変更せずに適用の結果だけを確認します。
routerd では、新しいコントローラーや実機検証の初期段階で、予行実行を既定とします。

```bash
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

## 適用する

`routerd apply` は、YAML の意図に合わせてホストを変更します。
一度だけ実行するなら `--once` を付けます。
常駐させるなら `routerd serve` を使います。

```bash
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
sudo routerd serve --config /usr/local/etc/routerd/router.yaml
```

## 生成する

ドキュメント内の「生成」は、routerd が dnsmasq 設定、nftables 設定、systemd ユニット、NixOS 設定などのホスト向けファイルを組み立てることを指します。
生成しただけでホストが変わるとは限りません。
実際に反映するかどうかは、適用処理と予行実行の指定で決まります。

現在の routerd では、dnsmasq は DNS 応答を担当しません。
dnsmasq 向けには、DHCPv4、DHCPv6、中継、RA の設定だけを生成します。
DNS の待ち受け、ローカルゾーン、条件付き転送、暗号化 DNS は `DNSResolver` が扱います。
`DNSResolver` は `routerd-dns-resolver` の実行設定です。

## 調整する

常駐モードでは、routerd はイベントを受け取り、必要なリソースを再評価します。
この「意図と現在状態の差を縮める処理」を、このドキュメントでは調整（リコンサイル）と呼びます。
たとえば DHCPv6-PD の Renew 後にプレフィックスが変わると、LAN アドレス、RA、DNS 応答、DS-Lite 経路の順に調整が伝わります。
