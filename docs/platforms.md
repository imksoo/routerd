---
title: 対応プラットフォーム
---

# 対応プラットフォーム

routerd は cross-OS を前提に設計されています。
利用するホスト側の機構は OS ごとに異なります。
このページでは、routerd が各プラットフォームで使う OS 機能を明示します。
適用する前に、生成されるファイルと、実行時の所有範囲を確認してください。

## Linux (Ubuntu / Debian)

systemd を使う Linux が主対象です。
リリースインストーラーの配置先は、既定で `/usr/local` 配下です。
Linux 用のリリースアーカイブを展開し、`sudo ./install.sh` を実行します。
インストーラーは `apt-get`、`dnf`、`apk`、`pacman` のいずれかで、ランタイムのパッケージを導入できます。

routerd が Linux 上で利用する OS 機能は次の通りです。

- systemd ユニット
- `/run/routerd` と `/var/lib/routerd`（ランタイムと永続状態）
- dnsmasq（DHCPv4 / DHCPv6 / DHCP relay / RA）
- nftables（フィルター + NAT）
- conntrack（コネクション観測）
- iproute2（interface + 経路）
- pppd / rp-pppoe（PPPoE）
- WireGuard、Tailscale、strongSwan、radvd

Ubuntu でも、パッケージが事前に導入されていることは前提にしません。
初回の準備では、`install.sh` が実用的な既定セットを導入します。
継続的な宣言管理では、`Package` リソースで依存関係を宣言してください。
参考までに、対象パッケージは次の通りです。

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
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

## Alpine Linux

Alpine は、ライブ ISO と最小構成の導入済みホスト向けの Linux 対象です。
まだ Ubuntu と同等の扱いではありません。
routerd は使える範囲で Linux の data plane ツールを使いますが、service の activation は systemd ではなく OpenRC 側に課題が残っています。

実装済みの項目は次の通りです。

- Alpine でのライブ ISO 起動と USB 永続化
- `install.sh` による `apk` 依存パッケージの導入
- `pkg/platform` での Alpine 検出と `HasOpenRC`
- `Package` リソースの `os: alpine` / `manager: apk`
- Alpine の `install.sh --list-deps` と、最小限の `Package` validate / dry-run apply path の CI smoke coverage
- `routerd render alpine --out-dir` による OpenRC script と dnsmasq 設定の生成
- 明示的な `generated service artifacts`、managed dnsmasq、`routerd-healthcheck`、DHCPv4 / DHCPv6 client、DNS resolver、firewall logger、PPPoE、Tailscale の OpenRC script の rendering
- `rc-update` / `rc-service` による apply 時の activation。状態が変わらない場合は enable / start / restart を重複実行しない確認を入れています
- 導入済み Alpine guest 向けの `make alpine-vm-smoke` harness
- Alpine 向けの nftables、conntrack、iproute2、dnsmasq、PPP、WireGuard、strongSwan、radvd、診断パッケージの名前の整理

Ubuntu と同等と呼ぶ前の backlog は次の通りです。

- DNS resolver の runtime config を、OpenRC service を有効化する前に materialize すること。script は生成しますが、現時点では enable / start しません
- ライブ ISO の bootstrap 以外での、導入済みホストの networking 所有
- Alpine の導入済みホスト向け smoke harness を通常の VM CI job に昇格し、OpenRC activation と実際の package-manager command path を継続的に確認すること
- OpenRC 上で未対応のまま残す、systemd 専用 resource の詳しいドキュメント

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `ppp-pppoe`, `wireguard-tools`, `strongswan`, `radvd` |
| Diagnostics | `bind-tools`, `iputils`, `iputils-tracepath`, `tcpdump` |
| OS 制御 | `alpine-conf`, `kmod`, `util-linux`, `e2fsprogs`, `dosfstools`, `exfatprogs` |

## NixOS

NixOS は Ubuntu と同じ routerd リソースモデルを使います。
ただし、反映は NixOS モジュール経由です。
一時的な systemd ユニットを書く代わりに、`/etc/nixos/routerd-generated.nix` を生成します。
その後、`nixos-rebuild test` / `nixos-rebuild switch` で有効化します。

実装済みの項目は次の通りです。

