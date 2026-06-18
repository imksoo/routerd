---
title: アーキテクチャ概要
---

# routerd アーキテクチャ概要

このドキュメントでは、routerd の設計思想と内部構造を、運用担当者やコントリビューター向けに概観します。
個別機能の使い方は [チュートリアル](./tutorials/getting-started.md) と [How-to](./how-to/multi-wan.md) を、
リソース定義は [API リファレンス](./api-v1alpha1.md) を参照してください。

![router YAML と routerctl から validation、effective config、controller、SQLite state、renderer、所有 host artifact へ流れる routerd architecture 図](/img/diagrams/routerd-architecture.png)

---

## 1. routerd の位置づけ

routerd は宣言型のルーターフレームワークです。
家庭用ルーター、SOHO ルーター、小規模データセンターのエッジルーターを、同じ primitive で構築できることを目標としています。

具体的な置き換え対象として、次の 3 つを想定しています。

| ターゲット | 対象範囲 | 必要な機能段階 |
| --- | --- | --- |
| 家庭ルーター置換 | 1 ホスト、1-2 アップリンク、1-3 LAN VLAN | H |
| ハイパーバイザーの SDN ルーター | クラスター内の VXLAN / EVPN / underlay routing | C |
| Kubernetes クラスターのエッジ | BGP で Pod CIDR / LoadBalancer IP を広告、ingress 終端 | S → C |

3 つは同じ宣言型の primitive で扱える設計とし、目的に応じて段階的に機能を有効化します。

### 1.1 機能段階 (capability tier)

| tier | 用途 | 主機能 |
| --- | --- | --- |
| **H** (Home) | 家庭・小規模オフィス | WAN acquire (PD/RA/PPPoE/DHCPv4/DS-Lite)、LAN service (RA/DHCPv6/dnsmasq)、NAT44、firewall、`EgressRoutePolicy` |
| **S** (SOHO/branch) | 数拠点・VPN 中心 | + WireGuard / IPsec、VRF、VPN 上の dynamic routing、commit-confirmed |
| **C** (Campus / Small DC) | 数十ノード | + EVPN-VXLAN、iBGP RR、BFD、RouteMap DSL、より高度な routing policy |
| **E** (Enterprise / SP) | 数百ノード以上 | + フル BGP、MP-BGP L3VPN、segment routing、HA leader election |

primitive は H から E まで共通です。tier が上がるにつれて、routing と policy の controller が増えていきます。

---

## 2. 動作環境

### 2.1 配備形態

routerd は仮想マシン上での動作を想定しています。物理アプライアンスへの組み込みは今後の検討課題です。

仮想化環境への要件は次の通りです。

- virtio NIC（vmxnet・ne2k などは対象外）
- 特権カーネルモジュールに依存しない（DPDK / XDP は任意、host passthrough は不要）
- コンソールと SSH で運用する
- 検証ではスナップショットとクローンを活用する

### 2.2 OS 戦略

routerd は cross-OS を前提に設計し、同一バイナリ・同一設定で複数の OS をサポートします。

| OS | 評価 | 用途 |
| --- | --- | --- |
| **Linux (Ubuntu / Debian)** | systemd 標準、入手容易、kernel 新しめ | 開発・本番双方の主流 |
| **FreeBSD** | base 安定、リソース小、jail 隔離 | 長期運用・低リソース環境 |

OS 固有の差分は `pkg/platform` 層で吸収します。
nftables ↔ pf、systemd-networkd ↔ rc.conf、systemd unit ↔ rc.d スクリプトといった対応は、各 OS の renderer が引き受けます。

バージョン方針として、routerd は `vYYYYMMDD.HHmm` 形式の日付と時刻に基づく版番号を使います。従来の `0.x.y` 形式と `yyyymmdd.N` 形式のプレリリース番号は廃止します。

---

## 3. アーキテクチャ全体図

