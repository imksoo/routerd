---
title: 適用と生成
slug: /concepts/apply-and-render
sidebar_position: 4
---

# 適用と生成

![routerd で設定を検証し、dry-run し、サービスを起動して routerctl で状態を見る流れ](/img/diagrams/concept-apply-and-render.png)

routerd には、設定を書く段階で使う `routerd` と、動いている daemon
（起動し続けるプログラム）に話しかける
`routerctl` があります。最初は、この順番を守ると安全です。

```text
YAML を書く
    ↓
routerd validate
    ↓
routerd apply --once --dry-run
    ↓
安全を確認して routerd.service を起動
    ↓
routerctl get status
```

## 1. 検証する

`routerd validate` は YAML の書き方を確認します。Kind 名、必須の項目、値の範囲、
分かりやすい参照ミスを見つけます。daemon はまだ必要なく、ホストのネットワークも
変更しません。

```sh
sudo routerd validate --config ./router.yaml
```

初回に `routerctl validate` を使わない理由は、`routerctl` が動いている
`routerd.service` のローカルソケットに接続するからです。

## 2. dry-run する

dry-run は、読み込んだリソースの順番と生成する内容を確認する予行演習です。
初回は常設の状態ファイルを使わず、使い捨ての場所を明示します。

```sh
LAB_DIR="$(mktemp -d)"
sudo routerd apply --config ./router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
sed -n "1,120p" "$LAB_DIR/status.json"
```

- **state** は、routerd が見た状態を保存するデータベースです。
- **ledger** は、routerd が所有する成果物の記録です。
- **status** は、今回の結果を書いた JSON ファイルです。

`--dry-run` がある間はネットワークを本当に変更しません。それでも、設定の内容は
正しく読む必要があります。最初は隔離した Ubuntu Server VM とコンソールで行います。

## 3. 適用する

`routerd apply --once` から `--dry-run` を外すと、本当にホストを変更できます。
これは WAN、LAN、経路、NAT、サービスを変える可能性がある操作です。
最初の VM では、いきなり一回だけの live apply を勧めません。

代わりに、dry-run が安全だと確認してから設定を
`/usr/local/etc/routerd/router.yaml` に置き、`routerd.service` を起動します。
常駐するサービスは、設定と実際の状態の差を見つけて必要な処理を続けます。

```sh
sudo systemctl enable --now routerd.service
sudo systemctl is-active routerd.service
```

## 4. サービス起動後に routerctl を使う

サービスが作るローカルソケットに接続できるようになったら、`routerctl` を使います。
状態を読む例は次のとおりです。

```sh
sudo routerctl get status
sudo routerctl get events --limit 20
```

動いている routerd に候補 YAML を渡して検証や計画を見る場合も、この後です。

```sh
sudo routerctl validate -f candidate.yaml --replace
sudo routerctl plan -f candidate.yaml --replace
```

この 2 つは host の状態を変えませんが、稼働中の daemon が必要です。

## 生成とは

「生成」は、routerd が dnsmasq の設定、nftables の設定、systemd の unit など、
ホスト向けのファイルを組み立てることです。生成しただけで、必ずホストが変わる
わけではありません。dry-run では生成内容を確認し、live の適用やサービス起動で
必要な変更を反映します。

現在の routerd では、dnsmasq 向けには DHCPv4、DHCPv6、中継、RA の設定を
生成します。DNS の待ち受け、ローカルゾーン、条件付き転送、暗号化 DNS は
`DNSResolver` が扱います。

## プラットフォームの範囲

このページの初回手順は Ubuntu Server を対象にしています。netplan、systemd、
nftables のような Linux 固有の生成は、対応する機能がある環境だけで使います。
FreeBSD と NixOS の導入・サービス連携は土台がありますが、Ubuntu と同じ
レンダラーの対応を意味しません。
