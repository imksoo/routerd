---
title: CloudEdge SAM 内部実装
---

# CloudEdge SAM 内部実装

このページは CloudEdge SAM（Selective Address Mobility）の **アーキテクチャ・
内部実装・設定項目** を、運用者と実装者の両方が「中で何が起きているか」を追える
粒度で解説します。概念的な導入は [CloudEdge SAM とは](../concepts/cloudedge-sam.md)
を、設定の書き方は [Selective Address Mobility](selective-address-mobility.md) を
先に読んでください。

実装は `pkg/controller/mobility/` にあります。本ページの記述はその実装（特に
`planner.go` / `controller.go`）と一致させています。

## アーキテクチャ：2つの平面

CloudEdge SAM は到達性とクラウド受け口を明確に分離します。

### 平面1：オーバーレイ到達性 — BGP best-path が真実

各 `MobilityPool` の owned アドレスは **IPv4 unicast `/32` の BGP 広告** として
表現されます。

- ある `/32` の **ホルダー（持ち主）= その prefix の BGP best-path を勝ち取って
  いるノード**。
- 非ホルダーノードは BGP best-path から remote owned アドレスを学習し、オーバー
  レイ next-hop 経由の delivery route を FIB にインストールします。
- アドレスの移動は **BGP の withdraw / advertise と path preference の変更** で
  表現されます。オペレーターはリースや claim を手で書きません。
- 失敗検知は **BFD**（FRR `bfdd`）で高速化し、BFD が不安定なときは BGP hold timer
  が非破壊的な権威として route 撤回を担います。

これは [ADR 0012](../adr/0012-bgp-address-mobility.md) の決定で、旧来の
`AddressLease` / `ownershipEpoch` / `captureEpoch` といった独自台帳を置き換えた
ものです。

### 平面2：クラウド受け口 — プロバイダー操作は背景同期

VPC / VNet / VCN を通って **外部から** 入ってくるパケットは、BGP オーバーレイで
はなくクラウドファブリックのルーティングに従います。そのため routerd は、

- ホルダー VM の NIC に対象 `/32` を **secondary IP** として割り当て、
- その NIC の **forwarding を有効化**（AWS `sourceDestCheck=false` / Azure
  `ipForwarding=true` / OCI `skipSourceDestCheck=true` / GCP `canIpForward=true`）

します。ただしこれらは **到達性の真実の源ではなく**、BGP mobility ビューと
プロバイダー inventory から **背景で eventual に同期される** 操作です。プロバイダー
API が遅れても、オーバーレイ経由の到達性は BGP 収束だけで回復します。

## BGP community 体系

mobility が `/32` 広告に付ける BGP community は、ノードの役割・広告の素性・持ち主
かどうかを他ノードへ伝える **シグナル線** です。定義は
`pkg/controller/mobility/controller.go` にあります。

| Community | 定数名 | 意味 |
| --- | --- | --- |
| `64512:100` | `…CommunityOwner` | この広告は mobility owner `/32` である |
| `64512:101` | `…CommunityRoleOnPrem` | 広告ノードの role は on-prem |
| `64512:102` | `…CommunityRoleCloud` | 広告ノードの role は cloud |
| `64512:110` | `…CommunitySourceObserved` | 素性：観測由来の広告 |
| `64512:111` | `…CommunitySourceStatic` | 素性：static owned アドレスの広告 |
| `64512:112` | `…CommunitySourceHandover` | 素性：ハンドオーバー中の広告 |
| `64512:113` | `…CommunitySourceCapture` | 素性：プロバイダー capture 背景 route |
| `64512:120` | `…CommunityFailover` | failover による seize 広告 |
| **`64512:121`** | **`…CommunityActiveHolder`** | **ホルダービーコン：active なホルダーのみが付与** |
| （ノード別） | node-identity community | どのノードの広告かを一意に示す（nodeRef から導出） |

LOCAL_PREF は `bgpMobilityLocalPrefBase = 200` を基準に、active 広告が standby の
make-before-break 広告より高い preference を持つよう設定されます。

### ホルダービーコン（`64512:121`）が要

`bgpMobilityPathAttrs`（`controller.go`）は、広告が **active なホルダー** によるもの
かつ provider-capture 背景 route でないときに限り `64512:121` を付与します。

