# routerd 設計 (統合版)

これまでの議論を 1 本に統合した authoritative な設計仕様。これ以前の `/tmp/routerd-claude-{review,naming,rewrite,eventbus,foundation}.md` は履歴として残すが、矛盾があった場合は **本 doc が優先**。

memory 既知事項を前提とする (IX2215 完全置換 / 複数 OS / user 1 名 / breaking change OK / 見通し最優先 / lab は pve05-07)。

---

## 0. ドキュメントの読み方

1. § 1-3 で「何を作るか / どこで動かすか」のスコープを確認
2. § 4-7 で「全体構造と responsibility 境界」を理解
3. § 8-12 で「resource / event / read / config / naming」の各 primitive を読む
4. § 13-15 で「実装方針と OS 抽象」を確認
5. § 16-18 で「具体例 / 失敗対処 / state 永続」を確認
6. § 19-20 で「破壊と再構築の順序」を確認 (これが実装計画)
7. § 21 で「未決事項」を user 判断する

---

## 1. ビジョン

routerd は **declarative な multi-OS ホームルータ / SOHO ルータ / 小規模 DC ルータ** の framework。

3 つの具体 deployment target:

| target | scope | 必要 tier |
|--------|------|----------|
| **IX2215 完全置換** | NTT NGN HGW 配下、IPv4 + IPv6 PD、NAT44、DHCPv6 server、dnsmasq DNS、firewall | H |
| **Proxmox SDN 置換** | PVE cluster の VXLAN / EVPN / OSPF VTEP / underlay routing | C |
| **k8s cluster の外部接続性ルータ** | BGP で k8s pod CIDR / MetalLB IP を広告、ingress traffic 終端、上流 ISP / VPN へ | S → C |

3 つを **同じ primitive 集合で扱える** architecture を作る。Phase A は IX2215 置換だけが完成ゴール、PVE SDN と k8s ext は architecture 検証だけ (壊れない確認)。

### 1.1 capability tier

| tier | scope | 主機能 |
|------|-------|-------|
| **H** (Home) | 1 host / 1-2 uplink / 1-3 LAN VLAN | WAN acquire (PD/RA/PPPoE/DHCPv4/DS-Lite)、LAN service (RA/DHCPv6/dnsmasq)、NAT44、firewall、WANEgressPolicy |
| **S** (SOHO/branch) | 1-5 site / VPN | + WireGuard/IPsec、VRF、VPN 上の dynamic routing (BGP over WG)、commit-confirmed |
| **C** (Campus/DC small) | 10-50 node | + EVPN-VXLAN、iBGP RR、BFD、RouteMap DSL、FRR wrap |
| **E** (Enterprise/SP) | 100+ node | + full BGP table、MP-BGP L3VPN、segment routing、HA leader election |

primitive は H → E を通じて同一。tier が上がるごとに wrap 対象 (FRR 等) が増えるだけ。

---

## 2. 動作環境

### 2.1 deployment 形態

**PVE VM として動く** (重要前提)。physical box は Phase 後段、当面は PVE VM。

VM 制約:
- virtio NIC (vmxnet/ne2k 等は対象外)
- 特権 kernel module 依存禁止 (DPDK / XDP は optional、host-passthrough 不要)
- console + SSH で運用
- snapshot / clone を活用してテスト

### 2.2 OS 戦略

| OS | 評価 | 用途 |
|----|------|-----|
| **FreeBSD** | base が安定、release cycle 長い、リソース小、jail 隔離 | continuous-ops の本命 |
| **NixOS** | declarative OS と routerd declarative config の親和性高い、reproducible | dev 環境 + production 双方の本命 |
| **Alpine** | 最小 footprint、musl、apk | routerd が全機能自前化した時の minimum profile |
| **Ubuntu / Debian** | 入手容易、systemd 標準、kernel 新しめ | 開発 / 検証用、ad-hoc 構築 |

target は **Linux + FreeBSD の cross-OS portability**。NixOS は Linux に乗る module 形態。Alpine は将来。

各 OS で routerd は **同一 binary + 同一 config + OS-specific service unit** で動く。OS 固有差分は `pkg/platform` で吸収。

### 2.3 lab 環境 (実装テスト)

memory: pve01-04 の vmbr0 untagged 1901 path は DHCPv6-PD broken。試行錯誤の浪費を避けるため:

- **実装テストは pve05-07 (vmbr0 → vlan1901 trunk) のみで行う**
- pve01-04 は触らない (broken 環境への depend を作らない)
- これまでの試行錯誤による code 上の hack (hung 検出 / acquisition strategy / phantom binding 等) は **新環境では一旦全部捨てる** (§ 19 の demolition 計画)

memory: router03 (dhcpcd 派) / router01 + router04 (experimental routerd-pdclient) は既に pve05-07 で PD 取得実績あり (`2409:10:3d60:1240::/60` 等)。新 routerd の実装検証はこれと並走させる。

---

## 3. アーキテクチャ全体図

