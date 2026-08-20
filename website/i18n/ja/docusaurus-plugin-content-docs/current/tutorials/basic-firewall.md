---
title: NAT44 とファイアウォールの準備
sidebar_position: 6
---

# NAT44 とファイアウォールの準備

![WAN、LAN、NAT44Rule、FirewallZone、FirewallPolicy と確認手順を示す図](/img/diagrams/tutorial-basic-firewall.png)

このページでは、LAN の IPv4 を WAN へ出す NAT44 の書き方と、
ファイアウォール機能を扱うときの大切な注意を説明します。

:::danger ファイアウォールはまだ唯一の防御にしない

routerd のファイアウォール用リソースは、現在も API、検証、生成の土台を
整えている段階です。このページの設定は、インターネット公開用ルーターの
完成した安全設計やセキュリティ保証ではありません。

WAN をインターネットへ公開したり、自宅・学校・職場の唯一の防御にしたり
しないでください。隔離した Ubuntu Server VM での学習に限り、実運用では
別に設計・検証した防御を用意してください。

:::

## NAT44 は何をするか

NAT44 は、LAN の複数のプライベート IPv4 アドレスが、WAN 側へ出るときに
1 つの外向き IPv4 アドレスを共有できるようにする仕組みです。
NAT は「外からの通信を安全に止める」仕組みそのものではありません。
NAT とファイアウォールは別の役目だと考えてください。

ここでは、次の構成を想定します。

- `wan`: 上流ネットワークにつながるインターフェース
- `lan`: テスト用クライアントにつながるインターフェース
- `192.168.50.0/24`: テスト用 LAN のアドレス範囲

## 正しい NAT44Rule の形

次のリソースを、`Router.spec.resources` の中へ追加します。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: lan-to-wan
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.168.50.0/24
```

- `type: masquerade` は、LAN の送信元を WAN 側の IPv4 アドレスへ置き換える NAT を選びます。
- `egressInterface` は、外へ出る側の routerd リソース名です。
- `sourceRanges` は、NAT してよい LAN の範囲です。

DHCP でアドレスを受ける WAN、PPPoE、DS-Lite のどれでも、基本の形は同じです。
外向きのリソース名だけを実際の構成に合わせます。

## ファイアウォール用のリソース

`FirewallZone`、`FirewallPolicy`、`FirewallRule` は、どの線をどの役割として
扱いたいかを書くためのリソースです。たとえば、WAN を `untrust`、LAN を `trust`
として書く形は次のようになります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: wan
  spec:
    role: untrust
    interfaces:
      - wan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: lan
  spec:
    role: trust
    interfaces:
      - lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: lab-default
  spec: {}
```

この YAML が通ることと、外部から安全であることは同じではありません。
特に初めての構成では、VM コンソールを残し、WAN を外部へ公開しません。

## daemon の前に確認する

設定ファイル全体を作ったら、サービスを起動する前に `routerd` 本体で確認します。

```sh
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config ./router.yaml
sudo routerd apply --config ./router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
```

NAT の対象が `192.168.50.0/24` だけか、WAN と LAN の取り違えがないか、
dry-run の結果で確認します。

## サービス起動後の観察

サービスを起動した**後で**、`routerctl` と `nft` で観察できます。

```sh
sudo systemctl is-active routerd.service
sudo routerctl get status
sudo routerctl describe NAT44Rule/lan-to-wan
sudo routerctl firewall test from=wan to=lan proto=tcp dport=22
sudo nft list table ip routerd_nat
sudo nft list table inet routerd_filter
```

これらは「今見えている設定」を確認するためのコマンドです。外部攻撃に耐える
ことの証明にはなりません。

## 次に読むもの

- [基本的な IPv4 NAT ルーター](../config-examples/basic-ipv4-nat.md)
- [ファイアウォールのコンセプト](../concepts/firewall.md)
- [対応プラットフォーム](../platforms.md) — Linux 以外の現在の対応範囲
