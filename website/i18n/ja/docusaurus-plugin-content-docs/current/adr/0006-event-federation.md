# ADR 0006: CloudEdge Event Federation（routerd 間の型付きイベント）

![ADR 0006 Event Federation の図。手動記述の claim 問題から、EventGroup、EventPeer、EventSubscription の設計判断、observed-fact の不変条件まで](/img/diagrams/adr-0006-event-federation.png)

## ステータス

承認済み。実験的実装を進行中（2026-05-30）。
Phase 1、1.5、2、3 は **`event-federation` ブランチで実装済み**：

- **Phase 1**（イベントエンベロープ + `EventGroup` Kind + SQLite ローカルストア + `routerctl
  federation event emit/list`）完了。
- **Phase 1.5**（`EventPeer`/`EventSubscription` Kind + バリデーション）完了。
- **Phase 2**（オーバーレイ経由のピア配送、`routerd-eventd`、HMAC、リトライ、
  リテンション prune）完了。**lab-smoke PASS**
  （[トランスポートエビデンス](../releases/evidence/cloudedge-event-federation-transport-20260530.md)）。
- **Phase 3**（subscription から plugin を経由して `RemoteAddressClaim` `DynamicConfigPart` を生成）
  完了。**lab-smoke PASS**
  （[subscription エビデンス](../releases/evidence/cloudedge-event-federation-subscription-20260530.md)、
  [how-to](../how-to/event-federation-subscription.md)）。

Phase 4（プロバイダー `actionPlan` プラグイン、dry-run）は**次のフェーズで未着手**。
Phase 5（プロバイダーアクション実行）は **MVP スコープ外**。

## 背景

SAM（[リファレンス](../reference/selective-address-mobility)、
[マイルストーン](../releases/cloudedge-sam-mvp-milestone.md)）は
Azure、PVE、AWS、OCI でクリーン検証済みである（3 クラウドパリティ）。
SAM は**捕捉（プロバイダー固有）と配送+claim（routerd 共通）**の分離を証明した。
しかし、これを駆動する `RemoteAddressClaim` は**現時点では手動記述**である。
次のステップは、claim を**イベント駆動**で発見、伝搬、実体化することである。

> オンプレミスの routerctl がクライアント IPv4（ARP/Clients/DHCP）を検知し、型付きイベントを発行する。
> フェデレーションバスがクラウド側 routerd に配送し、subscription がプロバイダープラグインを起動する。
> プラグインが `RemoteAddressClaim` を `DynamicConfigPart` として返却する
> （+ プロバイダー secondary-IP `actionPlan`）。
> **クラウド設定を人手で編集することなく**、クラウド側が `provider-secondary-ip` 捕捉の準備を完了する。

### 既存の資産（MVP はグリーンフィールドではない）

設計を現在のコードツリーに基づかせる。
ほとんどのビルディングブロックは既に存在しており、真に新規の作業は**ノード間フェデレーショントランスポート**と**イベントからプラグインへの subscription トリガー**である。

- **型付きイベントエンベロープ**：`pkg/daemonapi` の `DaemonEvent{Type,Time,Daemon,Resource,
  Severity,Reason,Message,Attributes}` + `NewEvent(...)`。現在は daemon から main へのフローだが、
  既に型付きのトピック付きエンベロープになっている。
- **daemon から routerd へのトランスポートパターン**：daemon が UNIX ソケット上の
  HTTP で制御ソケットに POST する（`cmd/routerd-dhcp-event-relay` から `controlapi.Prefix +
  /dhcp-lease-event` へ、`unix:/run/routerd/routerd.sock` 経由）。イベントリレー daemon の前例もある。
- **分離された長寿命 daemon の前例**：13 個の `cmd/routerd-*` daemon
  （`routerd-bgp`、`routerd-ra-observer`、`routerd-dhcp-event-relay` 等）。
  gobgp pivot（ADR 0004）が「再起動によるドロップを避けるため in-process より分離プロセスを優先する」方針を確立した。