受け取り側の `bgpObservedGroupHolder`（`planner.go`）は、ある `/32` の best-path
が **「node-identity community」と「`64512:121`」の両方** を持つときだけ、その
ノードをグループのホルダーと判定します。これにより、

- standby の弱い（低 preference の）make-before-break 広告、
- 起動直後（cold-start）のまだ active でない広告、

をホルダーと **誤認しません**。プラグイン非依存（BGP は常に存在）かつ best-path
非依存（ビーコンは active だけが出す）の **権威あるホルダーシグナル** です。

> 設計上の経緯：当初は next-hop 照合やプロバイダー self-scan でホルダーを判定しよう
> としましたが、それぞれ「next-hop がトンネル underlay で SAM endpoint と別物」
> 「peer の NIC 保持を観測できない」という理由で破綻しました。BGP best-path 上の
> 専用ビーコン community に寄せることで、cold-start の相互譲り合いデッドロックも
> 含めて解決しています。

## placement：アクティブ/スタンバイの決定

`MobilityPool` の各メンバーは `placement.group` と `placement.priority` を持ちます。

- **group** — アクティブ/スタンバイを競わせる単位（例 `azure-edge`）。
- **priority** — **数字が小さいほど高優先**。`0`（未指定）のメンバーは
  `autoPlacementPriorities` が group 内で `10, 20, 30, …` と自動採番します。

### 決定ロジック（`evaluatePlacementWithIncumbent`）

1. 同 group の非 drain メンバーを **priority 昇順 → nodeRef 昇順** で並べる。
2. 先頭を active 候補にする。
3. **no-preempt のタイブレーク**：同 priority で並んだ場合、nodeRef の辞書順
   勝者ではなく **現ホルダー（incumbent）** を優先する。これにより、復帰した
   peer が稼働中のホルダーを奪い返して無駄なハンドオーバーを起こすことを防ぐ。
4. ただし **厳密に高い priority（小さい数字）のメンバーは奪い返す**。incumbent
   優先はあくまで「同 priority を共有しているとき」だけ適用される。

`incumbentHolder` が空のときは純粋な priority/nodeRef 順になり、これがホルダー未
観測時のグループ bootstrap になります。

## no-preempt と failover を両立させる3機構

placement の素の決定に、復帰時の事故と切り替え揺れを抑える3つの機構を重ねます
（すべて `planner.go`）。

### 1. startup fence（起動フェンス）

```
placementSettleStart  = time.Now()        // プロセス起動時に確定（=再起動でリセット）
placementSettleWindow = 120 * time.Second
```

`placementSettleDefersActive` は、**「これから active を主張する」「incumbent peer
をまだ観測していない」「settle window 内」** の3条件が揃ったときだけ active 主張
を保留します。`fencePlacementForStartup` がこれを適用し、fenced な active を
standby に変換します。

- 復帰直後のノードは、自分の BGP RIB / プロバイダー観測が収束する前に同 priority
  タイブレークを勝ってホルダーを奪い返してしまう。fence はこれを防ぎます。
- settle window が過ぎたノード（=長く動いている standby）は fence されないため、
  **本物の failover は遅延しません**。稼働系が死ねば即座に seize します。

### 2. ホルダー保持（holder retention）

`applyHolderRetention` は、**自分が実際に capture を保持している間（`selfHolds`）**
は active を維持します。適用条件は次の通り。

- すでに active なら何もしない。
- `selfHolds` が false なら維持しない。
- `yieldToHigherPriority` が true なら維持しない（後述）。
- startup settle window を過ぎてから適用する（復帰直後の stale な「以前持っていた」
  記憶ではなく、fresh な self-capture 観測を信頼するため）。

これにより稼働中のホルダーは、決定的タイブレーク勝者や一時的な peer 観測ゆらぎに
よって持ち主を手放しません（ADR 0016 の原則：**自分の holdership を失ったときだけ
譲る。peer を観測したからではない**）。

### 3. 異 priority の自動復帰（`higherPriorityHolderActive`）

`higherPriorityHolderActive` は、BGP ホルダービーコンで観測されたホルダーが
**自分より厳密に高優先（priority 数字が小さい）peer** であるとき true を返します。
これが `applyHolderRetention` の `yieldToHigherPriority` 引数に渡ります。

- **同 priority** のときは常に false → retention が効き、no-preempt になる。
- **異 priority** のとき、低優先の現ホルダーは、高優先ノードが復帰してビーコンを
  出し始めると retention を解除して **譲る** → 設定通りの自動復帰になる。

