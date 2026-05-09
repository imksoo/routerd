---
title: ファイアウォールゾーンを定義する
---

# ファイアウォールゾーンを定義する

## 想定するシーン

「WAN は LAN に届かない、LAN は WAN に届く、管理経路は全部に届く」というのが家庭・SOHO ルーターの基本ポリシーマトリクスです。
これを個別の `accept` / `drop` ルールで書くのは繰り返しが多くミスの温床になります。

## routerd での解決方法

`FirewallZone` でインターフェースを **役割 (role)** に紐付けます。
routerd は内蔵のロールマトリクスから方向ごとの既定アクションを導出するため、典型構成では `FirewallRule` を書く必要すらありません。

| role | 用途 |
| --- | --- |
| `untrust` | WAN 側 (上流回線、DSLite トンネル、PPPoE 仮想インターフェース) |
| `trust` | 通常 LAN セグメント |
| `mgmt` | 帯域外管理ネットワーク |

暗黙のマトリクス：

| from \ to | self | trust | mgmt | untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | accept | accept | n/a | accept |
| `trust` | accept | accept | drop | accept |
| `untrust` | drop | drop | drop | n/a |
| `self` | accept | accept | accept | accept |

established/related な接続は常に許可されます。

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

典型的な家庭ルーターはこれで十分です。`FirewallRule` は例外を表現するときだけ追加してください。

## 関連項目

- [ファイアウォール例外を追加する](./firewall-rule.md)
- [MAC アドレスでゲスト端末を隔離する](./guest-mode.md)
- [Firewall コンセプト](../concepts/firewall.md)
