---
title: NTT NGN 系アクセス網での DHCPv6-PD と AFTR
---

# NTT NGN 系アクセス網での DHCPv6-PD と AFTR

NTT NGN (日本の IPv6 光回線) のような IPv6 アクセス網に繋がる residential gateway 配下で routerd を使う場合のフィールドノートです。
DHCPv6-PD と、網内 AFTR への DS-Lite を組み合わせる他の carrier にも同じパターンが適用できます。

## DHCPv6-PD

`routerd-dhcpv6-client` はこれら residential gateway 配下で安定して DHCPv6-PD を取得できます。
過剰な再送や特殊な取得手順は不要で、通常の solicit / advertise / request / renew で十分です。

定常状態で観測されること：

- 同じ RGW 配下の複数ルーターが、互いに重ならない prefix を取得する。
- T1 / T2 で Renew が継続的に成功する。
- daemon 再起動でも `lease.json` から lease が復元される。

## DHCPv6 information-request で AFTR が返らない場合がある

一部の RGW / ONU 構成では、DHCPv6 information-request で DNS、SNTP、domain-search は返るが、AFTR option は返りません。空 AFTR は正常です。

この場合、DS-Lite には次のいずれかを明示します：

- `DSLiteTunnel.spec.aftrIPv6` — AFTR の IPv6 アドレスを直接固定。
- `DSLiteTunnel.spec.aftrFQDN` — FQDN を resolve。

## AFTR FQDN は条件付き DNS 転送が必要なことが多い

carrier 管理の AFTR FQDN (例: `gw.transix.jp`) は、carrier 内 DNS でしか解けないことが多いです。公衆 resolver は NXDOMAIN を返します。

routerd では `DNSResolver` の `forward` source で表現します：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: resolver
  spec:
    listen:
      - name: local
        addresses: [127.0.0.1]
        port: 53
    sources:
      - name: aftr
        kind: forward
        match: [transix.jp]
        upstreams:
          - udp://[2404:8e00::feed:101]:53
```

DS-Lite controller は AFTR FQDN を `routerd-dns-resolver` 経由で resolve します。system stub resolver は経由しません。

## DS-Lite end-to-end チェックリスト

DS-Lite が正常動作している場合、以下が見えます：

- 条件付き forwarder が AFTR FQDN を resolve できる。
- `ip6tnl` tunnel device が存在する。
- IPv4 default route が tunnel に向く。
- nftables NAT44 が LAN→外向き IPv4 用に設定されている。
- LAN client から外向き IPv4 (HTTP / ICMP) が成功する。

## 本ノートの位置づけ

これらは routerd の評価環境で carrier が出荷する RGW を使った観測結果です。
類似配備のガイダンスとして利用できますが、すべての日本国内 ISP プランや RGW firmware バージョンに対する保証ではありません。
自身の検証の出発点として扱ってください。
