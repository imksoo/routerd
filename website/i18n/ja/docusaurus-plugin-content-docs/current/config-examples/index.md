---
title: 設定事例集
sidebar_position: 0
---

# 設定事例集

このセクションは、小さく写経しやすいルーター構成のパターンを集めたものです。
設計ドキュメントというより、機器ベンダーの設定事例集に近い形式にしています。
各ページは構成図から始め、routerd が管理できる範囲を示したうえで、最小限の YAML を載せます。

ここにある設定は出発点です。
本番に投入する前に、インターフェース名、アドレス範囲、ISP 固有値、管理アクセスの経路を、必ず自分の環境に合わせてください。

![構成図の番号、対応表、YAML 抜粋、ローカル編集、validate-plan-dry-run、apply、routerctl 確認の流れを示す設定事例の読み方図](/img/diagrams/config-example-workflow.png)

:::tip 標準の出発点
家庭ルーターを routerd で置き換える場合は、
[`examples/home-router-mgmt-protected.yaml`](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
を出発点にしてください。3 ロールのファイアウォール（untrust / trust / mgmt）、
DS-Lite 優先 + PPPoE フォールバック、apply 時のロックアウト防止のための
`ManagementAccess`、管理アドレスへ bind した `WebConsole` を含む、
**安全最小の標準構成**です。インターフェースと ISP を自環境に合わせ、
下の安全チェックの順序で適用してください。
:::

## 読み方

各事例は同じ流れで読めるようにしています。

1. **構成図**: 物理構成または論理構成。
2. **図の対応表**: 図の番号が何を表すか。
3. **設定例**: 完全な YAML は `examples/` に置き、ページ内では番号付きで要点を抜粋します。
4. **適用手順**: 先に実行する validate、plan、dry-run。
5. **確認方法**: 収束したことを確認するコマンド。

構成図の `[1]` と YAML コメントの `# [1]` は同じ対象を指します。
図を見ながら、どのリソースがどの場所を管理するのか追えるようにしています。

## すぐ試せる事例

| 事例 | 状態 | 使う場面 |
| --- | --- | --- |
| [基本的な IPv4 NAT ルーター](./basic-ipv4-nat.md) | 現在の実装で利用可能 | WAN は DHCPv4、LAN はプライベート IPv4 と DHCPv4 で構成したい。 |
| [LAN DHCP とローカル DNS](./lan-dns-dhcp.md) | 現在の実装で利用可能 | 1 つの LAN で DHCPv4、ローカル DNS ゾーン、DHCP 由来の名前を配りたい。 |
| [DS-Lite ホームルーター](./dslite-home.md) | ISP 固有値を入れれば現在の実装で利用可能 | IPv6 を主回線として使い、IPv4 は DS-Lite tunnel に通したい。 |
| [PPPoE IPv4 NAT ルーター](./pppoe-ipv4-nat.md) | ISP 認証情報を入れれば現在の実装で利用可能 | Ethernet の WAN 上に PPPoE セッションを張って IPv4 インターネットに出たい。 |
| [内部 Web サーバーへのポートフォワード](./port-forward-web.md) | WAN アドレスが分かっていれば現在の実装で利用可能 | 内部の HTTPS サーバーを 1 つ公開し、LAN からも同じ公開名で到達したい。 |
| [BGP 付き Kubernetes API VIP](./kubernetes-api-vip.md) | `routerd-bgp` GoBGP と keepalived で現在の実装で利用可能 | Kubernetes API VIP を routerd が保持し、control plane をヘルスチェックし、Service prefix を BGP で受けたい。 |
| [ゲスト / IoT 端末の分離](./guest-isolation.md) | Linux nftables で利用可能 | 一部の MAC アドレスだけインターネットを許可し、LAN と管理網へは届かせたくない。 |
| [ファイアウォールのレート制限と ICMP ルール](./firewall-rate-limit.md) | Linux nftables で利用可能 | 複数ポートのサービス開放、ICMP type のマッチ、SSH ブルートフォース緩和を使いたい。 |
| [Multi-WAN IPv4 failover](./multi-wan-failover.md) | 現在の実装で利用可能。ヘルスチェックは慎重に調整 | 複数の IPv4 出口から正常な default route を選びたい。 |
| [パブリック DNS をローカルリゾルバーへリダイレクト](./local-dns-redirect.md) | Linux nftables で利用可能 | LAN クライアントが平文 DNS を外へ直接投げるのを、ルーターの DNS に集約したい。 |
| [Tailscale subnet / exit node](./tailscale-subnet-exit.md) | Tailscale が利用できる環境で利用可能 | LAN の経路や exit node を tailnet に広告したい。 |
| [WireGuard ハブ＆スポーク template](./wireguard-hub-spoke.md) | 鍵と peer の経路を置き換える template | routed な WireGuard hub の出発点が欲しい。 |
| [OTLP collector への telemetry エクスポート](./telemetry-export.md) | collector があれば利用可能 | routerd の logs、metrics、traces を観測基盤へ送りたい。 |

## まだそのまま実行できるとは書かない事例

対応する生成（レンダリング）や運用指針が揃うまで、そのまま適用できる YAML としては出さないものです。

| パターン | 現状 |
| --- | --- |
| MAP-E / v6plus 系の IPv4 over IPv6 | 一級リソースとしては未実装。 |
| OSPF など BGP 以外の動的ルーティング | 未実装。Kubernetes 風の Service prefix インポートには `routerd-bgp` GoBGP を利用可能。 |
| IPsec site-to-site cookbook | IPsec の土台はあるが、本番向けの生成（レンダリング）が同等水準に達していない。 |

## 安全チェック

実利用中のルーターに適用する前に、必ず次を確認してください。

- コンソールまたは hypervisor から入れる経路を残す。
- 管理通信がどのインターフェースを通っているか把握する。
- `routerctl validate` と `routerctl plan` を先に実行する。
- plan が管理インターフェースのアドレス、経路、ファイアウォールの開放を消さないことを確認する。
- ルーター上にインストールしたリリースバイナリで適用し、別の開発ツリーからは実行しない。

```bash
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
routerctl status
```

## 関連ページ

- [最初のルーターを起動する](../tutorials/first-router.md)
- [WAN 側サービス](../tutorials/wan-side-services.md)
- [LAN 側サービス](../tutorials/lan-side-services.md)
- [基本的な NAT とファイアウォールポリシー](../tutorials/basic-firewall.md)
