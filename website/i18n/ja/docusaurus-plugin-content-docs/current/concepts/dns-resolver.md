---
title: DNS リゾルバー
slug: /concepts/dns-resolver
---

# DNS リゾルバー

Phase 2.0 では、DNS を 2 つの Kind に分けます。

`DNSZone` はローカル権威データを保持します。
手動レコードと DHCP リース由来のレコードを扱います。

`DNSResolver` はデーモンの実行単位です。
待ち受けアドレス、応答元の順序、上流、キャッシュ方針を定義します。
1 つの `DNSResolver` が、1 つの `routerd-dns-resolver` プロセスを起動します。

## 応答元の順序

`DNSResolver.spec.sources` は上から順に評価します。
`zone` は `DNSZone` から応答します。
`forward` は一致したゾーンを指定上流へ転送します。
`upstream` は既定の再帰問い合わせ経路です。

リゾルバーは DoH、DoT、DoQ、平文 UDP DNS を扱います。
上流は優先順に試します。
上位の上流が失敗した場合は下位へ切り替えます。

## 複数の待ち受けプロファイル

`spec.listen` は配列です。
各待ち受けは、利用する応答元の部分集合を選べます。
これにより、LAN と VPN で異なる応答を返せます。

## 制限されたネットワークの上流

`sources[].viaInterface` は、Linux で送信インターフェースを固定します。
値には `Interface`、`WireGuardInterface`、`IPsecConnection`、`VRF` の状態参照を使えます。

`sources[].bootstrapResolver` は、DoH や DoT のエンドポイント名を解決する補助 DNS サーバーです。
アクセス回線内でしか解決できない名前に使います。

## dnsmasq との境界

dnsmasq は DHCPv4、DHCPv6、DHCP 中継、RA だけを担当します。
`server=`、`local=`、`host-record=` の行は生成しません。
DNS の応答と転送は `routerd-dns-resolver` が担当します。