```
┌─────────────────────────────────────────────────────────────────┐
│ User                                                              │
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
│   WANEgressCtrl / ServiceLifecycleCtrl / ConfigLoaderCtrl         │
│   EventRuleEngine / DerivedEventEngine / (WorkflowEngine)         │
│   FRRConfigCtrl (Tier C+) / BGPPeerCtrl / VRFCtrl                 │
│         │                                                         │
│         ▼ SQLite state DB (objects/events/artifacts/generations)  │
└─────────┬─────────────────────────────────────────────────────────┘
          │ Unix socket HTTP+JSON                fsnotify (lease/snapshot)
          ▼                                            ▲
┌─────────────────────────────────────────────────────────────────┐
│ Layer 1 source daemons (各々 1 process)                           │
│   routerd-dhcpv6-client    routerd-ra-receiver                     │
│   routerd-pppoe-client    routerd-dhcpv4-client                    │
│   routerd-link-monitor    routerd-route-monitor                   │
│   routerd-frr-monitor (Tier C+)   routerd-healthcheck             │
└─────────┬─────────────────────────────────────────────────────────┘
          │ raw protocol packet / netlink / kqueue / vtysh
          ▼
┌─────────────────────────────────────────────────────────────────┐
│ OS / wrapped daemons / kernel                                     │
│   netlink, sysctl, nftables/pf, kernel (PPP/ip6tnl/vxlan)         │
│   dnsmasq, radvd, chrony, WireGuard, strongSwan                   │
│   FRR (Tier C+: bgpd, ospfd, zebra, ...)                          │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. 4 層 responsibility 分離

| Layer | 役割 | 性質 | プロセス境界 |
|-------|------|------|-----------|
| **L1 Sources** | protocol packet / kernel observation を扱う daemon。**publish only** | long-running、protocol state machine 持ち | 別 process (systemd unit) |
| **L2 Bus** | event 永続化 + topic match + fanout、in-memory channel + SQLite events table | passive backbone | routerd 本体内 |
| **L3 Controllers** | reconcile reactor。L1 event → L4 sink の反映、cross-protocol orchestration | reactive、idempotent | routerd 本体内 goroutine |
| **L4 Sinks** | kernel state、wrapped daemon、外部 service | stateful resource | OS / 外部 process |

**境界規則**:
- L1 同士は直接通信しない。常に L2 経由
- L1 は L4 を直接触らない (LAN reflection は必ず L3 経由)
- L3 は他 L3 を直接呼ばない。bus event publish で間接連携
- L2 は passive。自発的に動かない (controller が pull/push する)

memory「PD broken 時に AAAA 出さない」を構造で守る分界線がここ。L1 PD daemon が止まれば L3 DNSAnswerCtrl は AAAA を引っ込める、これは L1 が直接 dnsmasq を触らない設計の自然な帰結。

---

## 5. Layer 1 source daemon catalog

| daemon | 観測対象 | 主 publish topic |
|--------|---------|-----------------|
| `routerd-dhcpv6-client` | DHCPv6 IA_PD / IA_NA / info-request | `routerd.dhcpv6.client.{solicit,advertise,request,reply,prefix,address,info,server}.<verb>` |
| `routerd-ra-receiver` | upstream RA (M/O/Prf/PIO/RDNSS/default) | `routerd.ra.receiver.{ra,prefix,rdnss,flag,default-route}.<verb>` |
| `routerd-pppoe-client` | PPPoE / LCP / IPCP / IPv6CP | `routerd.pppoe.client.{session,lcp,ipcp,ipv6cp}.<verb>` |
| `routerd-dhcpv4-client` | DHCPv4 lease | `routerd.dhcpv4.client.{discover,offer,lease}.<verb>` |
| `routerd-link-monitor` | netlink RTM_NEWLINK / carrier | `routerd.link.<iface>.{up,down,carrier-up,carrier-down,mtu-changed}` |
| `routerd-route-monitor` | netlink RTM_NEWROUTE | `routerd.route.<table>.{added,removed,changed}` |
| `routerd-frr-monitor` (Tier C+) | vtysh + FRR daemon socket | `routerd.frr.{bgp,ospf,bfd}.<subject>.<verb>` |
| `routerd-healthcheck` | ICMP / TCP / DNS / HTTP probe | `routerd.healthcheck.<probe>.{passed,failed,timeout}` |

**naming**: 全 daemon が `routerd-<protocol>-<role>` 形式 (前 doc § naming.2 を継承)。

**lifecycle**:
- 1 daemon = 1 protocol × 1 role (ex: PD と NA は同 daemon、サーバ系は別 daemon)
- 1 process = 1 resource インスタンス (`routerd-dhcpv6-client@wan-pd.service`)
- 起動は `ServiceLifecycleController` が `POST /v1/commands/start` で kick (常時 enabled だが daemon 内部 idle/active を切替)

LAN service (DHCPv6 server / RA sender / DNS) は当面 wrap (dnsmasq / radvd)。将来 `routerd-dhcpv6-server` 等を Layer 1 に追加する余地は残す (§ 12 の build vs wrap)。

---

## 6. Bus 設計

### 6.1 transport

- **永続層**: SQLite `events` table。cursor = `events.id` (autoincrement, 単調増加)
- **通知層**: in-memory channel。SQLite insert と同時に subscriber channel に push
- **late join**: per-topic ring buffer (in-memory, N=200 件) で遅刻 subscriber 対応

routerd 本体内 in-process bus。**外部 MQ (NATS/Redis/MQTT) を導入しない** (router box の依存と attack surface 拡大を避ける、複数 OS 前提でも難しい)。

### 6.2 topic 体系

```
routerd.<area>.<subject>.<verb>

# Layer 0 (kernel)
routerd.link.<iface>.{up,down,carrier-up,carrier-down,mtu-changed}
routerd.route.<table>.{added,removed,changed}
routerd.address.<iface>.{added,removed,dad-failed}

# Layer 1 (daemons)
routerd.dhcpv6.client.{solicit,advertise,request,reply,prefix,address,info,server}.<verb>
routerd.dhcpv4.client.<subject>.<verb>
routerd.ra.receiver.<subject>.<verb>
routerd.pppoe.client.<subject>.<verb>
routerd.frr.{bgp,ospf,bfd}.<subject>.<verb>
routerd.healthcheck.<probe>.<verb>

# Layer 3 (controller emit, downstream-facing)
routerd.lan.address.<verb>            # applied, withdrawn, dad-failed
routerd.lan.route.<verb>
routerd.lan.firewall.<verb>
routerd.lan.service.<service>.<verb>  # dnsmasq.started, ra-sender.reloaded
routerd.lan.dns.{rdnss,upstream,answer}.<verb>
routerd.tunnel.{ds-lite,wireguard,ipsec}.<verb>

# Daemon lifecycle
routerd.daemon.lifecycle.{started,ready,stopped,crashed}
routerd.daemon.command.{received,executed,rejected}
routerd.daemon.health.changed

# Config (notify only / explicit trigger 後)
routerd.config.file.changed                            # fsnotify 検出のみ、副作用なし
routerd.config.parse.{started,succeeded,failed}        # 明示 apply 後の parse phase
routerd.config.diff.computed
routerd.config.resource.{added,modified,removed}
routerd.config.generation.{applied,confirmed,rolled-back}

# Virtual / derived (DerivedEvent)
routerd.virtual.internet-reachable.{ipv4,ipv6}
routerd.virtual.dns.dual-stack-ready
routerd.virtual.uplink-switch-needed

# Workflow
routerd.workflow.<name>.{started,completed,failed,rolled-back}
```

### 6.3 subscription

```go
type Subscription struct {
    Topics   []string         // glob: "routerd.dhcpv6.client.prefix.*"
    Resource *ResourceRef     // 任意 resource scope filter
    Source   *DaemonRef       // 任意 source filter
    Filter   func(Event) bool // attribute level
}
```

ワイルドカード: `*` = 1 セグメント、`**` = 複数セグメント。

### 6.4 配信セマンティクス

- **at-least-once**。controller は冪等に書く
- **同一 topic 内**: cursor 順保証 (SQLite autoincrement)
- **topic 間**: 順序保証なし (必要なら attribute timestamp で sort)
- **同一 source**: cursor 順 (source connector は単一 goroutine で順次 ingest)
- 5 分周期の reconcile が backstop

### 6.5 失敗モード

- daemon 落ち → bus connector 再接続、復旧時 last cursor から replay
- routerd 落ち → daemon は自分の `events.jsonl` に書き続け、復旧時に吸い上げ
- controller bug → recover() で隔離、PeriodicReconcile が backstop

---

## 7. Resource / spec / status / conditions

### 7.1 resource 構造 (k8s 流)

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: <Kind>
metadata:
  name: <name>
  ownerRefs:
    - kind: <ParentKind>
      name: <ParentName>
spec:                # 意図 (desired)
  ...
status:              # 観測 (observed) — controller が書く
  phase: <enum>      # short summary
  conditions:        # typed transitions の配列
    - type: <CondType>
      status: "True" | "False" | "Unknown"
      lastTransitionTime: <RFC3339>
      reason: <enum>
      message: <free text>
  observed:          # 任意の field
    ...
change_on:           # 任意 (escape hatch)
  - topic: <pattern>
    source: { kind, instance }
    reason: <doc>
emits:               # 任意 (informational)
  - <topic>
ready_when:          # 任意 (gating)
  - field: ${X.status.y}
    equals: <value> | not_empty: true | ...
```

