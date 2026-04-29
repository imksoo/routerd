---
title: LAN 側サービス
sidebar_position: 3
---

# LAN 側サービス

[最初のルーター](./first-router) でホストは WAN に出られて LAN にアドレスが付きました。
ここでは LAN クライアントが期待するサービス、つまり DHCPv4 リース、DNS、IPv6 RA を
追加します。routerd はこれを `IPv4DHCPServer` + `IPv4DHCPScope` (および IPv6 の対) を
通じて、管理対象 `dnsmasq` インスタンスとして実現します。

## 何を足すか

- LAN にバインドする dnsmasq インスタンスを起動する `IPv4DHCPServer`。WAN の上流
  リゾルバへ DNS をフォワードします。
- LAN クライアントにリース範囲とゲートウェイを渡す `IPv4DHCPScope`。
- 任意の IPv6: 上流にプレフィックスを要求する `IPv6PrefixDelegation`、LAN に `/64`
  を載せる `IPv6DelegatedAddress`、ステートレス DHCPv6 / RA のための
  `IPv6DHCPServer` + `IPv6DHCPScope`。

## 1. IPv4 DHCP サーバとスコープを追加する

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4DHCPServer
      metadata:
        name: dhcp4
      spec:
        server: dnsmasq
        managed: true
        listenInterfaces:
          - lan
        dns:
          enabled: true
          upstreamSource: dhcp4
          upstreamInterface: wan
          cacheSize: 1000

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4DHCPScope
      metadata:
        name: lan-dhcp4
      spec:
        server: dhcp4
        interface: lan
        rangeStart: 192.168.10.100
        rangeEnd: 192.168.10.199
        leaseTime: 12h
        routerSource: interfaceAddress
        dnsSource: self
        authoritative: true
```

LAN クライアントに渡るもの:

- `192.168.10.100–199` の DHCPv4 リース。ゲートウェイは `192.168.10.1`
  (`routerSource: interfaceAddress` でルーターの LAN アドレスを広告するため)
- `192.168.10.1` への DNS。`upstreamSource: dhcp4` で、WAN の DHCPv4 で学習した
  リゾルバにフォワード

## 2. apply と確認

```bash
sudo routerd apply --once \
  --config /usr/local/etc/routerd/router.yaml
```

dnsmasq の動作確認:

```bash
sudo systemctl status routerd-dnsmasq-dhcp4.service
ss -lntu | grep -E ':(53|67)\b'
```

LAN クライアント側から:

```bash
# 192.168.10.100-199 の範囲でリースが付くはず
sudo dhclient -v <lan-iface>

# ルーター経由の DNS 解決
dig @192.168.10.1 example.com
```

## 3. IPv6 を追加する (任意、上流の PD が必要)

上流が IPv6 プレフィックス委譲をしている場合 (光回線の多くがそうです)、LAN に
IPv6 プレフィックスを伸ばせます。

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv6PrefixDelegation
      metadata:
        name: wan-pd
      spec:
        interface: wan
        client: networkd
        prefixLength: 60

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv6DelegatedAddress
      metadata:
        name: lan-ipv6
      spec:
        prefixDelegation: wan-pd
        interface: lan
        subnetID: "0"
        addressSuffix: "::1"
        announce: true
```

これで上流に `/60` を要求し、最初の `/64` を LAN に割り当て、ホスト部 `::1` を
付けます。`announce: true` で、続けて足す DHCPv6/RA 経路から LAN クライアントに
広告されます。

NTT フレッツ系の上流ならプロファイルを指定すると妥当な既定値が引き当たります。

```yaml
        profile: ntt-hgw-lan-pd
```

NTT 特有のラボ環境上の落とし穴は [FLET'S IPv6 設定](../how-to/flets-ipv6-setup) を
参照してください。

## 4. IPv6 DHCP サーバと RA を追加する

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv6DHCPServer
      metadata:
        name: dhcp6
      spec:
        server: dnsmasq
        managed: true
        listenInterfaces:
          - lan
        ra:
          enabled: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv6DHCPScope
      metadata:
        name: lan-dhcp6
      spec:
        server: dhcp6
        interface: lan
        mode: stateless
```

dnsmasq が LAN にプレフィックスを RA で広告し、DHCPv6 で DNS 情報を要求してくる
クライアントにステートレス DHCPv6 で応答します。

## 5. LAN 側 IPv6 を確認

apply 後、LAN 側に委譲プレフィックスから導いた IPv6 アドレスが付きます。

```bash
ip -6 addr show ens19
# 委譲プレフィックスから導いたグローバル /64 が出るはず
```

LAN クライアントは SLAAC で IPv6 アドレスを取得します。

## まだやっていないこと

LAN クライアントは名前解決ができ、アドレスが取れますが、トラフィックはまだ意味の
ある場所には届きません。IPv4 の NAT もファイアウォールも無いからです。それは
[次のチュートリアル](./basic-firewall) で扱います。

## 次へ

- [基本のファイアウォール](./basic-firewall) — IPv4 NAT と既定拒否の構え
- [API リファレンス: IPv4DHCPServer / Scope](../reference/api-v1alpha1#ipv4dhcpserver-と-ipv4dhcpscope)
- [API リファレンス: IPv6PrefixDelegation](../reference/api-v1alpha1#ipv6prefixdelegation)