```
┌─────────────────────────────────────────────────────────────────┐
│ ユーザー                                                          │
│   /etc/routerd/*.yaml  +  routerctl CLI                          │
└─────────┬─────────────────────────────────────────┬───────────────┘
          │ inotify                          HTTP+JSON
          │ (検出のみ)                       (明示 apply)
          ▼                                         ▼
┌─────────────────────────────────────────────────────────────────┐
│ routerd (1 binary, multi-OS)                                      │
│                                                                   │
│   ConfigWatcher ──notify only──▶ Bus                              │
│   ConfigLoader ◀──explicit trigger───── routerctl apply           │
│                                                                   │
│   ┌──────────────────────────────────────────────────────────┐   │
│   │ Bus (in-process channel + SQLite events 永続層)           │   │
│   │  topics: routerd.<area>.<subject>.<verb>                  │   │
│   │  cursor: events.id (autoincrement)                        │   │
│   │  fanout: subscribe pattern match → controller channel     │   │
│   └─────┬─────────────────────────────────────────────────────┘   │
│         │                                                         │
│         ▼ Controllers (in-process reactor 群)                     │
│   PrefixDelegationCtrl / LANAddressCtrl / RAAnnouncerCtrl         │
│   DNSAnswerCtrl / DNSResolverCtrl / FirewallCtrl / RouteCtrl      │
│   EgressRouteCtrl / ServiceLifecycleCtrl / ConfigLoaderCtrl       │
│   EventRuleEngine / DerivedEventEngine                            │
│         │                                                         │
│         ▼ SQLite state DB (objects/events/artifacts/generations)  │
└─────────┬─────────────────────────────────────────────────────────┘
          │ Unix socket HTTP+JSON                fsnotify (lease/snapshot)
          ▼                                            ▲
┌─────────────────────────────────────────────────────────────────┐
│ Layer 1 source daemons (各々 1 process)                           │
│   routerd-dhcpv6-client / routerd-dhcpv4-client                   │
│   routerd-pppoe-client / routerd-dns-resolver                     │
│   routerd-healthcheck@<resource> / routerd-firewall-logger        │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. リソースモデル

routerd の設定はリソースの集合として記述します。Kubernetes に似ていますが、apiVersion の階層と controller の構造はより単純です。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: ds-lite-primary
  spec:
    aftrFQDN: gw.transix.jp
```

### 4.1 主要 apiVersion

| apiVersion | 役割 |
| --- | --- |
| `net.routerd.net/v1alpha1` | ネットワーク機能 (Interface、IPv4Static、DSLite、PPPoE、EgressRoute、HealthCheck など) |
| `dns.routerd.net/v1alpha1` | DNS (DNSZone、DNSResolver、DHCPv4Reservation など) |
| `firewall.routerd.net/v1alpha1` | ファイアウォール（FirewallZone、FirewallPolicy、FirewallRule、NAT44Rule など） |
| `system.routerd.net/v1alpha1` | OS bootstrap intent と override (Package、SysctlProfile、WebConsole など)。host runtime artifact は resource から自動導出します |
| `control.routerd.net/v1alpha1` | controller chain と routerctl の制御 API |

完全な一覧は [API リファレンス](./api-v1alpha1.md) を参照してください。

### 4.2 リソース間の参照

あるリソースが別のリソースの status を参照する場合は、値を直接書かずに、型付きの `*From` フィールドで書きます。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WebConsole
  spec:
    listenAddressFrom:
      resource: Interface/mgmt
      field: ipv4Addresses
    port: 8080
```

`addressFrom`、`ipv4From`、`ipv6From`、`prefixFrom`、`rdnssFrom`、`gatewayFrom`、`upstreamFrom` は同じ形式です。依存関係（`dependsOn`）も同じ仕組みで宣言します。

`*From` 参照の対象がまだ値を公開していない状態は、エラーではなく通常のブートストラップ条件です。参照元のコントローラーはそのリソースを `Pending`（未解決の参照を理由に記載）と報告し、参照先の status が変わったときに再リコンサイルします。明示的な `dependsOn` は不要です。たとえば、`upstreamFrom` が `DHCPv6Information` のサーバーを指す `DNSResolver` のフォワードソースは、そのサーバーが DNS サーバーを学習するまで `Pending` にとどまり、次のリコンサイルで `Applied` になります。upstream を一切宣言しないソース（`upstreams` も `upstreamFrom` もなし）は本当の設定誤りであり、検証で拒否されます。

詳細は [リソースモデル](./concepts/resource-model.md) と [状態と所有権](./concepts/state-and-ownership.md) を参照してください。

---

## 5. Event bus と controller chain

routerd は in-process の event bus と複数の controller を組み合わせて、宣言された望ましい状態へ収束させます。

### 5.1 Event bus

- in-process channel と SQLite イベントログによる永続化
- topics は `routerd.<area>.<subject>.<verb>` 形式（例: `routerd.dhcpv6.bind.changed`）
- subscribers は pattern match で受信する
- すべてのイベントは `events.id` を cursor として持ち、再起動後も再評価できる

### 5.2 コントローラーチェーン

すべてのコントローラーは、共通の `framework.FuncController` パターンに従います。

- `Subscriptions`: 関心のある topic
- `Bootstrap`: 起動時に 1 回だけ行う初期化
- `PeriodicFunc`: 定期的な再評価（idempotent）
- `ReconcileFunc`: イベント受信時の状態収束

`eventedStore` ラッパーは、状態を保存するときに必ず `routerd.resource.status.changed` を発行します。
これにより下流のコントローラーが連鎖的に再評価され、リソース間の依存解決が成立します。

Kubernetes エッジリソースもこの status フローを直接使います。`IngressService` のヘルスチェックがアクティブなバックエンドを選択し、NAT レンダラーが次のリコンサイルでその status を使います。`BGPRouter` / `BGPPeer` の status は、常駐する `routerd-bgp` デーモンから型付きの `ListPeer` / `ListPath` API 呼び出しで観測し、`track` を通じて `VirtualAddress` の VRRP 優先度を下げることもできます。BGP の設定変更は、FRR 形式のテキスト設定のレンダリングやリロードツールの呼び出しではなく、GoBGP API オブジェクトでデーモンに適用します。`VirtualAddress` と `IngressService` のホスト名は、DNSResolver が提供するゾーンに導出 A/AAAA レコードとして反映されます。BGP/VRRP/Ingress の status は専用の `routerctl show` ビューと、遷移やバックエンド正常性の低カーディナリティ OTel メトリクスでも可視化されます。

### 5.3 デーモン契約

長時間動作する OS プロセス（DHCPv6 client、DNS リゾルバー、healthcheck など）は、コントローラーではなく **デーモン** として動かします。
デーモンは controller chain と Unix domain socket 上の JSON で通信し、自身の状態を `lease.json` などのファイルに永続化します。

詳細は [reconcile loop の動作](./operations/reconcile) を参照してください。

---

## 6. 設定ファイル運用

routerd の設定ファイル（既定では `/usr/local/etc/routerd/router.yaml`）は、次の流れで反映します。

```
edit → routerctl validate → routerctl apply
                              │
                              └─ ホスト状態の観測
                                 → plan
                                 → ホストアーティファクトのレンダリング
                                 → 状態の記録と終了

