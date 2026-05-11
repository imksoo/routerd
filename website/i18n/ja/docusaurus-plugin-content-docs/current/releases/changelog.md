---
title: Changelog
---

# Changelog

routerd のリリース履歴です。形式は [Keep a Changelog](https://keepachangelog.com/) に準拠します。
routerd は `vYYYYMMDD.HHmm` 形式の日付と時刻に基づく版番号を使います。
ソフトウェアは v1alpha1 段階のため、リリース間で破壊的変更を含むことがあります。

## v20260511.1240

## v20260511.1041

## v20260511.1017

## v20260510.1956

## v20260510.1811

### 追加

- PVE live ISO のシリアルコンソール検証ログを `internal/notes/` に追加しました。
  walkthrough の画面キャプチャと実行ログを、test evidence として同じ release に残します。

## v20260510.1802

### 変更

- 日本語、簡体字中国語、繁体字中国語のディスクレス mini PC walkthrough に、
  PVE live ISO boot test で取得した実際の画面キャプチャを埋め込みました。
- ディスクレス mini PC walkthrough に残っていた古い placeholder 画像参照を削除しました。

## v20260510.1750

### 追加

- ディスクレス mini PC walkthrough に、PVE live ISO 実機検証で取得した
  画面キャプチャを追加しました。
- 簡体字中国語版と繁体字中国語版に、位置づけ、USB 永続化、
  法務と再配布の不足ページを追加しました。

### 変更

- website footer の著作権表示を、著作権表示を先に置く慣習的な形式へ変更しました。
- ディスクレス mini PC walkthrough の PVE 例を、VGA と serial console の
  両方を有効にする構成へ更新しました。これにより、QEMU screenshot と
  `qm terminal` 検証を同じ実行で取得できます。

### 修正

- live ISO の設定ウィザードで、DHCPv4 pool の既定値を選択した LAN
  アドレスの prefix から導出するように修正しました。
- PVE live ISO boot test を再実行し、
  `/tmp/iso-boot-test-20260510-1742.log`、QEMU screenshot、routerd apply、
  Healthy status、USB persistence flush まで確認しました。

## v20260510.1722

### 追加

- routerd の Go ソース、インストーラースクリプト、プラグインスクリプト、
  Web Console ソースに BSD 3-Clause の SPDX 識別子を追加しました。
- README にライセンスバッジを追加し、英語版と日本語版 README から
  BSD 3-Clause License へリンクしました。
- 公開ドキュメントに貢献ガイドを追加し、ドキュメントの sidebar から
  辿れるようにしました。
- SECURITY にメールと GitHub Security Advisories の報告先を明記しました。

### 変更

- repository root の `LICENSE` にある著作権表示を
  `Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors`
  に統一しました。
- SPDX ヘッダーが routerd ソースファイルだけに適用されることを
  法務ドキュメントに明記しました。同梱する第三者ソフトウェアは
  `THIRD_PARTY_LICENSES.md` に記載された個別ライセンスに従います。
- README から製品比較表を削除し、routerd 自身の対象範囲と特徴を説明する
  記述に整理しました。

## v20260510.1626

### 追加

- 公開ドキュメントに法務と再配布ページを追加し、release checklist を整理しました。
- 生成される第三者ライセンス一覧に Go module source URL を追加しました。
- BSD routerd binary と aggregate live ISO distribution model の内部 license audit note を記録しました。

## v20260510.1612

### 追加

- Go module とライブ ISO で使う Alpine package の第三者ライセンス一覧を自動生成できるようにしました。
- release archive とライブ ISO にライセンス通知を同梱する場所を追加しました。
- routerd 本体の BSD 3-Clause License と、ライブ ISO の aggregate distribution としての扱いを文書化しました。

## v20260510.1547

### 追加

- routerd 自身の対象範囲と deployment spectrum を中心に、公開向けの位置づけ説明を広げました。
- Intel NUC、N100 mini PC、Raspberry Pi 5、thin client、Proxmox VM の hardware compatibility を拡充しました。
- 中国語の hardware compatibility ページを追加し、ライブ ISO と USB 永続化の流れを明確にしました。

## v20260510.1534

## v20260510.1508

## v20260510.1451

## v20260510.1429

## v20260510.1412

## v20260510.1354

## v20260510.1310

## v20260510.1301

## 20260510.4

## 20260510.3

## 20260510.2

## 20260510.1

## 20260510.0

## 20260509.16

### 追加

- 版番号付きアーカイブに加えて、`routerd-linux-amd64.tar.gz` のような固定名 alias をリリースアーカイブに追加しました。
- 固定名アーカイブと `.sha256` ファイルを GitHub Releases に配置します。これにより、ドキュメントで `releases/latest/download/...` URL を使えます。

### 変更

- クイックスタートのドキュメントを、固定された latest download URL に変更しました。
- release workflow で、対応している GitHub JavaScript actions が Node.js 24 runtime を使うようにしました。

## 20260509.15

### 追加

- branch push と pull request 用の `CI` GitHub Actions workflow を追加しました。
- CI workflow は Ubuntu 上で `go test ./...`、schema 確認、example 検証、Web サイト生成を実行します。
- ローカル commit の前に Go テストと schema 確認を実行する任意の `scripts/pre-commit.sh` hook を追加しました。
- CI、pre-commit 確認、tag で起動する release workflow の役割分担を説明する開発ドキュメントを追加しました。

## 20260509.14

### 検証

- Ubuntu lab ルーターの router05 で、`ClientPolicy` ゲストモードを検証しました。
- Linux nftables で、include mode のゲスト MAC アドレス集合、ゲスト向け DNS/DHCP/NTP 許可、自己隔離、RFC 1918 / ULA 拒否規則が生成されることを確認しました。
- exclude mode は、nftables 生成テストで確認しました。

## 20260509.13

### Added

- ゲストモードガイドを詳細化しました。ユースケース、内部実装、`ClientPolicy` の全フィールド、確認手順、トラブルシューティング、セキュリティ上の限界を追加しました。
- include mode、exclude mode、複数ゲスト端末、カスタム拒否・許可リスト、ローカル探索サービス、IoT 固定割り当ての例を追加しました。
- `ClientPolicy.spec.guestServices` で、`dhcp`、`dns`、`ntp` に加えて `mdns` と `ssdp` を指定できるようにしました。

## 20260509.12

### Added

- `ClientPolicy` を追加しました。Linux nftables で LAN 端末を MAC アドレスごとに分類するゲストモードです。
- ゲスト端末は DNS、DHCP、NTP を使えます。プライベート IPv4 宛てと ULA IPv6 宛ての通信は既定で拒否します。
- `examples/guest-mode.yaml` と、include mode / exclude mode の分類方法を説明するドキュメントを追加しました。

### Changed

- FreeBSD pf では `ClientPolicy` を明示的に未対応として扱います。pf は同じ MAC ベースの routed filtering モデルを持たないためです。

## 20260509.11

### Added

- 最小 Tailscale mesh 参加、WireGuard hub-spoke 経路、VRF lab、multi-WAN home fallback の用途別 example を追加しました。
- 各 example の用途を説明する `examples/README.md` を追加しました。

### Changed

- `make validate-example` が `examples/` 配下の全 YAML ファイルを検証するようにしました。

## 20260509.10

### Added

- Web Console の Overview に、世代、リソース phase、HealthCheck 状態の簡易時系列チャートを追加しました。
- Config 画面で、現在の YAML ファイルと最新適用世代を比較できるようにしました。`routerd apply` の前に差分を確認できます。
- Resource テーブルで、kind、name、phase、詳細の検索、phase 絞り込み、検索結果の強調表示ができるようにしました。
- VPN 画面に Tailscale と WireGuard の peer 状態を示す視覚サマリーを追加しました。

## 20260509.9

### Added

- リリースアーカイブに `share/doc/TARGET` を含め、`install.sh` がホストの OS と CPU アーキテクチャーを確認するようにしました。
- GitHub Actions で Linux と FreeBSD の `amd64` / `arm64` アーカイブを生成するようにしました。
- release CI で `install.sh` と `uninstall.sh` に `shellcheck` を実行します。

### Changed

- `install.sh --list-deps` の出力を、OS、CPU アーキテクチャー、パッケージマネージャー、パッケージ、確認対象コマンドが分かる形に整理しました。
- PPPoE、RA、IPsec、パケット取得、経路制御、ファイアウォールで使う実用パッケージを依存リストへ追加しました。

## 20260509.8

### Fixed

- zh-Hant と zh-Hans のドキュメントリンクを修正し、翻訳ページが未翻訳のロケール内ページを指さないようにしました。
- 翻訳がそろうまで、概要ページから英語版の正準リファレンスへリンクする形にしました。

## 20260509.7

### Added

- `EgressRoutePolicy` で、DS-Lite 主経路、RA 由来 DS-Lite、PPPoE、WAN 直結の多段フォールバックを表現できるようにしました。
- 宣言的な `Telemetry` リソースと OTLP 環境変数の伝播により、ルーター群へ OpenTelemetry 設定を展開しました。
- DS-Lite の例は、RFC 6333 の B4-AFTR link prefix `192.0.0.0/29` を tunnel 内側 IPv4 送信元として使う形にしました。
- `PPPoEInterface.disabled` と無効化された経路候補により、PPPoE フォールバック定義を YAML に残しつつ、本番 PPPoE セッションの漏れを防げるようにしました。

### Changed

- リリース版番号を `0.x.y` から日付ベースの値へ変更しました。
- `routerd --version`、`routerctl --version`、リリースアーカイブで同じ release tag の値を使うようにしました。
- Linux nftables と FreeBSD pf の NAT44 生成を、インターフェース単位のルールへ寄せました。
- 3-role firewall モデルを Linux と FreeBSD で確認し、service hole を広い zone 全体ではなく、所有する受信インターフェースへ束縛しました。
- FreeBSD pf で `PathMTUPolicy` の TCP MSS clamp を生成できるようにし、Linux nftables とそろえました。
- dnsmasq の RA 生成で、IPv6 RA MTU option により path MTU を配布できるようにしました。

### Fixed

- FreeBSD pf で DHCPv6、WireGuard、VXLAN の service hole が `wan` zone の全インターフェースへ広がる問題を修正しました。
- FreeBSD の NAT artifact を nftables ではなく `pf.anchor/routerd_nat` として報告するようにしました。
- NAT 生成の前に、PPPoE のリソース名を実 OS インターフェース名へ解決するようにしました。

## 0.4.0

### Added

- nftables の暗黙拒否ログを `routerd-firewall-logger` で取り込み、`firewall-logs.db` に保存するようになりました。Linux では `nfnetlink` を直接読み取り、FreeBSD では `pflog` を `tcpdump` 経由で取り込みます。
- Web Console に Connections タブ (実時間 conntrack / pf state)、Clients タブ (DHCP リース + トラフィック統合)、Firewall タブ (拒否ランキング + 時系列テーブル) を追加しました。
- `TailscaleNode` で Tailscale の exit node と subnet router を広告できるようにしました。生成した systemd ユニットで `tailscale up` を実行します。NixOS 向け生成では `services.tailscale` を有効化し、ユニットの `path` も設定します。
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
