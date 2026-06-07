---
title: ファイアウォールゾーンを定義する
---

# ファイアウォールゾーンを定義する

![FirewallZone が WAN、LAN、management network の interface role と stateful role-matrix default を定義する流れ](/img/diagrams/how-to-firewall-zone.png)

## 想定するシーン

「WAN は LAN に届かない、LAN は WAN に届く、管理経路はすべてに届く」というのが、家庭や SOHO のルーターの基本的なポリシーマトリクスです。
これを個別の `accept` / `drop` ルールで書くと、繰り返しが多くミスの温床になります。

## routerd での解決方法

`FirewallZone` で、インターフェースを **役割（role）** に紐付けます。
routerd は内蔵のロールマトリクスから方向ごとの既定アクションを導くため、典型的な構成では `FirewallRule` を書く必要すらありません。

| role | 用途 |
| --- | --- |
| `untrust` | WAN 側（上流回線、DSLite トンネル、PPPoE 仮想インターフェース） |
| `trust` | 通常の LAN セグメント |
| `mgmt` | 帯域外管理ネットワーク |

暗黙のマトリクスは次のとおりです。

| from \ to | self | trust | mgmt | untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | accept | accept | n/a | accept |
| `trust` | accept | accept | drop | accept |
| `untrust` | drop | drop | drop | n/a |
| `self` | accept | accept | accept | accept |

established/related な接続は常に許可します。

## 例

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: wan
  spec:
    role: untrust
    interfaces:
      - Interface/wan
      - DSLiteTunnel/ds-lite-primary

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: lan
  spec:
    role: trust
    interfaces:
      - Interface/lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: management
  spec:
    role: mgmt
    interfaces:
      - Interface/mgmt
```

典型的な家庭ルーターはこれで十分です。`FirewallRule` は、例外を表すときだけ追加してください。

## 関連項目

- [ファイアウォール例外を追加する](./firewall-rule.md)
- [MAC アドレスでゲスト端末を隔離する](./guest-mode.md)
- [ファイアウォールのコンセプト](../concepts/firewall.md)
