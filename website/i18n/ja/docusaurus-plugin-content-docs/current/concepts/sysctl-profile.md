---
title: Sysctl プロファイル
slug: /concepts/sysctl-profile
---

# Sysctl プロファイル

routerd は Linux ルーター向け sysctl を router resource から自動導出します。
通常の home-router config では `SysctlProfile` や大量の `Sysctl` を並べません。
NAT、DS-Lite、BGP、IPv6 prefix delegation、RA、LAN service などの resource から、
forwarding、redirect、reverse path filter、conntrack、TCP、interface ごとの RA 設定を導出します。

`Sysctl` と `SysctlProfile` は、routerd がまだ導出できない hardware / kernel /
distribution 固有設定を補う narrow escape hatch として残します。ルーター要件を表す
主な手段ではなく、実装上の override として扱います。

`runtime: true` は controller chain が serve しているときに実行中のカーネルへ即時反映します。
`persistent: true` は `/etc/sysctl.d/` に永続設定を書きます。
`routerd apply --once` は明示的な `Sysctl` / `SysctlProfile` だけを host に反映します。
派生 sysctl は plan / render の対象になり、実際の適用は `routerd serve` が担当します。

明示的な profile escape hatch を使う場合だけ、差分を `overrides` で上書きします。

```yaml
spec:
  profile: router-linux
  overrides:
    net.netfilter.nf_conntrack_max: "524288"
```

routerd は読み戻した値を確認してから設定します。
現在値が期待を満たす場合は書き込みません。
その場合は適用イベントも発行しません。

一部の sysctl は、カーネルが値を上方へ丸めます。
そのような値は `compare: atLeast` を使います。
`value` は書き込む値です。
`expectedValue` は読み戻しで期待する値です。
`expectedValue` を省略すると `value` を使います。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: socket-buffer
spec:
  key: net.core.rmem_max
  value: "16777216"
  expectedValue: "16777216"
  compare: atLeast
  runtime: true
```

## router-linux の値

| キー | 値 | 理由 |
| --- | --- | --- |
| `net.ipv4.ip_forward` | `1` | IPv4 転送を有効にします。 |
| `net.ipv4.conf.all.forwarding` | `1` | インターフェース単位の IPv4 転送を有効にします。 |
| `net.ipv4.conf.all.rp_filter` | `0` | ポリシールーティングや DS-Lite トンネルの戻り通信を reverse path filter が破棄しないようにします。 |
| `net.ipv4.conf.default.rp_filter` | `0` | 後から作られるトンネルインターフェースでも reverse path filter を無効にします。 |
| `net.ipv4.conf.all.send_redirects` | `0` | ルーターから ICMP redirect を出さないようにします。 |
| `net.ipv4.conf.default.send_redirects` | `0` | 後から作られるインターフェースにも同じ設定を適用します。 |
| `net.ipv4.conf.all.src_valid_mark` | `1` | fwmark を使う経路選択で reverse path 判定が mark を考慮できるようにします。 |
| `net.ipv6.conf.all.forwarding` | `1` | IPv6 転送を有効にします。 |
| `net.ipv6.conf.default.forwarding` | `1` | 後から作られるインターフェースでも IPv6 転送を有効にします。 |
| `net.netfilter.nf_conntrack_acct` | `1` | conntrack の packet / byte accounting を有効にし、Web Console の client traffic 集計に使います。conntrack が未ロードの環境では任意扱いです。 |
| `net.netfilter.nf_conntrack_max` | `262144` | 多数の端末とアプリの同時接続で conntrack が詰まることを避けます。conntrack が未ロードの環境では任意扱いです。 |
| `net.netfilter.nf_conntrack_buckets` | `65536` | `nf_conntrack_max / 4` を目安にします。環境によって書けないため任意扱いです。 |
| `net.netfilter.nf_conntrack_tcp_timeout_established` | `86400` | 既定の 5 日は家庭用ルーターでは長すぎるため、24 時間へ短縮します。conntrack が未ロードの環境では任意扱いです。 |
| `net.netfilter.nf_conntrack_udp_timeout` | `30` | 単発 UDP の保持時間を短くします。conntrack が未ロードの環境では任意扱いです。 |
| `net.netfilter.nf_conntrack_udp_timeout_stream` | `180` | 継続 UDP の保持時間を 3 分にします。conntrack が未ロードの環境では任意扱いです。 |
| `net.core.rmem_max` | `16777216` | 受信バッファーの上限を 16 MiB にします。 |
| `net.core.wmem_max` | `16777216` | 送信バッファーの上限を 16 MiB にします。 |
| `net.ipv4.tcp_rmem` | `4096 87380 16777216` | TCP 受信バッファーの自動調整範囲を広げます。 |
| `net.ipv4.tcp_wmem` | `4096 65536 16777216` | TCP 送信バッファーの自動調整範囲を広げます。 |
| `net.core.netdev_max_backlog` | `5000` | 瞬間的な受信バーストで破棄されにくくします。 |
| `net.core.somaxconn` | `4096` | listen backlog の上限を明示します。 |
| `net.ipv4.ip_local_port_range` | `1024 65535` | ルーター自身が使う一時ポート範囲を広げます。 |
| `net.ipv4.tcp_fin_timeout` | `30` | FIN-WAIT-2 の保持時間を短くします。 |
| `net.ipv4.tcp_mtu_probing` | `1` | Path MTU notification が届かない経路でも TCP が小さい segment へ戻れるようにします。 |
| `net.ipv4.tcp_tw_reuse` | `1` | TIME-WAIT ソケットの再利用を許可します。 |
| `net.ipv6.route.max_size` | `16384` | IPv6 経路キャッシュの上限を引き上げます。 |

`net.ipv4.route.max_size` は現在の Linux では実効性が薄い環境があります。
routerd の既定プロファイルでは設定しません。
必要な環境では `overrides` ではなく個別の `Sysctl` として追加し、実機で存在確認してください。

`net.netfilter.nf_conntrack_udp_timeout` の既定値は Linux conntrack の unreplied UDP default に合わせて `30` 秒です。Firewall deny や DPI 観測との相関を少し長く持ちたい運用では、`60` 秒に上書きできます。

```yaml
spec:
  profile: router-linux
  overrides:
    net.netfilter.nf_conntrack_udp_timeout: "60"
```

conntrack、NFLOG、WireGuard などの module loading は、NAT、firewall log、
traffic flow log、WireGuard などの resource から routerd が自動導出します。
`KernelModule` は user-facing config kind ではありません。導出漏れがあれば、
実装側の derivation bug として直します。

## 個別 Sysctl との使い分け

個別 `Sysctl` は、routerd の導出モデルから本当に外れる値だけに使います。
DS-Lite tunnel の `rp_filter=0`、WAN/LAN の `accept_ra=2`、LAN の
`send_redirects=0` のような routerd が理解できる interface 設定は resource から導出されるため、
通常 config に書きません。

例: 検証用 kernel で一時的に socket buffer を上げる場合

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: lab-rmem-max
spec:
  key: net.core.rmem_max
  value: "33554432"
  compare: atLeast
  runtime: true
  persistent: true
```