ハンドオーバーは `/32` を1つずつ移すため、データプレーンは瞬断しません。

## フェンシング：stale なプロバイダー操作の排除

プロバイダー操作（secondary IP の assign/unassign 等）は、生成時点の **mobility
path signature（`mobilityPathSig`）** と desired なホルダー、観測されたプロバイダー/
ジャーナル遷移を伴います。reconcile 時に **desired な BGP path がもう一致しない
操作は skip** されます。旧来の ownership/capture epoch テーブルは廃止されました。

seize（failover 時の奪取）には専用の hold-down があります。

- `bgpSeizeLivenessMissingHold = 30s` — liveness marker が欠けたときの seize 抑止
- `bgpProviderMissingRetryHold = 30s` — プロバイダー観測欠落時の再試行抑止
- `bgpTrapRIBMissingHold = 2m` — RIB に trap route が無いときの保持

## capture strategy（クラウド受け口の作り方）

`capture.captureStrategy` でクラウド受け口の作り方を選びます。

| strategy | 対応プロバイダー | 動作 |
| --- | --- | --- |
| `secondary-ip`（既定） | AWS / Azure / OCI / GCP | NIC に `/32` を secondary IP として割り当てる |
| `route-table` | AWS / Azure | ルートテーブル/UDR のエントリをホルダーの NIC に向ける |
| `proxy-arp` | on-prem | L2 セグメントで proxy-ARP/GARP により capture |
| `addr-add` | （汎用） | OS アドレス追加 |

`route-table` 戦略は AWS/Azure のみ対応で、Azure では `capture.target.nextHopIPAddress`
が必須です。route-table/UDR 戦略の実機検証は [#516](https://github.com/imksoo/routerd/issues/516)
で継続中です。

各 capture には必ず **forwarding 有効化** アクションが伴い、その NIC が自分宛て
でないパケットを転送できるようにします。

## オンプレ LAN の権威は不変

BGP は **remote オーバーレイ到達性** を決めますが、ローカル L2/ARP の権威は置き
換えません。オンプレ側では引き続き、

- VRRP-master gating、
- proxy-ARP / GARP、
- 非 master の fail-closed 動作、
- duplicate-holder の doctor チェック、

がローカルの安全機構として有効です。

## graceful stop（make-before-break ハンドオーバー）

`routerd serve --graceful-stop-timeout`（既定 `20s`）は、SIGTERM/SIGINT を受けた
ときに **mobility の make-before-break ハンドオーバーを最大この時間まで待つ**
設定です。`0` で無効化します。計画的な再起動で、新しいホルダーが広告を確立してから
旧ホルダーが退くことで、瞬断を避けます。

## 状態（status）フィールド

`MobilityPool` の status には placement 関連の観測値が出ます。

- `placementActive` — 自ノードがこのグループの active か
- `placementActiveNode` — グループの active ノード
- `placementGroup` — グループ名
- `livenessMarkers` — 観測された peer の liveness marker（node-identity community）

これらは `routerctl doctor` の SAM 診断や `routerctl show` で確認できます。

## 実機で観測された振る舞い（参考）

priority 10 vs 20 の異優先ペア（Azure 実機）での実測：

- **A1 failover**：高優先ノードを停止 → 低優先ノードが3つの `/32` を約132秒で
  全 seize → データプレーン全復旧。
- **A2 restore**：高優先ノードを復帰 → 3つの `/32` を1つずつ回収（フラッピング
  なし）。回収中のクライアント 1 秒間隔 ping は **損失0%**。
- 同優先ペアでは no-preempt が成立し、561秒にわたり持ち主の入れ替わり・split・
  瞬断・cold-start デッドロックなし。

## 関連

- [CloudEdge SAM とは](../concepts/cloudedge-sam.md) — 概念と新しい用語
- [Selective Address Mobility](selective-address-mobility.md) — `MobilityPool` 設定モデル
- [ADR 0012: BGP /32 Address Mobility](../adr/0012-bgp-address-mobility.md) — BGP を真実の源にした決定
- [ADR 0008: Capture Coordination via Fencing](../adr/0008-capture-coordination-fencing.md) — フェンシングの背景
- [provider action execution](provider-action-execution.md) — プロバイダー操作の承認/実行ゲート
