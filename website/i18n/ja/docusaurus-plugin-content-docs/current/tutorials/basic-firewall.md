---
title: 基本のファイアウォール
sidebar_position: 4
---

# 基本のファイアウォール

ここまででルーターは WAN に出られ、LAN アドレスを持ち、dnsmasq が動いています。
ただし LAN クライアントのトラフィックはまだ上流に届きません。このチュートリアルでは
次を足します。

- WAN から出る IPv4 トラフィックの送信元 NAT (SNAT)
- 既定拒否の小さなホームルーター用ファイアウォール一式
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

## 2. ホームルーター用ファイアウォール一式を入れる

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: HomeRouterFirewall
      metadata:
        name: home
      spec:
        wan: wan
        lan: lan
```

この一式は小さな既定拒否の構成です。

- WAN 側から来る新規接続を drop
- 双方向で established / related フローを許可
- LAN → WAN を許可
- LAN → ルーターの DNS、DHCP、ICMP を許可
- 送信元偽装 (uRPF 違反) を drop

意図的に保守的な構成です。これを超える許可は明示的なリソースで足します。

## 3. apply

```bash
sudo routerd apply --once \
  --config /usr/local/etc/routerd/router.yaml
```

レンダリングされた nftables を見ます。

```bash
sudo nft list ruleset
```

`routerd_*` チェイン群への jump と、ホームルーター一式のルールが入っているはずです。

## 4. LAN クライアントから試す

```bash
# IPv4 アウトバウンド
curl -v https://example.com

# IPv6 アウトバウンド (前のチュートリアルで PD を設定していれば)
curl -v https://[2606:2800:220:1:248:1893:25c8:1946]/

# ルーター経由の DNS
dig @192.168.10.1 example.com
```

ホームルーター一式は LAN のサービスを WAN 側から到達させません。インバウンドの
公開はオプトインです。

## 5. 1 つだけポートを開ける (任意)

たとえば WAN 側に SSH を公開したい場合は `Service` と `PortForward` (または明示的な
ファイアウォールルール) を加えます。ファイアウォール系のすべての種類は
[API リファレンス](../reference/api-v1alpha1#zone) にあります。

## 残るもの

これで小さなルーターが動く形になりました。

- WAN の DHCPv4 (任意で IPv6 PD)
- 静的 LAN アドレスと DHCP / DNS / RA
- IPv4 SNAT と既定拒否のファイアウォール

よくある次の一歩:

- ヘルスチェック付きのマルチ WAN (`Ipv4DefaultRoutePolicy` の `healthChecks`)
- 上流技術別の DS-Lite、MAP-E、PPPoE
- スプリットホライゾン用の条件付き DNS フォワード

これらもそれぞれ別のリソースです。3 つのチュートリアルで使った「1 つ足す → apply
→ 確認」のパターンを繰り返して足していきます。

## 次へ

- [ルーターラボ](./router-lab) — もう少し本格的な構成
- [API リファレンス](../reference/api-v1alpha1) — 種類の全カタログ
- [リソース所有](../reference/resource-ownership) — リモートルーターに当てる前に
  apply が約束することを確認