`Spec` (意図) と `Status` (事実) を厳密分離。`Status.Conditions[]` は **enterprise tier (BGP セッション等) で必須**になるので H から導入する (後付けで breaking change 回避)。

### 7.2 path expression `${...}`

YAML 内の任意の場所で書ける読出式。

```
selector  := Kind/name | event(topic) | daemon(routerd-X-Y/instance) | self | config
accessor  := spec.<field> | status.<field> | attributes.<key> | observed.<key>

例:
${DHCPv6PrefixDelegation/wan-pd.status.currentPrefix}
${event(routerd.dhcpv6.client.prefix.bound).attributes.prefix}
${daemon(routerd-dhcpv6-client/wan-pd).status.observed.serverDUID}
${self.spec.interface}
${config.metadata.host}
```

書ける場所: `spec` 内、`ready_when:` 述語、`change_on:` filter、`EventRule.emit.attributes` template、daemon `POST /v1/config/update` の値、`routerctl eval`、`routerctl get --field`。

`${...}` の評価は (a) dependency edge を graph に登録、(b) 値変化時に該当 controller を enqueue、(c) 値が必要な箇所で lazy 評価。

### 7.3 ownerRefs (lifecycle 連鎖)

`metadata.ownerRefs` で親→子の関係を宣言。親が削除 / Lost / Expired になると子が cascade suspend。

```yaml
kind: IPv6DelegatedAddress
metadata:
  ownerRefs: [{ kind: DHCPv6PrefixDelegation, name: wan-pd }]
```

`pkg/apply/orphans.go` (現 838 行) の手書き削除順序を ownerRefs に置き換える。memory 「PD broken 時に AAAA 出さない」が ownerRef 連鎖で構造的に成立する。

### 7.4 ready_when (gating predicate)

resource を reconcile してよい条件。満たすまで `Phase = Pending`。

```yaml
ready_when:
  - field: ${DHCPv6PrefixDelegation/wan-pd.status.phase}
    equals: Bound
  - field: ${Link/lan.status.phase}
    equals: Up
```

memory「PD 無い時に AAAA 出さない」「DS-Lite up しないと AAAA 出さない」を declarative に表現。

OR 条件が必要な resource は `any_of` を使う。例: NGN HGW 経由の DHCPv6 Information-request は DNS/SNTP/domain-search を返すが AFTR option を返さないため、DS-Lite は DHCPv6 由来 AFTR と static fallback のどちらかで ready になる必要がある。

```yaml
ready_when:
  - any_of:
      - - field: ${DHCPv6Information/wan-info.status.aftrName}
          not_empty: true
      - - field: ${DSLiteTunnel/ds-lite.spec.aftrFQDN}
          not_empty: true
      - - field: ${DSLiteTunnel/ds-lite.spec.aftrIPv6}
          not_empty: true
```

### 7.5 change_on (escape hatch)

default + 値参照 で表現できない依存だけ書く。**頻度は全 resource の 5% 未満を想定**。多用されたら default が間違ってる合図 → code 修正。

### 7.6 Reader API (controller 内)

```go
type Reader interface {
    Get(ref ResourceRef) (Resource, error)
    Eval(path string) (any, error)
    List(kind string, where map[string]string) []Resource
    LatestEvent(topic string) (Event, bool)
    DaemonStatus(ref DaemonRef) (DaemonStatus, error)
}
```

reconcile 1 回内で snapshot consistency 保証 (同 reconcile 中に同じ世代の値が返る)。

`Eval()` は read = 暗黙 subscribe。値変化時に当該 controller が再 reconcile される。

---

## 8. event coordination 4 層

### 8.1 Layer A: implicit dep

`${X.status.y}` 値参照、`ownerRefs`、`ready_when:` で **data-flow 依存** を表現。全依存の 80% をカバー。

### 8.2 Layer B: EventRule (compositional operator)

bus event stream への operator。8 個に絞る (これ以上は Go controller 行き)。

| operator | 意味 |
|---------|------|
| `all_of` | 列挙 input topic の latest-of-each が全部揃ったら emit |
| `any_of` | いずれか入力で emit (OR) |
| `sequence` | 指定順序で window 内に揃ったら emit |
| `window` | window 内 event 数 ≥ threshold で emit |
| `absence` | trigger から timeout 内に expected が来なければ emit |
| `throttle` | rate limit (超過 drop) |
| `debounce` | quiet period 後だけ emit (burst 抑制) |
| `count` | 累積カウントを周期 publish |

```yaml
kind: EventRule
metadata: { name: link-flap-quarantine }
spec:
  pattern:
    operator: window
    topic: routerd.link.{up,down}
    duration: 60s
    threshold: 6
    correlate_by: attributes.interface
  emit:
    topic: routerd.link.flapping
```

中間状態は in-memory のみ (再起動 reset)。永続化が必要なら Go controller 行き。

Phase 2-B.2 の EventRule engine は in-memory state のみを持ち、SQLite events table は durable input log として扱う。`correlate_by` の初期 grammar は `attributes.<key>`, `resource.{name,kind,apiVersion}`, `daemon.{instance,kind}`。missing correlation key は既定で ignore + warning count、`allow_missing_correlation: true` の時だけ空 key として扱う。`emit.attributes` は `${event.type}`, `${attributes.<key>}`, `${resource.name}`, `${resource.kind}`, `${resource.apiVersion}`, `${correlation}`, `${count}` の最小 template を展開する。

### 8.3 Layer C: DerivedEvent (virtual topic)

複数 status field の組合せを 1 つの "意味のある signal" に materialize。**retract semantics 必須** (信号が落ちた瞬間の反応)。

```yaml
kind: DerivedEvent
metadata: { name: internet-reachable-ipv4 }
spec:
  topic: routerd.virtual.internet-reachable.ipv4
  inputs:
    - field: ${WANEgressPolicy/ipv4-default.status.selectedCandidate}
      not_empty: true
    - field: ${HealthCheck/internet-icmp4.status.phase}
      equals: Healthy
  emitWhen:    all_true
  retractWhen: any_false
  hysteresis:  10s
```

EventRule = stream → stream、DerivedEvent = state → event。使い分け。

Phase 2-B.3 の DerivedEvent engine は status path を評価して `<topic>.asserted` / `<topic>.retracted` を publish する。`hysteresis` は publish 前の安定確認時間で、timer 中に入力が戻った場合は pending transition を cancel する。`emitInitial` は default false で、起動時の初期評価では event を出さず status だけを materialize する。status は `phase`, `asserted`, `pendingTransition`, `lastAssertedAt`, `lastRetractedAt` を持つ。

### 8.4 HealthCheck (Phase 2-B.4a/b)

Phase 2-B.4a では routerd 本体内 goroutine の embedded probe を先に実装し、Phase 2-B.4b で `routerd-healthcheck@<name>` daemon に分離する。production path は daemon。embedded は test / development 用に残す。

```yaml
kind: HealthCheck
metadata: { name: internet-tcp443 }
spec:
  daemon: routerd-healthcheck
  socketSource: /run/routerd/healthcheck/internet-tcp443.sock
  targetSource: static
  target: 1.1.1.1
  protocol: tcp
  port: 443
  interval: 30s
  timeout: 3s
  healthyThreshold: 1
  unhealthyThreshold: 3
```