- NixOS の有効化、再起動後の復元、DHCPv6-PD、dnsmasq による LAN サービス、DNS リゾルバ、DS-Lite、nftables の NAT とファイアウォール、HealthCheck、Web 管理画面の世代差分、OpenTelemetry 送信の実機検証
- `routerd-dhcpv6-client` の systemd ユニット生成
- `routerd-dhcpv4-client` の systemd ユニット生成
- `routerd-pppoe-client` の systemd ユニット生成
- `Package` override、`SysctlProfile`、derived host runtime artifact、`generated service artifacts` の NixOS モジュール生成
- `nixos-rebuild test` / `nixos-rebuild switch` 連携
- `nixos-rebuild switch` 失敗時の `nixos-rebuild switch --rollback` 試行
- `nixos-rebuild` 前後の `generation` 記録
- DHCPv6-PD が `Bound` まで到達
- DHCP または RA リソースが dnsmasq を必要とする場合の `routerd-dnsmasq` サービス生成
- `routerd-dnsmasq` サービスでは、NixOS のシステムプロファイル内の絶対パスを使います。root のまま実行する指定も入れるため、systemd の保護設定下でも `PATH` 探索や権限降格に依存しません
- DNS resolver、HealthCheck、firewall logger、Tailscale、DHCPv4 クライアント、DHCPv6 クライアント、PPPoE クライアントのサービス生成
- NAT、firewall、policy routing、Path MTU リソースが nftables を必要とする場合の `networking.nftables.enable = true` の生成
- WireGuard、Tailscale、VXLAN、systemd-networkd による VRF の生成
- NixOS の native な network 宣言では表せない Linux 実行時リソースは、NixOS の有効化後に `routerd.service` が調整

NixOS では、routerd が必要とするコマンドを `systemd.services.routerd.path` に入れてください。
`install.sh` は NixOS を検出すると、`nix-env` を実行せず警告だけ出します。
NixOS のパッケージ状態は宣言型で管理してください。
`Package` リソースに `os: nixos` を書く場合、routerd は実行時にパッケージを導入しません。
`routerd render nixos` が `environment.systemPackages` を生成します。

NixOS の有効化後の inventory は次の通りです。

| 領域 | 現在の所有者 | メモ |
| --- | --- | --- |
| package と routerd service path | 生成された NixOS module | `Package` resource は `environment.systemPackages` になります。routerd は `nix-env` を呼びません。 |
| helper daemon service 定義 | 生成された NixOS module | DHCPv4、DHCPv6、PPPoE、HealthCheck、firewall logger、Tailscale、dnsmasq は Nix の systemd service として表現します。 |
| nftables の有効化 | 生成された NixOS module | NAT、firewall、policy routing、Path MTU resource が必要とする場合に `networking.nftables.enable = true` を出します。 |
| runtime のみの network 変更 | 有効化後の `routerd.service` | 動的な DS-Lite、一時的な経路判断、status 由来の変更には runtime の reconciliation が必要です。 |
| 旧 runtime dnsmasq unit の cleanup | 有効化後の `routerd.service` | 古い `/run/systemd/system/routerd-dnsmasq.service` artifact を移行時に消すため、一時的に残します。導入済み host が 1 release cycle を経たら削除します。 |

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `strongswan`, `radvd` |
| Diagnostics | `bind`, `iputils`, `tcpdump`, `traceroute`, `nettools` |
| OS 制御 | `procps`, `systemd`, `kmod` |

## FreeBSD

FreeBSD も Ubuntu と同じ routerd リソースモデルを使います。
反映先は FreeBSD のホスト機構です。
DHCPv6-PD クライアントは `daemon(8)` で実行し、リースを安定して維持します。
routerd は Linux 用の機構ではなく、FreeBSD の `rc.conf`、`rc.d`、`pf`、`mpd5`、`ifconfig`、dnsmasq にリソースを対応付けます。
FreeBSD 用のリリースアーカイブを展開し、`sudo ./install.sh` を実行します。
インストーラーは `pkg` で ports のパッケージを導入し、基本システムのコマンドは導入せず確認だけ行います。

実装済みの項目は次の通りです。

- DHCPv6-PD デーモンとリースの永続化
- WireGuard による Linux / NixOS との相互接続
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

