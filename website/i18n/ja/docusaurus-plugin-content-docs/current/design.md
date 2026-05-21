---
title: アーキテクチャ概要
---

# routerd アーキテクチャ概要

このドキュメントは routerd の設計思想と内部構造を、運用担当者・コントリビューター向けに概観します。
個別機能の使い方は [チュートリアル](./tutorials/getting-started.md) と [How-to](./how-to/multi-wan.md) を、
リソース定義は [API リファレンス](./api-v1alpha1.md) を参照してください。

---

## 1. routerd の位置づけ

routerd は宣言的に設定するルーター・フレームワークです。
家庭用ルーター、SOHO ルーター、小規模データセンターのエッジルーターを、同じ primitive で構築できることを目標としています。

具体的な置き換え対象として、次の 3 つを想定しています。

| ターゲット | 対象範囲 | 必要な機能段階 |
| --- | --- | --- |
| 家庭ルーター置換 | 1 ホスト、1-2 アップリンク、1-3 LAN VLAN | H |
| ハイパーバイザーの SDN ルーター | クラスター内の VXLAN / EVPN / underlay routing | C |
| Kubernetes クラスターのエッジ | BGP で Pod CIDR / LoadBalancer IP を広告、ingress 終端 | S → C |

3 つは同じ宣言的 primitive で扱える設計とし、目的に応じて段階的に機能を有効化します。

### 1.1 機能段階 (capability tier)

| tier | 用途 | 主機能 |
| --- | --- | --- |
| **H** (Home) | 家庭・小規模オフィス | WAN acquire (PD/RA/PPPoE/DHCPv4/DS-Lite)、LAN service (RA/DHCPv6/dnsmasq)、NAT44、firewall、`EgressRoutePolicy` |
| **S** (SOHO/branch) | 数拠点・VPN 中心 | + WireGuard / IPsec、VRF、VPN 上の dynamic routing、commit-confirmed |
| **C** (Campus / Small DC) | 数十ノード | + EVPN-VXLAN、iBGP RR、BFD、RouteMap DSL、より高度な routing policy |
| **E** (Enterprise / SP) | 数百ノード以上 | + フル BGP、MP-BGP L3VPN、segment routing、HA leader election |

primitive は H から E まで共通です。tier が上がるにつれ、routing と policy controller が増えます。

---

## 2. 動作環境

### 2.1 配備形態

routerd は仮想マシン上での動作を想定しています。物理アプライアンスへの組み込みは将来検討です。

仮想化環境への要件:

- virtio NIC (vmxnet・ne2k などは対象外)
- 特権カーネルモジュール非依存 (DPDK / XDP は任意、host passthrough は不要)
- コンソールと SSH で運用
- 検証ではスナップショットとクローンを活用

### 2.2 OS 戦略

routerd は cross-OS を前提に設計し、同一バイナリ・同一設定で複数 OS をサポートします。

| OS | 評価 | 用途 |
| --- | --- | --- |
| **Linux (Ubuntu / Debian)** | systemd 標準、入手容易、kernel 新しめ | 開発・本番双方の主流 |
| **NixOS** | declarative OS と routerd の親和性高、再現性 | declarative 運用の本命 |
| **FreeBSD** | base 安定、リソース小、jail 隔離 | 長期運用・低リソース環境 |
| **Alpine** | 最小フットプリント、musl、apk | 将来の最小プロファイル |

OS 固有差分は `pkg/platform` 層で吸収します。
nftables ↔ pf、systemd-networkd ↔ rc.conf、systemd unit ↔ rc.d スクリプトといった対応は、各 OS の renderer が引き受けます。

バージョン方針: routerd は `vYYYYMMDD.HHmm` 形式の日付と時刻に基づく版番号を使います。従来の `0.x.y` 形式と `yyyymmdd.N` 形式のプレリリース番号は廃止します。

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

routerd の設定はリソースの集合として記述します。Kubernetes と類似ですが、apiVersion 階層と controller 構造はより単純です。

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
| `firewall.routerd.net/v1alpha1` | Firewall (FirewallZone、FirewallPolicy、FirewallRule、NAT44Rule など) |
| `system.routerd.net/v1alpha1` | OS bootstrap intent と override (Package、SysctlProfile、WebConsole など)。host runtime artifact は resource から自動導出します |
| `control.routerd.net/v1alpha1` | controller chain と routerctl の制御 API |

