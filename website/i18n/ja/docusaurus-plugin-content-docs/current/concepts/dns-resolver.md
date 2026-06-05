---
title: DNS リゾルバ
slug: /concepts/dns-resolver
---

# DNS リゾルバ

routerd の DNS は、権威データ、リゾルバプロセス、転送ルール、上流エンドポイントを、それぞれ小さなリソースに分けて表します。

`DNSZone` はローカルの権威データを持ちます。手で書いたレコードと、DHCP リースから派生したレコードを保存します。

`DNSResolver` はデーモンインスタンスを管理します。待ち受けアドレス、キャッシュ、メトリクス、query log を定義します。1 つの `DNSResolver` リソースが、1 つの `routerd-dns-resolver` プロセスを起動します。

`DNSForwarder` はリゾルバに属する 1 つの match ルールです。`DNSZone` から応答するか、一致した問い合わせを `DNSUpstream` へ転送します。

`DNSUpstream` は 1 つの上流エンドポイントです。平文の UDP/TCP DNS、DoT、DoH を表します。

## 起動と部分的な立ち上げ

`DNSResolver` は、すべての依存関係がそろうまで応答開始を待ちません。起動時には、
その時点で解決できている待ち受けアドレスと source でデーモンを立ち上げ、
残りが準備できたあとに収束します。

- listen entry は、その時点で解決できるアドレスを bind します。`*From` source が
  まだ準備できていないアドレス（たとえば DHCPv6 prefix delegation を待っている
  delegated-prefix address）は、後続の reconcile で追加します。
- 動的な上流が未解決の forward/upstream source（たとえば上流を
  `DHCPv6Information` server から得る AFTR forwarder）は、その上流が現れるまで
  省略します。zone source と、静的または解決済み上流を持つ source はすぐに応答します。

一部がまだ待機中の間、リソースは `phase: Degraded` と、各 listen/source が何を
待っているかを示す `waiting` list を報告します。これは通常の bootstrap 状態であり、
障害ではありません。一般 DNS はすでに応答しています。依存リソースが status を公開すると、
コントローラーが再度調整し、完全な構成で `phase: Applied` に収束します
（最初からすべて解決済みで起動した場合と同一です）。リゾルバが `phase: Pending`
（何も応答しない）を報告するのは、待ち受けアドレスを 1 つも解決できない場合、または
利用可能な source が 1 つも残らない場合だけです。

これにより、DHCPv6 prefix delegation を待つ間に DNS が拒否される起動時の空白時間が
なくなります（本番ルータでの実測では、AFTR forwarder が `Degraded` を示している間も
起動直後から一般 DNS が応答し、delegated prefix 到着後に `Applied` へ収束しました）。
リゾルバは独立したサービスユニットとして動くため、意図的に `routerd` を再起動しても
DNS は再起動されません。プロセスを再起動したいときはリゾルバのユニットを明示的に
再起動してください。

## 応答元の順序

リゾルバを参照する `DNSForwarder` は、config に書いた順に評価します。
`zoneRefs` を持つフォワーダーは `DNSZone` から応答します。
`upstreams` を持つフォワーダーは、一致した問い合わせを上流へ転送します。
`match: ["."]` は既定の再帰問い合わせ経路です。

リゾルバは DoH、DoT、TCP DNS、平文 UDP DNS を扱います。
上流は優先順に試します。
優先度の高い上流が失敗したら、次の上流へ切り替えます。

## 複数の待ち受けプロファイル

`spec.listen` は配列です。
各待ち受けは、使う応答元名の部分集合を選べます。
これにより、LAN 用と VPN 用の待ち受けで挙動を変えられます。
それでも 1 つのリゾルバリソースを共有できます。
`listen[].sources` の名前は `DNSForwarder` を参照します。省略すると、そのリゾルバに属するすべてのフォワーダーを使います。

待ち受けアドレスをほかのリソース状態から得る場合は、`listen[].addressFrom` を使います。
依存関係が明示されるため、元リソースが変わったときにデーモンを再構成できます。

```yaml
listen:
  - name: lan
    addresses:
      - 192.0.2.1
    addressFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    port: 53
```

必要なアドレスをまだ解決できないときは、リゾルバは古いアドレスで起動しません。
`Pending(AddressUnresolved)` のまま待ちます。

## 動的なゾーンレコード

`DNSZone.spec.records[].ipv4` と `ipv6` は固定値です。
レコードのアドレスをほかのリソース状態から得る場合は、`ipv4From` または
`ipv6From` を使います。

```yaml
records:
  - hostname: router
    ipv4From:
      resource: IPv4StaticAddress/lan-base
      field: address
    ipv6From:
      resource: IPv6DelegatedAddress/lan-base
      field: address
```

必要な参照先をまだ解決できないときは、そのレコードを `DNSZone.status.pendingRecords`
に記録します。
参照先リソースが変わるとリゾルバを再生成し、解決できたあとにレコードを公開します。

## ネットワークを限定した上流

`DNSUpstream.spec.sourceInterface` は、Linux で送信先インターフェースを束縛します。
`ens18` や `wg0` のような OS インターフェース名を固定値で指定します。
トンネルや VRF のリソースがそのインターフェースを作る場合は、リソースの所有や順序で依存関係を明示します。
インターフェースができるまで、リゾルバを待機させます。

`DNSUpstream.spec.bootstrap` は、DoH や DoT の接続先名を解決する補助 DNS サーバーです。
接続先名がアクセス網の内側でしか解決できないときに使います。

上流サーバーの一覧をほかのリソース状態から得る場合は、`addressFrom` を使います。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSForwarder
metadata:
  name: ngn-aftr
spec:
  resolver: DNSResolver/lan-resolver
  match:
    - transix.jp
  upstreams:
    - DNSUpstream/wan-dns
---
apiVersion: net.routerd.net/v1alpha1
kind: DNSUpstream
metadata:
  name: wan-dns
spec:
  protocol: udp
  addressFrom:
    - resource: DHCPv6Information/wan-info
      field: dnsServers
```

ユーザーが書く YAML では `DNSResolver.spec.sources` を受け付けません。以前のインライン source は `DNSForwarder` と `DNSUpstream` に分けます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSForwarder
metadata:
  name: default
spec:
  resolver: DNSResolver/lan-resolver
  match:
    - "."
  upstreams:
    - DNSUpstream/cloudflare
---
apiVersion: net.routerd.net/v1alpha1
kind: DNSUpstream
metadata:
  name: cloudflare
spec:
  protocol: doh
  address: cloudflare-dns.com
  path: /dns-query
```

## dnsmasq との境界

dnsmasq は DHCPv4、DHCPv6、DHCP 中継、RA に限定します。
`server=`、`local=`、`host-record=` は生成しません。
DNS の応答と転送は、すべて `routerd-dns-resolver` が担当します。