- **Plugin から DynamicConfigPart へのパイプライン**：`pkg/plugin/runner.go`、
  `pkg/plugin/dynamic_config.go`、`pkg/dynamicconfig/{types,merge}.go`、
  `PluginRequest`/`PluginResult`。effective = startup + active dynamic − masks。
- **状態**：SQLite（`pkg/state/sqlite.go`）。
- **プロバイダープロファイル + 外部認証**：`CloudProviderProfile`、
  `auth.mode=external-command`（specs.go:1193）がプロバイダー固有プラグインのフックとなる。
  `provider: oci|aws|azure|gcp` はバリデーション済み。

## 決定

**CloudEdge Event Federation** を、マージ済みの実験的 SAM の上に、新ブランチで次の実験的 MVP として構築する。
**スコープは削らず、順序付きの独立して受け入れ可能なフェーズに分解し、各フェーズをワークフローとして駆動する。**
各フェーズは動作するデモ可能なスライスを出荷し、次のフェーズのゲートとなる。

### 設計原則

1. **イベントは観測事実であり、設定ではない。**
   ノードは `routerd.client.ipv4.observed` を送信し、生の `RemoteAddressClaim` は送信しない。
   受信側の*信頼されたローカルプラグイン*が、それを型付き claim + actionPlan に変換するかどうか、どう変換するかを決定する。
   ワイヤ上にコマンドは流れない。
2. **at-least-once + idempotent**。exactly-once ではない。
   ストアの冪等性はイベント `id` をキーとする（重複 `id` は no-op insert）。
   `dedupeKey` は subscription 側のグルーピングキーで、同一事実の繰り返し観測を集約するためのものであり、Phase 1 では DB のユニーク制約**ではない**。
   動的リソース名は決定的（`onprem-10-88-60-9`）。
   プロバイダーアクションは既に充足されていれば no-op。
   コンセンサス、ゴシップ、全順序はなし。
3. **再利用し、再発明しない。**
   `DaemonEvent` エンベロープ、制御ソケット HTTP トランスポートイディオム、Plugin から DynamicConfigPart へのパイプライン、SQLite 状態、`CloudProviderProfile`/`Plugin` を再利用する（新規 `CloudProviderPlugin` Kind は不要）。
4. **新規 Kind を最小限に。**
   MVP は **3 つ**を導入する。
   `EventGroup`（バスの識別子 + 認証 + リテンション）、`EventPeer`（配送先 + インラインの push/receive フィルター）、`EventSubscription`（受信イベントからローカルプラグインへのトリガー）。
   提案されていたスタンドアロンの `EventFilter` は `EventPeer` に統合し、フィルターをピア間で共有する必要が生じた場合にのみ独立 Kind に昇格する。
5. **分離された daemon。**
   フェデレーションの送受信は新しい `cmd/routerd-eventd` 長寿命 daemon に置く（ADR 0004 の前例に従う）。
   reconcile ループ内ではない。
   オーバーレイ（`wg-hybrid`）にのみバインドする。
6. **MVP ではプロバイダーの mutation は dry-run のまま。**
   プラグインは `actionPlan` を発行する。
   実行は後のフェーズで、明示的な approval/auto-apply ポリシーの背後に置く。

### トランスポートとセキュリティ（MVP）

- 受信側 = **WireGuard オーバーレイインターフェースのアドレスにのみバインドする** HTTP リスナー（例：`169.254.x.y:9443`）。
  WG トンネルが機密性の境界となる。
  整合性と誤配送防止のために**メッセージレベル HMAC**（ファイルからの共有秘密）を追加する。
  **TLS は延期**する。TLS リスナーは証明書プロビジョニングを必要とし、SAM stocktake が指摘したブートストラップの摩擦を再導入してしまう（将来：mTLS、ピアごとの Ed25519、クラウド KMS 署名）。
- MVP では push-only（`onprem→cloud` の観測、`cloud→onprem` の claim/result ack）。
- バックオフ付きリトライ。(event, peer) ごとの配送状態を SQLite に保持。

