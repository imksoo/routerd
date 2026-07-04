---
title: 適用と生成
slug: /concepts/apply-and-render
sidebar_position: 4
---

# 適用と生成

![routerd の validate、plan、dry-run、apply、render が同じ有効リソースグラフを使う流れ](/img/diagrams/concept-apply-and-render.png)

routerd には、日常の運用でよく使う操作がいくつかあります。
このページでは、ドキュメント内で使う言葉をそろえて説明します。

## 検証する

`routerctl validate` は YAML の形を確認します。
Kind 名、必須フィールド、値の範囲、明らかな依存関係の誤りを検出します。

```bash
routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
```

スクリプトから使う場合、`routerctl validate` は返された
`ValidateResult` が valid のときだけ終了コード `0` になります。
routerd に到達でき、候補 config が invalid の場合は `1`、候補
ファイルを読めない場合や daemon に接続できない場合などの実行・
transport エラーでは `2` で終了します。routerd が JSON 結果を
返した場合、その出力は従来どおり stdout に書かれます。
`valid: true` の warning だけなら終了コードは `0` のままです。

## 計画を見る

`routerctl plan` は、ホストに対して何をしようとしているかを表示します。
本番ルーターへ適用する前に、管理用の接続が切れないか、予期しない経路変更がないかを確認できます。

```bash
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
```

## 予行実行する

`--dry-run` は、ホストを変更せずに適用の流れだけを確認します。実際に変わる箇所を事前に把握できます。

```bash
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
```

## 適用する

`routerctl apply` は、一度きりのホスト操作です。意図を検証し、必要に応じてホストの現在状態を観測し、生成した成果物を書き出し、状態を記録して終了します。長時間動くデーモンのライフサイクルは管理しません。管理対象デーモンの起動・有効化・再起動・再読み込みは `routerd serve` が担当します。

```bash
sudo routerctl apply -f /usr/local/etc/routerd/router.yaml --replace
sudo routerd serve --config /usr/local/etc/routerd/router.yaml
```

## 生成する

ドキュメント内の「生成」は、routerd が dnsmasq 設定、nftables 設定、systemd ユニットなどのホスト向けファイルを組み立てることを指します。
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
