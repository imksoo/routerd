---
title: 対応プラットフォーム
---

# 対応プラットフォーム

![Diagram showing supported platforms with Linux systemd primary integration, FreeBSD rc.d and pf groundwork, and pkg/platform feature-gated implementation rules](/img/diagrams/platforms.png)

routerd は cross-OS を前提に設計されています。
利用するホスト側の機構は OS ごとに異なります。
このページでは、routerd が各プラットフォームで使う OS 機能を明示します。
適用する前に、生成されるファイルと、実行時の所有範囲を確認してください。

## Linux (Ubuntu / Debian)

systemd を使う Linux が主対象です。
リリースインストーラーの配置先は、既定で `/usr/local` 配下です。
Linux 用のリリースアーカイブを展開し、`sudo ./install.sh` を実行します。
インストーラーは `apt-get`、`dnf`、`pacman` のいずれかで、ランタイムのパッケージを導入できます。

routerd が Linux 上で利用する OS 機能は次の通りです。

- systemd ユニット
- `/run/routerd` と `/var/lib/routerd`（ランタイムと永続状態）
- dnsmasq（DHCPv4 / DHCPv6 / DHCP relay / RA）
- nftables（フィルター + NAT）
- conntrack（コネクション観測）
- iproute2（interface + 経路）
- 長寿命の `routerd-bgp` GoBGP デーモン（BGP ピアリングと経路投入）
- keepalived（VRRP の VIP 管理）
- pppd / rp-pppoe（PPPoE）
- WireGuard、Tailscale、strongSwan、radvd

Ubuntu でも、パッケージが事前に導入されていることは前提にしません。
初回の準備では、`install.sh` が実用的な既定セットを導入します。
継続的な宣言管理では、`Package` リソースで依存関係を宣言してください。
参考までに、対象パッケージは次の通りです。

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `keepalived`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS 制御 | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` は、Linux 上では systemd サービスとして動作します。

Ubuntu 26.04 LTS（`resolute`）は、managed dnsmasq、nftables、DHCPv6-PD、
委任された LAN IPv6 アドレス、control API について、Ubuntu 24.04 と同じ Linux
data-plane renderer で実機検証済みです。ただし host bootstrap 側では、OS の
ネットワーク設定に注意点があります。routerd が DHCPv6-PD や LAN RA/DHCPv6
を所有する interface では、OS 側の systemd-networkd が DHCPv6 client socket
を開かないようにしてください。そうしないと、`routerd-dhcpv6-client` より先に
systemd-networkd が UDP port 546 を bind することがあります。

Ubuntu 26.04 の lab router では、OS の DHCP は management interface にだけ残し、
routerd 所有の WAN/LAN interface は OS レイヤーでは link-local のみにします。

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    ens18:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    ens19:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    ens20:
      dhcp4: true
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
```

WAN link で RA から得る IPv6 default route が必要な場合は、WAN interface と
DHCPv6 / RA resource を宣言します。routerd は systemd-networkd drop-in として
`IPv6AcceptRA=yes` と `[IPv6AcceptRA] DHCPv6Client=no` を導出するため、RA は受けつつ、
OS 側の DHCPv6 client は無効のままにできます。

## FreeBSD

FreeBSD も Ubuntu と同じ routerd リソースモデルを使います。
反映先は FreeBSD のホスト機構です。
DHCPv6-PD クライアントは `daemon(8)` で実行し、リースを安定して維持します。
routerd は Linux 用の機構ではなく、FreeBSD の `rc.conf`、`rc.d`、`pf`、`mpd5`、`ifconfig`、dnsmasq にリソースを対応付けます。
FreeBSD 用のリリースアーカイブを展開し、`sudo ./install.sh` を実行します。
インストーラーは `pkg` で ports のパッケージを導入し、基本システムのコマンドは導入せず確認だけ行います。

実装済みの項目は次の通りです。

- DHCPv6-PD デーモンとリースの永続化
- WireGuard による Linux との相互接続
- VXLAN over WireGuard
- `mpd5.conf`、`mpd_enable`、`mpd5` サービスの再起動による PPPoE
- `Package` の `pkg` 経由での導入
- `gateway_enable`、`ipv6_gateway_enable`、`cloned_interfaces`、`ifconfig_*`、`static_routes`、`ipv6_static_routes`、`pf_enable`、`pflog_enable`、`mpd_enable` の、FreeBSD らしい `rc.conf.d` 出力
- `routerd render freebsd --out-dir` による `dhclient.conf`、`mpd5.conf`、`pf.conf`、dnsmasq 設定、`rc.d` スクリプトの生成
- `FirewallZone` / `FirewallPolicy` / `FirewallRule` からの pf のレンダリング
- `NAT44Rule` からの pf NAT のレンダリング
- 生成された `pf.conf` の `pfctl -nf` による検証と `pfctl -f` による適用
- `pfctl -ss -v` 出力の traffic flow への変換
- `pflog0` を BPF から直接読む firewall log。packet を解析するため、tcpdump の文字列表現の差異に依存しません
- DHCPv4、DHCPv6、RA 用の管理対象 dnsmasq
- `/var/db/routerd/dnsmasq` 配下での dnsmasq リースの永続化
- サービス再起動前の `dnsmasq --test` による設定確認
- DHCP、DNS、RA、DHCPv6-PD、DS-Lite、WireGuard、HealthCheck に必要な pf の穴の自動生成
- `generated service artifacts` からの rc.d スクリプトの生成
- `routerd-healthcheck` の rc.d スクリプトの生成
- `routerd-firewall-logger` の rc.d スクリプトの生成と `pflog0` の直接読み取り

