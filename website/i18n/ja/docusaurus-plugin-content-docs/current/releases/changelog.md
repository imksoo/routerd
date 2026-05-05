---
title: Changelog
---

# Changelog

routerd は出荷前のソフトウェアです。
この履歴は、利用者が現在の API 名と実装済み範囲を間違えないために残します。

## 0.4.0

- Phase 3.1d: Linux の NFLOG 入力を `tcpdump` 経由から nfnetlink の直接読み取りへ変更しました。
  `routerd-firewall-logger --nflog-group` は NFLOG prefix、インターフェース、パケットファミリー、プロトコル、アドレス、ポートをそのまま保存します。
  Web Console の Firewall タブには、拒否回数のランキングに加えて時系列テーブルを追加しました。
- Phase 3.1a: 実時間の conntrack / pf state 表示を `connections` に改名しました。
  Web Console API は `/api/v1/connections`、CLI は `routerctl connections` を使います。
  IPv4 NAPT だけでなく、IPv6 の経路通過コネクションも同じ表で扱います。
  Web Console は文字列連結ではなく DOM ノード生成で表を描画し、列ずれを防ぎます。
- Phase 3.0a: NixOS 向けの宣言的な生成を拡張しました。
  `Package` の NixOS パッケージ、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` を `routerd render nixos` の出力へ反映します。
  NixOS 上の `Package` は実行時に導入せず、生成された NixOS 設定で管理する扱いにしました。
- Phase 3.0b: FreeBSD 向け `Package` の実行経路を追加しました。
  `pkg info -e` で導入済みパッケージを確認し、不足があれば `pkg install -y` で導入します。
- Phase 3.0c: FreeBSD 向け pf 生成器の最初の実装を追加しました。
  `FirewallZone`、`FirewallPolicy`、`FirewallRule`、`IPv4SourceNAT`、`NAT44Rule` から `pf.conf` を生成します。
  実機適用はまだ接続せず、`routerd render freebsd --out-dir ...` の生成物として確認する段階です。
- Phase 3.0d: FreeBSD 向け pf state 観測の土台を追加しました。
  FreeBSD では `pfctl -ss -v` の出力を traffic flow 用の共通コネクション表現へ変換します。
- Phase 3.0e: `routerd-firewall-logger` に FreeBSD `pflog` 入力を追加しました。
  `--pflog-interface pflog0` を指定すると `tcpdump` 経由で pf のログを読み、既存の `firewall-logs.db` へ保存します。
- Phase 3.0f: `SystemdUnit` から FreeBSD rc.d スクリプトを生成する経路を追加しました。
  `routerd render freebsd --out-dir ...` は `rc.d-*` ファイルも出力します。
- Phase 3.0g: FreeBSD 向け DS-Lite の静的 gif 生成を追加しました。
  `localAddress` と `remoteAddress` または `aftrIPv6` が固定で分かる場合に、`ifconfig_gif*` と IPv4 既定経路を生成します。
  AFTR FQDN や delegated address 由来の動的 DS-Lite は、実行時コントローラー側の未実装として warning にします。
- Phase 3.0h: `examples/freebsd-edge.yaml` と platform 文書を、NixOS / FreeBSD の現在の到達点へ同期しました。

## 0.3.0

- Phase 2.7d-e: 宣言的な OS bootstrap リソースとして `Package` と `SysctlProfile` を追加しました。
  apt、dnf、nix、pkg のパッケージ宣言と、ルーター向け sysctl 推奨値を 1 リソースで適用します。
  `nf_conntrack_max`、socket buffer、TCP/UDP timeout、ip_forward など、家庭ルーター実トラフィック向けのチューニングを `router-linux` プロファイルにまとめています。
- Phase 2.7f: 設定の `${...status.field}` 文字列参照を typed な `*From` フィールドへ整理しました。
  `addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`dependsOn` を導入し、依存関係を型レベルで明示します。
  互換別名はありません。
- Phase 2.7g: controller chain を pure event-loop 型へ作り直しました。
  共通 `framework.FuncController` (Subscriptions + Bootstrap + PeriodicFunc) と `eventedStore` で、状態保存時に必ず `routerd.resource.status.changed` を発行し、下流が再評価する形にしました。
  per-resource error isolation、daemon 直読みショートカットの排除、daemon snapshot の per-socket timeout を入れています。
- Phase 2.7h: bus event を `slog` 経由で systemd journal へ出力します。
  `journalctl -u routerd.service -f | grep "routerd event"` で controller の意思決定を追えます。
  `routerd.resource.status.changed` などの高頻度イベントは debug レベルです。
- Phase 2.7i: 静的バイナリビルドと、Ubuntu / NixOS / FreeBSD 別の依存パッケージ一覧を整理しました。
  `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"` で全 binary を作ります。
  `dnsmasq-base`、`nftables`、`conntrack`、`iproute2`、`ppp`、`wireguard-tools`、`strongswan-swanctl`、`radvd`、`tcpdump` などを OS 別に明示します。
- Phase 2.7c: `HealthCheck.sourceInterface` を YAML ではリソース名で書き、実行時に OS の ifname へ解決する形にしました。
- Phase 2.7j: SystemdUnit に `runtimeDirectoryPreserve` を追加しました。
  routerd.service と routerd-healthcheck@... の RuntimeDirectory 競合で再起動時に socket が消える問題を declarative に解消します。
