# routerd

[プロジェクトサイトとドキュメント: routerd.net](https://routerd.net/)

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

## 現在できること

- インターフェース別名、リンク、ブリッジ、VRF、VXLAN、WireGuard、
  cloud VPN 向け IPsec の土台
- DHCPv6 プレフィックス委譲、DHCPv6 情報要求、DHCPv4 リース、PPPoE、
  DS-Lite による WAN 取得
- 管理対象 dnsmasq による DHCPv4 スコープ、固定割り当て、DHCPv6、
  DHCP 中継、RA、PIO、RDNSS、DNSSL、MTU オプション
- `DNSZone` と `DNSResolver` によるローカル権威ゾーン、DHCP 由来レコード、
  条件付き転送、DoH、DoT、DoQ、UDP フォールバック、DNSSEC フラグ、
  複数待ち受け、キャッシュ
- IPv4/IPv6 アドレス派生、静的経路、既定経路ポリシー、経路対象外指定、
  Path MTU 方針、TCP MSS 調整、NAT44、DS-Lite
- `HealthCheck`、`EgressRoutePolicy`、`EventRule`、`DerivedEvent` による
  状態連携
- `Package`、`Sysctl`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit`、
  `NTPClient`、`LogSink`、`LogRetention`、`WebConsole`
- `routerctl` による NAPT と conntrack の確認
- 状態、イベント、コネクション、DNS クエリー、通信フロー、ファイアウォール
  ログ、現在の設定を表示する読み取り専用 Web Console
- OpenTelemetry exporter を設定した場合のログ、メトリクス、トレース送信

状態を持つファイアウォールフィルターは発展中です。
NAT44 と、限定されたファイアウォールおよびログの土台はあります。
ただし、まだ汎用ファイアウォール規則言語ではありません。

## 設定例の位置付け

本番に近い設定例で全体像を確認できます。

- `examples/homert02.yaml`: Ubuntu の家庭用ルーター構成です。OS 準備、
  DHCPv6-PD、DS-Lite、HGW LAN への静的経路、DNS リゾルバー、DHCP サーバー、
  RA、NAT44、ログ保存、Web Console を含みます。
- `examples/router-lab.yaml`: 小さめの Linux ラボ構成です。
- `examples/nixos-edge.yaml`: NixOS 向け生成経路の例です。
- `examples/freebsd-edge.yaml`: FreeBSD のサービス管理とパッケージの土台です。

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

## ビルド

Go 1.24 以降を前提にします。

```sh
make test
make build
make check-schema
make validate-example
make website-build
```

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
routerd validate --config examples/homert02.yaml
routerd plan --config examples/homert02.yaml
routerd apply --config examples/homert02.yaml --once --dry-run
routerctl status
routerctl events --limit 20
routerctl connections --limit 50
```

## 配置

ソースインストール時の既定:

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

主対象は Ubuntu Server です。
NixOS と FreeBSD は、動作するバイナリとサービス管理の土台があります。
ただし、すべての生成器が同等という意味ではありません。
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