- `TailscaleNode` の rc.d スクリプトの生成
- `mode: vrrp` の `VirtualAddress` で、CARP を使った親インターフェース上の `vhid` 設定
- dnsmasq の rc.d 順序を `mpd5` の後に配置（PPPoE との共存）
- 静的 DS-Lite gif トンネルのレンダリング
- 静的 AFTR IPv6、AFTR FQDN、委任アドレス由来の local source による動的 DS-Lite の適用
- cloud VPN 向け `IPsecConnection` の検証と、strongSwan `swanctl` 接続定義の生成。クラウドゲートウェイとの実疎通確認は環境ごとに行います
- DNS リゾルバーデーモンの FreeBSD ビルド。`DNSUpstream.spec.sourceInterface` は `fib:<n>` で FIB バインド型の上流ルーティングに対応
- healthcheck 向けの native `route -n get` と、`RTF_PROTO1` による BGP FIB 所有権（replace / withdraw / foreign route 保持を含む）
- FreeBSD peer 上の FRR `bfdd` reconcile と、実機で観測した Up → Down → Up 回復
- FreeBSD native doctor、KernelModule の `kldload` reconcile、BGP 専用 `routerd_bgp` rc.d 生成
- FreeBSD には probe mark を request-scoped policy route に対応付ける Linux `SO_MARK` 相当がないため、fwmark healthcheck を明示拒否（unmarked route と `sourceInterface`/`sourceAddress` を使用）
- FreeBSD に Linux `IP_FREEBIND` 相当がないため、non-local DNS resolver bind を明示拒否（resolver 起動前に address を割り当てる）。outbound DNS は別の明示機構である `sourceInterface: fib:<n>` を使用可能

ARP/RA observer daemon は FreeBSD 基本システムの tcpdump/libpcap BPF 経路で capture し、proactive ARP write は独立した direct-BPF descriptor を維持します。provisioned native CI は disposable VNET 上で両 daemon を実行し、ARP observation event と rogue-RA event を必須にします。tag 付き native DPI backend は FreeBSD ports の `ndpi` 5.0 ABI に対応し、同じ native gate の TLS/SNI classification self-test で検証します。

FreeBSD でも `ClientPolicy` を利用できます。IPv4 は `DHCPv4Reservation` を基にした pf 近似で、IPv6 guest identity は `classification[].ipv6Addresses` に明示します。routerd は IPv4 reservation、MAC、hostname、OUI、DHCP fingerprint から IPv6 identity を推測しません。明示 IPv6 address は `inet6` guest-egress deny rule に描画されます。
これは Linux の MAC ベース隔離と同等ではありません。pf は routed filter path で nftables の Ethernet 送信元 selector に一致できず、privacy/unlisted IPv6 address はこの slice の対象外です。別ネットワーク segmentation を使ってください（[#849](https://github.com/imksoo/routerd/issues/849)）。

FreeBSD は、Linux 専用の nftables / conntrack / iproute2 を使いません。
`Package` の例は、FreeBSD 側の置き換えを宣言します。
pf と `pflog0` は基本システムを使います。
PPPoE は `mpd5`、DS-Lite は `ifconfig gif`、LAN の DHCP/RA は dnsmasq を使います。
WireGuard、Tailscale、strongSwan は ports のパッケージを使います。

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| 任意の native DPI | `ndpi` |
| Diagnostics | `bind-tools` |
| 基本システム | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `tcpdump`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` は次を出力します。

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `rc.d-*`

`routerctl apply` は生成した `pf.conf` を導入します。
その前に `pfctl -nf` で構文を確認します。
dnsmasq も `dnsmasq --test` で設定を確認してから再起動します。
導入後は `pfctl -f` で反映し、生成した rc.d スクリプトを `service <name> onestart` で起動します。
静的な `rc.conf` の生成だけでは足りない DS-Lite tunnel は、`ifconfig gif` で動的に適用します。
本番運用の前には、`routerd render freebsd` で出力を確認してください。

## プラットフォーム差分の残課題

Ubuntu と FreeBSD を比較したときの既知の差分です。

| 領域 | 現在の差分 | 残課題 |
| --- | --- | --- |
| CI と実機検証の網羅 | PR CI は FreeBSD amd64/arm64 binary を compile し、provisioned FreeBSD 14.3 amd64 VM で省略なし `go test ./...`、live routerd smoke、ARP/RA observer、native nDPI を実行します。保持済み VM115 evidence は route lookup、BFD、対応 PF dataplane slice も対象にします。 | PR の native runtime certification は現在 amd64 が対象です。arm64 は PR CI では compile-only です。 |
| FreeBSD の機能制約 | `ClientPolicy` は DHCPv4 reservation による IPv4 と明示 `classification[].ipv6Addresses` による IPv6 pf rule を使います。MAC/L2 照合と IPv4 reservation からの IPv6 推測はできません。 | 明示 address と MAC/L2 制約を維持し、unlisted/privacy IPv6 address は別 segmentation を要求します（[#849](https://github.com/imksoo/routerd/issues/849)）。 |
| パッケージのブートストラップ | Ubuntu、FreeBSD はパッケージを命令的に導入できます。 | `apt`、`pkg` について、スキーマ、検証、インストーラーのパッケージ一覧、設定例、生成ドキュメントの同期を保ちます。 |

## OS 抽象化の実装方針

新しい OS 固有の振る舞いを足すときは、ビジネスロジック層で `runtime.GOOS` を直接読まないでください。
`pkg/platform` 層（`platform.Features`）または Go のビルドタグを使って、境界を明示します。
対象外の OS で実行時に予期せず失敗するよりも、validation や planning の段階で明示的にエラーにすることを優先します。