- Phase 2.7k: SystemdUnit `state: absent` を正しく Drifted 判定し、unit 削除を plan へ含めます。
- Phase 2.7l: DNSResolver に NextDNS 専用の bootstrap forwarder (NGN DNS 優先 + public DNS 予備) を追加しました。
- Phase 2.7m: SysctlProfile の observe で型ゆらぎによる spurious drift を抑えました。
- 新 Kind 群の追加: `Package`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` を `system.routerd.net/v1alpha1` に整列しました。
  `NetworkAdoption` は systemd-networkd の DHCP/RA を yaml で無効化、`SystemdUnit` は routerd 自身が unit を render + install + enable します。
- routerctl 強化: `routerctl events --limit N --topic X --resource K/N -o json` で sqlite3 不要に bus event を確認できます。
  `routerd plan --diff` で apply 前差分を表示します。
- routerd serve に client daemon supervisor を組み込み、`--controller-chain-supervise-client-daemons=true` で `routerd-dhcpv4-client`、`routerd-dhcpv6-client`、`routerd-pppoe-client` を子プロセスとして起動・監視します。
  ユーザーが個別に `systemctl enable` する手間が消えました。
- homert02 (本番自宅ルーター、IX2215 置換) に Stage 1 として実適用し、DS-Lite primary、IX2215 fallback、DNS、firewall、NAT、HealthCheck を `routerctl status` Healthy で確認しました。
  `examples/homert02.yaml` を canonical reference として整備しました。

## 0.2.0

- Phase 2.6: `WANEgressPolicy` を `EgressRoutePolicy` に改名しました。
  互換別名はありません。
  `destinationCIDRs`、`gateway`、`gatewaySource` を追加しました。
  `HealthCheck` は `via`、`sourceInterface`、`sourceAddress` で probe の送信経路を指定できます。
- Phase 2.5: `FirewallZone`、`FirewallPolicy`、`FirewallRule` を追加しました。
  nftables の `inet routerd_filter` table で状態を持つ firewall を生成します。
- Phase 2.0: DNS を `DNSZone` と `DNSResolver` に整理しました。
  `routerd-dns-resolver` がローカルゾーン、条件付き転送、DoH、DoT、DoQ、平文 UDP DNS、キャッシュを扱います。
  dnsmasq は DHCPv4、DHCPv6、中継、RA に限定しました。
  `viaInterface`、`bootstrapResolver`、複数 listen、DNSSEC 指定を追加しました。
- Phase 1.9: router05 の常駐運用を systemd ユニットへ移しました。
  その後、暗号化 DNS プロキシーを実適用し、NextDNS 側のログ照合まで確認しました。
- Phase 1.8: 文書を現在のコードに合わせて全面整理しました。
  旧 DHCPv6 クライアント経路、旧 Kind 名、古いラボ前提を利用者向け本文から取り除きました。
- Phase 1.7: router02 の NixOS 設定を宣言的な `routerd-dhcpv6-client` ユニットへ移しました。
  `/etc/nixos/routerd-generated.nix` を使い、`nixos-rebuild test` と `switch` で反映します。
- Phase 1.6: DHCP 関連 Kind とバイナリ名を RFC 表記へ整理しました。
  `routerd-dhcpv4-client` と `routerd-dhcpv6-client` が現在名です。
  旧名の互換別名はありません。
- Phase 1.5e: router05 で DS-Lite を実適用しました。
  条件付き DNS 転送で AFTR FQDN を解決し、ip6tnl、IPv4 既定経路、NAT44、IPv4 HTTP 通信を確認しました。
- Phase 1.5d: `routerd-pppoe-client` と `PPPoESession` を追加しました。
  Linux は pppd と rp-pppoe、FreeBSD は ppp(8) の経路を持ちます。
- Phase 1.5c: `routerd-dhcpv4-client` と `DHCPv4Lease` を追加しました。
  DHCPv4 リースを lease ファイルと Unix ソケット API で管理します。
- Phase 1.5b: `NAT44Rule` と conntrack 観測を追加しました。
  `/proc/net/nf_conntrack` がない環境では、sysctl 由来の集計へ縮退します。
- Phase 1.5a: dnsmasq による LAN 側サービスを拡張しました。
  DHCPv4、DHCPv6、RA、中継の設定を管理対象 dnsmasq 設定へ統合しました。
- Phase 2-B: `EgressRoutePolicy`、`EventRule`、`DerivedEvent`、`HealthCheck` を追加しました。
- Phase 2-A: DHCPv6 情報要求、DS-Lite、IPv4 経路、RA、DHCPv6 サーバーをカスケードに追加しました。
- Phase 1: DHCPv6-PD から LAN アドレス、DNS 応答へつながる最初の controller chain を追加しました。
- Phase 0: `routerd-dhcpv6-client`、daemon contract、bus、controller framework を追加しました。

## 0.1.0

初期の v1alpha1 実装です。
この版以降、出荷前の整理として API 名や実装方針を大きく変更しています。
現在の設定例では、この changelog の `Unreleased` に書かれた名前を使ってください。
