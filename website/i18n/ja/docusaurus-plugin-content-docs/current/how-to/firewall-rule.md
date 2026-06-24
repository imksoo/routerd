---
title: ファイアウォール例外を追加する
---

# ファイアウォール例外を追加する

![FirewallRule exception が management SSH、router service port、scoped destination CIDR、implicit role matrix 前の評価順を扱う流れ](/img/diagrams/how-to-firewall-rule.png)

## 想定するシーン

`FirewallZone` のロールベースの既定で大半はまかなえますが、例外が必要になる場面もあります。

- 特定の管理サブネットからの SSH を許可したい。
- ルーター本体上のサービスポート（メトリクスのエンドポイント、独自のリスナー）を開けたい。
- 特定の LAN ホストへの、WAN からの着信接続を通したい（ポート転送や DMZ のような用途）。

## routerd での解決方法

`FirewallRule` で、暗黙のロールマトリクスを上書きする例外を宣言します。
ルールはロールマトリクスより **先** に評価し、routerd が自動で派生する内部用の穴（DHCP、DNS、DHCPv6-PD、DS-Lite 制御など）は、さらにユーザールールより先に評価します。
この順序のおかげで、制限ルールを追加しても管理対象サービスは生き残ります。

## 例: 管理ネットワークからの SSH を許可する

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
`toZone: self` は、ルーター自身が終端する通信（転送ではない）を意味します。

## 例: ルーター本体のサービスポートを開ける

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-metrics
  spec:
    fromZone: lan
    toZone: self
    protocol: tcp
    destinationPorts:
      - "9100"
    action: accept
```

## 例: LAN から管理セグメント上の 1 台だけを許可する

宛先ゾーン内の特定ホストだけを例外にしたい場合は、
`destinationCIDRs` を指定します。
これにより、管理セグメント全体を開けずに済みます。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-lan-to-admin-console
  spec:
    fromZone: lan
    toZone: management
    destinationCIDRs:
      - 192.0.2.126/32
    protocol: tcp
    destinationPorts:
      - "8080"
    action: accept
```

## 例: 複数の web ポートと ICMP echo

TCP / UDP の複数ポートを 1 つのルールで扱う場合は `destinationPorts` を使います。
ICMP ルールは type 名で絞り込めます。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: wan-web
  spec:
    fromZone: wan
    toZone: self
    protocol: tcp
    destinationPorts:
      - "80"
      - "443"
    action: accept

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: wan-icmp-echo
  spec:
    fromZone: wan
    toZone: self
    protocol: icmp
    icmpType: echo-request
    action: accept
```

## 例: レート制限や接続数制限を超えた SSH を拒否する

`rateLimit` は、設定した閾値を超えた通信に一致します。`connLimit` は、同じ送信元が
許可数を超える同時追跡状態をすでに持っている場合に一致します。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: ssh-bruteforce-over-limit
  spec:
    fromZone: wan
    toZone: self
    protocol: tcp
    destinationPorts:
      - "22"
    action: reject
    rateLimit:
      rate: 8
      burst: 16
      unit: packet
      per: minute
      log: true
    connLimit:
      maxPerSource: 4
      log: true
```

## 適用前の確認

ローカルのシミュレーターで動作を確かめてから適用してください。

```sh
routerctl firewall test from=wan to=self proto=tcp dport=22
routerctl describe firewall
```

最初のコマンドは、指定した 5-tuple に対して `accept` / `drop` を返します。
2 つ目は、ロールマトリクスの既定と管理対象の穴を含めた、実効ルール全体を表示します。

## 関連項目

- [ファイアウォールゾーンを定義する](./firewall-zone.md)
- [ファイアウォールのコンセプト](../concepts/firewall.md)
