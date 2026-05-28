---
title: 安定版マイルストーン
sidebar_label: 安定版マイルストーン
sidebar_position: 0
---

# 安定版マイルストーン

routerd は `vYYYYMMDD.HHmm` 形式で頻繁にリリースしますが、その中から**本番運用に推奨できる版**を「安定版マイルストーン」として節目ごとに選びます。新しく導入するときは、このページで案内する版を使ってください。

## 現在の推奨版

| 項目 | 内容 |
| --- | --- |
| バージョン | **v20260528.2308** |
| 位置づけ | 推奨安定版（v20260528.1805 を置き換え。reverse DNS lookup の goroutine 数を上限化し、heap/goroutine/fd を継続観測する `routerctl doctor runtime` を追加。実機 runtime soak で検証済み） |
| 稼働実績 | 本番ルーター（homert02）で検証済みです。`routerctl doctor runtime -o json` soak（10 分間隔 ×4 サンプル）で `numGoroutine` は 123、`openFds` は 25（/ 524287）で終始 flat、各サンプル `status=pass` でした。heap は健全な GC sawtooth（heapObjects 28k → 52k → 41k → 83k、numGC 29 → 277 と頻繁に reclaim）で単調増加ではありません。BGP は 2/2 Established を維持、`routerctl doctor dslite` / `routerctl doctor reconcile` ともに PASS、routerd-bgp PID 不変、NRestarts=0。これは v20260528.1805 の 2 時間 fd+heap soak（all_fd / sockets / SQLite ledger fd が flat、RssAnon plateau）の上に積み上げたものです。v20260528 系列を通じて、3 つの fd 漏洩根本原因（#39 SQLite ledger、#40 control/status socket keep-alive、#40 BGP gobgp client）、2 つの heap 増加源（リクエストごとの OTel instrument 再生成、無制限の reverse DNS cache）、そして reverse DNS lookup の goroutine fan-out をそれぞれ解消しました |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）、CI と Release ワークフローをすべて通過 |

## v20260528.2308 を推奨する理由

推奨の理由は**新機能の追加ではなく運用上の成熟**です。v20260528.2308 は
v20260528.1805 の本番安全特性（#39 / #40 fd 漏洩修正、v20260528.1805 の
heap 漏洩修正＝OTel instrument singleton ＋ bounded reverse DNS cache、
#36 / #37 / #38 の観測性契約、BGP の idempotent reconcile、doctor dslite
の selectedSource 整合、Gateway Health の独立画面化、`install.sh` の即時
失敗、シークレット伏字化、`ManagementAccess` 適用ガード、機械可読な
`routerctl doctor`、推奨安定版表示の整合 CI ガード）をすべて受け継ぎ、
リソース漏洩調査の最後の 2 ピースと小さな Web Console UX 修正を加えて
います。

- **reverse DNS lookup の goroutine 数が上限化されました。**
  `reverseDNSCache.lookupMany` は従来 pending アドレスごとに goroutine を
  起動していました（並列 lookup は semaphore で 8 に制限していたが
  goroutine 自体は大量に作られた）。1 回の `/api/v1/summary`（最大
  1000 行）で ~1000 個の blocked goroutine が生じ得ました。現在は固定
  サイズの worker pool (`reverseDNSLookupConcurrency = 8`) を使い、
  `reverseDNSPendingMax = 1000` が呼び出し側に依存せず 1 回あたりの
  処理件数を上限化します。homert02 soak で summary polling 下でも
  `numGoroutine` が flat（123）であることを確認しました。

- **`routerctl doctor runtime` で継続的なリソース可視性を提供します。**
  新しい doctor area ＋ 読み取り専用 control-API `/runtime` エンドポイント
  が routerd 自身の heap / goroutine / GC / fd footprint を報告するため、
  この一連の調査で行ったようなリーク調査が self-service になります
  （ssh ＋ /proc 不要）。`numGoroutine` 10000 超または fd が
  `RLIMIT_NOFILE` の 80% 以上で WARN。観測用で FAIL にはなりません。

- **Web Console の Firewall「Deny activity」を軸ラベル付き棒グラフに**
  変更しました（従来はラベルの無い曖昧な sparkline）。縦軸（peak / 0、
  「高いほど拒否が多い」）と横軸（「24h ago」→「now」）を明示します。

v20260528.1805 から受け継ぎ、homert02 v20260528.2308 でも再検証された
本番安全契約:

- **`/api/v1/summary` の polling で heap が無制限に増えなくなりました。**
  `recordConsoleMetrics` は従来リクエストごとに 7 つの OpenTelemetry
  ゲージを作り直していましたが、`sync.Once` シングルトン
  (`getConsoleMetrics`) で一度だけ生成するようにしました。
  `reverseDNSCache` は TTL を再ルックアップ判定にしか使っておらず、
  期限切れエントリの削除もサイズ上限もなかったため、ファイアウォール
  ログ / コネクション表 / 通信フローに現れた個別の宛先アドレスが
  すべて永久エントリになっていました。現在は期限切れを削除し、
  4096 件のハード上限を呼び出しの入口と出口の両方で適用します。
  homert02 の 2 時間 soak で `RssAnon` が単調増加せず plateau する
  ことを確認しました。これらは v20260528.0402 の fd 漏洩対応に
  対応する heap 側の修正として、調査を完結させるものです。

v20260528.0402 から受け継ぎ、homert02 v20260528.1805 でも再検証された
本番影響のある fd 漏洩修正 2 件と観測性契約 3 件:

- **routerd serve が SQLite ledger の fd を漏らさなくなりました。**
  これまで `resource.LoadLedger` は呼び出しごとに
  `/var/lib/routerd/routerd.db` を新しい `*sql.DB` で開いていましたが、
  `Ledger` には `Close()` がありませんでした。
  `IPv4PolicyRouteController.cleanupLedgerOwnedPolicyRoutes` の reconcile
  経路は約 30 秒周期で走り、毎回 `routerd.db` と `routerd.db-wal` の
  fd を 1 組ずつ増やしていました。homert02 v20260526.2335 では SQLite fd
  が約 300 まで蓄積していました。本修正では `Ledger` インターフェースに
  `Close()` を追加し、`LoadLedger` の全呼び出し元で `defer` し、
  `OpenSQLiteLedger` に `SetMaxOpenConns(1)` / `SetMaxIdleConns(1)` も
  追加して、Close 漏れでも 1 接続を超えないようにしました。Linux 限定の
  回帰テスト 2 件で 10 回の open/close サイクル後に `/proc/self/fd` が
  増えないことを検証しています。homert02 検証では SQLite ledger 系
  fd が ~300 から flat 4 に下がりました（#39）。

- **routerd serve は Unix socket の fd も漏らさなくなりました。** 2 つ
  の別個の原因を解消しました。(a) 制御 / ステータスソケットの
  `http.Server` で `SetKeepAlivesEnabled(false)` を呼び、
  `controlapi.NewUnixClient` の `Transport.DisableKeepAlives` を
  `true` に。これまで polling 系クライアントが IdleTimeout 未満で
  再接続することで keep-alive 接続が「アイドル」にならず、無期限に
  open のままでした。(b) BGP コントローラーの gobgp HTTP クライアント
  (`pkg/controller/bgp/gobgp_client.go`) は ~30 秒 reconcile ごとに
  `/run/routerd/bgp/control.sock` を 2 回 dial しますが、`DisableKeepAlives`
  / `req.Close` / `defer CloseIdleConnections()` のパターンが抜けていた
  唯一の内部 HTTP クライアントで、+4 fd / 分の残存ドリフトの正体でした。
  homert02 v20260528.0402 検証では、16 分間の 5 分間隔 4 サンプルで
  `all_fd=24` と `sockets=16` が完全に flat、Unix ストリームの ESTAB は
  71 から 9 に減少しました（#40）。

- **HealthCheck プローブが egress / source / route の根拠情報を記録
  し、リソースごとに直近 N 件の失敗履歴を保持するようになりました。**
  各結果は `FailureKind`（timeout / connection_refused /
  network_unreachable / host_unreachable / no_route / dns_error /
  tls_error / ...）、`EgressInterface`、`SourceAddress`、
  `SourceOrigin`（pd / ra / static / dynamic）、`NextHop`、
  `OutInterface`、`RouteSource`、`TunnelLocal`、`TunnelRemote` を
  保持します。`State` には `FirstFailureTime` / `LastFailureTime` /
  `LastSuccessTime` / `FailureCount` と、設定可能な 20 件履歴
  `History []ProbeRecord` が入りました。`cmd/routerd-healthcheck` に
  運用者ヒント用フラグ `--source-origin` / `--tunnel-local` /
  `--tunnel-remote` を追加し、プローブが推定できない情報を補えます。
  イベント属性と既存の `StatusMap` にも新フィールドを反映している
  ため、`routerctl show / describe` で自動的に閲覧できます（#37）。

