---
title: 対応プラットフォーム
---

# 対応プラットフォーム

routerd は cross-OS を前提に設計されています。
各 OS では、利用するホスト側の機構が異なります。
このページでは、routerd が各プラットフォームで使う OS 機能を明示します。
適用前に、生成されるファイルと実行時の所有範囲を確認してください。

## Linux (Ubuntu / Debian)

Linux が主対象です。
リリースインストーラーの配置先は既定で `/usr/local` 配下です。
Linux 用リリースアーカイブを展開し、`sudo ./install.sh` を実行します。
インストーラーは `apt-get`、`dnf`、`pacman` のいずれかで実行時パッケージを導入できます。

routerd が Linux 上で利用する OS 機能：

- systemd ユニット
- `/run/routerd` と `/var/lib/routerd` (ランタイムと永続状態)
- dnsmasq (DHCPv4 / DHCPv6 / DHCP relay / RA)
- nftables (フィルタ + NAT)
- conntrack (コネクション観測)
- iproute2 (interface + 経路)
- pppd / rp-pppoe (PPPoE)
- WireGuard、Tailscale、strongSwan、radvd

Ubuntu でも、パッケージが事前導入されていることを前提にしません。
初回準備では `install.sh` が実用的な既定セットを導入します。
継続的な宣言管理では、`Package` リソースで依存関係を宣言してください。
リファレンス：

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS 制御 | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` は Linux 上では systemd サービスとして動作します。

## NixOS

NixOS は Ubuntu と同じ routerd リソースモデルを使います。
ただし、反映は NixOS モジュール経由です。
一時的な systemd ユニットを書く代わりに、`/etc/nixos/routerd-generated.nix` を生成します。
その後、`nixos-rebuild test` / `nixos-rebuild switch` で有効化します。

実装済み：

- NixOS の有効化、再起動後の復元、DHCPv6-PD、dnsmasq による LAN サービス、DNS リゾルバー、DS-Lite、nftables の NAT とファイアウォール、HealthCheck、Web Console の世代差分、OpenTelemetry 送信の実機検証
- `routerd-dhcpv6-client` の systemd ユニット生成
- `routerd-dhcpv4-client` の systemd ユニット生成
- `routerd-pppoe-client` の systemd ユニット生成
- `Package`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` の NixOS モジュール生成
- `nixos-rebuild test` / `nixos-rebuild switch` 連携
- `nixos-rebuild switch` 失敗時の `nixos-rebuild switch --rollback` 試行
- `nixos-rebuild` 前後の `generation` 記録
- DHCPv6-PD が `Bound` まで到達
- DHCP または RA リソースが dnsmasq を必要とする場合の `routerd-dnsmasq` サービス生成
- `routerd-dnsmasq` サービスでは NixOS のシステムプロファイル内の絶対パスを使います。root のまま実行する指定も入れるため、systemd の保護設定下でも `PATH` 探索や権限降格に依存しません
- DNS resolver、HealthCheck、firewall logger、Tailscale、DHCPv4 クライアント、DHCPv6 クライアント、PPPoE クライアントのサービス生成
- NAT、firewall、policy routing、Path MTU リソースが nftables を必要とする場合の `networking.nftables.enable = true` 生成
- WireGuard、Tailscale、VXLAN、systemd-networkd による VRF 生成
- NixOS の native network 宣言では表せない Linux 実行時リソースは、NixOS の有効化後に `routerd.service` が調整

NixOS では、routerd が必要とするコマンドを `systemd.services.routerd.path` に入れてください。
`install.sh` は NixOS を検出した場合、`nix-env` を実行せず警告だけ出します。
NixOS のパッケージ状態は宣言的に管理してください。
`Package` リソースに `os: nixos` を書く場合、routerd は実行時にパッケージを導入しません。
`routerd render nixos` が `environment.systemPackages` を生成します。

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `strongswan`, `radvd` |
| Diagnostics | `bind`, `iputils`, `tcpdump`, `traceroute`, `nettools` |
| OS 制御 | `procps`, `systemd`, `kmod` |

## FreeBSD

