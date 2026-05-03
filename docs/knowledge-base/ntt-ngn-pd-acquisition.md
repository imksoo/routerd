# NTT NGN HGW 配下の DHCPv6-PD と AFTR

このページは、現在のラボで得た NTT NGN ホームゲートウェイ配下の要点をまとめます。
過去の不安定な仮想ネットワークでの推測ではなく、pve05 から pve07 の安定ラボを基準にします。

## DHCPv6-PD

`routerd-dhcpv6-client` は、HGW 配下で通常の DHCPv6-PD を取得できます。
現在の運用では、過激な再送や特殊な取得手順は不要です。
通常の取得、lease 保存、起動時復元、Renew で十分です。

確認済み:

- router01 から router05 まで Bound を維持しています。
- 複数回の T1 通過後も Renew が成功しています。
- プレフィックス重複は解消済みです。
- 5 台間の IPv6 疎通を確認済みです。

## AFTR は情報要求で返らない場合があります

利用者の回線と ONU/HGW の組み合わせでは、DHCPv6 情報要求に AFTR option が返りません。
DNS、SNTP、domain-search は返ることがありますが、AFTR は空であることが正常です。

そのため、DS-Lite では次のどちらかを使います。

- `DSLiteTunnel.spec.aftrIPv6`
- `DSLiteTunnel.spec.aftrFQDN`

## AFTR FQDN は条件付き転送が必要です

`gw.transix.jp` などの AFTR FQDN は、公開 DNS では解決できない場合があります。
HGW が広告する RDNSS へ問い合わせる必要があります。

routerd では `DNSResolverUpstream` の `zones` で条件付き転送を表します。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolverUpstream
  metadata:
    name: resolver
  spec:
    zones:
      - zone: transix.jp
        servers:
          - 2404:8e00::feed:101
```

DS-Lite の AFTR 解決は、routerd 管理 dnsmasq を経由します。
system resolver へ直接問い合わせません。

## DS-Lite 実適用の確認

router05 では次を確認済みです。

- 条件付き転送で AFTR FQDN を解決しました。
- `ip6tnl` トンネルを作成しました。
- IPv4 既定経路を DS-Lite トンネルへ向けました。
- nftables の NAT44 を適用しました。
- IPv4 HTTP 通信が成功しました。

## 過去の不安定経路

pve01 から pve04 の vmbr0 VLAN 1901 経路では、マルチキャストや DHCPv6-PD が不安定に見えました。
現在は、その経路を設計判断の根拠にしません。
実装検証は pve05 から pve07 のラボで行います。