probe ごとに `routerd.healthcheck.<name>.passed|failed|timeout` を publish し、status は `phase`, `lastResult`, `lastCheckedAt`, `lastTransitionAt`, `consecutivePassed`, `consecutiveFailed` を持つ。state machine は `Unknown → Passing → Healthy ↔ Failing → Unhealthy`。WANEgressPolicy の candidate が `healthCheck: <name>` を持つ場合は `HealthCheck/<name>.status.phase == Healthy` を ready 条件に追加する。

`routerd-healthcheck` は `protocol: icmp|tcp|dns|http` を扱う。ICMP は raw socket が必要なので systemd unit では daemon だけに `CAP_NET_RAW` を与える。`POST /v1/commands/renew` は「即時 probe 実行」を意味する。state は `/var/lib/routerd/healthcheck/<name>/state.json` に永続化し、events は `/var/lib/routerd/healthcheck/<name>/events.jsonl` に append する。

OTel は `pkg/otel` の薄い wrapper で全 daemon / routerd 本体に共通導入する。`OTEL_EXPORTER_OTLP_ENDPOINT` 系 env var が未設定なら exporter は起動しない。設定時は slog bridge logs、probe / Renew / controller reconcile metrics、主要 operation span を OTLP へ出す。

### 8.5 Layer D: Workflow (saga, optional, Phase 4+)

多段 orchestration with rollback。最初は不要、必要になったら state-machine DSL を入れる。

---

## 9. config 取扱 — fsnotify は notify only、apply は明示 trigger

**重要**: config file 自動適用は危険なので **やらない**。

### 9.1 fsnotify は検出だけ

```
T+0.5s  publish: routerd.config.file.changed (path, mtime, sha256)
        ※ parse しない、diff しない、apply しない
```

`routerd.config.file.changed` を subscribe するのは:
- routerctl status で「未適用変更あり」表示
- audit log
- (任意) operator 通知

### 9.2 適用は明示 trigger

3 経路、いずれも同じ pipeline:

```
1. routerctl apply
2. routerctl apply --confirm-within 60s   # commit-confirmed
3. systemctl reload routerd                # SIGHUP / sd_notify
   ↓
ConfigLoaderController:
   read all config → parse → validate → diff vs current
   ↓
publish: routerd.config.diff.computed
   ↓
(commit-confirmed の場合は確認待ち)
   ↓
publish: routerd.config.resource.{added,modified,removed} per resource
   ↓
controller が反応 (Layer A の subscribe 経路で自然に enqueue)
```

### 9.3 dry-run

```
$ routerctl plan
[parse]   ok (12 resources)
[validate] ok
[diff]    ~ DHCPv6PrefixDelegation/wan-pd
            spec.profile: ntt-ngn-direct → ntt-hgw-lan-pd
          + DNSAnswerScope/lan-aaaa
[plan]    - PD daemon 再起動 (現 lease release)
          - dnsmasq AAAA 応答有効化 (PD bound 後)
ready to apply. run: routerctl apply
```

### 9.4 commit-confirmed

```
$ routerctl apply --confirm-within 60s
generation 42 applied. provisional. confirm within 60s.
$ routerctl confirm
generation 42 confirmed.
```

confirm 無ければ `routerd.config.generation.rolled-back` を publish して generation 41 に戻す。SSH 切断時の救済、リモート管理事故防止。

### 9.5 fsnotify の正しい使い所 — 「事実」の自動取込

「意図 (config) は手動」「事実 (lease/snapshot/鍵) は自動」で使い分ける。

| 用途 | 仕組み | 自動反映 |
|------|--------|---------|
| daemon が lease/snapshot を file に書く → routerd が拾う backup 経路 | `/var/lib/routerd/<daemon>/<resource>/snapshot.json` を atomic write、routerd が fsnotify で検出 → bus event | ◯ |
| FRR の reload 完了検知 (`/var/run/frr/<daemon>.pid` 変化) | fsnotify | ◯ |
| WireGuard / IPsec 鍵 file 外部更新 (vault 等) | fsnotify、protocol level に閉じる | ◯ |
| `/etc/routerd/*.yaml` 編集 | fsnotify は **検出のみ**、operator 確認 → 明示 apply | ✕ |

原則: **意図の自動適用は禁止、事実の自動取込は OK**。

---

## 10. 命名規約

### 10.1 daemon 名 (`routerd-<protocol>-<role>`)

```
routerd-dhcpv6-client    routerd-dhcpv4-client
routerd-ra-receiver     routerd-ra-sender (将来)
routerd-pppoe-client
routerd-link-monitor    routerd-route-monitor
routerd-frr-monitor (Tier C+)
routerd-healthcheck
```

過去の `routerd-pdclient` は `routerd-dhcpv6-client` に **改名してから** 出荷。実装途中の今がチャンス。

### 10.2 9 次元の一貫性

| 次元 | 形式 | 例 (`routerd-dhcpv6-client@wan-pd`) |
|------|------|----------------------------------|
| binary | `routerd-<protocol>-<role>` | `routerd-dhcpv6-client` |
| systemd unit | `routerd-<p>-<r>@<resource>.service` | `routerd-dhcpv6-client@wan-pd.service` |
| FreeBSD rc.d | `routerd_<p>_<r>_<resource>` | `routerd_dhcpv6_client_wan_pd` |
| NixOS module | `services.routerd.<pCamel><rCamel>.<resource>` | `services.routerd.dhcpv6Client.wan-pd` |
| Unix socket | `/run/routerd/<p>-<r>/<resource>.sock` | `/run/routerd/dhcpv6-client/wan-pd.sock` |
| lease file | `/var/lib/routerd/<p>-<r>/<resource>/lease.json` | `/var/lib/routerd/dhcpv6-client/wan-pd/lease.json` |
| state file | `/var/lib/routerd/<p>-<r>/<resource>/state.json` | `/var/lib/routerd/healthcheck/internet-icmp/state.json` |
| event ring | `/var/lib/routerd/<p>-<r>/<resource>/events.jsonl` | (同 dir) |
| journal id | binary 名と一致 | `routerd-dhcpv6-client` |
| DaemonRef | `Kind=binary, Instance=resource` | `{ Kind: routerd-dhcpv6-client, Instance: wan-pd }` |

### 10.3 event topic

```
routerd.<area>.<subject>.<verb>
```

verb は 過去形 / 受動態 (`sent, received, bound, renewed, expired, started, ready, executed, changed`)。area 内で `.` 多階層 (例 `dhcpv6.client`) は OK、ただし最終 verb は必ず最後。

### 10.4 command verb

全 daemon 共通: `renew | rebind | release | reload | stop | start | flush`。protocol 固有は body の `attributes` で表現、endpoint を増やさない。

### 10.5 phase / health 語彙

`DaemonStatus.Phase`: `Starting | Running | Blocked | Draining | Stopped`

`ResourceStatus.Phase`: `Idle | Acquiring | Bound | Refreshing | Rebinding | Expired | Lost | Released | Pending` (lease 系統一語彙)