FreeBSD も Ubuntu と同じ routerd リソースモデルを使います。
反映先は FreeBSD のホスト機構です。
DHCPv6-PD クライアントは `daemon(8)` で実行され、リースを安定維持します。
routerd は Linux 用の機構ではなく、FreeBSD の `rc.conf`、`rc.d`、`pf`、`mpd5`、`ifconfig`、dnsmasq にリソースを対応付けます。
FreeBSD 用リリースアーカイブを展開し、`sudo ./install.sh` を実行します。
インストーラーは `pkg` で ports パッケージを導入し、基本システムのコマンドは導入せず確認だけ行います。

実装済み：

- DHCPv6-PD デーモンとリース永続化
- WireGuard で Linux / NixOS と相互接続
- VXLAN over WireGuard
- `mpd5.conf`、`mpd_enable`、`mpd5` サービス再起動による PPPoE
- `Package` の `pkg` 経由 install
- `gateway_enable`、`ipv6_gateway_enable`、`cloned_interfaces`、`ifconfig_*`、`static_routes`、`ipv6_static_routes`、`pf_enable`、`pflog_enable`、`mpd_enable` の FreeBSD らしい `rc.conf.d` 出力
- `routerd render freebsd --out-dir` による `dhclient.conf`、`mpd5.conf`、`pf.conf`、dnsmasq 設定、`rc.d` スクリプト生成
- `FirewallZone` / `FirewallPolicy` / `FirewallRule` からの pf レンダリング
- `IPv4SourceNAT` / `NAT44Rule` からの pf NAT レンダリング
- 生成された `pf.conf` の `pfctl -nf` 検証と `pfctl -f` 適用
- `pfctl -ss -v` 出力の traffic flow 変換
- `pflog0` を BPF から直接読む firewall log。packet を解析するため、tcpdump の文字列表現の差異に依存しません
- DHCPv4、DHCPv6、RA 用の管理対象 dnsmasq
- `/var/db/routerd/dnsmasq` 配下での dnsmasq リース永続化
- サービス再起動前の `dnsmasq --test` による設定確認
- DHCP、DNS、RA、DHCPv6-PD、DS-Lite、WireGuard、HealthCheck に必要な pf 穴の自動生成
- `SystemdUnit` からの rc.d スクリプト生成
- `routerd-healthcheck` の rc.d スクリプト生成
- `routerd-firewall-logger` の rc.d スクリプト生成と `pflog0` 直接読み取り
- `TailscaleNode` の rc.d スクリプト生成
- 静的 DS-Lite gif tunnel レンダリング
- static AFTR IPv6、AFTR FQDN、delegated address 由来の local source による動的 DS-Lite 適用
- cloud VPN 向け `IPsecConnection` の検証と strongSwan `swanctl` 接続定義生成。クラウドゲートウェイとの実疎通確認は環境ごとに行います

FreeBSD は Linux 専用の nftables / conntrack / iproute2 を使いません。
`Package` の例は FreeBSD 側の置き換えを宣言します。
pf と `pflog0` は基本システムを使います。
PPPoE は `mpd5`、DS-Lite は `ifconfig gif`、LAN DHCP/RA は dnsmasq を使います。
WireGuard、Tailscale、strongSwan は ports のパッケージを使います。

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| Diagnostics | `bind-tools`, `tcpdump` |
| 基本システム | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `tcpdump`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` は次を出力します：

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `rc.d-*`

`routerd apply` は生成した `pf.conf` を導入します。
その前に `pfctl -nf` で構文を確認します。
dnsmasq も `dnsmasq --test` で設定を確認してから再起動します。
導入後は `pfctl -f` で反映し、生成した rc.d スクリプトを `service <name> onestart` で起動します。
静的な `rc.conf` 生成だけで足りない DS-Lite tunnel は、`ifconfig gif` で動的に適用します。
実運用の前には、`routerd render freebsd` で出力を確認してください。

## OS 抽象化の実装方針

新しい OS 固有の振る舞いを足すときは、business logic 層で `runtime.GOOS` を直接読まないでください。
`pkg/platform` 層 (`platform.Features`) または Go の build tag を使って境界を明示します。
対象外 OS で実行時に予期せず失敗するより、validation や planning の段階で明示的にエラーにする方を優先します。
