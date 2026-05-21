---
title: 設定事例集
sidebar_position: 0
---

# 設定事例集

このセクションは、小さく写経しやすい router 構成パターンを集めたものです。
設計ドキュメントというより、機器ベンダーの設定事例集に近い形式にしています。
各ページは構成図から始め、現在 routerd が管理できる範囲を明示し、そのあとで
最小限の YAML を示します。

ここにある設定は出発点です。本番投入前に、interface 名、アドレス範囲、ISP 固有値、
管理アクセスの経路を必ず自分の環境に合わせてください。

## 読み方

各事例は同じ流れで読めるようにしています。

1. **構成図**: 物理構成または論理構成。
2. **図の対応表**: 図の番号が何を表すか。
3. **設定例**: 完全な YAML は `examples/` に置き、ページ内では番号付きで要点を抜粋。
4. **適用手順**: 先に実行する validate、plan、dry-run。
5. **確認方法**: 収束したことを確認するコマンド。

構成図の `[1]` と YAML comment の `# [1]` は同じ対象を指します。
図を見ながら、どの resource がどの場所を管理するのか追えるようにしています。

## すぐ試せる事例

| 事例 | 状態 | 使う場面 |
| --- | --- | --- |
| [基本的な IPv4 NAT ルーター](./basic-ipv4-nat.md) | 現在の実装で利用可能 | WAN は DHCPv4、LAN は private IPv4 と DHCPv4 で構成する。 |
| [LAN DHCP とローカル DNS](./lan-dns-dhcp.md) | 現在の実装で利用可能 | 1 つの LAN で DHCPv4、ローカル DNS zone、DHCP 由来の名前を配りたい。 |
| [DS-Lite ホームルーター](./dslite-home.md) | ISP 固有値を入れれば現在の実装で利用可能 | IPv6 を主回線として使い、IPv4 は DS-Lite tunnel に通す。 |
| [PPPoE IPv4 NAT ルーター](./pppoe-ipv4-nat.md) | ISP 認証情報を入れれば現在の実装で利用可能 | Ethernet の WAN 上に PPPoE session を張って IPv4 internet に出る。 |
| [内部 Web server への port forward](./port-forward-web.md) | WAN address が分かっていれば現在の実装で利用可能 | 内部の HTTPS server を 1 つ公開し、LAN からも同じ公開名で到達したい。 |
| [BGP 付き Kubernetes API VIP](./kubernetes-api-vip.md) | embedded GoBGP と keepalived で現在の実装で利用可能 | Kubernetes API VIP を routerd が保持し、control plane を health check し、Service prefix を BGP で受けたい。 |
| [Guest / IoT client の分離](./guest-isolation.md) | Linux nftables で利用可能 | 一部の MAC address だけ internet 可、LAN と管理網は不可にしたい。 |
| [Firewall rate limit と ICMP rule](./firewall-rate-limit.md) | Linux nftables で利用可能 | 複数 port の service opening、ICMP type match、SSH brute-force 緩和を使いたい。 |
| [Multi-WAN IPv4 failover](./multi-wan-failover.md) | 現在の実装で利用可能。health check は慎重に調整 | 複数の IPv4 出口から正常な default route を選びたい。 |
| [Public DNS を local resolver に redirect](./local-dns-redirect.md) | Linux nftables で利用可能 | LAN client が平文 DNS を外へ直接投げるのを router の DNS に集約したい。 |
| [Tailscale subnet / exit node](./tailscale-subnet-exit.md) | Tailscale が利用できる環境で利用可能 | LAN route や exit node を tailnet に広告したい。 |
| [WireGuard hub-and-spoke template](./wireguard-hub-spoke.md) | 鍵と peer route を置き換える template | routed WireGuard hub の出発点が欲しい。 |
| [OTLP collector への telemetry export](./telemetry-export.md) | collector があれば利用可能 | routerd の logs、metrics、traces を観測基盤へ送りたい。 |

## まだ ready-to-run とは書かない事例

初めて触る人には重要ですが、対応する renderer や運用指針が揃うまで
そのまま適用できる YAML としては出さないものです。

| パターン | 現状 |
| --- | --- |
| MAP-E / v6plus 系 IPv4 over IPv6 | まだ一級 resource としては未実装です。 |
| OSPF など BGP 以外の動的 routing | 未実装です。Kubernetes 風の Service prefix import には embedded GoBGP を利用できます。 |
| IPsec site-to-site cookbook | IPsec の土台はありますが、本番 renderer の parity を完了済みとは書いていません。 |

## 安全チェック

実利用中の router に適用する前に、必ず次を確認してください。

- console または hypervisor から入れる経路を残す。
- 管理通信がどの interface を通っているか把握する。
- `routerd validate`、`routerd plan`、dry-run apply を先に実行する。
- plan が管理 interface の address、route、firewall opening を消さないことを確認する。
- router 上にインストールした release binary で apply し、別の開発 tree から実行しない。

```bash
routerd validate --config router.yaml
routerd plan --config router.yaml
routerd apply --config router.yaml --once --dry-run
routerd apply --config router.yaml --once
routerctl status
```

## 関連ページ

- [最初の router を起動する](../tutorials/first-router.md)
- [WAN 側サービス](../tutorials/wan-side-services.md)
- [LAN 側サービス](../tutorials/lan-side-services.md)
- [基本的な NAT と firewall policy](../tutorials/basic-firewall.md)
