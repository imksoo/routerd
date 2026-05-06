---
title: 設計メモ
---

# 設計メモ

このドキュメントは routerd で残すべき設計判断の記録です。
過去の試行錯誤の時系列ログではなく、現在のコードが従っている原則と、今後の変更で守るべき指針だけを残します。

## 1. Daemon contract

状態を持つ処理は専用 daemon が担当します。
ツール側が一様に扱えるよう、すべての daemon は同じ surface を公開します：

- Unix domain socket 上の HTTP+JSON API
- `/v1/status`
- `/v1/healthz`
- `/v1/events`
- `/v1/commands/reload`
- `/v1/commands/renew`
- `/v1/commands/stop`
- state または lease ファイル
- `events.jsonl` (append-only)

この contract は `routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` で共通です。

## 2. DHCPv6-PD

DHCPv6-PD は `routerd-dhcpv6-client` が所有します。OS 付属クライアント向けに設定を生成する経路は、もう存在しません。

通常の residential gateway 環境では、標準の solicit / advertise / request / renew + lease 永続化 + T1 renew で十分です。
壊れた環境を回避するための過剰な再送は、現在の規定では使いません。

## 3. 誠実な LAN 広告

DHCPv6-PD が `Bound` でない場合、routerd は LAN へ古い IPv6 情報を出しません。
RA、DHCPv6 server、AAAA レコード、prefix から導出した LAN アドレスのすべてに適用されます。
「明らかに壊れた状態を維持する」方針です。届かない prefix を配り続けません。

## 4. DS-Lite

一部のアクセス網では DHCPv6 information-request で AFTR option が返りません。
そのため `DSLiteTunnel` は、`aftrFQDN` または `aftrIPv6` の静的指定を fallback ではなく正規経路として扱います。

AFTR FQDN は公衆 DNS で解けないことが多いです。`DNSResolver.spec.sources[].kind: forward` で carrier 内 resolver に転送してください。

## 5. Event 連携

routerd は in-process bus を持ちます。controller は event を受けて影響範囲のリソースだけを再評価します。

高位連携には次の Kind を使います：

- `EgressRoutePolicy`
- `EventRule`
- `DerivedEvent`
- `HealthCheck`

`EventRule` は event 列を入力にして別の event 列を生成します。
`DerivedEvent` は観測状態から asserted / retracted の仮想 event を合成します。

## 6. Tier S 構成要素

WireGuard、IPsec、VRF、VXLAN は Tier S (SOHO / branch) の構成要素です。
WireGuard と VXLAN-over-WireGuard は対応 OS 間での相互運用を確認済みです。

抽象的な `VPNTunnel` リソースは作りません。
WireGuard、IPsec、将来の Tailscale や SoftEther 統合は、それぞれ別 Kind として追加します。
意図は、各々の状態機械が大きく異なるためで、polymorphic な 1 Kind に潰すと semantics を失うからです。

## 7. 残課題

- 状態を持つ firewall の本番運用：適用は始まったが、ルール表現力 / ICMP type 一致 / 複数 port / rate limit を拡張する必要がある。
- LAN 向け DoH 代理。
- Tier C のための FRR 経由 BGP / OSPF 統合。
- 高可用 (leader 選出、耐障害 control plane)。
- 本番 observability (OpenTelemetry collector + 遠隔 log sink)。
- 家庭用回線で routerd を唯一の WAN ルーターとして長時間運用する検証。
