---
title: ファイアウォール例外を追加する
---

# ファイアウォール例外を追加する

## 想定するシーン

`FirewallZone` のロールベース既定で大半は事足りますが、例外が必要になる場面があります：

- 特定の管理サブネットからの SSH を許可したい。
- ルーター本体上のサービスポート (メトリクス endpoint、独自 listener) を開けたい。
- 特定 LAN ホストへの WAN からの inbound 接続を通したい (port forward / DMZ 的)。

## routerd での解決方法

`FirewallRule` で暗黙のロールマトリクスを上書きする例外を宣言します。
ルールはロールマトリクスより **先** に評価され、routerd が自動派生する内部用穴 (DHCP、DNS、DHCPv6-PD、DS-Lite 制御等) は更にユーザールールより先に評価されます。
この順序のおかげで、制限ルールを追加しても管理対象サービスは生き残ります。

## 例：管理ネットワークからの SSH を許可

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-admin-ssh
  spec:
    fromZone: management
    toZone: self
    protocol: tcp
    port: 22
    action: accept
```

`fromZone` / `toZone` は `FirewallZone` 名を参照します。
`toZone: self` はルーター自身が終端するトラフィック (forward ではない) を意味します。

## 例：ルーター本体のサービスポートを開ける

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-metrics
  spec:
    fromZone: lan
    toZone: self
    protocol: tcp
    port: 9100
    action: accept
```

## 適用前の確認

ローカルシミュレーターで動作を確かめてから apply してください。

```sh
routerctl firewall test from=wan to=self proto=tcp dport=22
routerctl describe firewall
```

最初のコマンドは指定 5-tuple に対して `accept` / `drop` を返します。
2 つ目はロールマトリクスの既定と管理対象の穴を含めた実効ルール全体を表示します。

## 関連項目

- [ファイアウォールゾーンを定義する](./firewall-zone.md)
- [Firewall コンセプト](../concepts/firewall.md)