`Health`: `ok | degraded | failed | unknown`

### 10.6 API resource Kind (新規追加)

H 必須: `DHCPv6PrefixDelegation`, `IPv6DelegatedAddress`, `IPv6RouterAdvertisement` (LAN 送出), `IPv6RAObservation` (WAN 受信、旧 `IPv6RAAddress`), `DHCPv6Server`, `DHCPv4Server`, `DHCPv4Reservation`, `DHCPv4Relay`, `IPv4Address`, `IPv4Route`, `Link`, `Bridge`, `VLAN`, `NAT44Rule`, `ConntrackObserver`, `Firewall*`, `DNSAnswerScope`, `DNSResolverUpstream`, `WANEgressPolicy`, `HealthCheck`, `EventRule`, `DerivedEvent`

H+ オプション: `DSLiteTunnel`, `WireGuardInterface`, `WireGuardPeer`, `MAPETunnel` (将来)

S+: `VRF`, `IPsecConnection`, `VXLANTunnel`, `BGPSession` (or peer-group), `Workflow`

C+: `EVPNInstance`, `EVPNVNI`, `RouteMap`, `BFDSession`, `OSPFArea`, `FRRConfigOverride`

Tier S の VPN/overlay primitive は protocol ごとに Kind を分ける。抽象 `VPNTunnel` は置かず、`WireGuardInterface`/`WireGuardPeer`, `IPsecConnection`, 将来の `TailscaleNode`, `SoftetherSession` を additive に足す。`WANEgressPolicy` は VPN 種別を知らず、ready な candidate を weight/hysteresis で選ぶ。

- WireGuard: Linux は kernel WireGuard、FreeBSD は kernel `if_wg` か `wireguard-go` を wrap する。設定は `wg setconf` 互換形式を正とし、status は handshake timestamp / transfer bytes / endpoint を観測する。
- IPsec: strongSwan `swanctl.conf` を生成し、`swanctl --load-conns` で lifecycle を反映する。対象は AWS/Azure/GCP cloud-managed VPN gateway 接続に限定し、legacy enterprise remote-access や iOS/macOS profile 互換は scope 外。
- VRF: Linux VRF device (`ip link add <name> type vrf table <id>`) は L3 routing table separation であり network namespace ではない。process/socket の namespace isolation はしない。guest/staff/IoT の L3 分離と per-VRF WAN egress 選択に使う。
- VXLAN: `VXLANTunnel` は kernel VXLAN device を直接表現する。underlay は WireGuard tunnel または direct IP を想定し、BGP-EVPN による control plane は Tier C+ で `EVPNInstance` と FRR wrap に逃がす。
- OTel: controller/daemon は共通 `pkg/otel` を使い、未設定時 no-op、`OTEL_EXPORTER_OTLP_ENDPOINT` 等の設定時のみ export する。代表 metric は `routerd.wireguard.peer.handshake.timestamp`, `routerd.wireguard.transfer.bytes`, `routerd.ipsec.sa.established.count`, `routerd.ipsec.tunnel.bytes`, `routerd.vrf.member.count`, `routerd.vxlan.peers.count`。

E+: `RouteReflectorCluster`, `L3VPNInstance`, `MPBGPPeer`, `HALeader`

---

## 11. daemon contract

### 11.1 transport

- Unix socket HTTP+JSON
- socket path: `/run/routerd/<p>-<r>/<resource>.sock` (mode 0660、owner routerd group)
- TLS なし、認証なし (filesystem perm が境界)

### 11.2 必須 endpoint

```
GET  /v1/status                             # 現スナップショット (DaemonStatus + ResourceStatus[])
GET  /v1/healthz                            # liveness probe
GET  /v1/events?since=<cursor>&wait=<dur>&topic=<glob>
                                            # long-poll、{cursor, events, more} を返す
POST /v1/commands/{renew,rebind,release,reload,stop,start,flush}
                                            # 全て at-least-once / accepted only / 結果は event で
POST /v1/config/update                      # routerd 側からの config push
```

一部 daemon は verb を protocol 用語に map する。例: `routerd-healthcheck` の `renew` は lease renew ではなく即時 probe trigger。

### 11.3 lifecycle

- daemon 起動時 lease / state file (`<lease-dir>/lease.json` or `<state-dir>/state.json`) から `Restore()` → bus に `routerd.daemon.lifecycle.started` を publish
- 準備完了 → `routerd.daemon.lifecycle.ready`
- protocol state 変化のたびに event publish + lease file atomic write (rename)
- event ring (1000 件) を内部保持 + `events.jsonl` に append
- 終了 → `routerd.daemon.lifecycle.stopped`

### 11.4 active control の義務

memory 「PD 取得は時間と共に必ず崩壊する。OS client 任せの Renew は失敗、active controller で能動 Renew が必須」。

contract で明示:
- daemon は T1 到達前 (margin 入り) に **能動的に** Renew を送る
- T2 到達前に Rebind
- Reply 失敗時は再送間隔を exponential backoff
- daemon は **OS の自然タイマに任せない**
- `/v1/commands/renew` で routerd から強制 Renew できる

### 11.5 observability の保持

memory「過去の試行錯誤痕」の中で唯一守るべきもの = 生 packet log。HGW 個体差デバッグの命綱。

- daemon 内部に packet ring (1000 件) を保持
- `GET /v1/status` の `observed.transactions[]` で公開
- routerctl で `routerctl debug packets routerd-dhcpv6-client/wan-pd` を提供

`pkg/dhcp6recorder` の機能をここに吸収して廃止する。

### 11.6 ablation 機能の保持

memory「dhcpv6 ablation CLI を残すべき」。HGW 個体差調査用 flag を daemon の `--once` モードに移植:

```
routerd-dhcpv6-client --once \
  --src-mac <mac> \
  --src-ll <ll> \
  --hop-limit <h> \
  --client-duid <duid> \
  --iaid <iaid>
```

---

## 12. build vs wrap

### 12.1 必ず自前 (routerd core)

- declarative resource framework + reconcile + bus + read model
- daemon contract (HTTP+JSON over Unix socket)
- WAN acquire daemons (`routerd-dhcpv6-client`, `routerd-ra-receiver`, `routerd-pppoe-client`, `routerd-dhcpv4-client`)
- kernel observation daemons (`routerd-link-monitor`, `routerd-route-monitor`)
- controllers (LAN address / RA / firewall / route / DNS / WANEgress / ServiceLifecycle)
- config watcher / loader
- multi-OS abstraction (`pkg/platform`)
- LAN reflection logic (delegated address derive、RA payload generation、dnsmasq config 生成)

これらが **routerd の独自価値**。

### 12.2 当面 wrap

| target | wrap 対象 | 役割 |
|--------|---------|------|
| DHCPv4 server + DHCPv6 server (stateless) + DNS forwarder | dnsmasq | LAN service。設定 file 生成 + reload 管理 |
| LAN RA sender | radvd (or 自前 routerd-ra-sender) | LAN RA 送出 |
| NTP | chrony / ntpd | (rendering layer のみ) |
| WireGuard | kernel module / wireguard-go | netlink で peer 管理、`wg setconf` 生成 |
| IPsec IKEv2 | strongSwan (charon) | `swanctl.conf` 生成 + lifecycle |
| BGP / OSPF / IS-IS / LDP / PIM (Tier C+) | FRR | `frr.conf` 生成 + vtysh で観測 + `routerd-frr-monitor` で event 化 |