`ClientPolicy` は、現時点では Linux 専用のファイアウォール機能です。
MAC アドレスでゲスト端末を隔離するために、nftables の Ethernet 送信元アドレス set を使います。
FreeBSD pf は同じモデルを routed filter path で扱えないため、routerd はこのリソースを明示的に未対応として扱います。
- `TailscaleNode` の rc.d スクリプトの生成
- 静的 DS-Lite gif tunnel のレンダリング
- 静的 AFTR IPv6、AFTR FQDN、委任アドレス由来の local source による動的 DS-Lite の適用
- cloud VPN 向け `IPsecConnection` の検証と、strongSwan `swanctl` 接続定義の生成。クラウドゲートウェイとの実疎通確認は環境ごとに行います

FreeBSD は、Linux 専用の nftables / conntrack / iproute2 を使いません。
`Package` の例は、FreeBSD 側の置き換えを宣言します。
pf と `pflog0` は基本システムを使います。
PPPoE は `mpd5`、DS-Lite は `ifconfig gif`、LAN の DHCP/RA は dnsmasq を使います。
WireGuard、Tailscale、strongSwan は ports のパッケージを使います。

| 分類 | パッケージ |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| Diagnostics | `bind-tools`, `tcpdump` |
| 基本システム | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `tcpdump`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` は次を出力します。

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
静的な `rc.conf` の生成だけでは足りない DS-Lite tunnel は、`ifconfig gif` で動的に適用します。
本番運用の前には、`routerd render freebsd` で出力を確認してください。

## Platform parity backlog

Ubuntu、NixOS、FreeBSD、Alpine を比較したときの既知の差分です。

| 領域 | 現在の差分 | backlog |
| --- | --- | --- |
| CI / runtime coverage | CI は Ubuntu 上で unit test と Linux static check を実行します。Alpine は host に依存しない installer dependency smoke、最小限の `Package` validate / dry-run coverage、導入済みホスト向けの smoke harness を持っていますが、Alpine の activation はまだ通常の VM job ではありません。FreeBSD は release 時に cross build しますが、NixOS の activation もまだ VM job になっていません。 | FreeBSD VM、NixOS VM、Alpine VM の smoke job を追加し、validate、plan、dry-run apply、実際の package-manager check、service activation、renderer の syntax check を流します。 |
| Alpine の service manager | Alpine は明示的な `generated service artifacts`、managed dnsmasq、`routerd-healthcheck`、DHCP client、DNS resolver、firewall logger、PPPoE、Tailscale の OpenRC rendering を持っています。apply 時の activation は `rc-update` / `rc-service` を使い、状態が変わらない場合の重複した enable / start / restart を避けます。DNS resolver script は生成しますが、runtime config の materialization が入るまでは enable / start しません。 | OpenRC 向けの DNS resolver runtime config の materialization、導入済みホストの networking 所有の拡張、Alpine smoke harness の CI 昇格を進めます。 |
| NixOS に残る命令的な部分 | NixOS は module を生成し、activation は `nixos-rebuild` に任せます。runtime のみの network 変更と、旧 dnsmasq unit の cleanup は、activation 後の `routerd.service` に残っています。この cleanup は、生成された NixOS dnsmasq service の ownership を含む最初の release に向けて、意図的に残しています。 | その release cycle のあとで旧 dnsmasq cleanup を削除し、NixOS の native な declaration で表せるものについては activation 後の reconciliation を減らし、残す runtime のみの resource には test を追加します。 |
| FreeBSD の機能例外 | `ClientPolicy` は nftables の Ethernet source address set に依存するため、Linux 専用です。 | 同じ isolation の意味を保てる設計ができるまで、明示的に拒否します。 |
| Package bootstrap | Ubuntu、Alpine、FreeBSD は package を命令的に導入できます。NixOS は意図的に package declaration を生成します。schema、validation、example、installer の dependency list、CI smoke coverage は `apk` を含むように更新済みです。 | `apt`、`apk`、`pkg`、Nix declaration について、schema、validation、installer の package list、example、生成ドキュメントの同期を保ちます。 |

## OS 抽象化の実装方針

新しい OS 固有の振る舞いを足すときは、business logic 層で `runtime.GOOS` を直接読まないでください。
`pkg/platform` 層（`platform.Features`）または Go の build tag を使って、境界を明示します。
対象外の OS で実行時に予期せず失敗するよりも、validation や planning の段階で明示的にエラーにすることを優先します。