### 状態機械でレビューすべき不変条件（差分だけではなく）

プロジェクトの out-of-process ステートフル daemon に関するルールに従い、正当性条件を不変条件として記述する。

- **フィードバックループの禁止。**
  ノードは、自身が*捕捉*しているアドレス（provider-secondary-ip または proxy-arp）に対して `*.observed` を再発行してはならない。
  観測は `ownerSide` + `domain` でスコープされ、捕捉済み/secondary アドレスはオブザーバーのソースセットから除外される。
  これがないと、クラウド自身の secondary `.9` が再観測、再伝搬され、フラップする。
- **provision と de-provision の非対称性。**
  provisioning（claim の出現）は即時でよい。
  **de-provisioning（TTL 失効 / `*.expired`）はヒステリシスを持たなければならない。**
  300 秒の observe TTL よりも遥かに長い猶予 + デバウンスが必要である。
  フラッピングするクライアントがクラウド secondary-IP の assign/unassign を繰り返し駆動するわけにはいかない（API レート制限 + コスト + データプレーンチャーン）。
  TTL から teardown へのポリシーは明示的かつ保守的にする。
- **(domain, address) あたり単一ライター。**
  所有側が権威を持つ。
  受信側は、`ownerSide` が*送信側*であるアドレスに対してのみ claim を提案する。
- **冪等なプロバイダーアクション。**
  "already assigned" は aws/azure/oci 全体で success/no-op とする。

### プロバイダープラグインフレームワーク

OS CLI を呼び出すローカル実行ファイルとして実装する。
SDK を routerd に静的リンクする方式**ではない**（SDK の churn/auth をコアから排除し、クラウドネイティブ識別情報を有効化し、デバッグを容易にするため）。

- **AWS**：`aws ec2 assign-private-ip-addresses`。認証：**IAM インスタンスプロファイル**優先、`AWS_PROFILE`/env フォールバック。
- **Azure**：`az network nic ip-config …`。認証：**マネージド識別情報**優先、`az login`/SP env フォールバック。
- **OCI**：`oci network private-ip create` / `vnic`。認証：**インスタンスプリンシパル**優先、OCI config プロファイルフォールバック。

`Plugin.capabilities` がプラグインの権限をゲートする（`observe.events`/`propose.dynamicConfig`/`propose.providerAction`）。

## フェーズ分解（フェーズごとに 1 ワークフロー、順に実行）

各フェーズ = 独立して受け入れ可能なスライス。後のフェーズは先行フェーズの受け入れがゲートとなる。
実装は codex に委託し、claude がオーケストレーション + レビューを担当する。

- **Phase 1 完了: イベントモデル + ローカルストア。**
  `EventGroup` Kind。
  `DaemonEvent` を外部 `Event` エンベロープとして再利用、拡張する（id, group, sourceNode, type, subject, ttl, dedupeKey, payload）。
  SQLite `federation_events` テーブル。`routerctl federation event emit/list`。
  *受け入れ条件:* emit したイベントが stored される（TTL 付き）。重複 id は冪等。期限切れは無視。
- **Phase 1.5 完了（lab-smoke PASS）: `EventPeer`/`EventSubscription` Kind + バリデーション。**
- **Phase 2 完了（lab-smoke PASS）: オーバーレイ経由のピア配送。**
  `EventPeer` Kind。
  `routerd-eventd` レシーバーが `wg-hybrid` にバインド。HMAC。push + バックオフ。`event_deliveries`。
  *受け入れ条件:* オンプレミスが `wg-hybrid` 経由でクラウドに push する。重複 push は冪等。不正 HMAC は拒否。
  `routerctl event deliveries`。`routerd-eventd` が `EventGroup` リテンション（`maxAge`/`maxEvents`）に従って `federation_events` を定期的に prune する。
  `routerctl federation event prune --dry-run` が削除対象を報告する。
