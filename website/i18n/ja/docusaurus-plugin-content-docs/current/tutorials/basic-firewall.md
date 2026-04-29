---
title: 基本のファイアウォール
sidebar_position: 4
---

# 基本のファイアウォール

ここまででルーターは WAN に出られ、LAN アドレスを持ち、dnsmasq が動いています。
ただし LAN クライアントのトラフィックはまだ上流に届きません。このチュートリアルでは
次を足します。

- WAN から出る IPv4 トラフィックの送信元 NAT (SNAT)
- `Zone` と `FirewallPolicy` で組む、既定拒否の小さなホームルーター用一式
- IPv6 転送 (IPv6 はグローバルアドレスなので NAT しません)

## 1. IPv4 アウトバウンドの送信元 NAT

`IPv4SourceNAT` リソースを追加します。

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4SourceNAT
      metadata:
        name: lan-out
      spec:
        outInterface: wan
        sourceInterface: lan
```

routerd がこれを nftables にレンダリングします。LAN 側のトラフィックは WAN から
出る際にマスカレードされます。

## 2. ゾーンと home-router プリセットの FirewallPolicy を入れる

```yaml
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: Zone
      metadata:
        name: lan
      spec:
        interfaces:
          - lan

    - apiVersion: firewall.routerd.net/v1alpha1
      kind: Zone
      metadata:
        name: wan
      spec:
        interfaces:
          - wan

    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallPolicy
      metadata:
        name: default-home
      spec:
        preset: home-router
        input:
          default: drop
        forward:
          default: drop
```

このプリセットは小さな既定拒否の構成です。

- WAN 側から来る新規接続を drop
- 双方向で established / related フローを許可
- LAN → WAN を許可
- LAN → ルーターの DHCP / DNS を許可 (前のチュートリアルで足した LAN 側サービス)
- **LAN からルーターへの SSH (TCP/22) を許可。** `FirewallPolicy.spec.routerAccess.ssh.fromZones`
  は省略時に `["lan"]` が既定値になるため、このポリシーを apply した時点で LAN
  ホストはそのままルーターに `ssh` で入れます。
- ICMPv6 を input チェインで許可

意図的に保守的な構成です。これを超える許可 (WAN からの SSH、サービス公開など) は
明示的なリソースで足します。

### SSH アクセスの整理

上の YAML だけで:

| 接続元 | ルーターへの SSH | 許可 |
|---|---|---|
| LAN ホスト | `ssh root@<router の LAN IP>` | ✅ 可 (LAN が既定の fromZones) |
| WAN ホスト | `ssh root@<router の WAN IP>` | ❌ 不可 (WAN の input は drop) |

LAN 側 SSH のために専用の管理用インターフェースは **要りません**。別の `mgmt`
ゾーンが意味を持つのは、管理専用の別 NIC を確保し、設定ミスでロックアウトされない
ように routerd の apply ガードを掛けたいときです。そのガードを有効にしたいなら:

```yaml
spec:
  apply:
    protectedZones:
      - mgmt
  resources:
    # 通常のゾーンに加えて、mgmt という Zone を定義
```

`protectedZones` を指定すると、routerd は `FirewallPolicy` の内容にかかわらず、
列挙したゾーンに対して TCP/22 を常に許可します。リストには定義済みの `Zone` の
名前を入れます。

## 3. apply

```bash
sudo routerd apply --once \
  --config /usr/local/etc/routerd/router.yaml
```

レンダリングされた nftables を見ます。

```bash
sudo nft list ruleset
```

`routerd_*` チェイン群への jump、input/forward の既定拒否、そして
`iifname "<lan-iface>" tcp dport 22 accept` (SSH 用) が入っているはずです。

## 4. LAN クライアントから試す

```bash
# IPv4 アウトバウンド
curl -v https://example.com

# IPv6 アウトバウンド (前のチュートリアルで PD を設定していれば)
curl -v https://[2606:2800:220:1:248:1893:25c8:1946]/

# ルーター経由の DNS
dig @192.168.10.1 example.com

# ルーターへ SSH (既定で許可)
ssh <user>@192.168.10.1
```

home-router プリセットは LAN のサービスを WAN 側から到達させません。
インバウンドの公開はオプトインです。

## 5. 1 つだけポートを開ける (任意)

たとえば WAN 側に HTTPS を公開したい場合は `ExposeService` リソースを足します。
ファイアウォール系のすべての種類は
[API リファレンス](../reference/api-v1alpha1#zone) にあります。

## 残るもの

これで小さなルーターが動く形になりました。

- WAN の DHCPv4 (任意で IPv6 PD)
- 静的 LAN アドレスと DHCP / DNS / RA
- IPv4 SNAT と既定拒否のファイアウォール
- LAN 側からの SSH を既定で許可

よくある次の一歩:

- ヘルスチェック付きのマルチ WAN (`IPv4DefaultRoutePolicy` の `healthChecks`)
- 上流技術別の DS-Lite、MAP-E、PPPoE
- スプリットホライゾン用の条件付き DNS フォワード

これらもそれぞれ別のリソースです。3 つのチュートリアルで使った「1 つ足す → apply
→ 確認」のパターンを繰り返して足していきます。

## 次へ

- [ルーターラボ](./router-lab) — もう少し本格的な構成
- [API リファレンス](../reference/api-v1alpha1) — 種類の全カタログ
- [リソース所有](../reference/resource-ownership) — リモートルーターに当てる前に
  apply が約束することを確認
