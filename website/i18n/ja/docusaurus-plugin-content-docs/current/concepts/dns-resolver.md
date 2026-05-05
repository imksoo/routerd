---
title: DNS リゾルバー
slug: /concepts/dns-resolver
---

# DNS リゾルバー

Phase 2.0 以降、routerd の DNS は 2 種類のリソースに分かれています。

`DNSZone` はローカル権威データを持ちます。
手動レコードと、DHCP リースから派生したレコードを保存します。

`DNSResolver` はデーモンインスタンスを管理します。
待ち受けアドレス、応答元の順序、上流選択、キャッシュ方針を定義します。
1 つの `DNSResolver` リソースが、1 つの `routerd-dns-resolver` プロセスを起動します。

## 応答元の順序

`DNSResolver.spec.sources` は上から順に評価されます。
`zone` は `DNSZone` から応答します。
`forward` は一致したゾーンを選択された上流へ送ります。
`upstream` は既定の再帰問い合わせ経路です。

リゾルバーは DoH、DoT、DoQ、平文 UDP DNS を扱います。
上流は優先順に試します。
優先度の高い上流が失敗した場合は、次の上流へ切り替えます。

## 複数の待ち受けプロファイル

`spec.listen` は配列です。
各待ち受けは、利用する応答元名の部分集合を選べます。
これにより、LAN と VPN の待ち受けで違う挙動を持たせられます。
それでも 1 つのリゾルバーリソースを共有できます。

待ち受けアドレスがほかのリソース状態から来る場合は、`listen[].addressFrom` を使います。
依存関係が明示されるため、元リソースが変化したときにデーモンを再構成できます。

```yaml
listen:
  - name: lan
    addresses:
      - 172.18.0.1
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
    ipv4: 172.18.0.1
    ipv6From:
      resource: IPv6DelegatedAddress/lan-base
      field: address
```

必要な参照先がまだ解決できない場合、そのレコードは `DNSZone.status.pendingRecords`
に記録されます。
参照先リソースが変化するとリゾルバーを再生成し、解決後にレコードを公開します。

## ネットワークが制限された上流

`sources[].viaInterface` は、Linux で送信先インターフェースを束縛します。
`ens18` や `wg0` のような OS インターフェース名を固定値で指定します。
トンネルや VRF リソースがそのインターフェースを作る場合は、リソース所有や順序で依存関係を明示します。
インターフェースが存在するまで、リゾルバーを待機させます。

`sources[].bootstrapResolver` は、DoH や DoT の接続先名を解決する補助 DNS サーバーです。
接続先名がアクセス網の内側でしか解決できない場合に使います。

上流サーバー一覧がほかのリソース状態から来る場合は、`upstreamFrom` を使います。

```yaml
sources:
  - name: ngn-aftr
    kind: forward
    match:
      - transix.jp
    upstreamFrom:
      - resource: DHCPv6Information/wan-info
        field: dnsServers
```

## dnsmasq との境界

dnsmasq は DHCPv4、DHCPv6、DHCP 中継、RA に限定します。
`server=`、`local=`、`host-record=` は生成しません。
DNS 応答と転送はすべて `routerd-dns-resolver` が担当します。
