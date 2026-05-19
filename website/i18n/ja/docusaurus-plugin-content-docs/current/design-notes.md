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

WireGuard、Tailscale、IPsec、VRF、VXLAN は Tier S (SOHO / branch) の構成要素です。
WireGuard と VXLAN-over-WireGuard は対応 OS 間での相互運用を確認済みです。
`TailscaleNode` は exit node と subnet router の広告を扱います。
VPN をすべて抽象的な 1 種類の形に押し込まないためです。

抽象的な `VPNTunnel` リソースは作りません。
WireGuard、Tailscale、IPsec、将来の SoftEther 統合は、それぞれ別 Kind として追加します。
各々の状態機械は大きく異なります。
多態的な 1 Kind にまとめると意味を失うためです。

## 7. OpenRC service rendering

Alpine は systemd ではなく OpenRC を使います。
OpenRC 対応は、まず applier ではなく renderer として始めます。
`routerd render alpine --out-dir` は review 可能な init script と関連設定を書き出し、routerd が OpenRC 状態を変更する前に installed host の挙動を確認できるようにします。

最初に対応する OpenRC surface は狭く保ちます：

- 明示的な `SystemdUnit` resource から OpenRC script への変換
- `routerd-healthcheck` script の自動生成
- DHCP または RA resource が dnsmasq を必要とする場合の managed dnsmasq script 自動生成
- DHCPv4 / DHCPv6 client、firewall logger、PPPoE、Tailscale の script 自動生成
- DNS resolver script。resolver runtime config を controller loop 外で materialize できるまでは enable / start しません

これは互換性の袋小路を避けるためです。
API shape は当面 `SystemdUnit` のままですが、OpenRC に写すのは明確に init script の意味を持つ field に限ります：`ExecStart`、`ExecStartPre`、environment、working directory、user/group、runtime/state/log directory。
systemd sandboxing、`NetworkAdoption`、networkd、resolved、timesyncd の意味は OpenRC 上で模倣しません。

apply-time activation は `HasOpenRC` で分岐します。
script は内容または mode が変わる場合だけ書き込み、`rc-update show default` で登録状態を確認してから add / del し、`rc-service <name> status` を見てから start / restart / stop します。
systemd 側と同じく、望む状態と file が変わらない場合は service-manager command を重複実行しません。

次の実装段階は Alpine installed-host smoke harness の通常 VM job 化です。

## 8. 残課題

- 状態を持つ firewall の本番運用：`FirewallRule` は ICMP type 一致、
  複数 port、nftables rate limit、送信元ごとの connection limit を扱えます。
  今後は基本的な expression coverage ではなく、rule grouping と上位 policy の
  使いやすさを改善します。
- LAN 向け DoH 代理。
- Tier C のための FRR 経由 BGP / OSPF 統合。
- 高可用 (leader 選出、耐障害 control plane)。
- 本番 observability (OpenTelemetry collector + 遠隔 log sink)。
- 家庭用回線で routerd を唯一の WAN ルーターとして長時間運用する検証。