### 12.3 状況次第

- BGP: Tier S なら GoBGP 組込 (single binary)、Tier C+ は FRR wrap
- DHCPv6 server stateful: 当面 dnsmasq、要件増えたら自前 `routerd-dhcpv6-server`

### 12.4 wrap layer の型

```
ConfigWatcher + Loader → routerd internal model
                              ↓
                       <Foo>ConfigController (e.g., FRRConfigCtrl, DnsmasqConfigCtrl)
                              ↓
                       config file 生成 (template + atomic write)
                              ↓
                       reload 操作 (vtysh / dnsmasq SIGHUP / wg syncconf)
                              ↓
                       routerd-<foo>-monitor (Layer 1 daemon、bus に観測 publish)
```

`<foo>ConfigCtrl` は Layer 3 controller、`routerd-<foo>-monitor` は Layer 1 daemon。同じ contract に乗る。

---

## 13. multi-OS 抽象化

### 13.1 OS 別の supervisor

| OS | service unit | 起動 |
|----|------------|------|
| Linux | systemd unit (`routerd-<foo>-<role>@<resource>.service`) | systemctl start |
| FreeBSD | rc.d script (`/usr/local/etc/rc.d/routerd_<foo>_<role>_<resource>`) | service start |
| NixOS | nix module (`services.routerd.<fooCamel><roleCamel>.<resource>`) | nixos-rebuild switch |
| Alpine | OpenRC init script | rc-service start |

`pkg/platform/<os>.go` で抽象化。

### 13.2 kernel 操作の抽象

| 操作 | Linux | FreeBSD | NixOS |
|------|-------|--------|------|
| netlink address | `pkg/netlink` | route(4) socket | (Linux と同) |
| nftables / pf | nftables | pf | nftables |
| route table | `ip route` / netlink | `route` / route socket | netlink |
| sysctl | `/proc/sys` | `sysctl` | (Linux と同) |
| firewall | nftables | pf | nftables |
| bridge | bridge / iproute2 | if_bridge | bridge |
| VLAN | iproute2 | vlan(4) | iproute2 |

`pkg/platform/{linux,freebsd}/` に分離。

### 13.3 fsnotify 抽象

`fsnotify` ライブラリで Linux inotify / FreeBSD kqueue / macOS FSEvents を吸収。NixOS は Linux と同。

---

## 14. resource cascade 例 (PD → DS-Lite → AAAA)

```yaml
# WAN
kind: DHCPv6PrefixDelegation
metadata: { name: wan-pd }
spec: { interface: wan, prefixLength: 60, iaid: "1", duidType: link-layer }
---
kind: DHCPv6Information
metadata: { name: wan-info }
spec:
  interface: wan
  request: [aftr-name, dns-servers]
  ready_when:
    - field: ${DHCPv6PrefixDelegation/wan-pd.status.phase}
      equals: Bound
---
# 条件付きフォワーダー
kind: DNSResolverUpstream
metadata: { name: ngn-resolver }
spec:
  zones:
    - zone: transix.jp
      servers:
        - ${DHCPv6Information/wan-info.status.dnsServers}
  ready_when:
    - field: ${DHCPv6Information/wan-info.status.dnsServers}
      not_empty: true
---
# LAN address (derived)
kind: IPv6DelegatedAddress
metadata:
  name: lan
  ownerRefs: [{ kind: DHCPv6PrefixDelegation, name: wan-pd }]
spec:
  interface: lan
  prefixSource: ${DHCPv6PrefixDelegation/wan-pd.status.currentPrefix}
  subnetID: "1"
  addressSuffix: "::1"
  ready_when:
    - field: ${DHCPv6PrefixDelegation/wan-pd.status.phase}
      equals: Bound
    - field: ${Link/lan.status.phase}
      equals: Up
---
# RA
kind: IPv6RouterAdvertisement
metadata:
  name: lan-ra
  ownerRefs: [{ kind: IPv6DelegatedAddress, name: lan }]
spec:
  interface: lan
  prefixSource: ${DHCPv6PrefixDelegation/wan-pd.status.currentPrefix}
  mFlag: false
  oFlag: true
  rdnss:
    - ${DHCPv6Information/wan-info.status.dnsServers}
  dnssl: [lan]
  mtu: 1500
  prfPreference: high
  ready_when:
    - field: ${IPv6DelegatedAddress/lan.status.phase}
      equals: Applied
---
kind: DHCPv4Server
metadata: { name: lan-dhcpv4 }
spec:
  interface: lan
  addressPool: { start: 192.168.10.100, end: 192.168.10.199, leaseTime: 12h }
  gateway: 192.168.10.1
  dnsServers: [192.168.10.1]
  ntpServers: [192.168.10.1]
  domain: lan
---
kind: DHCPv4Reservation
metadata: { name: fixed-host }
spec:
  server: lan-dhcpv4
  macAddress: "02:00:00:00:10:10"
  hostname: fixed-host
  ipAddress: 192.168.10.10
---
# DS-Lite
kind: DSLiteTunnel
metadata: { name: ds-lite }
spec:
  localIPv6Source: ${DHCPv6PrefixDelegation/wan-pd.status.currentPrefix}
  aftrSource:      ${DHCPv6Information/wan-info.status.aftrName}
  # NGN HGW 経由では aftrSource が空になり得る。
  # AFTR FQDN は public DNS ではなく routerd 管理 dnsmasq の条件付きフォワーダー経由で解決する。
  aftrFQDN:        gw.transix.jp
  # DNS を使わず固定する場合は aftrIPv6: 2404:8e00::feed:100
  ready_when:
    - any_of:
        - - field: ${DHCPv6Information/wan-info.status.aftrName}
            not_empty: true
        - - field: ${DSLiteTunnel/ds-lite.spec.aftrFQDN}
            not_empty: true
          - field: ${DNSResolverUpstream/ngn-resolver.status.phase}
            equals: Applied
        - - field: ${DSLiteTunnel/ds-lite.spec.aftrIPv6}
            not_empty: true
---
kind: IPv4Route
metadata:
  name: default-via-dslite
  ownerRefs: [{ kind: DSLiteTunnel, name: ds-lite }]
spec:
  destination: 0.0.0.0/0
  device: ${DSLiteTunnel/ds-lite.status.interface}
---
# AAAA 応答 (memory: 「PD broken / DS-Lite down 時に出さない」)
kind: DNSAnswerScope
metadata:
  name: lan-aaaa
  ownerRefs: [{ kind: IPv6DelegatedAddress, name: lan }]
spec:
  interface: lan
  family: ipv6
  ready_when:
    - field: ${IPv6DelegatedAddress/lan.status.phase}
      equals: Applied
    - field: ${IPv4Route/default-via-dslite.status.phase}
      equals: Installed
```

PD bound から AAAA 応答開始まで ~1.5s (詳細 timeline は前 doc eventbus § 17.2)。

逆向き (PD 失効 → AAAA 停止) は ownerRefs cascade で自動。

