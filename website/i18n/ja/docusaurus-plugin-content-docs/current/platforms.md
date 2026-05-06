---
title: 対応プラットフォーム
---

# 対応プラットフォーム

routerd は cross-OS を前提に設計されていますが、実装の成熟度は OS ごとに異なります。
このページでは、各プラットフォームでの「実装済み範囲」「土台のみ」「scope 外」を明示するので、現状の制約を踏まえて選択してください。

## Linux (Ubuntu / Debian)

Linux が主対象です。ソースインストール先は既定で `/usr/local` 配下です。

routerd が Linux 上で利用する OS 機能：

- systemd unit
- `/run/routerd` と `/var/lib/routerd` (ランタイムと永続状態)
- dnsmasq (DHCPv4 / DHCPv6 / DHCP relay / RA)
- nftables (フィルタ + NAT)
- conntrack (コネクション観測)
- iproute2 (interface + 経路)
- pppd / rp-pppoe (PPPoE)
- WireGuard、Tailscale、strongSwan、radvd

Ubuntu でも、パッケージが事前導入されていることを前提にしません。`Package` リソースで依存関係を宣言してください。リファレンス：

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS 制御 | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` は Linux 上では systemd サービスとして動作します。

## NixOS

NixOS は first-class な secondary プラットフォームです。
transient な systemd unit を書く代わりに、`/etc/nixos/routerd-generated.nix` に対して生成し、`nixos-rebuild test` / `nixos-rebuild switch` で activation を任せます。

実装済み：

- `routerd-dhcpv6-client` の systemd unit 生成
- `Package`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` の NixOS module 生成
- `nixos-rebuild test` / `nixos-rebuild switch` 連携
- DHCPv6-PD が `Bound` まで到達
- WireGuard、Tailscale、VXLAN の対応
- VRF の部分対応

未対応：

- 全 NixOS module を実機適用するパス (一部は生成だけで自動 apply されない)
- nftables / dnsmasq / DNS resolver / HealthCheck の end-to-end 動作
- NixOS の `generation` rollback semantics 連携

NixOS では、routerd が必要とするコマンドを `systemd.services.routerd.path` に入れてください。
`Package` リソースに `os: nixos` を書く場合、routerd は実行時にパッケージを導入しません。`routerd render nixos` が `environment.systemPackages` を生成します。

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `strongswan`, `radvd` |
| Diagnostics | `bind`, `iputils`, `tcpdump`, `traceroute`, `nettools` |
| OS 制御 | `procps`, `systemd`, `kmod` |

## FreeBSD

FreeBSD はもう一つの secondary プラットフォームです。
DHCPv6-PD クライアントは `daemon(8)` で実行され、リースを安定維持します。
レンダラ系は概ね動きますが、本番適用までは段階的な対応です。

実装済み：

- DHCPv6-PD daemon と lease 永続化
- WireGuard で Linux / NixOS と相互接続
- VXLAN over WireGuard
- PPPoE のスケルトン
- `Package` の `pkg` 経由 install
- `FirewallZone` / `FirewallPolicy` / `FirewallRule` からの pf レンダリング
- `IPv4SourceNAT` / `NAT44Rule` からの pf NAT レンダリング
- `pfctl -ss -v` 出力の traffic flow 変換
- `pflog0` を `tcpdump` 経由で読む firewall log
- `SystemdUnit` からの rc.d スクリプト生成
- 静的 DS-Lite gif tunnel レンダリング

未対応：

- FreeBSD らしい完全なネットワーク設定生成
- 生成した pf と rc.d の自動適用 (`pfctl -nf` と `service <name> onestart` は手動)
- AFTR FQDN や delegated address 由来の動的 DS-Lite
- ベンダー固有 pf log 形式
- DNS resolver / HealthCheck / DHCP server の常駐運用

FreeBSD は Linux 専用の nftables / conntrack / iproute2 を使いません。
`Package` の例は移植済みかスケルトンがあるものだけを含みます。

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `strongswan`, `mpd5` |
| Diagnostics | `bind-tools` |
| Base system | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `tcpdump`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` は次を出力します：

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `rc.d-*`

`pf.conf` と `rc.d-*` は本番レンダラの土台です。`pfctl -nf pf.conf` と `service <name> onestart` を FreeBSD 上で確認してから本番投入してください。

## OS 抽象化の実装方針

新しい OS 固有の振る舞いを足すときは、business logic 層で `runtime.GOOS` を直接読まないでください。
`pkg/platform` 層 (`platform.Features`) または Go の build tag を使って境界を明示します。
未対応 OS で実行時に予期せず失敗するより、validation や planning の段階で明示的にエラーにする方を優先します。
