---
title: NTT NGN 系アクセス網での DHCPv6-PD と AFTR
---

# NTT NGN 系アクセス網での DHCPv6-PD と AFTR

![Diagram showing DHCPv6-PD and AFTR acquisition on NTT NGN-style access from prefix delegation and information request through carrier DNS AFTR resolution to DS-Lite tunnel, IPv4 route, NAT44, and LAN connectivity checks](/img/diagrams/knowledge-base-ntt-ngn-pd-acquisition.png)

NTT NGN (日本の IPv6 光回線) のような IPv6 アクセス網につながる HGW 配下で routerd を使う場合のフィールドノートです。
DHCPv6-PD と、網内 AFTR への DS-Lite を組み合わせる他の事業者にも、同じパターンが適用できます。

## DHCPv6-PD

`routerd-dhcpv6-client` は、これらの HGW 配下で安定して DHCPv6-PD を取得できます。
過剰な再送や特殊な取得手順は不要で、通常の solicit / advertise / request / renew で十分です。

定常状態では、次のように観測できます。

- 同じ HGW 配下の複数のルーターが、互いに重ならないプレフィックスを取得します。
- T1 / T2 のタイミングで、Renew が継続して成功します。
- デーモンを再起動しても、`lease.json` からリースが復元されます。

## DHCPv6 の information-request で AFTR が返らない場合がある

一部の HGW / ONU の構成では、DHCPv6 の information-request で DNS、SNTP、domain-search は返るものの、AFTR オプションは返りません。AFTR が空であること自体は正常です。

この場合、DS-Lite には次のいずれかを明示します。

- `DSLiteTunnel.spec.aftrIPv6` — AFTR の IPv6 アドレスを直接固定します。
- `DSLiteTunnel.spec.aftrFQDN` — FQDN を解決します。

## AFTR の FQDN は条件付き DNS 転送が必要なことが多い

事業者が管理する AFTR の FQDN (例: `gw.transix.jp`) は、事業者内の DNS でしか解けないことが多いです。公衆のリゾルバーは NXDOMAIN を返します。

routerd では、`DNSResolver` の `forward` source で表現します。

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

DS-Lite コントローラーは、AFTR の FQDN を `routerd-dns-resolver` 経由で解決します。システムの stub resolver は経由しません。

## DS-Lite の end-to-end チェックリスト

DS-Lite が正常に動作している場合は、次のように見えます。

- 条件付き転送が、AFTR の FQDN を解決できる。
- `ip6tnl` のトンネルデバイスが存在する。
- IPv4 のデフォルト経路がトンネルへ向く。
- nftables の NAT44 が、LAN から外向きの IPv4 用に設定されている。
- LAN クライアントから、外向きの IPv4 (HTTP / ICMP) が成功する。

## 本ノートの位置づけ

これらは、routerd の評価環境で、事業者が出荷する HGW を使って得た観測結果です。
類似の配備に向けたガイダンスとして利用できますが、国内のすべての ISP プランや HGW のファームウェアバージョンに対する保証ではありません。
ご自身の検証の出発点として扱ってください。
