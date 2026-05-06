---
title: Changelog
---

# Changelog

routerd のリリース履歴です。形式は [Keep a Changelog](https://keepachangelog.com/) に準拠します。
ソフトウェアは v1alpha1 段階のため、`0.x` の minor バージョン間でも破壊的変更を含みます。

## Unreleased

### Added

- nftables の暗黙拒否ログを `routerd-firewall-logger` で取り込み、`firewall-logs.db` に保存するようになりました。Linux では `nfnetlink` を直接読み取り、FreeBSD では `pflog` を `tcpdump` 経由で取り込みます。
- Web Console に Connections タブ (実時間 conntrack / pf state)、Clients タブ (DHCP リース + トラフィック統合)、Firewall タブ (拒否ランキング + 時系列テーブル) を追加しました。
- `WebConsole.spec.listenAddressFrom` と `DNSResolver` 系のリスニングアドレスを `Interface/<name>.status.ipv4Addresses` から導出できるようにしました。即値の代わりに参照で書けます。
- conntrack accounting (`net.netfilter.nf_conntrack_acct=1`) を `SysctlProfile/router-linux` 既定値に追加し、`TrafficFlowLog` で `bytesOut` / `bytesIn` を集計できるようにしました。

### Changed

- 実時間コネクション表示の API / CLI を `connections` に統一しました (旧称 `conntrack-snapshot`)。`/api/v1/connections`、`routerctl connections` を使います。IPv6 を含む全ファミリを同じ表で扱います。
- NixOS 向けの宣言的レンダリングを拡張しました。`Package` (NixOS パッケージ宣言)、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` を `routerd render nixos` の出力に統合します。NixOS 上の `Package` は実行時に導入せず、生成された NixOS 設定で管理します。
- `SystemdUnit` から FreeBSD `rc.d` スクリプトを生成できるようになりました (`routerd render freebsd --out-dir`)。

### Fixed

- `IPv6DelegatedAddress` controller が `Link/<name>` の status が空のとき、PD 由来アドレスをホストインターフェースに付与しない問題を修正しました。
- `SystemdUnit` controller が変更のない active unit を毎回再起動する問題を修正しました。

## 0.3.0

### Added

- 宣言的な OS bootstrap リソースとして `Package` と `SysctlProfile` を追加しました。apt、dnf、nix、pkg のパッケージ宣言と、ルーター用途向けの sysctl 推奨値 (`nf_conntrack_max`、socket buffer、TCP/UDP timeout、`ip_forward` など) を 1 つのリソースで適用します。
- `NetworkAdoption` で systemd-networkd の DHCP / RA を YAML から無効化できます。`SystemdUnit` で routerd 自身が unit を render + install + enable できます。
- `routerctl events --limit N --topic X --resource K/N -o json` で sqlite3 不要に bus event を確認できます。
- `routerd plan --diff` で apply 前差分を表示します。
- `DNSResolver` に bootstrap forwarder (RFC1918 内部 DNS を優先しつつ public DNS を予備にする) を追加しました。

### Changed

- 設定ファイル中の `${...status.field}` 文字列参照を、型付きの `*From` フィールドへ整理しました (`addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`dependsOn`)。互換別名はありません。
- controller chain を pure event-loop 型に再構築しました。共通 `framework.FuncController` (Subscriptions + Bootstrap + PeriodicFunc) と `eventedStore` で、状態保存時に必ず `routerd.resource.status.changed` を発行し、下流が再評価する設計です。
- bus event を `slog` 経由で systemd journal へ出力します (`journalctl -u routerd.service -f | grep "routerd event"` で controller の意思決定を追跡できます)。高頻度イベントは debug レベルです。
- 全バイナリを静的ビルドにしました (`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`)。OS 別の依存パッケージ (`dnsmasq-base`、`nftables`、`conntrack`、`iproute2`、`ppp`、`wireguard-tools`、`strongswan-swanctl`、`radvd`、`tcpdump` など) を Ubuntu / NixOS / FreeBSD ごとに整理しました。
- `HealthCheck.sourceInterface` を YAML 上ではリソース名で書き、実行時に OS の interface 名に解決します。

### Fixed

- `SystemdUnit` 同士の `RuntimeDirectory` 競合で再起動時に socket が消える問題を、`runtimeDirectoryPreserve` で declarative に解消しました。
- `SystemdUnit` の `state: absent` を正しく Drifted として検出し、unit 削除を plan に含めるようにしました。
- `SysctlProfile` の observe で型ゆらぎによる不要な drift を抑えました。

## 0.2.0

### Added

- Stateful firewall を導入しました。`FirewallZone`、`FirewallPolicy`、`FirewallRule` で nftables の `inet routerd_filter` table を生成します。
- `EgressRoutePolicy` (旧 `WANEgressPolicy`) に `destinationCIDRs`、`gateway`、`gatewaySource` を追加しました。`HealthCheck` は `via`、`sourceInterface`、`sourceAddress` で probe の送信経路を指定できます。
- DNS サブシステムを再構成しました。`DNSZone` (権威ゾーン定義) と `DNSResolver` (フォワーダー / キャッシュ) に分離し、ローカルゾーン、条件付き転送、DoH / DoT / DoQ、平文 UDP DNS をサポートします。dnsmasq は DHCPv4 / DHCPv6 / RA / 中継に専念します。
- DS-Lite (`DSLiteTunnel`)、PPPoE (`PPPoESession`、`routerd-pppoe-client`)、DHCPv4 client (`routerd-dhcpv4-client`、`DHCPv4Lease`) を追加しました。
- NAT44 (`NAT44Rule`) と conntrack 観測を追加しました。`/proc/net/nf_conntrack` がない環境では sysctl 由来の集計に縮退します。

### Changed

- `WANEgressPolicy` を `EgressRoutePolicy` に改名しました。互換別名はありません。
- DHCP 関連 Kind とバイナリ名を RFC 表記に統一しました (`routerd-dhcpv4-client`、`routerd-dhcpv6-client`)。旧名の互換別名はありません。

## 0.1.0

最初の v1alpha1 実装です。

- DHCPv6-PD クライアント、daemon contract、event bus、controller framework を導入しました。
- DHCPv6-PD → LAN アドレス導出 → DNS 応答までの controller chain を実装しました。
- DHCPv6 情報要求、DS-Lite (試作)、IPv4 経路、RA、DHCPv6 サーバー、`HealthCheck`、`EventRule`、`DerivedEvent` を追加しました。

このバージョン以降、出荷前の整理として API 名や実装方針に大きな変更が入っています。最新の利用方法は `Unreleased` の項目と `examples/` を参照してください。