- **Phase 3 完了（lab-smoke PASS）: subscription トリガーによるプラグインから DynamicConfigPart への変換。**
  `EventSubscription` Kind。イベントバッチから `PluginRequest` を生成し、`PluginResult` を `DynamicConfigPart`（`routerd.net/dynamic-source`、`event-id`、`event-group` アノテーション付き）に変換する。
  デバウンス/batchWindow。`event_subscription_runs`。
  *受け入れ条件:* クラウドが `10.88.60.9/32` の `client.ipv4.observed` を受信し、プラグインを経由して `RemoteAddressClaim` DynamicConfigPart が `routerctl dynamic render` で確認可能になる。
  actionPlan は表示のみで実行しない。
- **Phase 4、次（未着手）: プロバイダー actionPlan プラグイン（dry-run）。**
  `aws/azure/oci-address-claim` サンプルプラグイン。標準化された `actionPlan` フォーマット。インスタンス識別情報による認証。
  *受け入れ条件:* プラグインが assign-secondary-IP を提案する。mutation なし。プランが `routerctl plugin`/`dynamic` で確認可能。
- **Phase 5（MVP 後）: プロバイダーアクション実行。**
  approval/auto-apply ポリシー、アクションジャーナル、ベストエフォートの undo、識別情報ドキュメント。MVP スコープ外。

最初のエンドツーエンドスモークは**手動 `routerctl federation event emit` からフェデレーションを経由して DynamicConfigPart を生成する**パス（Phase 1-3）。
ARP/Clients オブザーバープラグインはそのスモークの*後*に導入し（`routerd-ra-observer` をモデルにする）、障害を分離可能にする。

### MVP イベントタイプ

`routerd.client.ipv4.observed`、`…ipv4.expired`、`…dynamic.part.accepted/rejected`、`…provider.action.planned/succeeded/failed`。
最初のスモークには `observed`+`expired` だけで十分。

## 結論

- **正の影響：** SAM を手動記述からイベント駆動に転換する。
  小さくデモ可能なフェーズ。
  既存のエンベロープ、トランスポート、プラグイン、状態を再利用する。
  新規 Kind の増殖なし（3 つ）。
  プロバイダー mutation はゲート付き。
  クラウドネイティブ識別情報を初日からサポート。
- **負の影響とリスク：** 新しいネットワークリスナーが増える（オーバーレイバインド + HMAC で緩和）。
  ループ、フラップと provision/de-provision の非対称性は不変条件として強制する必要がある（上記参照）。
  at-least-once は冪等性をプラグインとネーミングに押し出す。
  TLS/mTLS は延期。
  de-provisioning の自動化は意図的に*最後に*有効化する。
- **MVP スコープ外：** コンセンサス、exactly-once、ゴシップメッシュ、任意のリモートコマンド実行、プロバイダー mutation の自動化、完全な IP ライフサイクル自動化、リモートプラグインレジストリ、クロスノード設定書き換え。

## 既知の制限事項（実験的）

- **`routerd-eventd` の supervision は systemd と FreeBSD `rc.d` 向けに生成される。**
  他のサービスマネージャーでは、eventd を自動管理するためにレンダラーの明示的なサポートが必要になる。
- **`EventSubscription` の `batchWindow`/`debounce` は受け入れられるが粗い。**
  フィールドはバリデーションされ、ポール粒度で反映される。
  コントローラーはイベントを**ポール tick ごと**にバッチし、正確なサブ tick タイマーでは動作しない。
  短いデバウンスウィンドウは実質的に tick 間隔に切り上げられる。

## スコープ外と将来の検討事項

- `cloud→onprem` に ack 以上のもの（例：クラウド secondary が存在してからオンプレミスの proxy-arp をトグルする捕捉準備完了シグナル）が必要かどうか。
- ピア間でフィルターを共有する機能（`EventFilter` を独立 Kind に昇格）。
- マルチピア（3 ノード以上のグループ）。MVP は検証済みのペアトポロジーを対象とする。
