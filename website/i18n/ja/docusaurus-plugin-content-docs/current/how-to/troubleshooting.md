---
title: トラブルシューティング
slug: /how-to/troubleshooting
---

# トラブルシューティング

![routerctl status と dry-run intent から OS state、daemon socket、event、DHCP/DNS/conntrack check へ進む routerd troubleshooting order](/img/diagrams/how-to-troubleshooting.png)

routerd の調査では、まず **routerd の意図** と **ホストの実状態** を分けます。
routerd が何を意図しているかを確認してから、OS の状態と突き合わせてください。

## 基本順序

1. `routerctl status` — 全体を見る
2. `routerctl describe <kind>/<name>` — 対象リソースを掘り下げる
3. `routerctl plan` — 次の適用で何が変わるか
4. OS コマンド (`ip`、`nft`、`ss`、`journalctl`) — 実状態
5. 該当デーモンの `/v1/status` とイベントログ

## DHCPv6-PD

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
tail -n 20 /var/lib/routerd/dhcpv6-client/wan-pd/events.jsonl
```

確認点は次の通りです。

- `phase` が `Bound` になっている
- `currentPrefix` が入っている
- `renewAt` が未来の時刻になっている
- イベントログに `Reply` や `Renew` が記録されている

`Bound` でない場合、LAN 側の IPv6 RA、AAAA、DHCPv6 は止まるべきです。
古いプレフィックスを配り続けないことが、routerd の安全上の約束です。

## DHCPv4

```bash
curl --unix-socket /run/routerd/dhcpv4-client/wan.sock http://unix/v1/status
```

`DHCPv4Client` が `Bound` かどうかを確認します。
即時の更新が必要なら、`POST /v1/commands/renew` で要求します。

## dnsmasq

現在の routerd では、dnsmasq は DHCPv4、DHCPv6、DHCP 中継、Router Advertisement に専念しています。
DNS の応答と転送は、`routerd-dns-resolver` が担当します。

生成された dnsmasq の設定が、以下を満たしているか確認します。

- 期待する `dhcp-range` が含まれている
- `port=0` になっている (DNS 機能が止まっている。DNS は `routerd-dns-resolver` の責務です)
- `dhcp-script=/usr/local/libexec/routerd/dhcp-event-relay` がある (リースの変化を routerd へ通知する経路)
- 必要な構成で `enable-ra` が入っている

## DNS リゾルバー

```bash
sudo curl --unix-socket /run/routerd/dns-resolver/<resource>.sock http://unix/v1/healthz
dig @<lan-ip> router.lan.example.org A
dig @<lan-ip> example.com A
```

順に確認します。

- 待ち受けが、想定したアドレスとポートで開いているか (`ss -lnup`)
- ローカルの権威ゾーンが応答するか (`DNSZone` の手動レコードと DHCP 由来のレコード)
- 条件付き転送が、指定した上流へ届いているか (`dig @<lan-ip> <forwarded-domain>`)
- 既定の上流が、DoH / DoT / TCP / 平文 UDP のいずれで応答しているか (リゾルバーの status と上流の health を見る)

## DS-Lite

```bash
ip -6 tunnel show
ip route show default
nft list table ip routerd_nat
```

AFTR の FQDN が解決できない場合は、`DNSResolver` の `forward` source を確認します。
公衆 DNS では、特定のアクセス網向けの AFTR レコードを解けないことが多いです。

## conntrack

環境によっては、`/proc/net/nf_conntrack` がありません。
この場合、routerd は sysctl 由来の集計へ縮退します。
詳細なフロー一覧が空でも、必ずしも NAT が壊れているとは限りません。`routerctl connections` のサマリーを見てください。

## 調査時に避けること

- 本番 WAN で、古い DHCP クライアントや手動の試験用デーモンを、routerd と並行して動かさないでください。同じインターフェースから複数の DHCPv6-PD クライアントを出すと、上流のリース状態を壊すことがあります。
- 経路変更時に `nf_conntrack` を flush しないでください。routerd は意図的に flush しません。flush すると、確立済みのセッションが切れます。
- 1 ホスト上で `/usr/local/etc/routerd/router.yaml` を編集しつつ、別の場所にその場しのぎの YAML オーバーレイを置かないでください。1 ホストにつき設定ファイルを 1 つに保つと、調整（リコンサイル）の予測性が保たれます。

## 関連項目

- [状態と所有権](../concepts/state-and-ownership.md)
- [リコンサイルループ](../operations/reconcile)
