---
title: Sysctl プロファイル
slug: /concepts/sysctl-profile
---

# Sysctl プロファイル

routerd は Linux ルーター向けの `SysctlProfile` を持ちます。
単発の `Sysctl` を並べる代わりに、家庭用ルーターで必要になりやすい値をまとめて宣言できます。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: SysctlProfile
metadata:
  name: router-runtime
spec:
  profile: router-linux
  runtime: true
  persistent: true
```

`runtime: true` は実行中のカーネルへ即時反映します。
`persistent: true` は `/etc/sysctl.d/` に永続設定を書きます。
個別に変えたい値は `overrides` で上書きします。

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
| `net.ipv6.conf.all.forwarding` | `1` | IPv6 転送を有効にします。 |
| `net.ipv6.conf.default.forwarding` | `1` | 後から作られるインターフェースでも IPv6 転送を有効にします。 |
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
| `net.ipv4.tcp_tw_reuse` | `1` | TIME-WAIT ソケットの再利用を許可します。 |
| `net.ipv6.route.max_size` | `16384` | IPv6 経路キャッシュの上限を引き上げます。 |

`net.ipv4.route.max_size` は現在の Linux では実効性が薄い環境があります。
routerd の既定プロファイルでは設定しません。
必要な環境では `overrides` ではなく個別の `Sysctl` として追加し、実機で存在確認してください。

## 個別 Sysctl との使い分け

`SysctlProfile` は推奨値のまとまりです。
WAN の RA 受信のようにインターフェース名が入る値は、個別の `Sysctl` で書きます。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: wan-accept-ra
spec:
  key: net.ipv6.conf.ens18.accept_ra
  value: "2"
  runtime: true
  persistent: true
```
