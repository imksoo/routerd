---
title: WAN 側サービス
sidebar_position: 4
---

# WAN 側サービス

このページでは、ルーターの WAN 側を扱う routerd リソースを紹介します。
上流リンクの確立、ISP からの IP アドレスとプレフィックスの取得、トンネルの終端、複数の上流経路を controller chain に提供する、といった役割を担います。

LAN 側 (ルーターから内側に提供するサービス) は [LAN 側サービス](./lan-side-services.md) を参照してください。

## サマリ

| 役割 | リソース | 担当デーモン |
| --- | --- | --- |
| 物理 / 仮想インターフェース | `Interface`、`Link`、`IPv4StaticAddress`、`NetworkAdoption` | (kernel) |
| ISP から DHCP で IPv4 を取得 | `DHCPv4Lease` | `routerd-dhcpv4-client` |
| ISP から IPv6 prefix を取得 | `DHCPv6PrefixDelegation`、`IPv6DelegatedAddress` | `routerd-dhcpv6-client` |
| その他の DHCPv6 オプション (DNS、AFTR 等) | `DHCPv6Information` | `routerd-dhcpv6-client` |
| PPPoE セッション | `PPPoESession` | `routerd-pppoe-client` |
| IPv6 上の IPv4 (DS-Lite) | `DSLiteTunnel` | (kernel `ip6tnl`) |
| WAN 経路選択 | `EgressRoutePolicy`、`HealthCheck` | `routerd-healthcheck@<name>` |
| IPv4 NAT (masquerade) | `NAT44Rule` | (nftables) |
| 静的 IPv4 経路 | `IPv4Route` | (kernel) |

ISP の提供形態に応じて、必要なリソースの組み合わせを選びます。

## パターン A: ネイティブデュアルスタック (IPv4 + IPv6)

ISP が同一 WAN インターフェースで IPv4 (DHCPv4) と IPv6 prefix (DHCPv6-PD) を配布する形。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata: {name: wan}
  spec:
    ifname: ens18
    role: untrust

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Lease
  metadata: {name: wan-v4}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata: {name: wan-pd}
  spec:
    interface: wan
    iaid: 1

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata: {name: lan-base}
  spec:
    pdRef: wan-pd
    interface: lan
    suffix: ::1/64

- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata: {name: lan-to-wan}
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.0.2.0/24
```

`DHCPv4Lease` は `routerd-dhcpv4-client` を起動し、リース内容を `lease.json` に書き込みます。アドレス自体は kernel が保持し、routerd は下流リソース向けにイベントを発行します。

`DHCPv6PrefixDelegation` は `routerd-dhcpv6-client` で IA_PD を取得します。`IPv6DelegatedAddress` がそこから LAN 側に配る `/64` (またはその他の長さ) を切り出します。

## パターン B: PPPoE (IPv4) + DHCPv6-PD (IPv6)

旧来の xDSL 系で、IPv4 は PPPoE、IPv6 は同一物理リンクのネイティブ DHCPv6-PD という構成。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata: {name: wan-pppoe}
  spec:
    interface: wan
    user: "user@isp.example"
    passwordFromSecret: pppoe-password
    mtu: 1454
    mru: 1454

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata: {name: wan-pd}
  spec:
    interface: wan
```

`PPPoESession` は `routerd-pppoe-client` を起動し、Linux では `pppd`/`rp-pppoe`、FreeBSD では `ppp(8)` をラップします。PPPoE セッションのインターフェース (通常 `ppp0`) は経路や `NAT44Rule` の対象として参照できます。

## パターン C: DS-Lite (IPv6 のみのアクセス網で IPv4 をトンネルする)

ISP がネイティブで IPv4 を渡さず、IPv6 のみ提供する場合。IPv4 は AFTR (Address Family Transition Router) への DS-Lite トンネルで実現します。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata: {name: wan-pd}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata: {name: wan-info}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata: {name: ds-lite-primary}
  spec:
    sourceInterface: wan
    aftrFQDN: gw.transix.jp
    aftrFQDNResolverFromResource:
      resource: DHCPv6Information/wan-info
      field: dnsServers
    mtu: 1454
```

`DSLiteTunnel` は AFTR アドレスが解決できた時点で kernel の `ip6tnl` デバイスとして作成されます。
AFTR レコードはアクセス網内の DNS でしか解けないことが多いので、`aftrFQDNResolverFromResource` で ISP の DNS を使うようにしてください。

## パターン D: マルチ WAN (主回線 + バックアップ)

複数経路がある場合は、WAN 取得リソースに `EgressRoutePolicy` と `HealthCheck` を組み合わせます。詳細は [マルチ WAN 切替](../how-to/multi-wan.md) を参照。

## 状態確認

各 WAN リソースの状況は `routerctl describe <kind>/<name>` で確認できます。例:

```sh
routerctl describe DHCPv6PrefixDelegation/wan-pd      # phase: Bound, prefix: 2001:db8:1::/56
routerctl describe DSLiteTunnel/ds-lite-primary       # phase: Up, aftr: 2001:db8:cafe::1
routerctl describe EgressRoutePolicy/ipv4-default     # selectedCandidate: ds-lite-primary
```

Web Console の「Overview」「Resources」タブからも同じ情報が見られます。「Connections」タブでは WAN 経路ごとの実 conntrack/pf state を表示します。

## 関連項目

- [LAN 側サービス](./lan-side-services.md)
- [マルチ WAN 切替](../how-to/multi-wan.md)
- [NTT NGN での DS-Lite](../how-to/flets-ipv6-setup.md)
- [Path MTU と MSS clamping](../concepts/path-mtu.md)
