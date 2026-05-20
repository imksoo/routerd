---
title: DNS リゾルバー
slug: /concepts/dns-resolver
---

# DNS リゾルバー

routerd の DNS は、権威データ、resolver プロセス、転送ルール、上流 endpoint を小さなリソースに分けて表現します。

`DNSZone` はローカル権威データを持ちます。手動レコードと、DHCP リースから派生したレコードを保存します。

`DNSResolver` は daemon インスタンスを管理します。待ち受けアドレス、キャッシュ、metrics、query log を定義します。1 つの `DNSResolver` リソースが、1 つの `routerd-dns-resolver` プロセスを起動します。

`DNSForwarder` は resolver に属する 1 つの match rule です。`DNSZone` から応答するか、match した問い合わせを `DNSUpstream` に転送します。

`DNSUpstream` は 1 つの上流 endpoint です。平文 UDP/TCP DNS、DoT、DoH を表します。

## 応答元の順序

resolver を参照する `DNSForwarder` が config の順番で評価されます。
`zoneRefs` を持つ forwarder は `DNSZone` から応答します。
`upstreams` を持つ forwarder は一致した問い合わせを上流へ転送します。
`match: ["."]` は既定の再帰問い合わせ経路です。

リゾルバーは DoH、DoT、TCP DNS、平文 UDP DNS を扱います。
上流は優先順に試します。
優先度の高い上流が失敗した場合は、次の上流へ切り替えます。

## 複数の待ち受けプロファイル

`spec.listen` は配列です。
各待ち受けは、利用する応答元名の部分集合を選べます。
これにより、LAN と VPN の待ち受けで違う挙動を持たせられます。
それでも 1 つのリゾルバーリソースを共有できます。
`listen[].sources` の名前は `DNSForwarder` を参照します。省略すると、その resolver に属するすべての forwarder を使います。

待ち受けアドレスがほかのリソース状態から来る場合は、`listen[].addressFrom` を使います。
依存関係が明示されるため、元リソースが変化したときにデーモンを再構成できます。

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

必要なアドレスがまだ解決できない場合、リゾルバーは古いアドレスで起動しません。
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

必要な参照先がまだ解決できない場合、そのレコードは `DNSZone.status.pendingRecords`
に記録されます。
参照先リソースが変化するとリゾルバーを再生成し、解決後にレコードを公開します。

## ネットワークが制限された上流

`DNSUpstream.spec.sourceInterface` は、Linux で送信先インターフェースを束縛します。
`ens18` や `wg0` のような OS インターフェース名を固定値で指定します。
トンネルや VRF リソースがそのインターフェースを作る場合は、リソース所有や順序で依存関係を明示します。
インターフェースが存在するまで、リゾルバーを待機させます。

`DNSUpstream.spec.bootstrap` は、DoH や DoT の接続先名を解決する補助 DNS サーバーです。
接続先名がアクセス網の内側でしか解決できない場合に使います。

上流サーバー一覧がほかのリソース状態から来る場合は、`addressFrom` を使います。

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

user YAML の `DNSResolver.spec.sources` は受け付けません。旧 inline source は `DNSForwarder` と `DNSUpstream` に分割します。

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
DNS 応答と転送はすべて `routerd-dns-resolver` が担当します。