完全な一覧は [API リファレンス](./api-v1alpha1.md) を参照してください。

### 4.2 リソース間の参照

ある status を別のリソースが参照する場合、即値ではなく型付きの `*From` フィールドで書きます。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WebConsole
  spec:
    listenAddressFrom:
      resource: Interface/mgmt
      field: ipv4Addresses
    port: 8080
```

`addressFrom`、`ipv4From`、`ipv6From`、`prefixFrom`、`rdnssFrom`、`gatewayFrom` などが共通の参照スタイルです。
依存関係 (`dependsOn`) も同じ仕組みで宣言します。

詳細は [リソースモデル](./concepts/resource-model.md) と [状態と所有権](./concepts/state-and-ownership.md) を参照してください。

---

## 5. Event bus と controller chain

routerd は in-process の event bus と複数の controller を組み合わせて、宣言された望ましい状態に収束させます。

### 5.1 Event bus

- in-process channel + SQLite イベントログによる永続化
- topics は `routerd.<area>.<subject>.<verb>` 形式 (例: `routerd.dhcpv6.bind.changed`)
- subscribers は pattern match で受信
- すべてのイベントは `events.id` を cursor として持ち、再起動後も再評価できる

### 5.2 Controller chain

すべての controller は共通の `framework.FuncController` パターンに従います。

- `Subscriptions`: 興味のある topic
- `Bootstrap`: 起動時の 1 回限りの初期化
- `PeriodicFunc`: 定期再評価 (idempotent)
- `ReconcileFunc`: イベント受信時の状態収束

`eventedStore` ラッパーが状態を保存するときに必ず `routerd.resource.status.changed` を発行します。
これにより下流の controller が連鎖的に再評価され、リソース間の依存解決が成立します。

### 5.3 Daemon contract

長時間動作する OS プロセス (DHCPv6 client、DNS resolver、healthcheck など) は、controller ではなく **daemon** として動かします。
daemon は controller chain と Unix domain socket + JSON で通信し、自身の状態を `lease.json` などのファイルに永続化します。

詳細は [reconcile loop の動作](./operations/reconcile) を参照してください。

---

## 6. 設定ファイル運用

routerd の設定ファイル (`/usr/local/etc/routerd/router.yaml` 既定) は、次のフローで反映します。

```
編集 → routerctl validate → routerctl apply (or auto reload)
                              │
                              └─ controller chain が状態 DB を更新
                                 → daemon が再起動 / reload
                                 → OS 状態 (nftables / netlink / systemd) に反映
```

設定ファイルは git で管理することを強く推奨します。
本番ホストへの反映は、routerd 経由で declarative に行い、ホスト上で `nft add rule`、`ip route add`、`sysctl -w` のような ad hoc 変更を加えないでください。
ad hoc な変更は次回の reconcile で打ち消されるか、もしくは routerd の状態 DB と OS 実状態の drift を作ります。

drift を見つけたときは、設定ファイル側で表現し直してから apply するのが正解です。
これにより設定ファイル ↔ 状態 DB ↔ OS 実状態の三者が常に一致します。

---

## 7. observability と debug

routerd は次の手段で運用状態を観測できます。

- `routerctl status`: 全リソースの phase 一覧
- `routerctl describe <kind>/<name>`: 個別リソースの spec、status、最近の event
- `routerctl events --topic <pattern> --resource <kind>/<name>`: bus event を tail
- `routerctl plan --diff`: apply 前の差分プレビュー
- Web Console (既定 `http://<mgmt-ip>:8080/`): summary、events、connections、clients、firewall、config をブラウザで表示
- `journalctl -u routerd.service -f | grep "routerd event"`: bus event を systemd journal で追跡

ログは `events.db` (controller 由来)、`dns-queries.db` (DNS resolver 由来)、`traffic-flows.db` (conntrack/pf 由来)、`firewall-logs.db` (NFLOG/pflog 由来) の 4 つに分離して永続化します。
詳細は [ログストレージ](./concepts/log-storage.md) を参照してください。

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