---

## 15. WAN egress 選択

```yaml
kind: WANEgressPolicy
metadata: { name: ipv4-default }
spec:
  family: ipv4
  candidates:
    - source: PPPoESession/wan-pppoe
      device: ${PPPoESession/wan-pppoe.status.interface}
      gateway: ${PPPoESession/wan-pppoe.status.peerAddress}
      weight: 100
      ready_when:
        - field: ${PPPoESession/wan-pppoe.status.phase}
          equals: Up
        - field: ${HealthCheck/wan-pppoe-internet.status.phase}
          equals: Healthy
    - source: DSLiteTunnel/transix
      weight: 80
      ready_when: [...]
    - source: DHCPv4Lease/wan-dhcpv4
      weight: 50
      ready_when: [...]
  selection: highest-weight-ready
  hysteresis: 30s
```

`WANEgressController` が候補の status 変化 + `routerd.healthcheck.<probe>.{passed,failed}` を購読、selection 再評価、変化時に `routerd.lan.route.changed` を publish。`FirewallController` / `MSSClampController` / `DNSResolverController` が独立に反応。手書き if/else 消滅。

Phase 2-B.1 の初期実装は `selection: highest-weight-ready` のみ実行する。`weighted-ecmp` は enum 予約で、status は `Pending(UnsupportedSelection)` とする。route install はまだ sink に流さず、`WANEgressPolicy.status` に `selectedCandidate`, `selectedDevice`, `selectedGateway`, `lastTransitionAt` を保存し、selection 変化時だけ `routerd.lan.route.changed` を publish する。hysteresis は default 30s。

---

## 16. state 永続 (SQLite schema)

`pkg/state/sqlite.go` 既存資産を活用。新たに必要な拡張:

```sql
CREATE TABLE IF NOT EXISTS objects (...) -- 既存
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,  -- bus cursor
  topic TEXT NOT NULL,                   -- 追加
  source_kind TEXT,                      -- 追加 (DaemonRef.Kind)
  source_instance TEXT,                  -- 追加 (DaemonRef.Instance)
  resource_api_version TEXT,             -- 追加
  resource_kind TEXT,                    -- 追加
  resource_name TEXT,                    -- 追加
  severity TEXT NOT NULL,
  reason TEXT,
  message TEXT,
  attributes TEXT,                       -- 追加 (JSON)
  generation INTEGER,
  created_at TEXT NOT NULL
);
CREATE INDEX events_topic ON events(topic, id);
CREATE INDEX events_resource ON events(resource_kind, resource_name, id);
CREATE TABLE IF NOT EXISTS generations (...) -- 既存、commit-confirmed の rollback で活用
```

書き手は **routerd 単独**。daemon は SQLite を直接触らない (Unix socket 経由で routerd が吸い上げて書く)。理由は前 review § C6 の通り (二重書込み回避)。

daemon の lease/snapshot は `/var/lib/routerd/<daemon>/<resource>/lease.json` に atomic write。再起動時に Restore。

---

## 17. failure & recovery

| 失敗 | 検出 | 対処 |
|-----|------|------|
| daemon 落ち | bus connector の reconnect 失敗 | systemd / rc.d が restart、bus 復旧時 last cursor から replay |
| routerd 落ち | systemd / rc.d が restart | 起動時 `/v1/status` を全 daemon から pull で warm-start、events table の last cursor から replay |
| controller bug (panic) | recover() で隔離 | 1 controller crash で他は影響なし、PeriodicReconcile が backstop |
| event 欠落 | controller 5 分 backstop reconcile で吸収 | at-least-once + idempotent で重複は無害 |
| daemon → routerd Unix socket 切断 | bus connector が再接続 | daemon は events.jsonl に書き続け、復旧時に吸い上げ |
| config parse 失敗 | ConfigLoaderController | `routerd.config.parse.failed` publish、current state 維持、operator 通知 |
| 適用後 SSH 切断 (リモート管理事故) | commit-confirmed の confirm 待ち timeout | generation 自動 rollback |

---

## 18. 過去の試行錯誤の demolition (新環境 pve05-07 でやり直し)

memory 「pve01-04 で何日も試行錯誤した、新環境でやり直して」を踏まえ、過去の hack を **全部捨てる** 。残すのは framework infrastructure のみ。

### 18.1 残す (infrastructure)

`pkg/api`, `pkg/config`, `pkg/state`, `pkg/apply` (枠), `pkg/controlapi`, `pkg/eventlog`, `pkg/inventory`, `pkg/lock`, `pkg/platform`, `pkg/plugin`, `pkg/resource`, `pkg/render` (template framework), `pkg/status`, `pkg/daemonapi`, `cmd/routerctl`, `cmd/routerd-schema`

### 18.2 全削除 (試行錯誤痕)

```
pkg/dhcp6control/        # 740 行、active sender 実験
pkg/dhcp6event/
pkg/dhcp6recorder/       # 281 行、機能は daemon 内 ring に移植
pkg/pdmonitor/           # 300+ 行、hung 検出
pkg/pdstrategy/          # acquisition strategy hack
pkg/ralistener/          # 240 行、routerd-ra-receiver に移植
pkg/render/dhcp6c.go     # OS DHCP client renderer
pkg/render/dhcpcd.go
pkg/render/dhcp6_hook.go
cmd/routerd-pdclient/    # routerd-dhcpv6-client に改名
cmd/routerd dhcpv6 サブコマンド + ablation flag 群  # daemon の --once モードに移植
pkg/api/specs.go の以下 spec field:
  DHCPv6PrefixDelegationSpec.Client (enum 削除、routerd 1 値)
  IPv6PDRecoverySpec
  IPv6PDLanFallbackSpec
  AcquisitionStrategy
  PriorPrefix, ServerID, DUIDRawData
pkg/state/pdlease.go の以下 field:
  Hung, Acquisition, WANObserved, Transactions
  (lease の最小形に縮める)
pkg/config/validate.go の DHCPv6/PD/RA 関連 validator (1941 行 → 半分以下)
pkg/apply/engine.go の per-Kind dispatch (1678 行 → 数百行を狙う)
```

期待効果: 全体 ~16500 行のうち 5000-7000 行縮む。残った code は新 architecture に乗る。

### 18.3 縮小 (renderer)

- `pkg/render/networkd.go` (515 行): DHCPv6/PD/RA セクション削除、static link のみに
- `pkg/render/freebsd.go` (637 行): DHCPv6 client 部分削除
- `pkg/render/nixos.go` (857 行): networking.useDHCP/useNetworkd 周辺整理

### 18.4 invariant 保護

memory 「PD broken 時に AAAA 出さない」「PD 取得は時間と共に必ず崩壊する → active Renew 必須」「IPv6 broken 時の AAAA は責任放棄」は **新 architecture の構造で守る**:

- AAAA 抑制 = `DNSAnswerScope.ready_when: [PD bound, DS-Lite up]` で declarative
- active Renew = daemon contract で義務化 (§ 11.4)
- 責任放棄禁止 = ownerRefs cascade で PD 失効 → 関連 LAN service 自動 suspend

---

## 19. 再構築順序 (rebuild on pve05-07)

