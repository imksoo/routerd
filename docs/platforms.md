# 対応プラットフォーム

routerd の第一対象は Ubuntu Server です。
NixOS と FreeBSD は実機検証済みの範囲がありますが、すべての生成器と適用処理が Ubuntu と同じではありません。
利用者向け文書では、実装済みの範囲と土台だけの範囲を分けて説明します。

## Ubuntu Server

Ubuntu は現在の主対象です。
標準のソースインストール先は `/usr/local` 配下です。

主に次を使います。

- systemd ユニット
- `/run/routerd` と `/var/lib/routerd`
- dnsmasq
- nftables
- conntrack
- iproute2
- pppd と rp-pppoe
- WireGuard
- strongSwan
- radvd

Ubuntu でも、既定で入っていることを前提にしません。
`Package` リソースで依存パッケージを明示します。
主な一覧は次の通りです。

| 分類 | パッケージ |
| --- | --- |
| 実行系 | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `strongswan-swanctl`, `radvd` |
| 診断系 | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS 制御 | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` は Ubuntu 上で systemd サービスとして動かします。

## NixOS

NixOS は第二対象です。
Phase 1.7 では router02 で DHCPv6-PD デーモンを宣言的な NixOS 設定へ移しました。
一時的な systemd ユニットではなく、`/etc/nixos/routerd-generated.nix` による管理です。

確認済みの範囲:

- `routerd-dhcpv6-client` の systemd ユニット生成
- `Package`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` の NixOS module 生成
- `nixos-rebuild test` と `nixos-rebuild switch`
- DHCPv6-PD の Bound 維持
- WireGuard と VXLAN の検証
- VRF の一部検証

未完了の範囲:

- NixOS module 生成物の実機適用範囲拡大
- nftables、dnsmasq、DNS resolver、HealthCheck などの常駐サービス全体の実機検証
- NixOS 固有の rollback と generation 管理との統合

NixOS では、使うコマンドを `systemd.services.routerd.path` に入れます。
`Package` リソースで `os: nixos` を書く場合は、次の名前を使います。
routerd は NixOS 上で実行時にパッケージを導入しません。
`routerd render nixos` が `environment.systemPackages` を生成します。

| 分類 | パッケージ |
| --- | --- |
| 実行系 | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `wireguard-tools`, `strongswan`, `radvd` |
| 診断系 | `bind`, `iputils`, `tcpdump`, `traceroute`, `nettools` |
| OS 制御 | `procps`, `systemd`, `kmod` |

## FreeBSD

FreeBSD も第二対象です。
router01 と router04 では `routerd-dhcpv6-client` を daemon(8) で動かし、DHCPv6-PD の Bound 維持を確認しています。

確認済みの範囲:

- DHCPv6-PD デーモンの起動とリース永続化
- FreeBSD と NixOS 間の WireGuard 検証
- VXLAN over WireGuard の検証
- FreeBSD 向け PPPoE 実装の土台
- `Package` の `pkg` 実行経路
- `FirewallZone`、`FirewallPolicy`、`FirewallRule` からの pf 生成
- `IPv4SourceNAT` と `NAT44Rule` からの pf NAT 生成
- `pfctl -ss -v` 出力の traffic flow 変換
- `pflog0` を `tcpdump` 経由で読む firewall log 入力
- `SystemdUnit` からの rc.d スクリプト生成
- 静的な DS-Lite gif 生成

未完了の範囲:

- FreeBSD らしいネットワーク設定生成の完全対応
- 生成した pf と rc.d の実機適用
- AFTR FQDN や delegated address 由来の動的 DS-Lite
- pf log の実機形式差分の取り込み
- FreeBSD 用 DNS resolver、HealthCheck、DHCP サーバーの常駐運用検証

FreeBSD では、Linux 専用の nftables、conntrack、iproute2 は使いません。
現在の `Package` 例では、移植済みまたは土台のあるものだけを入れます。

| 分類 | パッケージ |
| --- | --- |
| 実行系 | `dnsmasq`, `wireguard-tools`, `strongswan`, `mpd5` |
| 診断系 | `bind-tools` |
| base system | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `tcpdump`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` は、次の生成物を出します。

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `rc.d-*`

`pf.conf` と `rc.d-*` は、0.4.0 に向けた生成器の土台です。
実機に入れる前に、FreeBSD 上で `pfctl -nf pf.conf` と `service <name> onestart` による確認が必要です。

## ラボの扱い

現在の健全な実装ラボは pve05、pve06、pve07 上の VM です。
過去に pve01 から pve04 の vmbr0 VLAN 1901 経路で DHCPv6-PD が不安定に見えた時期がありました。
その経路は設計判断の根拠にしません。

現在の 5 台ラボは次の状態です。

| ホスト | OS | 役割 |
| --- | --- | --- |
| router01 | FreeBSD | `routerd-dhcpv6-client` で DHCPv6-PD |
| router02 | NixOS | 宣言的 systemd ユニットで DHCPv6-PD |
| router03 | Ubuntu | DHCPv6-PD と PPPoE 試験 |
| router04 | FreeBSD | DHCPv6-PD と複数 OS 横断検証 |
| router05 | Ubuntu | routerd コントローラーチェーン、DS-Lite 実適用、dnsmasq 検証 |

## OS 判定の実装方針

新しい OS 差分を追加するときは、`runtime.GOOS` を直接読むのではなく `pkg/platform` を使います。
Linux 専用機能は `platform.Features` や build tag で分けます。
対応していない OS で実行時に突然失敗するより、検証や計画の段階で明示することを優先します。
