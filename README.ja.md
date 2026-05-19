# routerd

[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

[プロジェクトサイトとドキュメント: routerd.net](https://routerd.net/)

Linux amd64 と FreeBSD amd64 のビルド済みアーカイブは
[GitHub Releases](https://github.com/imksoo/routerd/releases) で公開しています。
インストールとアップグレードは
[`docs/install-and-upgrade.md`](docs/install-and-upgrade.md) を参照してください。

routerd は、汎用ホストを見通しのよいルーターとして動かすための、
プレリリースの宣言的ルーター制御プレーンです。

netplan、systemd-networkd、dnsmasq、nftables、sysctl、個別スクリプト、
systemd ユニットに意図を分散させず、型付き YAML リソースとしてまとめます。
routerd は設定を検証し、計画を表示し、必要なホスト成果物を作成します。
管理対象デーモンを起動し、`routerctl`、ローカル API、ログ、読み取り専用
Web Console で状態を見えるようにします。

routerd の基本思想は単純です。
ルーターはシステムとして設定し、サービスとして観測できるべきです。

## routerd が目指すもの

- **1 つの意図ファイル**: インターフェース、WAN 取得、LAN サービス、DNS、
  NAT、経路ポリシー、sysctl、パッケージ、サービスユニットを同じリソース
  モデルで扱います。
- **小さな管理対象デーモン**: DHCPv4、DHCPv6-PD、PPPoE、ヘルスチェック、
  DNS、イベント中継は Unix ソケット上の HTTP+JSON で状態を公開します。
  shell hook に状態を隠しません。
- **収束する経路選択**: `HealthCheck` と `EgressRoutePolicy` により、起動直後は
  使える経路でサービスを始めます。優先度の高い経路が健康になったら、
  新しい通信からそちらへ流します。conntrack は消しません。
- **明示的な DNS 設計**: dnsmasq は DHCP と RA に限定します。DNS 応答、
  条件付き転送、DoH、DoT、DoQ、UDP フォールバック、キャッシュ、ローカル
  ゾーン、DHCP 由来の名前は `routerd-dns-resolver` が扱います。
- **運用時の可視性**: バスイベント、リソース状態、DNS クエリー、コネクション
  観測、通信フローログ、ファイアウォールログをローカルで確認できます。
  ブラウザーから設定は変更しません。
- **実ホストの準備も宣言**: パッケージ導入、sysctl 既定値、
  systemd-networkd の引き継ぎ、systemd ユニット、ログ転送、Web Console も
  リソースとして宣言します。

## routerd の位置づけ

routerd は、珍しい広さの spectrum を狙っています。
SDN/VNET セグメント間をつなぐ仮想ルーターと、ディスクレス物理 mini PC
ルーターを、同じリソースモデルで扱えます。
生成するホスト成果物は違っても、意図ファイルは同じ形で読めます。

routerd は、他のルータープロジェクトや appliance UI を置き換えるための
ものではありません。
同じネットワーク意図を Proxmox ラボ、NixOS/FreeBSD ルーター、Ubuntu 家庭用
gateway、ライブ ISO で起動するディスクレス mini PC の間で移せることに
強みがあります。

routerd は、次の独立した特徴を大切にします。

- **OS をまたぐ宣言的リソース**: Ubuntu、NixOS、FreeBSD のホスト統合を
  同じモデルで扱い、Alpine Linux はライブ ISO と最小ホスト向けの土台を
  持ちます。
- **ライブ ISO と USB 永続化**: ディスクレス mini PC ルーターを構成できます。
- **観測できる経路判断**: イベント、世代差分、ヘルスチェック、Web Console、
  OpenTelemetry で理由を追えます。
- **多段 WAN fallback**: DS-Lite、PPPoE、DHCP WAN、ローカル経路ポリシーを
  conntrack を消さずに切り替えます。
- **クライアントを意識した LAN policy**: DHCP 固定割り当て、neighbor inventory、
  対応 platform での MAC ベース guest isolation を扱います。

そのため routerd は、Proxmox ラボから家庭用 DS-Lite ルーター、
WireGuard/Tailscale overlay、USB 状態から復元できるディスクレス mini PC へと、
ネットワークが横に広がる場面で使いやすくなります。

## 現在できること

- インターフェース別名、リンク、ブリッジ、VRF、VXLAN、WireGuard、
  Tailscale の exit node と subnet router、cloud VPN 向け IPsec 接続定義、
  strongSwan `swanctl` 設定生成
- DHCPv6 プレフィックス委譲、DHCPv6 情報要求、DHCPv4 リース、PPPoE、
  DS-Lite による WAN 取得
- 管理対象 dnsmasq による DHCPv4 スコープ、固定割り当て、DHCPv6、
  DHCP 中継、RA、PIO、RDNSS、DNSSL、MTU オプション
- `DNSZone` と `DNSResolver` によるローカル権威ゾーン、DHCP 由来レコード、
  条件付き転送、DoH、DoT、DoQ、UDP フォールバック、DNSSEC フラグ、
  複数待ち受け、キャッシュ
- IPv4/IPv6 アドレス派生、静的経路、既定経路ポリシー、経路対象外指定、
  Path MTU 方針、TCP MSS 調整、NAT44、DS-Lite
- Kubernetes edge 用の構成要素として、任意で BFD を使える dual-stack FRR
  backend の BGP peer、Pod/Service CIDR 向け static route helper、
  keepalived backend の IPv4/IPv6 VIP、複数 backend `IngressService` の
  health/failover
- `ClientPolicy`、DHCPv4 固定割り当て、MAC アドレスベース nftables 規則に
  よる同一 LAN 上のゲスト端末隔離
- `HealthCheck`、`EgressRoutePolicy`、`EventRule`、`DerivedEvent` による
  状態連携
- `Package`、`Sysctl`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit`、
  `NTPClient`、`LogSink`、`ObservabilityPipeline`、`RouterdCluster`、
  `LogRetention`、`WebConsole`
- `routerctl` による NAPT と conntrack の確認
- 状態、イベント、コネクション、DNS クエリー、通信フロー、ファイアウォール
  ログ、現在の設定を表示する読み取り専用 Web Console
- OpenTelemetry exporter を設定した場合のログ、メトリクス、トレース送信と、
  stdout / syslog / Loki への内蔵 event log forwarding

状態を持つファイアウォールフィルターは、意図して範囲を絞っています。
routerd は NAT44、ゾーンポリシー、管理対象サービス用の許可、拒否ログ、
通信の確認を生成します。
汎用ファイアウォール規則言語ではありません。

## 設定例の位置付け

本番に近い設定例で全体像を確認できます。

- `examples/home-router.yaml`: Ubuntu の家庭用ルーター構成です。OS 準備、
  DHCPv6-PD、DS-Lite、HGW LAN への静的経路、DNS リゾルバー、DHCP サーバー、
  RA、NAT44、ログ保存、Web Console を含みます。
- `examples/router-lab.yaml`: 小さめの Linux ラボ構成です。
- `examples/nixos-edge.yaml`: NixOS 向け生成経路の例です。
- `examples/freebsd-edge.yaml`: FreeBSD の rc.d、pf、mpd5、dnsmasq、DS-Lite、
  パッケージ、サービスの例です。
- `examples/tailscale-exit-subnet.yaml`: Tailscale の exit node と subnet router
  の広告を管理対象 systemd ユニットで行う例です。
- `examples/guest-mode.yaml`: 同一 LAN 上の端末を MAC アドレスで分類し、
  ゲスト端末を隔離する例です。
- `examples/README.md`: 用途別の設定例一覧です。最小 Tailscale、
  WireGuard hub-spoke、VRF lab、multi-WAN home のテンプレートを含みます。

DHCPv4 の固定割り当ては、リソースとして宣言します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DHCPv4Reservation
metadata:
  name: printer
spec:
  server: lan-dhcpv4
  macAddress: 02:00:00:00:10:10
  hostname: printer
  ipAddress: 172.18.0.150
```

内部ネットワークを NAT の対象から外しつつ、インターネット向け通信だけを
選択された出口へ送れます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-wan
spec:
  type: masquerade
  egressInterface: wan
  sourceRanges:
    - 172.18.0.0/16
  excludeDestinationCIDRs:
    - 192.168.0.0/16
    - 172.16.0.0/12
    - 10.0.0.0/8
```

## クイックスタート

ルーターホスト上でリリースアーカイブを展開し、同梱のインストーラーを実行します。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

FreeBSD では latest release の `routerd-freebsd-amd64.tar.gz` を取得し、
同じ `./install.sh` を実行します。
arm64 ホストでは `routerd-linux-arm64.tar.gz` または
`routerd-freebsd-arm64.tar.gz` を使います。
特定の版を固定したい場合は、各 release ページの版番号付きアーカイブを使います。

Linux 用の release archive には、静的リンクした routerd バイナリを含めます
(`CGO_ENABLED=0`)。
配置先ホストの glibc 版には依存しません。

`install.sh` は必要な OS パッケージを導入し、実行ファイルを
`/usr/local/sbin` に配置します。
また、サービスのテンプレートと `router.yaml.sample` を配置します。
既存の `/usr/local/etc/routerd/router.yaml` は上書きしません。
パッケージ一覧は次のコマンドで確認できます。

```sh
./install.sh --list-deps
```

## ライセンスと再配布

routerd 本体は [BSD 3-Clause License](LICENSE) で配布します。
release archive とライブ ISO には、各 software が持つ別のライセンスが含まれます。
Alpine ベースのライブ ISO は aggregate distribution です。
dnsmasq、nftables、WireGuard tools、ppp、iproute2 などの GPL 系ツールは、
それぞれのライセンスとソース入手経路を保ちます。
ISO 全体が 1 つの GPL work として再ライセンスされるものではありません。

release archive には `share/doc/LICENSE` と
`share/doc/THIRD_PARTY_LICENSES.md` を同梱します。
ライブ ISO では `/usr/share/licenses/routerd/` から同じ通知を確認できます。
一覧は次のコマンドで再生成します。

```sh
make third-party-licenses
```

設定ファイルを作成し、検証します。

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml

routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

管理経路が残ることを確認してから反映します。

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

## 開発者向けビルド

Go 1.24 以降を前提にします。

```sh
make test
make build
make check-schema
make validate-example
make website-build
```

Makefile は開発用です。
利用者向けの配置はリリースアーカイブと `install.sh` で行います。

主な生成物:

- `routerd`
- `routerctl`
- `routerd-dhcpv4-client`
- `routerd-dhcpv6-client`
- `routerd-pppoe-client`
- `routerd-healthcheck`
- `routerd-dns-resolver`
- `routerd-dhcp-event-relay`
- `routerd-firewall-logger`

よく使う確認:

```sh
routerd validate --config examples/home-router.yaml
routerd plan --config examples/home-router.yaml
routerd apply --config examples/home-router.yaml --once --dry-run
routerctl status
routerctl events --limit 20
routerctl connections --limit 50
```

## 配置先

リリースインストーラーの既定:

- 設定: `/usr/local/etc/routerd/router.yaml`
- バイナリ: `/usr/local/sbin/routerd`, `/usr/local/sbin/routerctl`,
  `/usr/local/sbin/routerd-*`
- プラグイン: `/usr/local/libexec/routerd/plugins`
- Linux 実行時: `/run/routerd`
- Linux 状態: `/var/lib/routerd`
- FreeBSD 実行時と状態: `/var/run/routerd`, `/var/db/routerd`

管理対象デーモンは同じローカル契約を公開します。

- `GET /v1/status`
- `GET /v1/healthz`
- `GET /v1/events?since=<cursor>&wait=<duration>`
- `POST /v1/commands/<command>`

## プラットフォーム

もっとも多く検証している対象は Ubuntu Server です。
NixOS と FreeBSD も同じリソースモデルを使います。
反映先は、それぞれの OS に合った機構です。
Alpine はライブ ISO と `apk` package bootstrap に対応していますが、OpenRC
service parity はまだ groundwork として扱います。
現在の対応表は `docs/platforms.md` を参照してください。

routerd はプレリリースです。
ルーターをより安全に、運用しやすくするための整理であれば、
v1alpha1 の名前やフィールドは互換性なしで変わることがあります。

## 現在の非目標

- 遠隔プラグインレジストリー
- 遠隔プラグイン導入
- OS 変更の完全なロールバック
- Web Console からの対話的な設定変更
- 組み込み LLM アシスタント
- Proxmox ラボ自動化
- 汎用ファイアウォール規則言語

正確な設計状態は `docs/design.md` を参照してください。