| Phase | 内容 | 検証 |
|-------|------|------|
| **0. 土台** | `pkg/daemonapi` 拡張、`pkg/bus`、`pkg/source`、`pkg/controller/framework`、`cmd/routerd-dhcpv6-client` (PD daemon、ablation flag 移植) | pve05 で daemon 単体起動 + PD 取得 |
| **1. 1 chain** | `LANAddressController`、`DNSAnswerController`、`PrefixDelegationController` を実装。daemon → bus → controller → sink の 1 chain を動かす | PD bound から dnsmasq AAAA 開始まで < 5s 計測 |
| **1.5 LAN/WAN service** | dnsmasq wrap を拡張し、`DHCPv4Server`, `DHCPv4Reservation`, `DHCPv6Server` stateful/stateless/both, `IPv6RouterAdvertisement` PIO/RDNSS/DNSSL/M/O flag, `DHCPv4Relay`, `DNSAnswerScope` hostRecords/local domain/DDNS/DNSSEC を同一 instance に統合。続けて `NAT44Rule` の outbound NAPT と `/proc/net/nf_conntrack` aggregate observer を追加する。stateful firewall/filter chain は棚上げし no-op のまま | router05 dry-run で port 1053 test instance の config 出力と syntax、nftables NAT ruleset text、conntrack procfs snapshot を確認 |
| **2. cascade** | `IPv6RouterAdvertisement`, `DHCPv6Information`, `DSLiteTunnel`, `IPv4Route` 関連 controller、`WANEgressPolicy`、`HealthCheck`、`EventRule`、`DerivedEvent` engine | PD → DS-Lite → AAAA cascade 全 ~1.5s で完走 |
| **3. config 取扱** | `ConfigWatcher` (notify only)、`ConfigLoader` (明示 trigger)、`routerctl plan/apply/confirm`、commit-confirmed | リモート設定変更 → SSH 切断 → 自動 rollback テスト |
| **4. demolition** | § 18.2 を一気に削除、`pkg/api/specs.go` 縮小、`pkg/apply/engine.go` 縮小、`pkg/state/pdlease.go` 簡略化、test fixture 全更新 | go test ./... 通過、pve05-07 全 host で 24h × 2 cycle 安定 |
| **5. multi-OS** | `pkg/render/pdclient_{systemd,rcd,nixos}.go`、FreeBSD VM テスト、NixOS module 化 | FreeBSD VM + NixOS VM で同 config が動く |
| **6. RA / PPPoE** | `routerd-ra-receiver`、`routerd-pppoe-client` 追加 (Layer 1 daemon)、関連 controller | RA RDNSS 取得 → DNS upstream 反映、PPPoE up → DHCPv6 起動 |
| **7. 横展開** | `routerd-dhcpv4-client`, `routerd-link-monitor`, `routerd-healthcheck`、SOHO 機能 (WireGuard, IPsec, VRF, VXLAN) | router02/router04/router05 で WireGuard p2p、VRF test device、VXLAN over WireGuard、IPsec は swanctl 生成 test |
| **8. enterprise 拡張** (将来) | `routerd-frr-monitor`、`FRRConfigController`、`BGPSession`/`EVPNInstance` Kind、Tier C 検証 | PVE SDN 置換テスト、k8s ext 接続テスト |

各 Phase 完了 → `git tag` で rollback 経路確保。pve05-07 全 host で 1 cycle 以上動かしてから次 Phase。

---

## 20. 設計判断のサマリ (これが foundation の決定事項)

1. routerd は systemd + k8s + Ansible の交差点。各々から最小エッセンスを採る、重複を恐れない
2. routing protocol (BGP/OSPF/IS-IS/EVPN) は **FRR wrap**。自前実装しない
3. DHCP server / DNS / RA / NTP / VPN は当面 **既存実装 wrap** (dnsmasq / radvd / chrony / WireGuard / strongSwan)
4. WAN acquire 系 daemon (PD / RA / PPPoE / DHCPv4) は **自前**。これが routerd の独自価値
5. primitive は H から E まで通用。新 tier で追加 primitive を最小化
6. multi-OS 抽象は Linux + FreeBSD + NixOS で検証。OS 固有 quirk は `pkg/platform` で吸収。Alpine と Ubuntu は派生
7. **PVE VM が運用形態の前提**。physical box は後段
8. enterprise scale は HA + commit-confirmed + 観測 export + FRR wrap で届く。primitive 大改修なし
9. **fsnotify は notify only**、apply は明示 trigger。意図と事実の自動反映を分ける
10. 過去の試行錯誤痕は **新環境 pve05-07 で全部捨てて再構築**。memory の重要 invariant は新 architecture の構造で守る

---

## 21. 未決事項 (user 判断ほしい)

a. **doc 構成**: 旧 5 doc は archive 移動 / git rm / そのまま履歴で残す のどれにするか。本 doc を `docs/design.md` として repo にコミットするか
b. **Phase A ゴール**: Tier H (IX2215 置換) のみで完成宣言とするか、Tier S 一部 (WireGuard 等) も含めるか
c. **GoBGP 組込 vs FRR wrap の境界**: Tier S で routing 入れる時、まず GoBGP で試すか、最初から FRR wrap で行くか
d. **commit-confirmed**: Tier H から実装するか、Tier S+ 以降か
e. **`Status.Conditions[]`**: 最初から全 Kind の必須 field にするか段階導入か → 推奨は最初から
f. **routerctl plan / apply / confirm の UX**: VyOS 風 (commit / rollback / show) にどこまで寄せるか
g. **FreeBSD VM の検証タイミング**: Phase 5 で良いか、もっと早く (Phase 1 後) に入れるか
h. **NixOS module の形式**: `services.routerd.*` の下にぶら下げるか、独立 flake にするか
i. **Alpine 対応**: musl + OpenRC 対応をいつ着手するか (Phase 5? Phase 7?)
j. **IX2215 比較テスト**: 新 routerd と IX2215 の同等動作確認を Phase いつでやるか (推奨: Phase 4 完了後)
k. **PVE SDN 置換 PoC**: Phase 8 に置いたが、もっと早く architecture 検証だけでもやるか

---

## 22. 1 行サマリ

routerd は **PVE VM 上で動く multi-OS declarative router framework**。`/etc/routerd/*.yaml` で意図を宣言、Layer 1 daemon (PD/RA/PPPoE/DHCPv4 を自前) が protocol packet を扱い event 発行、in-process bus が SQLite に永続化しつつ controller を fanout、controller は path 式 (`${X.status.y}`)・ownerRefs・ready_when・change_on で Layer A 依存を declarative に表現、足りない連携は EventRule (8 op) と DerivedEvent (retract semantics) で補う、config 変更は fsnotify で検出のみ・適用は `routerctl apply` 明示・commit-confirmed で SSH 事故防止、routing protocol は FRR wrap で Tier C+ にスケール、過去の試行錯誤痕は pve05-07 上で **全部捨てて再構築**、memory invariant (PD broken 時に AAAA 出さない 等) は新 architecture の ownerRefs cascade と ready_when で構造的に守る、最終目標は **IX2215 完全置換 + PVE SDN 置換 + k8s 外部接続性ルータ** の 3 deployment を同 primitive 集合で支える。
