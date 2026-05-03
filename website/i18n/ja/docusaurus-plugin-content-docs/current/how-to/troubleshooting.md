---
title: トラブルシューティング
slug: /how-to/troubleshooting
---

# トラブルシューティング

routerd の調査では、まず routerd の見方とホストの見方を分けます。
routerd が何を意図しているかを確認し、その後で OS の状態と比べます。

## 基本順序

1. `routerctl status` で全体を見ます。
2. `routerctl describe <kind>/<name>` で対象リソースを見ます。
3. `routerd apply --once --dry-run` で、これから何を変更するかを見ます。
4. OS コマンドで実状態を確認します。
5. 専用デーモンの `/v1/status` と events を確認します。

## DHCPv6-PD

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
tail -n 20 /var/lib/routerd/dhcpv6-client/wan-pd/events.jsonl
```

見る点:

- `phase` が `Bound` か。
- `currentPrefix` が入っているか。
- `renewAt` が未来の時刻か。
- events に Reply や Renew が記録されているか。

`Bound` でない場合、LAN 側の IPv6 RA、AAAA、DHCPv6 は止まるべきです。
古くなったプレフィックスを配り続けないことが routerd の安全条件です。

## DHCPv4

```bash
curl --unix-socket /run/routerd/dhcpv4-client/wan.sock http://unix/v1/status
```

`DHCPv4Lease` が Bound になっているか確認します。
必要なら `POST /v1/commands/renew` で即時更新を依頼します。

## dnsmasq

Phase 2.0 以降、dnsmasq は DHCPv4、DHCPv6、DHCP 中継、RA だけを担当します。
DNS の応答と転送は `routerd-dns-resolver` が受け持ちます。

確認する点:

- 生成された設定に期待する `dhcp-range` があるか。
- `port=0` で DNS 機能が止まっているか (DNS は `routerd-dns-resolver` の責務)。
- `dhcp-script=/usr/local/libexec/routerd/dhcp-event-relay` が含まれているか (DHCP リース変化を routerd へ通知する経路)。
- `enable-ra` が必要な構成で入っているか。

## DNS リゾルバー

```bash
sudo curl --unix-socket /run/routerd/dns-resolver/<resource>.sock http://unix/v1/healthz
dig @192.168.160.5 router.lab.example A
dig @192.168.160.5 nwadmin03.lab.example A
```

`DNSResolver` の動作確認は次の流れで行います。

- 待ち受けが想定アドレスとポートで開いているか (`ss -lnup`)。
- ローカル権威ゾーンが応答するか (`DNSZone` の手動レコードと DHCP 由来レコード)。
- 条件付き転送が指定上流へ届いているか (`forward` 応答元、たとえば `transix.jp`)。
- 既定上流が DoH / DoT / DoQ / 平文 UDP のいずれで応答しているか (`/v1/status` の `sources[].upstreams` を見ます)。

## DS-Lite

```bash
ip -6 tunnel show
ip route show default
nft list table ip routerd_nat
```

AFTR FQDN が解決できない場合、`DNSResolver` の `forward` 応答元を確認します。
公開 DNS では AFTR FQDN が解決できない環境があります。

## conntrack

`/proc/net/nf_conntrack` が存在しない環境があります。
この場合、routerd は sysctl 由来の集計へ縮退します。
詳細なフロー一覧が空でも、必ずしも NAT が壊れているとは限りません。

## 調査で避けること

本番 WAN で、古い DHCP クライアントや手動の試験デーモンを同時に動かさないでください。
同じインターフェースから複数の DHCPv6-PD クライアントを出すと、HGW 側の状態を壊すことがあります。
