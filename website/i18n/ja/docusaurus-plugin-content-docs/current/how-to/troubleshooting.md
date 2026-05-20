---
title: トラブルシューティング
slug: /how-to/troubleshooting
---

# トラブルシューティング

routerd の調査では、まず **routerd の意図** と **ホストの実状態** を分けます。
routerd が何を意図しているかを確認してから、OS の状態と突き合わせてください。

## 基本順序

1. `routerctl status` — 全体を見る
2. `routerctl describe <kind>/<name>` — 対象リソースを掘る
3. `routerd apply --once --dry-run` — 次の適用で何が変わるか
4. OS コマンド (`ip`、`nft`、`ss`、`journalctl`) — 実状態
5. 該当 daemon の `/v1/status` と event log

## DHCPv6-PD

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
tail -n 20 /var/lib/routerd/dhcpv6-client/wan-pd/events.jsonl
```

確認点：

- `phase` が `Bound`
- `currentPrefix` が入っている
- `renewAt` が未来の時刻
- event log に `Reply` や `Renew` が記録されている

`Bound` でない場合、LAN 側 IPv6 RA、AAAA、DHCPv6 は止まるべきです。
古い prefix を配り続けないことが routerd の安全契約です。

## DHCPv4

```bash
curl --unix-socket /run/routerd/dhcpv4-client/wan.sock http://unix/v1/status
```

`DHCPv4Client` が `Bound` か確認します。
即時更新が必要なら `POST /v1/commands/renew` で要求します。

## dnsmasq

現在の routerd では dnsmasq は DHCPv4、DHCPv6、DHCP relay、Router Advertisement に専念しています。
DNS の応答と転送は `routerd-dns-resolver` が担当します。

生成された dnsmasq 設定が以下を満たしているか確認します：

- 期待する `dhcp-range` が含まれている
- `port=0` (DNS 機能が止まっている — DNS は `routerd-dns-resolver` の責務)
- `dhcp-script=/usr/local/libexec/routerd/dhcp-event-relay` がある (リース変化を routerd へ通知する経路)
- 必要な構成で `enable-ra` が入っている

## DNS resolver

```bash
sudo curl --unix-socket /run/routerd/dns-resolver/<resource>.sock http://unix/v1/healthz
dig @<lan-ip> router.lan.example.org A
dig @<lan-ip> example.com A
```

順に確認：

- 待ち受けが想定アドレスとポートで開いているか (`ss -lnup`)
- ローカル権威ゾーンが応答するか (`DNSZone` の手動レコードと DHCP 由来レコード)
- 条件付き転送が指定上流へ届いているか (`dig @<lan-ip> <forwarded-domain>`)
- 既定上流が DoH / DoT / DoQ / 平文 UDP のいずれで応答しているか (`/v1/status` の `sources[].upstreams` を見る)

## DS-Lite

```bash
ip -6 tunnel show
ip route show default
nft list table ip routerd_nat
```

AFTR FQDN が解決できない場合、`DNSResolver` の `forward` source を確認します。
公衆 DNS では特定アクセス網向けの AFTR レコードは解けないことが多いです。

## conntrack

`/proc/net/nf_conntrack` がない環境があります。
この場合、routerd は sysctl 由来の集計に縮退します。
詳細フロー一覧が空でも、必ずしも NAT が壊れているとは限りません — `routerctl connections` のサマリを見てください。

## 調査時に避けること

- 本番 WAN で、古い DHCP クライアントや手動の試験 daemon を routerd と並行して動かさないでください。同じインターフェースから複数の DHCPv6-PD クライアントを出すと、上流の lease 状態を壊すことがあります。
- 経路変更時に `nf_conntrack` を flush しないでください。routerd は意図的に flush しません。flush すると確立済セッションが切れます。
- 1 ホスト上で `/usr/local/etc/routerd/router.yaml` を編集しつつ、別の場所に ad hoc YAML overlay を置かないでください。1 ホスト 1 設定ファイルを保つと reconcile の予測性が保たれます。

## 関連項目

- [状態と所有権](../concepts/state-and-ownership.md)
- [Reconcile loop](../operations/reconcile)