routerd serve
  → 状態/イベントを消費
  → 管理対象デーモンの起動・有効化・リロード
  → OS 状態 (nftables / netlink / systemd) を継続的に更新
```

設定ファイルは git で管理することを強く推奨します。
本番ホストへの反映は routerd 経由で宣言型に行い、ホスト上で `nft add rule`、`ip route add`、`sysctl -w` のような ad hoc な変更を加えないでください。
ad hoc な変更は、次回の reconcile で打ち消されるか、あるいは routerd の状態 DB と OS の実状態との間に drift を生みます。

差分（drift）への正しい対処は、新しい desired 状態を設定で表現し直してから再度適用することです。`apply` は速やかに完了してデーモンのライフサイクルをコントローラーランタイムに引き渡す必要があります。常駐する `serve` プロセスが、設定ファイル ↔ 状態 DB ↔ OS の実状態の三角関係を整合させ続けます。

---

## 7. observability と debug

routerd は次の手段で運用状態を観測できます。

- `routerctl status`: 全リソースの phase 一覧
- `routerctl describe <kind>/<name>`: 個別リソースの spec、status、最近の event
- `routerctl events --topic <pattern> --resource <kind>/<name>`: bus event を tail する
- `routerctl plan --diff`: apply 前の差分プレビュー
- Web 管理画面（既定では `http://<mgmt-ip>:8080/`）: summary、events、connections、clients、firewall、config をブラウザで表示する
- `journalctl -u routerd.service -f | grep "routerd event"`: bus event を systemd journal で追跡する

ログは `events.db`（コントローラー由来）、`dns-queries.db`（DNS リゾルバー由来）、`traffic-flows.db`（conntrack/pf 由来）、`firewall-logs.db`（NFLOG/pflog 由来）の 4 つに分けて永続化します。
詳細は [ログストレージ](./concepts/log-storage.md) を参照してください。

OpenTelemetry エクスポートは `observability.routerd.net/v1alpha1` の `Telemetry` リソースで設定します。routerd は OTLP コレクターを同梱しません。エンドポイントが宣言されると、生成される systemd、FreeBSD rc.d のユニットに対応する `OTEL_*` 環境変数が設定され、既存の SDK パスがログ、メトリクス、トレースをそのエンドポイントに送信します。

---

## 8. 関連ドキュメント

- [routerd とは](./concepts/what-is-routerd.md)
- [リソースモデル](./concepts/resource-model.md)
- [設計思想](./concepts/design-philosophy.md)
- [apply と render](./concepts/apply-and-render.md)
- [状態と所有権](./concepts/state-and-ownership.md)
- [reconcile loop](./operations/reconcile)
- [状態 DB の運用](./operations/state-database.md)
- [API リファレンス v1alpha1](./api-v1alpha1.md)
- [プラグインプロトコル](./plugin-protocol.md)
- [対応プラットフォーム](./platforms.md)