- **コントローラーごとの reconcile 失敗履歴を control API に公開
  します。** `ControllerStatus` に `ReconcileErrorHistory
  []ReconcileErrorEntry` と `MaxDurationAt *time.Time` を追加。各
  エントリは `StartedAt` / `CompletedAt` / `Duration` / `DurationMs` /
  `Trigger` / `ResourceKind` / `ResourceName` / `Error` を持ちます。
  controller framework にはオプショナルな `ResourceObserver` 拡張を
  追加し、reconcile の対象 resource kind / name を runtime store まで
  配線します（既存 Observer 実装には互換）。`routerctl status
  --show-errors` でテーブル表示の各 controller 行の下に縦に履歴を
  表示します。JSON / YAML 出力では既存の StatusMap 経由で自動的に
  含まれます。新規 `routerctl doctor reconcile --since <duration>`
  はステータスソケットに問い合わせ、指定窓内の reconcile エラー件数を
  pass / warn (≥1) / fail (≥10) で判定し、detail に最大 5 件のサンプル
  を表示します。homert02 v20260528.0402 で `doctor reconcile` が
  `pass=1 warn=0` を返し、本番稼働が確認されています（#38）。

- **dns-queries / traffic-flows に絶対時刻範囲、フィルタ、集計が追加
  されました。** `--from` / `--to` は RFC3339 や `2006-01-02T15:04:05`、
  `2006-01-02 15:04:05` などの一般的な形式を受け取ります（タイムゾーン
  省略時は UTC）。DNS は `--rcode` / `--upstream` / `--qname-suffix` /
  `--duration-min`、flows は `--peer-suffix` / `--protocol` /
  `--asymmetric` を追加。新規 `--agg` / `--stats` モードは
  `SUMMARY` と、DNS では `BY RESPONSE CODE` / `BY CLIENT` /
  `BY UPSTREAM` / `BY QNAME SUFFIX`、flows では `BY CLIENT` /
  `BY PEER` / `BY PROTOCOL` を、duration の p50 / p95 / p99 と
  あわせて出力します。直接 DB 取得は `--chunk-size` で分割され、
  各 chunk が個別の ctx デッドラインを持ちます。`DeadlineExceeded`
  時のエラーには、ここまで取得した行数を含めます。`--limit` の
  既定値は 100 → 500、`--timeout` は 5 秒 → 30 秒、内部
  `DNSQueryFilter` / `TrafficFlowFilter` のハード上限は 1000 → 10000
  に引き上げました。Web Console には
  `/api/v1/dns-queries/aggregate` と
  `/api/v1/traffic-flows/aggregate` のエンドポイントが追加されています
  (#36)。

doctor の detail 表示、サブコマンド --help、推奨安定版表示の整合に
ついては v20260526.2335 から受け継ぎ、homert02 v20260528.0402 でも
再検証されています。

- **routerd 本体のバイナリ更新で BGP セッションが落ちなくなりました。**
  BGP コントローラーが reconcile の入り口で適用済みのポリシー状態を
  再構築するようになり、routerd 再起動時に同一内容のインポートポリシー
  割り当てを再 PUT して BGP セッションを reset しなくなりました。
  homert02 の **2 回連続の routerd 再起動**で検証済みです
  （PID 3368318 → 3407972 → 3428160）。BGP は終始 2/2 Established を
  維持し、uptime は再起動ごとに途切れず伸び続け、2-way ECMP（.38 /
  .53）も再投入なしでカーネルに残りました。
- **`routerctl doctor dslite` が実体と一致するようになりました。**
  doctor は DSLiteTunnel の `phase=Up` を健全と判定し、
  EgressRoutePolicy の選択を `status.selectedSource =
  "DSLiteTunnel/<name>"` 経由でも認識するようになりました（旧来の
  `selectedCandidate` 名一致も併用します）。homert02 のように
  `dslite-pd-balanced` のような集約候補名を使う本番構成でも、
  `gatewayHealth=ok` の DSLiteTunnel が WARN にならなくなりました。
  検証結果は warn=4 → pass=12 / warn=0 です。
- **Gateway Health の UI が独立画面になり、表示が安定しました。**
  Web Console は Gateway Health を Overview から独立画面に分離し
  （Connections / Clients と同じ構成）、`selectedPath` /
  `preferredPath` / `fallbackReason` / `failedProbes` /
  `lastTransition` を含む完全な根拠情報を表示します。Overview には
  集約カードのみを残します。部分更新中に `Components 0 / Unknown` と
  瞬時に表示されるちらつきも解消しました。`reconcileSummary` は空
  コンポーネントを含む薄いスナップショットで前回値を上書きしなく
  なりました。検証結果は **180 秒 / 90 サンプルで good=90 / bad=0、
  26 コンポーネント確認** です。
- **`install.sh` が暗黙の no-op に陥らなくなりました。** これまでは
  リリースツリーの外から起動すると（`cd /tmp/release &&
  ./pkg/install.sh ...` など）、cwd 相対の `bin/*` のグロブが一度も
  展開されず、`--with-ndpi-archive` のペイロードだけが反映される
  にもかかわらず終了コード 0 と `routerd upgrade completed` の成功
  表示だけが出ていました。cwd に `bin/routerd` のペイロードが無い
  場合は、明示メッセージとともに終了コード 2 で即座に失敗するように
  なりました。CI には両ケース（ペイロード欠落・正規 cwd）を再現する
  スモークテスト (`scripts/install-sh-cwd-smoke.sh`) を組み込んで
  います。homert02 での検証では、cwd 不一致のアンチパターンは
  **終了コード 2 で即座に失敗**、正規のパッケージディレクトリに `cd`
  してから実行するパターンは終了コード 0 でした。

**継承事項（v20260526.1607 から）:** Web Console の `/api/v1/config` と
generation 系エンドポイントは、WireGuard の `privateKey` /
`preSharedKey`、Tailscale の `authKey`、BGP / PPPoE / IPsec の
`password`、WebConsole の `initialPassword`、bearer / token 系の値を
シリアライズ前に伏字化します。`/api/v1/summary` は DNSResolver /
DSLiteTunnel / DHCPv6PrefixDelegation / EgressRoutePolicy / NAT44Rule /
HealthCheck を `gatewayHealth` に集約します。`routerctl doctor` は
v1alpha1 の機械可読契約（`-o json`、明文化された area / status の
列挙 / summary、不合格時は非 0 で終了）です。`ManagementAccess` の
適用前検証は `--allow-mgmt-lockout` 無しでは管理経路の締め出しを防ぎ
ます。DNS リゾルバは独立した長寿命サービスユニットとして動き、routerd
の再起動やアップグレードで DNS は中断しません。`install.sh` はバイナリ
更新時に `routerd-bgp` を自動再起動しないので、eBGP セッションと ECMP
は routerd バイナリの更新をまたいで残ります。`routerctl ledger` の
保守コマンド（`integrity-check` / `vacuum` / `backup` /
`prune-events`、非 dry-run の prune には監査イベントを発行）も
引き続き利用できます。

## 既知の観測（リリースを止めない事項）

- **`install.sh` 後に `routerd-bgp` が旧 inode のままで動く場合がある。**
  これは意図どおりです。`install.sh` はアップグレード時に `routerd-bgp`
  を自動再起動しないので、確立済みの BGP セッションと ECMP が routerd
  バイナリの更新をまたいで残ります。運用者が Graceful Restart の
  タイミングで `systemctl restart routerd-bgp` を実行するまで、
  プロセスは旧 inode のバイナリを掴み続けます。
- **`ManagementAccess` 未宣言の構成では `routerctl doctor mgmt` が SKIP
  になる。** これは稼働中の設定側の選択であり、リリースの欠陥では
  ありません。適用時の締め出しガードと `doctor mgmt` の判定を有効に
  したい場合は `ManagementAccess` リソースを宣言してください
  （[`examples/home-router-mgmt-protected.yaml`](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
  を参照）。

:::warning アップグレード時の注意
- **`install.sh` は必ず展開したリリースディレクトリに `cd` してから実行してください。** 別のディレクトリから（例: `cd /tmp && sudo ./routerd-release-vYYYYMMDD.HHmm/install.sh ...`）起動すると、終了コード 2 で実行を拒否するようになりました。これは意図した動作です。以前は同じ呼び出しで暗黙の no-op が発生し、`--with-ndpi-archive` のペイロードだけが反映されていました。
- **v20260523.1542 以前から上げる場合:** `disabled:` フィールド（`enabled: false` を使用してください）と無効化済みの `--controller-chain*` / `--observe-interval` フラグは削除済みです。該当する設定とホストのサービスユニットをアップグレード前に書き直してください。
- **DNS リゾルバのサービスユニット化:** リゾルバは `routerd-dns-resolver@<name>.service` として動くようになりました。この方式への初回アップグレード時だけ「子プロセス → ユニット」の切り替えで一度だけ短い DNS 瞬断が出ます。以降は routerd の再起動・アップグレードで DNS は中断しません。
:::

## 「安定版」の意味と注意点

:::warning API はまだ v1alpha1 です
「安定版マイルストーン」は、**この版が本番運用に堪える品質である**ことを示すもので、**API（リソーススキーマ）の後方互換を約束するものではありません**。
:::

- routerd のリソース API は現在 **v1alpha1** です。リリース間で**破壊的変更が入ることがあります**。
- バージョンを上げるときは、後方互換に頼らず、**新しいスキーマに合わせて設定（YAML）を書き直す**前提で進めてください。
- 移行用の互換コードは持たない方針です。各版の変更点は [変更履歴（Changelog）](./changelog.md) を確認してください。

## 導入とアップグレード

導入手順は [導入とアップグレード](../install-and-upgrade.md) を参照してください。アップグレードは、推奨マイルストーン版を起点に行うことを勧めます。
