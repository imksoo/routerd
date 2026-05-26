---
title: Changelog
---

# Changelog

routerd のリリース履歴です。形式は [Keep a Changelog](https://keepachangelog.com/ja/) に準拠します。
変更は「追加」「変更」「非推奨」「削除」「修正」「セキュリティ」に分類します。
ただし版番号は Semantic Versioning ではなく、日付と時刻に基づく `vYYYYMMDD.HHmm` 形式を使います。
ソフトウェアは v1alpha1 段階のため、リリース間で破壊的変更を含むことがあります。

## Unreleased

### 修正

- `install.sh` がスクリプト自身のディレクトリを解決して `cd` してから
  リリース payload を読むようになり、`bin/routerd` が解決先に存在しない
  場合は明示メッセージとともに非 0 終了するようになりました。これまでは
  リリースツリーの外から（例: `cd /tmp/routerd-release-vYYYYMMDD.HHmm
  && sudo ./pkg/install.sh ...`）実行すると cwd が payload 外になり、
  `bin/*` の glob が 1 度も展開されず、標準 routerd / routerctl
  バイナリは一切更新されないにも関わらず exit 0 + `routerd upgrade
  completed` の成功表示だけが出ていました（`--with-ndpi-archive` の
  payload だけが反映）。この silent no-op は不可能になりました。
  欠落 payload / 別 cwd 起動の両ケースを再現する smoke
  (`scripts/install-sh-cwd-smoke.sh`) を CI に組み込んでいます。
- Web Console の Gateway Health 画面が partial refresh 中に一時的に
  `Components 0 / Unknown / No gateway component status observed`
  と表示される瞬きを修正しました。`reconcileSummary` はこれまで
  `next.gatewayHealth ?? current.gatewayHealth` を使っていましたが、
  `??` は `null`/`undefined` でのみ fallback するため、
  `{ overall: "unknown", components: [] }` のような薄い snapshot が
  来ると populated な前回値を上書きしてしまっていました。空 components
  を持つ snapshot が来た時に前回の `gatewayHealth` を保持するよう変更
  しました。

## v20260526.2152

### 追加

- `/api/v1/summary` の `gatewayHealth` がコンポーネントごとに
  `selectedPath` / `preferredPath` / `fallbackReason` / `failedProbes` /
  `lastTransition` の根拠フィールドを返すようになりました。Web Console
  は選択中の egress path が preferred と異なる場合に、現在使用している
  fallback ターゲットを強調表示します。

### 変更

- Web Console の Gateway Health を Overview から専用画面へ分離しました
  （Connections / Clients と同じ構成）。Overview には、overall ステータス、
  pass / warn / fail / skip 件数、詳細画面へのジャンプボタン、degraded /
  down 時の worst コンポーネント名一行を含む集約カードのみを残します。

### 修正

- BGP コントローラーが reconcile 時に applied 済みポリシー状態を
  hydrate するようになり、routerd を再起動しても同一内容の
  import-policy 割り当てを再 PUT しなくなりました。これまで本番運用
  （homert02）では routerd 再起動のたびに全 BGP ピアが drop / 再確立
  し、hold-time 分の stale path を経て ECMP が回復していました。
- `routerctl doctor dslite` が DSLiteTunnel `phase=Up` を健全とみなし、
  EgressRoutePolicy の選択を `status.selectedSource = "DSLiteTunnel/<name>"`
  経由でも認識するようになりました（旧来の `selectedCandidate` 名一致も
  併用）。これまで `dslite-pd-balanced` のような集約候補名を使う本番
  構成では、`gatewayHealth` が `ok` と判定している DSLiteTunnel が
  毎回 WARN 表示になっていました。

## v20260526.1607

### 追加

- `routerctl ledger prune-events` の非 dry-run 実行時に監査イベント
  `routerd.ledger.events.pruned` を発行するようになりました（属性として
  `cutoff` / `deletedRows` / 実行 `uid`/`gid` を含む）。events テーブル
  から prune 自体の実行履歴を確認できます。

### 変更

- `/api/v1/summary` の `gatewayHealth` が `EgressRoutePolicy` /
  `NAT44Rule` / `HealthCheck` も集約するようになりました。Web Console の
  Overview バナーに選択中 egress path と preferred との一致状態が表示され、
  fallback 候補が使用されているときは目立つ警告になります。

### セキュリティ

- Web Console の `/api/v1/config` および generation の config/diff
  エンドポイントは、シリアライズ前に secrets を redact するようになりました
  （WireGuard `privateKey` / `preSharedKey`、Tailscale `authKey`、
  BGP/PPPoE/IPsec `password`、WebConsole `initialPassword`、bearer/token
  系フィールド等）。キーは残しマーカ値に置換するため、UI の構造は壊しません。
  特権チャネル（control socket、`routerctl describe`）は変更ありません。
  管理 LAN に到達可能な運用者が Web Console 経由（read-only でも）で生の
  secrets を見られる経路を塞ぎます。

## v20260526.1225

### 追加

- `routerctl doctor [area]`: wan / dns / dslite / dhcpv6-pd / nat /
  firewall / rollback / disk / mgmt の一連の読み取り専用チェックを実行し、
  PASS/WARN/FAIL を是正ヒント付きで報告します。FAIL があれば非0で終了するため
  スクリプトから利用できます。
- SQLite state DB の保守コマンド: `routerctl ledger integrity-check` /
  `vacuum` / `backup <dest>` / `prune-events --older-than <dur>`。prune は
  events 限定で、rollback と監査履歴を支える generations / objects /
  artifacts は保持されます。
- `ManagementAccess` リソース: 管理用インターフェースと管理元 CIDR を宣言
  します。宣言時、非 dry-run の `apply` は、宣言された管理 IF が欠落・
  firewall が SSH を遮断する設定（管理 IF が mgmt/trust の FirewallZone に
  属していない）・有効な WebConsole が全アドレス bind を検出すると失敗します
  （`--allow-mgmt-lockout` で上書き可）。
- `api/v1/summary` に `gatewayHealth` を追加。`DNSResolver` /
  `DSLiteTunnel` / `DHCPv6PrefixDelegation` を集約し全体判定とコンポーネント別
  状態を返します。Web Console の Overview 最上部に Gateway Health バナーを
  表示し、degraded / down のときは理由と waiting を強調します。
- `examples/home-router-mgmt-protected.yaml`: 家庭ルーター置き換えの
  「安全最小の出発点」 canonical example。3-role firewall（untrust / trust /
  mgmt）、DS-Lite 優先 + PPPoE フォールバック、`ManagementAccess`、mgmt
  アドレス固定 bind の `WebConsole` を含みます。

### 変更

- Go の module path を `github.com/imksoo/routerd` に変更しました
  （旧: `routerd`）。リリースアーカイブから導入するユーザーには影響しませんが、
  `go install github.com/imksoo/routerd/...` や外部 Go プロジェクトからの
  import が可能になります。

## v20260525.1631

### 追加

- `routerctl restart-dns-resolver [name]`: DNS リゾルバのサービスユニットを明示的に
  再起動します（デーモンの健全性が損なわれたときの復旧用）。

### 変更

- `DNSResolver` を `routerd serve` の子プロセスではなく、独立した長寿命サービスユニット
  （`routerd-dns-resolver@<name>.service`）として動かすようにしました。routerd の再起動・
  アップグレードで DNS が中断しなくなり、config 変更（DHCPv6-PD 収束を含む）はデーモンの
  reload エンドポイント経由でプロセスを再起動せずに反映され、`install.sh` は upgrade 時に
  リゾルバを自動再起動しなくなりました。config ファイルが未生成のときはデーモンが空状態で
  起動し、ランタイムに構成されます。

## v20260525.0112

### 変更

- `DNSResolver` は、すべての依存関係を待つのではなく、起動時に部分的にデーモンを
  立ち上げるようになりました。すでに解決できている待ち受けアドレスと source で応答し、
  残りが待機中の間は `waiting` list 付きの `phase: Degraded` を報告し、依存関係が
  解決すると `Applied` へ収束します。これにより、DHCPv6 prefix delegation を待つ間に
  DNS が拒否される起動時の空白時間がなくなります。

## v20260525.0006

### 追加

- `routerd rollback --list` と `routerd rollback --to <generation>` を追加しました。
  保存済みの設定世代を一覧し、通常の apply 経路で再適用します（既存の SQLite 世代を
  利用し、別途スナップショット保管は持ちません）。
- `routerctl set-log-level <debug|info|warning|error|default>` を追加しました。
  再起動せずに control socket 経由でログ詳細度を実行時変更できます（OTLP ログ sink にも反映）。
- `routerctl describe` がリソースの Phase / Reason / Message と、非正常 phase での
  対処ヒント（remediation）を表示するようになりました。
- 生成される設定 JSON Schema に、非自明なフィールドの説明（godoc 由来）が入るように
  なり、エディタ補完や検証メッセージが改善します。
- インストーラが `routerd` システムグループを作成します。グループに追加した運用者は
  sudo なしで `routerctl status` を実行できます。

### 変更

- 読み取り専用ステータス socket の所有を `root:routerd`・モード `0o660` にしました。
  socket 生成時に routerd 自身がグループ所有を設定するため、unit の `Group=` 設定に
  依存しません。読み書き用 control socket は root 専用のままです。

### 削除

- `disabled:` フィールドを削除しました。`PPPoESession` / `HealthCheck` /
  `DSLiteTunnel` / `EgressRoutePolicy` の候補では `enabled: false` を使ってください。
  **破壊的変更:** `disabled:` を使っていた設定は書き直しが必要です。
- 無効化済みで no-op だった `--controller-chain` / `--controller-chain-*` フラグと、
  `--observe-interval` の定期 observe を削除しました（イベント駆動のコントローラチェーンが
  常時有効。`--apply-interval` は変更なし）。これらのフラグを渡している host unit は
  アップグレード前に更新が必要です。

### 修正

- `install.sh` がアップグレード時に `routerd-bgp` を自動再起動しなくなり、routerd
  バイナリ更新をまたいで BGP セッションと ECMP が維持されます。
- 起動時に動的参照（`*From` / `upstreamFrom`）が未解決の場合、ハードエラーや値の
  暗黙ドロップではなく `Pending` として報告し、依存先の status が現れた時点で再 reconcile
  するようにしました（DNS リゾルバ / DS-Lite / DHCP サーバ / VRRP 静的アドレス）。
- 終了時の `sql: database is closed` ログノイズを解消しました。state store が close 後の
  アクセスを安全に拒否します。

### セキュリティ

- 読み取り専用ステータス socket が全ユーザーからアクセス可能でなくなり、root と
  `routerd` グループのメンバーに限定されました。

## v20260523.2327

### 追加

- `qemu-guest-agent` を `install.sh` の Alpine 依存先に追加しました。
  Alpine 環境では既定で仮想化用エージェントをインストールします。
- 仮想環境でのライブ ISO 起動時に、`scripts/build-live-iso.sh` が
  `qemu-guest-agent` を自動起動する処理を追加しました。

### 変更

- 将来のインタラクティブ運用に備え、サポート対象 OS 全体で
  SSH サーバー依存 (`openssh` / `openssh-server`) を既定に追加しました。

## v20260523.1542

### 追加

- built-in DPI classifier を、nDPI なしでも実用できる通信分類器として拡張しました。
  payload 由来の application hint を記録し、payload evidence と port fallback を区別します。
  また、unknown のまま accept された flow は最初の数 packet だけ再分類する budget で追跡し、
  よく使われる local protocol の軽量検出を追加しました。nDPI agent がある場合は、
  これまでどおり結果の enrichment に使えます。

### 修正

- NixOS render で、routerd 管理の dnsmasq と DHCPv4 client unit を修正しました。
  raw packet が必要な経路のため `RestrictAddressFamilies` に `AF_PACKET` を許可し、
  dnsmasq は `${pkgs.dnsmasq}` の store path で render します。あわせて生成される
  `accept_ra_defrtr = 0` sysctl を NixOS golden output に反映しました。
- Alpine/OpenRC live ISO で、managed GoBGP を使う config の場合に
  `routerd serve` より前に `routerd-bgp` を OpenRC 下で起動するよう修正しました。
  issue #28 の修正です。

## v20260522.1334

:::tip ★安定マイルストーン
この版は最初の**推奨安定版マイルストーン**です。本番ルーターでの稼働実績があります。詳細と推奨の理由は [安定版マイルストーン](./stable.md) を参照してください。
:::

### 追加

- routed eBGP peering 用に `BGPPeer.spec.ebgpMultihop` を追加しました。
  `0` と `1` は直結 peer の既定動作のままです。`2` から `255` は GoBGP の
  `EbgpMultihop.MultihopTtl` として設定され、`routerd-bgp` の適用済み状態にも
  保存されるため、デーモン再起動後も同じ peer TTL を復元します。

## v20260522.1045

### 修正

- GoBGP backend で、旧 FRR の `set ip next-hop peer-address` 相当の import
  動作を復元しました。`BGPRouter.spec.importPolicy.nextHopRewrite` は既定で
  `peer-address` になり、受理した eBGP route は downstream speaker が第三者
  next-hop を広告していても、学習元 peer address 経由で kernel FIB に投入され、
  ECMP を維持します。router status には rewrite mode と投入された next-hop を表示します。

## v20260522.0824

### 修正

- 生成される `routerd.service` から `ProtectSystem` と `ReadWritePaths` を削除しました。
  `routerd` は systemd のファイルシステム保護なしで動く前提であり、明示的な
  write-path のリストは、optional なディレクトリが存在しない clean host で systemd namespace
  エラーによるサービス起動失敗を招くことがありました。

## v20260522.0742

### 修正

- NixOS module の `services.routerd.extraFlags` escape hatch を削除し、
  アップグレード後も削除済みの `--controller-chain*` flag を渡し続けられないようにしました。
  生成される `routerd.service` は、簡素化したサービスライフサイクルに合う固定の
  `routerd serve` 起動形を使います。

## v20260522.0658

### 修正

- 旧 routerd release からの in-place アップグレードで、削除済みの
  `--controller-chain*` flag や `SystemdUnit` resource が残っている場合でも
  起動不能にならないようにしました。`serve` / `apply` は legacy な
  controller-chain flag を警告付きで無視し、installer はサービス再起動の前に
  legacy な routerd service unit を置き換え、保存済み config から user-facing な
  `SystemdUnit` resource を削除します。

## v20260522.0006

### 変更

- BGP controller backend を GoBGP ベースの長寿命 `routerd-bgp` デーモンに
  置き換えました。`BGPRouter` と `BGPPeer` は local gRPC Unix socket 経由で
  型付き GoBGP API object へ直接マップされ、`apply --once` は FRR artifact を
  render せず、`routerd` 再起動でも BGP process を再起動せず、established な
  session を落としません。peer/path status は `vtysh` のテキスト parse ではなく
  `ListPeer` / `ListPath` から取得します。
  import policy に一致する学習済み IPv4 best path は kernel FIB に投入し、equal best path は
  ECMP next-hop として扱います。未対応の BFD intent は黙って無視せず Pending にします。
  MVP での IPv6 FIB route や non-Linux platform など、kernel FIB に投入できない
  学習 route は黙って落とさず、prefix ごとの install reason と router の Degraded な
  status として表示します。`routerd-bgp` デーモンは最後に適用した global / peer /
  advertisement intent を `/var/lib/routerd/bgp/applied.json` に atomic rename で保存し、
  デーモン再起動時に復元します。これにより `routerd` reconnect 後も config drift を検出し、
  stale な live peer を黙って採用しません。
- コントローラー runtime status で、累積した reconcile failure と現在の異常を分離しました。
  `reconcileErrorCount` は lifetime counter のまま残し、`currentError`、
  `consecutiveErrorCount`、`lastErrorTime`、`lastErrorClearedAt` で最新 reconcile が
  失敗中なのか、過去の一時 error がすでに回復済みなのかを判定できます。
- `EgressRoutePolicy` の no-op reconcile 回帰 test を追加し、`mode: priority` の
  dry-run status を含む default-route selection が不変の場合に
  `routerd.lan.route.changed` や resource status event を churn しないことを保証しました。
- 起動時に supervised な DHCPv6 client socket の作成を待っている間、
  `DHCPv6Information` は想定内の socket race を bootstrap の WARN として繰り返し
  記録せず、Pending state として表示するようになりました。
- 各 `IPv6RouterAdvertisement` から `RogueRADetector` を自動導出するようにしました。
  新しい `routerd-ra-observer` デーモンは対象 interface の ICMPv6 Router Advertisement を
  passive に観測し、flat L2 segment で active な RA Guard を試みず、homert02 以外の
  router を status と `routerd.ipv6.ra.rogue_detected` event で通知します。
- selection-only の `EgressRoutePolicy` status/event 用語を、hard-code された
  `dryRun: true` から `role: advisory` / `advisory: true` に改名しました。
  CLI の `--dry-run` は、host 変更を適用しない preview の意味のままです。
- stale な legacy client daemon unit の cleanup は、active な unit を停止せず
  Pending status と警告 event で保留するようになりました。inactive な stale
  unit は引き続き status/event の証跡付きで削除します。

## v20260521.1953

### 修正

- routerd 再起動時に firewall と TCP MSS clamp の render 結果が変わって
  いない場合は既存の nftables dataplane rule を維持し、`routerd_filter` と
  `routerd_mss` への不要な `flush table` での再読み込みを避けるようにしました。
- 無変更 reconcile の冪等性を強化しました。stale な client daemon unit の cleanup は
  status/event に記録し、static/DHCP IPv4 route は live kernel route が一致する
  場合に skip し、動的な nftables address set は set 全体を flush せず element 差分で
  更新します。NTP/BGP のサービス操作理由も status/event で確認できるようにしました。

## v20260521.1155

### 修正

- `EgressRoutePolicy` の `mode: priority` が `selection: highest-weight-ready`、
  candidate の `weight`、`disabled: true` を正しく反映するようにしました。
  選択された経路の status も一貫して出し、candidate 削除後に残る
  ledger-owned な policy-route rule と route table を削除します。

## v20260521.0918

### 修正

- `EgressRoutePolicy` の selection-only reconciliation が `mode: priority`、
  `mode: mark`、`mode: hash` の policy-route status を上書きしないようにしました。
  これらの mode は status owner を 1 つに統一し、適用済みの policy selection が
  変わっていない場合に dry-run の `routerd.lan.route.changed` event が
  繰り返し発生する問題を防ぎます。

## v20260521.0843

### 修正

- Linux kernel が既存の delegated host address を設定上の `/64` ではなく
  `/128` など別の prefix length で表示する場合に、`IPv6DelegatedAddress` の
  apply event が繰り返し発生する問題を修正しました。
- `lastTransitionAt` timestamp だけが更新された status refresh では、
  `routerd.resource.status.changed` event を出さないようにしました。

## v20260521.0827

### 追加

- `NTPServer.spec.allowCIDRFrom` を追加しました。LAN NTP client の許可範囲を
  `IPv6DelegatedAddress/<name>.address` や
  `DHCPv6PrefixDelegation/<name>.currentPrefix` などの動的な status field から
  導出できます。

## v20260521.0802

### 追加

- `install.sh --with-ndpi-archive PATH` を追加しました。通常の static
  routerd archive と native `routerd-ndpi-agent-libndpi` archive を、1 つの
  rollback transaction で適用できます。installer は `--with-ndpi` を満たす前に、
  feature archive の target、path safety、存在する場合の checksum、
  `libndpiLoaded: true` self-test を検証します。

### 修正

- 現在の schema から削除済みの resource kind について、serve 起動時に stale な
  object status の行を cleanup するようにしました。routerd は削除前に timestamp
  付きの SQLite バックアップを作成し、audit event を記録します。バックアップを作成できない
  場合は cleanup を skip します。

## v20260521.0731

### 修正

- standard release archive に static fallback 版 `routerd-ndpi-agent` しか含まれない
  場合でも、既に install 済みの native `routerd-ndpi-agent` が `selftest` で
  `libndpiLoaded: true` を返すなら保持するようにしました。また
  `install.sh --with-ndpi` は、最終的に install された agent が
  `libndpiLoaded: true` を返さない場合に失敗します。
- `spec.includeApplicationLayer: true` が設定されているのに nDPI agent の native
  `libndpi` backend が load されていない場合、`TrafficFlowLog` を
  `TrafficFlowApplicationLayerUnavailable` reason 付きの `Pending` として表示する
  ようにしました。
- 派生された `routerd_mss` nftables table を router-owned artifact として登録し、
  routerd が再生成している table が orphan として誤表示されないようにしました。
- `routerctl show derived-resources` は stale な派生 state を既定で隠し、
  audit/debug 用に `--include-stale` を追加しました。また削除・rename 済み kind の
  state DB 行を手動 SQLite 編集なしで消せるように `routerctl delete --force` を追加しました。
- TCP MSS clamp を source path aware かつ downward-only にしました。
  `Interface.spec.mtu` で `tailscale0` のような低 MTU source interface を表せます。
  routerd は source/destination path ごとに `min(source MTU, destination path MTU)`
  を使い、nftables は advertised MSS が派生値より大きい SYN packet だけを書き換えます。

## v20260521.0039

### 修正

- 削除された `PPPoESession` について、ownership ledger に残る生成済み artifact を
  garbage collect するようにしました。対象は PPP peer file、runtime socket、
  runtime ディレクトリ、state ディレクトリ、停止・無効化済みの systemd unit です。
- Live ISO が CD-ROM として接続された read-only な ISO9660/UDF config media からも
  router config を import できるようにしました。Proxmox の `media=cdrom` で
  `ROUTERD_CONFIG` label を付けた config ISO を対象に含めます。
- 永続化された OpenRC の `routerd` default runlevel entry により、Live ISO の
  USB config restore より前に `routerd serve` が起動してしまう問題を防ぎます。
  live autostart helper はこの runlevel entry を削除し、config restore と
  `apply --once` の後に既存の `serve` process を再起動するため、復元された
  BGP config が FRR に再読み込みされます。

## v20260520.2307

### 修正

- FRR/keepalived 統合を含む場合だけ、生成される `routerd.service` に
  `CAP_DAC_OVERRIDE` を追加しました。Ubuntu の FRR では `/run/frr` が
  `frr:frr` かつ mode `0755` になることがあり、`frrvty` group だけでは
  `frr-reload.py` が `/var/run/frr/reload-*.txt` を作成できないためです。
- `frr-reload.py` の permission failure を generic な `FRRReloadFailed` ではなく
  `FRRReloadPermissionDenied` として分類するようにしました。
- `WireGuardInterface` / `WireGuardPeer` が config から消えた場合に、routerd 管理下の
  古い WireGuard interface と peer status を削除するようにしました。
  resource 削除後に state DB を手動編集する必要をなくします。

### 変更

- Kubernetes BGP example の import prefix を MetalLB LoadBalancer pool の
  `10.250.0.0/24` に更新し、home-router の sample は 2 台の k8s route node と
  個別に peer する構成へ調整しました。

## v20260520.2227

### 修正

- OpenRC `routerd` service script 追加後の Live ISO build を修正しました。
  script を書き込む前に overlay の `/etc/init.d` ディレクトリを作成します。

## v20260520.2222

### 追加

- BGP prefix status と `routerctl show bgp` に route selection diagnostics を
  追加しました。FRR が field を出す場合、select-deferred、no-best-path、
  not-installed-to-zebra の状態を確認できます。
- Kubernetes/edge router 向けに `BGPRouter.spec.convergenceProfile: fast` を
  追加しました。fast profile は短い BGP timer を派生し、fresh boot 時の stale-path
  selection defer を避けるため graceful restart を既定で無効にします。
- Live ISO が `ROUTERD_CONFIG` label の USB partition から config を読み込める
  ようにしました。boot helper は `/routerd/hosts/<hostname>.yaml`、
  `/routerd/hosts/<mac>.yaml`、`/routerd/router.yaml` を選び、source と SHA256 を
  `/run/routerd/` へ記録します。

## v20260520.2107

### 追加

- BGP / FRR control-plane design note を追加し、readiness、reload、
  verification、failure status、Live ISO 受入 scenario を明文化しました。

### 修正

- BGP controller が各 reconcile で FRR の service state を確認するようにしました。
  Alpine/OpenRC または systemd host で FRR が stopped/failed の場合、
  `vtysh` probe と `frr-reload.py` の前にサービスを起動・再起動します。
- BGPRouter の Healthy 判定を厳格化し、service state、`vtysh` round-trip、
  `tcp/179` listen、render 済みの `router bgp <asn>` stanza がすべて揃った
  場合だけ Healthy と判定します。
- `routerctl status` を resource phase から集約するようにし、Pending/Error の
  BGP resource が controller runtime の成功更新に隠れないようにしました。

## v20260520.2007

### 修正

- BGP controller の FRR readiness 判定から TCP VTY gate を取り除き、
  `vtysh -c "show running-config"` を control-plane probe と running config の
  diff の入力として使うようにしました。これにより TCP VTY を無効にした
  Alpine FRR build でも、初回収束時に `frr-reload.py` へ到達できます。
- FRR control 不可、権限不足、reload 試行、reload 後の反映不完全を status で
  明示するようにしました。
- Alpine Live ISO の autostart が、既に `routerd serve` が動いている場合は
  2 つ目の `routerd serve` を起動しないようにしました。

## v20260520.1904

### 修正

- BGP controller reconcile 中の一時的な FRR reload lock 失敗を retry し、
  初回 boot 時にも手動の `frr-reload.py` なしで `bgpd` config まで到達できる
  ようにしました。
- Alpine Live ISO の DHCP client を初回リース後も常駐させ、live router 用の
  安定した DHCP hostname を派生し、既定では DHCP option 61 を送らないことで
  Windows の DHCP reservation が Ethernet MAC に一致し続けるようにしました。

## v20260520.1737

### 追加

- `mode: vrrp` の `VirtualAddress` に FreeBSD CARP backend を追加しました。
  runtime controller、rc.d rendering、validation、tests、最小構成の
  `examples/freebsd-vrrp.yaml` を含みます。
- ingress/local router service の listen-port 衝突 validation と、
  Linux nftables 向けの `IngressService` `sourceHash` / `random` backend
  distribution を追加しました。
- FRR BGP の connected/static redistribution、BGP community の send/accept/set
  policy、観測 community の status 解析、
  `examples/lan-advertise-with-community.yaml` を追加しました。
- VRF-backed FRR BGP instance による multi-instance `BGPRouter` support、
  listen-address collision validation、router ごとの observed status、
  `examples/multi-instance-bgp.yaml` を追加しました。
- FRR 管理の BGP peer 向け BFD support、FRR `bfdd` daemon rendering、
  BGP watcher tuning field、BFD status observation、
  `examples/bgp-bfd.yaml` を追加しました。
- transit routing 用の BGP export policy allow-list と、`BGPRouter` がある場合の
  FRR `bgpd` daemon 自動 enable を追加しました。
- Kubernetes の Pod / Service CIDR static route 向け `ClusterNetworkRoute`
  helper と、BGP peer password / VRRP-CARP authentication 用の
  `passwordFrom` / `authenticationFrom` secret source を追加しました。
- 一時的な `IngressService` backend maintenance 用の `routerctl drain` /
  `undrain` と、VRRP production tuning documentation および
  `examples/vrrp-tuning-presets.yaml` を追加しました。
- BGP / VRRP / IngressService の Web 管理画面運用ページに SSE 更新、
  filter 付き event log、軽量なローカル SVG metric trend を追加しました。
- stateful なファイアウォール rule expression として ICMP / ICMPv6 type、送信元 /
  宛先の複数 port match、nftables rate limit、送信元ごとのコネクション
  上限を追加しました。
- IPv4/IPv6 unicast の dual-stack BGP rendering / observation、
  `VirtualAddress` による VRRPv3/CARP VIP support、AAAA record 自動派生、
  dual-stack BGP / Kubernetes API VIP example を追加しました。
- OTLP environment rendering と stdout / syslog / Loki への内蔵 routerd event
  forwarding 用の `ObservabilityPipeline`、および apply/controller mutation を
  file lease で gate する `RouterdCluster` を追加しました。
- Alpine/OpenRC 向け VRRP render support を追加しました。`routerd apply --once`
  が keepalived config artifact を書き、OpenRC の `keepalived` サービス管理と
  live VRRP role observation は controller runtime が担当します。Alpine 向けの
  Kubernetes VIP example も追加しました。
- Alpine Live ISO の経路を改善し、VRRP controller の既定を live にし、
  `routerctl show vrrp` は live address から role を再観測します。version
  output には commit を埋め込めるようにし、FRR reload tooling dependency と、
  非 blocking な setup wizard の動作も追加しました。
- live な VRRP reconcile で keepalived の no-op な reload/restart を避け、
  最後に keepalived を reload/restart した時刻と理由を controller status に
  出すようにしました。
- VRRP の daemon lifecycle は controller runtime に限定しました。
  `routerd apply --once` は keepalived artifact を render し、reload/restart
  せず controller handoff status を記録します。
- IngressService の live な nftables apply を独立した NAT44 dry-run mode から分離し、
  hostname の DNSZone coverage は警告に緩和しました。外部 DNS 管理の名前は
  `externalDNS` で自動公開と警告を抑止できます。
- IngressService の同一 interface hairpin SNAT と、forwarding 用の runtime
  `ip_forward` sysctl を自動適用し、`routerctl show ingress --verbose` で
  forwarding、nftables、conntrack の dataplane 状態を確認できるようにしました。
- listen interface prefix が YAML に無い Live ISO 風の構成でも、
  private `/24` 内の IngressService listen/backend address は
  `hairpin.mode: auto` で hairpin が必要と判定するようにし、verbose な ingress 出力は
  期待される nftables SNAT が無い場合に警告を出すようにしました。
- systemd、OpenRC、rc.d、NixOS の service artifact 名と lifecycle command を扱う
  `pkg/servicemgr` abstraction を追加し、service artifact の intent generation を
  そこへ寄せて、resource ごとの OS switch drift を減らしました。
- すべての checked-in な example config について Linux、Alpine/OpenRC、
  FreeBSD/rc.d、NixOS の render スナップショットを固定する golden test と、
  netns 側の compatibility wrapper を追加しました。`pkg/servicemgr` には lifecycle
  hook を追加し、FRR の config-check + live reload、keepalived の reload/restart
  分離、signal-based な daemon reload が generic restart に潰れないようにしました。
- bespoke lifecycle command の golden test と `make check-bespoke-lifecycle`
  gate を追加しました。FRR live reload、keepalived no-op/reload、dnsmasq
  SIGHUP、DHCP daemon IPC、BFD daemon enablement、IngressService の nftables-only
  backend rotation、VRRP track artifact、DS-Lite dataplane hook、DHCP event daemon
  ordering、FRR graceful-restart observation を固定します。
- nftables / pf の render・diff・reload 経路向けに、挙動を変えない
  firewall backend abstraction を追加しました。nftables の `ct state`、`jhash`、
  `numgen`、hairpin conntrack expression と、pf の `rdr`、`nat-anchor`、
  hairpin NAT syntax を regression contract で固定します。
- netplan、systemd-networkd drop-in、NixOS module、FreeBSD rc.conf fragment
  向けに、挙動を変えない network config backend abstraction を追加しました。
  IPv4/IPv6 の address と route は共通の declaration として扱います。
- PPPoE、VRRP/CARP、FRR、dnsmasq、DHCPv6 PD、DNS resolver、Tailscale の
  service-backed artifact intent を ServiceManager declaration table に整理し、
  systemd/OpenRC/rc.d/NixOS の ownership が出力変更なしで揃うようにしました。
- firewall hole derivation と OS 別 interface/network artifact の render golden
  coverage を拡張し、Linux の netplan/systemd-networkd output と Alpine の
  nftables snapshot も固定しました。
- abstraction layer regression coverage を強化し、cross-OS semantic test、
  invalid spec check、firewall backend error propagation の status/event、
  edge-case declaration、race-tested reload、80% coverage gate、4 OS の
  bespoke lifecycle command matrix を追加しました。

### 修正

- BGP の `apply --once` を daemon lifecycle から分離しました。`routerd apply
  --once` は FRR config と daemon artifact の render のみ行い、`bgpd` の
  enable/restart、`vtysh` validation、live reload、peer observation は
  `routerd serve` 側が担当します。
- FRR JSON が数値フィールドを文字列として返す場合の BGP observation を修正し、
  `routerctl show bgp` は古い stored status を live な `vtysh` output で更新して
  表示するようにしました。
- FRR の readiness と reload status は BGP controller 側に残し、controller runtime
  の serve が pending/error state を報告できるようにしました。`apply --once` は
  `bgpd` や `frr-reload.py` を待ちません。
- Web 管理画面に Routes ビューと `/api/v1/routes` endpoint を追加しました。kernel、
  BGP、static、DHCP、policy route の情報と BGP peer state を同じ画面で確認できます。
- `pkg/api/provides.go` で各 kind の status output (provides) を宣言型に定義し、
  config validator が `addressFrom` / `gatewayFrom` / `dnsServerFrom` /
  `sourceAddressFrom` / `dependsOn` の Kind/name 存在 + 参照先 field の provides
  整合を loader 時点で検査するようにしました。
- `routerctl show derived-resources` を追加し、router intent から自動派生される
  package / kernel module / sysctl / systemd-networkd/resolved adoption /
  tunnel `rp_filter` を確認できるようにしました。
- `spec.when` に `any:` / `all:` predicate を追加しました。`StatePolicy` を分離せずに
  resource を条件付きで活性化でき、入れ子も可能です。
- 新 kind: `DHCPv4Client`, `PPPoESession`, `VirtualAddress`
  (`spec.family: ipv4|ipv6`), `EgressRoutePolicy` (`mode: priority|mark|hash`
  と candidate `targets[]`), `DNSForwarder`, `DNSUpstream`, standalone
  `BFD`, `FirewallEventLog`, standalone `LogRetention` を追加。
- 型付き `LogSink` (`type: syslog|otlp|webhook|file|journald`) と
  `FirewallEventLog` (`events: deny|allow|rateLimit|connLimit` + zones/rules
  filter + sampleRate + sinks + retention ref) を追加。
- `make check-examples-line-limits` で `examples/*.yaml` を 200 行以下、各
  resource を 50 行以下に強制する CI gate を追加。examples/home-router.yaml
  を 1800 行から 194 行へ縮減しました。
- HealthCheck / VirtualAddress(mode:vrrp) / WAN tunnel から network-utils /
  vrrp(keepalived) / rp_filter sysctl を自動派生するようになりました。

### 変更

- `DNSResolver` を分割しました。`DNSResolver` 本体は listen + cache + queryLog のみです。
  条件付きフォワーダーと upstream は `DNSForwarder` / `DNSUpstream` で別の resource
  として宣言し、resolver を参照します。TCP upstream と DoT `tlsName` も新たに
  サポートしました。
- BGP の BFD を分離しました。`BGPPeer.spec.bfd` は `BFD/<name>` 参照のみ受け付け、inline な設定
  は loader で reject し、移行ガイドを返します。
- `TrafficFlowLog.spec.includeNDPI` を `spec.includeApplicationLayer` に rename、
  `retention` は独立した `LogRetention` resource に分離しました。
- `ClientPolicy.classification` を `mode` + 構造化 `match` (`macs` /
  `ouiPrefixes` / `hostnamePatterns` / `dhcpFingerprints`) に整理しました。
- DHCPv4 reservation を動的プールの範囲外にも置けるようにしました (dnsmasq の
  static-only な割当と整合します)。
- loader は不明な kind や削除済みの kind/field を黙って無視せず、移行ガイドつきで
  エラーを返します。

### 削除

- `SystemdUnit` の user-facing な宣言を削除しました。systemd / OpenRC / rc.d / NixOS の
  service unit は router intent から自動派生します。
- `KernelModule`, `NetworkAdoption`, `Link`, `NixOSHost`,
  `IPv4ReversePathFilter`, `PathMTUPolicy`, `StatePolicy`,
  `IPv4DefaultRoutePolicy`, `IPv4PolicyRoute`, `IPv4PolicyRouteSet`,
  `IPv4SourceNAT`, `DHCPv4Lease`, `PPPoEInterface`, `VirtualIPv4Address`,
  `VirtualIPv6Address`, `DHCPv4Scope`, `DHCPv6Scope`, `FirewallLog` を
  user-facing な kind から削除しました。それぞれ loader で reject され、移行先
  (自動派生 / narrow override / 吸収先の kind) を案内します。
- `Package` / `Sysctl` / `SysctlProfile` は narrow escape hatch としてのみ残し、
  通常の router intent には不要です。
- 低レベル mechanics field を削除: `HealthCheck` `daemon` / `socketSource` /
  `fwmark` / `sourceInterface` / `sourceAddress*` / `via`; BGP `keepalive` /
  `holdTime` / `connectRetry`; VRRP `advertInterval` / `preemptDelay`;
  WireGuard `fwmark` / `table`; Tailscale `operator` / `binaryPath`;
  DHCPv6PrefixDelegation `iaid` / `duidType`。
- `DNSResolver.spec.sources` を削除しました。代わりに `DNSForwarder` / `DNSUpstream`
  resource で宣言してください。
- `--controller-chain` public flag を `routerd serve` / `routerd apply` から
  削除しました。controller chain は本番 runtime の唯一の経路です。

## v20260519.0743

### 変更

- 公開 documentation と example configuration の名前を整理し、内部 lab の
  hostname、domain、management network address が website や再利用用の example
  ではなく internal notes に残るようにしました。
- internal design / soak note を公開 Docusaurus docs tree から外し、native nDPI
  と RA/DHCPv6-PD coverage の lab validation policy を `internal/notes/` へ
  記録しました。

## v20260519.0713

### 修正

- `routerctl show bgp`、`routerctl show vrrp`、`routerctl show ingress` が
  ownership ledger を開かないようにし、明示した status store を使う場合は
  default の ledger path が書き込めない環境でも動くようにしました。

## v20260519.0708

### 追加

- Kubernetes edge 用に、FRR backend の `BGPRouter` / `BGPPeer`、
  keepalived backend の `VirtualAddress`、および `IngressService`
  backend health/failover controller を追加しました。
- `routerctl show bgp`、`routerctl show vrrp`、`routerctl show ingress` の
  table ビュー、VIP/ingress の `hostname` field からの DNS record 自動派生、
  BGP/VRRP/Ingress の transition と backend health 用の OTel metrics を追加しました。
- Web 管理画面に BGP、VRRP、IngressService の専用ビューと JSON endpoint を追加しました。

### 変更

- FRR BGP 設定は `vtysh -C -f` で検証し、`frr-reload.py --reload` で
  差分適用します。VRRP は unicast peer と `nopreempt` を既定にし、
  track hysteresis と `preemptDelay` を扱います。BGP、VRRP、IngressService
  listen port の firewall hole も自動派生します。
- BGP reconcile では dry-run の書き込みが後続の live apply を隠さないようにし、
  初回の live 観測時は FRR running-config を比較してから reload するため、
  既に一致している session を no-op な reload で reset しません。

## v20260518.1810

### 追加

- native な nDPI classification を有効化する host 向けに、別 archive
  `routerd-ndpi-agent-libndpi-linux-amd64` を追加しました。通常の Linux
  release archive は完全な静的 binary のまま維持し、optional な nDPI agent の
  override は `CGO_ENABLED=1 -tags libndpi` で build し、libndpi self-test で
  検証します。

## v20260518.1431

### 追加

- controller reconcile の runtime status を control API、log、OpenTelemetry
  metrics/traces、Web 管理画面の controller ビューに追加しました。controller
  status は interval、trigger、実行回数、error 回数、last/average/max duration、
  最新の error を返します。

## v20260518.1301

### 変更

- 現在の controller runtime 設定の path では使われなくなった dead な compatibility
  helper と、旧 raw systemd unit renderer を削除しました。

## v20260517.2339

### 追加

- 番号付きの構成図、図と YAML の対応 comment、安全上の注意、検証済み sample
  YAML を含む「設定事例集」セクションを追加しました。基本的な IPv4 NAT、
  LAN DHCP/DNS、DS-Lite、PPPoE、port forwarding、guest 分離、multi-WAN
  failover、local DNS redirect、Tailscale、WireGuard、telemetry export の
  パターンを用意しました。
- IPv4 route policy resource から参照される health check は、参照元の route
  candidate または target から socket mark を導出するようにしました。単体 probe
  用の `spec.fwmark` は引き続き利用でき、明示した mark と導出した mark が衝突する設定は
  validation で拒否します。

### 変更

- Linux のアップグレードでは、routerd helper の systemd service が削除済みの旧 binary を
  実行している場合、または unit file が helper process の起動後に再生成された場合に
  限って helper を更新するようにしました。installer はその判定の前に
  `routerd.service` と routerd 管理の unit file の反映が落ち着くのを待ちます。
- release installer は NixOS で host service manager の変更を行わないようにしました。
  これにより、`/etc/systemd/system` が読み取り専用で service unit を宣言型に管理する
  host でも、archive からの binary 更新が失敗しません。
- conntrack の procfs file が host に存在しない場合、conntrack observation は interval
  ごとに警告を出す代わりに `Unavailable` status を記録するようにしました。
- FreeBSD の `--skip-service-manager` apply は、生成 helper、管理対象 dnsmasq、
  pf/pflog service activation の rc.d/service 操作を抑止するようにしました。
  その一方で、rc.conf による network state と直接の `pfctl` rule loading は継続します。
  recovery や bootstrapping の経路が base の rc boot sequence と競合するのを避けます。
- FreeBSD のアップグレードでは、config 管理の `routerd` rc.d script を generic な bootstrap
  template で置き換えず保持するようにしました。Linux で config 管理の
  `routerd.service` を保持する挙動と揃えています。
- `routerd serve` は SIGTERM/SIGINT を受けたときに control socket と status socket を
  clean に shutdown するようにしました。FreeBSD の `daemon(8)` 配下で rc.d restart する際、
  強制 KILL に進まず停止できます。
- routerd state の SQLite database は、既存の busy timeout と併せて WAL mode を使うように
  しました。status reader と controller が重なったときの一時的な `SQLITE_BUSY` を
  減らします。

## v20260517.1808

### 修正

- Debian/Ubuntu 用の release installer は、完全な `dnsmasq` package ではなく
  `dnsmasq-base` を導入するようにしました。distro 側の `dnsmasq.service` が
  有効化され、routerd 管理の dnsmasq instance と競合するのを避けます。

## v20260517.1800

### 修正

- controller と helper probe からの一回完結の HTTP-over-Unix 呼び出しは
  keep-alive を無効化し、idle な transport を明示的に閉じるようにしました。
  定期的な status polling により、`routerd`、health check helper、DHCP client、
  DNS/DPI helper service に多数の確立済み Unix socket が残り続けるのを防ぎます。

## v20260517.1533

### 修正

- release helper は、schema check の前に管理対象の config schema と control API
  schema を再生成するようにしました。API type の変更が release の終盤で失敗する
  代わりに、release commit に含まれます。
- `routerctl` は daemon 起動直後の一時的な Unix socket 接続失敗を、読み取り専用の
  control API request に限って retry するようにしました。`routerctl status` は
  既定で別の読み取り専用 status socket を使い、apply と delete は引き続き権限付きの
  control socket だけを使い、retry しません。

## v20260517.1510

### 追加

- Web 管理画面の Connections で、`LocalServiceRedirect` により処理されたフロー
  に印を付けるようにしました。live な conntrack tuple と解決済みの set status から
  判定できる場合は、redirect rule と宛先 `IPAddressSet` も表示します。
- Web 管理画面の Firewall で、拒否ログの行の宛先 `IPAddressSet` match を表示する
  ようにしました。明示的な `FirewallRule.destinationSetRefs` による match と、
  現在設定済みの set に含まれている宛先を区別して表示します。

## v20260517.1401

### 修正

- `syscall.Statfs_t` の block counter が signed integer type になる FreeBSD でも
  Web 管理画面の disk usage collection が compile できるようにしました。

## v20260517.1353

### 修正

- release helper は、最初の release セクションが `Unreleased` ではない changelog を
  拒否するようにしました。また、以前の helper 実行で残っていた空の release
  見出しを、管理対象の changelog file から削除しました。

## v20260517.1351

### 変更

- `routerd-dpi-classifier` に明示的な classifier engine facade を追加しました。
  既定は built-in parser で、`auto` / `ndpi-agent` mode では将来の
  `routerd-ndpi-agent` Unix socket service に問い合わせ、失敗時は built-in
  parser へ fallback できます。
- Web 管理画面の Connections で、DPI がフローを識別できない場合でも、
  TCP port 4317 を OTLP、TCP port 4318 を OTLP/HTTP として表示するようにしました。
- Web 管理画面の Overview に host の CPU、memory、root filesystem の使用率と
  classifier 側の DPI processing latency を表示し、router 内部の負荷悪化を
  routing や DPI の状態と並べて確認できるようにしました。
- Web 管理画面の Clients と Connections を相互に移動しやすくしました。
  client 行からその client の観測アドレスで絞り込んだ Connections を開け、
  connection 詳細から対応する local client identity へ戻れます。
- Web 管理画面の Connections で Clients スナップショットを作る際に、直近の traffic-flow
  観測も読み込むようにしました。これにより、IPv6 privacy address でも client へ
  戻せる可能性が上がります。また、source endpoint では既知の identity にまだ
  統合されていないアドレスでも Clients 検索へ移動できます。
- Web 管理画面の検索入力に、文字が入っているときだけ表示されるクリアボタンを
  追加しました。
- release helper は clean な working tree からだけ実行するようにし、空の tag
  見出しを作る代わりに、現在の `Unreleased` の内容を release tag へ昇格するように
  しました。

### 追加

- `IPAddressSet` と `LocalServiceRedirect` を追加しました。`IPAddressSet` は
  直接指定した IPv4/IPv6 address と FQDN の `A`/`AAAA` record を、再利用可能な nftables
  named set へ解決できます。`LocalServiceRedirect` は、その set 宛てに LAN
  client から出る平文 DNS/NTP 通信を router の local service へ redirect できます。
  DoH/DoT や router 自身が発信する health check は対象にしません。
- `FirewallRule`、`NAT44Rule`、`IPv4PolicyRoute`、`IPv4PolicyRouteSet` が
  `destinationSetRefs` と `excludeDestinationSetRefs` で `IPAddressSet` を参照できる
  ようになりました。FQDN-backed な address set を firewall filtering、NAT の適用範囲、
  IPv4 policy routing の条件として再利用できます。
- runtime の `IPAddressSet` refresh controller を追加しました。参照されている
  nftables set は DNS TTL に基づいてその場で更新します。観測した最小 TTL の半分を
  基本にし、60 秒より短くせず、必要に応じて `refreshInterval` で上限を指定できます。
  firewall、NAT、policy table の全体を reload せず、FQDN-backed set を新しい状態に保てます。
- optional command として、初期版の `routerd-ndpi-agent` service boundary を追加しました。
  既定の build は libndpi backend が利用不可であることを報告し、`-tags libndpi`
  build では同じ IPC surface の背後で native library に link します。
- `routerd-ndpi-agent` がフローごとの観測 state を持つようにしました。
  flow TTL、フロー数の上限、先頭 payload packet 数の上限と、observed、classified、
  unknown、skipped、error、pruned packet の status counter を持ちます。
- `routerd-ndpi-agent` 向けの初期 libndpi backend を追加しました。`libndpi`
  build tag で opt-in し、native な flow state を agent 内に閉じ込めたまま、
  firewall logger から届く full packet observation を分類できます。
- libndpi development files が入っている環境で optional native backend を build
  するための `make build-ndpi-agent-libndpi` target を追加しました。
- `routerd-dpi-classifier` が `--engine auto` または `--engine ndpi-agent`
  で設定されている場合に、systemd、OpenRC、FreeBSD rc.d、NixOS で
  `routerd-ndpi-agent` を render するようにしました。
- DPI フローと traffic flow の record が、従来の app label に加えて、
  detected protocol、application protocol、category、confidence、risk、
  metadata などの typed な classifier field を保存するようにしました。
- `routerd-dpi-classifier` の status が、daemon で処理した classify request の
  average latency と maximum latency を報告するようにしました。

### 修正

- Linux の upgrade 時に、差し替え前の削除済み binary を実行し続けている
  routerd helper の systemd service があれば、`install.sh` が再起動するようにしました。
- nDPI agent の結果が application を識別していても TLS SNI、HTTP Host、DNS query
  などの詳細を持っていない場合、`routerd-dpi-classifier` が built-in parser
  の有用な hint を保持するようにしました。
- DPI helper daemon が Unix socket を bind するとき、socket ではない path を
  誤って unlink しないようにしました。また `routerd-ndpi-agent` は native な
  libndpi state を明示的に close します。
- Web 管理画面の traffic-flow 読み取りは、writer が schema 移行を行う前の
  legacy な SQLite file に新しい DPI column がない場合でも成功するようにしました。

## v20260516.2302

### 変更

- Web 管理画面の Connections で、source から destination への経路を固定幅の
  route column に揃え、state、protocol、provider、traffic、timeout などの
  metadata を別の badge 領域に分けました。
- Web 管理画面の connection label は、transport/application identity と
  destination provider を分けて表示するようにしました。
  `google-https` のような旧 provider 固有の label は `TLS` に正規化し、
  Google、AWS、Microsoft、Apple、Cloudflare は別の destination provider badge
  として表示します。
- `https` などの destination service 名は、connection の行に追加情報を与える場合に、
  protocol badge として表示するようにしました。

### 修正

- 展開した connection detail で、destination service と provider の badge が
  detail column 全体に伸びず、内容の幅のまま表示されるようにしました。
- 展開した connection detail で、source と destination の identity text が
  compact な行用の幅で省略されず、利用可能な幅を使って折り返すようにしました。
- Connections の `Showing` metric で、API の取得上限により行が打ち切られた場合に、
  filtered rows、loaded rows、総 conntrack count を区別して表示するようにしました。

## v20260516.2155

### 変更

- Web 管理画面の Connections は、観測された転送バイト数の降順を既定の並び順にしました。
  Connections の sort menu に `Traffic` を追加し、connection card には合計バイト数を、
  詳細表示には conntrack accounting が使える場合の outbound、inbound、total の counter を表示します。
- Web 管理画面の connection 件数の上限を適用するとき、conntrack observer は
  family/protocol group ごとにバイト数の大きい entry を優先します。
  低トラフィックの entry に押し出されて、大きな active flow が隠れにくくなります。

## v20260516.1413

### 修正

- `routerd apply --dry-run` と関連する planning path で、存在しない SQLite ownership
  ledger を空の in-memory ledger として扱うようにしました。
  権限のない CI runner 上で `/var/lib/routerd` を作成しようとして失敗しなくなります。

## v20260516.1405

### 追加

- `firewall.routerd.net/v1alpha1` に `PortForward` と単一 backend の
  `IngressService` を追加しました。WAN 側 IPv4 TCP/UDP ingress DNAT を表せます。
- Linux nftables と FreeBSD pf の rendering で、これらの ingress service を公開できるようにしました。
  任意の hairpin NAT も生成でき、LAN クライアントが WAN アドレス経由で同じ port forward
  の service へ到達できます。
- 新しい ingress NAT resource 向けに、生成 JSON Schema、CLI alias、API documentation、
  resource ownership documentation を追加しました。

## v20260516.0804

### 変更

- Web 管理画面の Connections は、DPI application ごとに表を分けるのではなく、
  IP family と transport protocol の固定 bucket で active な flow をまとめるようにしました。
  TLS、DNS、QUIC などの app label は各 group 内の表示として残ります。

## v20260514.1433

### 追加

- Alpine Linux / OpenRC への apply support を追加しました。`routerd apply` が
  OpenRC service script を render し、Alpine host で routerd 管理 service を
  起動・管理できるようにしました。

## v20260514.0813

### 修正

- Web 管理画面の Clients で、IP address ベースの DNS、traffic、firewall、DPI、
  DHCP fingerprint 情報を、現在の DHCP リースと突き合わせる前に直近 1 時間の
  観測ウィンドウに揃えるようにしました。
- client inventory では sticky な DHCP lease annotation に active hold だけを使うようにし、
  古い lease history が現在の endpoint identity の判定に混ざらないようにしました。

## v20260514.0743

### 修正

- Web 管理画面の Clients で、期限切れの dnsmasq リースを無視するようにしました。
  古い host が無期限に残り続けないようにします。
- DHCP リースの統合では、まず有効期限が新しいリースを優先し、lease file の
  設定順は同条件の場合の tie-breaker としてだけ使います。
- routerd は controller runtime の dnsmasq lease file を Web 管理画面に先頭候補として渡します。
  これにより、管理対象の dnsmasq が実際に使う lease file に沿って表示します。

## v20260514.0654

### 修正

- Web 管理画面の Overview で、初回の軽量なスナップショットを 0 値の metric sample として
  記録しないようにしました。
- Overview の遅延 refresh は、必要な resource、event、conntrack、DNS、最近の
  traffic flow を取得します。一方で、重い firewall、VPN、client inventory の処理は
  引き続き避けます。
- Overview card は、まだ取得していない flow / connection data を 0 と見せず、
  loading state として表示します。

## v20260514.0037

### 修正

- DHCPv4 の LAN domain rendering で、明示的な domain-search option がない場合は `domain` / `domainFrom` から domain-name と domain-search の両方を生成するようにしました。

## v20260514.0025

### 追加

- `domainFrom`、`dnsslFrom`、`domainSearchFrom` を追加しました。
  DHCPv4、IPv6 RA、DHCPv6 で LAN の suffix を広告するとき、
  ローカルドメイン文字列を重複して書かず `DNSZone/<name>.zone` を参照できます。

## v20260513.2358

### 変更

- 長時間動き続けるイベント処理を堅牢化しました。
  `EventRule` と `DerivedEvent` の timer は発火後に map から取り除かれ、
  古い timer callback を無視し、共有状態を controller の lock で保護します。
- `EventRule` の相関状態に上限を設けました。
  高カーディナリティのイベント列でも、メモリ使用量が無制限に増え続けません。
- daemon の `events.jsonl` は追記し続けるのではなく、一定サイズで
  ローテーションするようにしました。
- local control、daemon event、DNS resolver、DoH、classifier の経路に
  request / response のサイズ上限を追加しました。
  local daemon server と Web 管理画面には HTTP header の timeout も追加しています。

### 修正

- `DerivedEvent` の hysteresis 中に、timer callback と reconcile が
  pending な transition state を同時に更新し得る race を修正しました。

## v20260513.2317

### 変更

- `v20260513.2252` の堅牢化に合わせて、本番環境での reconcile に関する
  ドキュメントを更新しました。
  operations、upgrade、state ownership、各言語の changelog で、実機状態の
  drift 確認、管理対象構成物の掃除、nftables named set の更新、
  設定で管理される `routerd.service` のアップグレード時の扱いを説明しています。

## v20260513.2252

### 変更

- 本番環境での reconcile を堅牢化しました。
  controller は処理を省略する前に、状態データベースだけでなく実機状態も確認します。
  対象には systemd unit、dnsmasq、DHCPv4 lease アドレス、
  route-policy の nftables table、NAT44、関連する管理対象構成物が含まれます。
- health check の `fwmark` を、生成する systemd unit、socket 設定、status の観測値、
  OpenTelemetry 属性まで通すようにしました。
  probe が、検査対象の経路と同じ policy-route mark を使えます。
- Linux firewall の rendering で、routerd が管理する named set を再定義前に
  消すようにしました。
  zone interface や client-policy の MAC アドレスを削除したときに nftables 上へ残らず、
  filter table 全体を destroy せずに再読み込みします。
- release installer は、設定で管理されている `routerd.service` を
  archive の template で上書きせず保持します。
  routerd が自分自身の unit を管理している場合、unit file の変更時は
  `systemd-run` で少し遅らせた self-restart を予約します。

### 修正

- YAML から消えた `HealthCheck` に対応する古い `routerd-healthcheck@*.service` を
  削除するようにしました。
- NAT rule が 0 件になったとき、管理対象の NAT44 table または pf anchor を
  空にするようにしました。
- status では DHCPv4 lease アドレスが存在すると見えていても、実際の
  インターフェースから消えている場合は再適用するようにしました。
- 設定内容が空の `WireGuardPeer` は、誤解を招く Pending ではなく
  `NotConfigured` として表示するようにしました。

## v20260513.1931

### 修正

- health check による経路切替の挙動を安定化しました。

## v20260513.1153

### 修正

- controller reconcile の冪等性を安定化しました。

## v20260513.0836

### 追加

- WireGuard mesh controller を追加しました。

## v20260513.0727

### 変更

- home-router の UDP conntrack timeout 設定を引き上げました。

## v20260512.0037

### 追加

- conntrack observer から DPI flow metrics を出力するようにしました。

## v20260512.0032

### 追加

- Web 管理画面 Overview に DPI summary card を追加しました。

## v20260512.0027

### 追加

- Web 管理画面 Clients ページに DPI activity summary を追加しました。

## v20260512.0008

### 追加

- Web 管理画面 Connections ページに DPI classification を表示するようにしました。

## v20260511.2357

### 変更

- forward flow へ DPI enrichment を広げました。

## v20260511.2307

### 修正

- Web 管理画面の横方向のオーバースクロールを抑制しました。

## v20260511.2300

### 修正

- Firewall timeline の横スクロールを修正しました。

## v20260511.2253

### 変更

- Web 管理画面を content-driven なレイアウトセクションへ整理しました。

## v20260511.2217

### 変更

- mobile での Web 管理画面のレイアウトを検証しました。

## v20260511.2211

### 変更

- Web 管理画面の page state を画面遷移後も保持するようにしました。

## v20260511.2154

### 変更

- Clients の inventory ビューを整理しました。

## v20260511.2145

### 追加

- Web 管理画面 SSE reconciliation を追加しました。

## v20260511.2130

### 追加

- client fingerprint inference を追加しました。

## v20260511.2106

### 変更

- 期限切れの conntrack return flow の相関を取るようにしました。

## v20260511.2045

### 変更

- firewall deny event に DPI context を付与するようにしました。

## v20260511.2018

### 変更

- DPI classifier の OS parity を検証しました。

## v20260511.1846

### 修正

- Web 管理画面の時刻 locale を英語に固定しました。

## v20260511.1840

### 追加

- 分離した DPI classifier proof of concept を追加しました。

## v20260511.1820

### 追加

- Connections protocol summary を追加しました。

## v20260511.1709

### 修正

- release artifact の checksum を修正しました。

## v20260511.1428

### 変更

- Web 管理画面の navigation section を改善しました。

## v20260511.1240

### 変更

- controller mode reason の表現を調整しました。

## v20260511.1041

### 追加

- dry-run controller の可視性を高めました。

## v20260511.1017

### 変更

- controller の dry-run mode を明示的に表示するようにしました。

## v20260510.1956

### 変更

- `NetworkAdoption` が resolved DNS を管理できるようにしました。

## v20260510.1811

### 追加

- PVE live ISO のシリアルコンソール検証ログを `internal/notes/` に追加しました。
  walkthrough の画面キャプチャと実行ログを、test evidence として同じ release に残します。

## v20260510.1802

### 変更

- 日本語、簡体字中国語、繁体字中国語のディスクレス mini PC walkthrough に、
  PVE live ISO boot test で取得した実際の画面キャプチャを埋め込みました。
- ディスクレス mini PC walkthrough に残っていた古い placeholder 画像参照を削除しました。

## v20260510.1750

### 追加

- ディスクレス mini PC walkthrough に、PVE live ISO 実機検証で取得した
  画面キャプチャを追加しました。
- 簡体字中国語版と繁体字中国語版に、位置づけ、USB 永続化、
  法務と再配布の不足ページを追加しました。

### 変更

- website footer の著作権表示を、著作権表示を先に置く慣習的な形式へ変更しました。
- ディスクレス mini PC walkthrough の PVE 例を、VGA と serial console の
  両方を有効にする構成へ更新しました。これにより、QEMU の screenshot と
  `qm terminal` 検証を同じ実行で取得できます。

### 修正

- live ISO の設定ウィザードで、DHCPv4 pool の既定値を選択した LAN
  アドレスの prefix から導出するように修正しました。
- PVE live ISO boot test を再実行し、
  `/tmp/iso-boot-test-20260510-1742.log`、QEMU screenshot、routerd apply、
  Healthy status、USB persistence flush まで確認しました。

## v20260510.1722

### 追加

- routerd の Go ソース、インストーラースクリプト、プラグインスクリプト、
  Web 管理画面ソースに BSD 3-Clause の SPDX 識別子を追加しました。
- README にライセンスバッジを追加し、英語版と日本語版 README から
  BSD 3-Clause License へリンクしました。
- 公開ドキュメントに貢献ガイドを追加し、ドキュメントの sidebar から
  辿れるようにしました。
- SECURITY にメールと GitHub Security Advisories の報告先を明記しました。

### 変更

- repository root の `LICENSE` にある著作権表示を
  `Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors`
  に統一しました。
- SPDX ヘッダーが routerd ソースファイルだけに適用されることを
  法務ドキュメントに明記しました。同梱する第三者ソフトウェアは
  `THIRD_PARTY_LICENSES.md` に記載された個別ライセンスに従います。
- README から製品比較表を削除し、routerd 自身の対象範囲と特徴を説明する
  記述に整理しました。

## v20260510.1626

### 追加

- 公開ドキュメントに法務と再配布ページを追加し、release checklist を整理しました。
- 生成される第三者ライセンス一覧に Go module source URL を追加しました。
- BSD routerd binary と aggregate live ISO distribution model の内部 license audit note を記録しました。

## v20260510.1612

### 追加

- Go module とライブ ISO で使う Alpine package の第三者ライセンス一覧を自動生成できるようにしました。
- release archive とライブ ISO にライセンス通知を同梱する場所を追加しました。
- routerd 本体の BSD 3-Clause License と、ライブ ISO の aggregate distribution としての扱いを文書化しました。

## v20260510.1547

### 追加

- routerd 自身の対象範囲と deployment spectrum を中心に、公開向けの位置づけ説明を広げました。
- Intel NUC、N100 mini PC、Raspberry Pi 5、thin client、Proxmox VM の hardware compatibility を拡充しました。
- 中国語の hardware compatibility ページを追加し、ライブ ISO と USB 永続化の流れを明確にしました。

## v20260510.1534

### 追加

- ディスクレス mini PC walkthrough の図、tutorial index、field-note blog post を追加しました。

## v20260510.1508

### 追加

- USB persistence の運用ドキュメントと live ISO の USB persistence support を追加しました。

## v20260510.1451

### 追加

- contribution、security、license、positioning、hardware compatibility、diskless mini PC の各ドキュメントを追加しました。

## v20260510.1429

### 追加

- Alpine live ISO build と install documentation を追加しました。

## v20260510.1412

### 追加

- live ISO validation note と、live ISO 経路の installer documentation を追加しました。

## v20260510.1354

### 修正

- Alpine 上の live ISO runtime apply を修正しました。

## v20260510.1310

### 追加

- live ISO の serial console support を有効にしました。

## v20260510.1301

### 変更

- release tag を JST timestamp 形式へ切り替えました。

## 20260510.4

### 修正

- live ISO overlay archive path を修正しました。

## 20260510.3

### 修正

- Alpine live ISO の release discovery を修正しました。

## 20260510.2

### 追加

- Alpine ベースの live ISO packaging を追加しました。

## 20260510.1

### 追加

- installer configuration wizard を追加しました。

## 20260510.0

### 変更

- fixed-download-asset release の後続として、20260510 release series を開始しました。

## 20260509.16

### 追加

- 版番号付きアーカイブに加えて、`routerd-linux-amd64.tar.gz` のような固定名の alias をリリースアーカイブに追加しました。
- 固定名アーカイブと `.sha256` ファイルを GitHub Releases に配置します。これにより、ドキュメントで `releases/latest/download/...` の URL を使えます。

### 変更

- クイックスタートのドキュメントを、固定された latest download URL に変更しました。
- release workflow で、対応している GitHub JavaScript actions が Node.js 24 runtime を使うようにしました。

## 20260509.15

### 追加

- branch push と pull request 用の `CI` GitHub Actions workflow を追加しました。
- CI workflow は Ubuntu 上で `go test ./...`、schema 確認、example 検証、Web サイト生成を実行します。
- ローカル commit の前に Go テストと schema 確認を実行する任意の `scripts/pre-commit.sh` hook を追加しました。
- CI、pre-commit 確認、tag で起動する release workflow の役割分担を説明する開発ドキュメントを追加しました。

## 20260509.14

### 変更

- Ubuntu lab ルーターで `ClientPolicy` ゲストモードを検証しました。
- Linux nftables で、include mode のゲスト MAC アドレス集合、ゲスト向け DNS/DHCP/NTP 許可、自己隔離、RFC 1918 / ULA 拒否規則が生成されることを確認しました。
- exclude mode は、nftables 生成テストで確認しました。

## 20260509.13

### 追加

- ゲストモードガイドを詳細化しました。ユースケース、内部実装、`ClientPolicy` の全フィールド、確認手順、トラブルシューティング、セキュリティ上の限界を追加しました。
- include mode、exclude mode、複数ゲスト端末、カスタム拒否・許可リスト、ローカル探索サービス、IoT 固定割り当ての例を追加しました。
- `ClientPolicy.spec.guestServices` で、`dhcp`、`dns`、`ntp` に加えて `mdns` と `ssdp` を指定できるようにしました。

## 20260509.12

### 追加

- `ClientPolicy` を追加しました。Linux nftables で LAN 端末を MAC アドレスごとに分類するゲストモードです。
- ゲスト端末は DNS、DHCP、NTP を使えます。プライベート IPv4 宛てと ULA IPv6 宛ての通信は既定で拒否します。
- `examples/guest-mode.yaml` と、include mode / exclude mode の分類方法を説明するドキュメントを追加しました。

### 変更

- FreeBSD pf では `ClientPolicy` を明示的に未対応として扱います。pf は同じ MAC ベースの routed filtering モデルを持たないためです。

## 20260509.11

### 追加

- 最小 Tailscale mesh 参加、WireGuard hub-spoke 経路、VRF lab、multi-WAN home fallback の用途別 example を追加しました。
- 各 example の用途を説明する `examples/README.md` を追加しました。

### 変更

- `make validate-example` が `examples/` 配下の全 YAML ファイルを検証するようにしました。

## 20260509.10

### 追加

- Web 管理画面の Overview に、世代、リソース phase、HealthCheck 状態の簡易時系列チャートを追加しました。
- Config 画面で、現在の YAML ファイルと最新適用世代を比較できるようにしました。`routerd apply` の前に差分を確認できます。
- Resource テーブルで、kind、name、phase、詳細の検索、phase 絞り込み、検索結果の強調表示ができるようにしました。
- VPN 画面に Tailscale と WireGuard の peer 状態を示す視覚サマリーを追加しました。

## 20260509.9

### 追加

- リリースアーカイブに `share/doc/TARGET` を含め、`install.sh` がホストの OS と CPU アーキテクチャーを確認するようにしました。
- GitHub Actions で Linux と FreeBSD の `amd64` / `arm64` アーカイブを生成するようにしました。
- release CI で `install.sh` と `uninstall.sh` に `shellcheck` を実行します。

### 変更

- `install.sh --list-deps` の出力を、OS、CPU アーキテクチャー、パッケージマネージャー、パッケージ、確認対象コマンドが分かる形に整理しました。
- PPPoE、RA、IPsec、パケット取得、経路制御、ファイアウォールで使う実用パッケージを依存リストへ追加しました。

## 20260509.8

### 修正

- zh-Hant と zh-Hans のドキュメントリンクを修正し、翻訳ページが未翻訳のロケール内ページを指さないようにしました。
- 翻訳がそろうまで、概要ページから英語版の正準リファレンスへリンクする形にしました。

## 20260509.7

### 追加

- `EgressRoutePolicy` で、DS-Lite 主経路、RA 由来 DS-Lite、PPPoE、WAN 直結の多段フォールバックを表現できるようにしました。
- 宣言型な `Telemetry` リソースと OTLP 環境変数の伝播により、ルーター群へ OpenTelemetry 設定を展開しました。
- DS-Lite の例は、RFC 6333 の B4-AFTR link prefix `192.0.0.0/29` を tunnel 内側 IPv4 送信元として使う形にしました。
- `PPPoESession.disabled` と無効化された経路候補により、PPPoE フォールバック定義を YAML に残しつつ、本番 PPPoE セッションの漏れを防げるようにしました。

### 変更

- リリース版番号を `0.x.y` から日付ベースの値へ変更しました。
- `routerd --version`、`routerctl --version`、リリースアーカイブで同じ release tag の値を使うようにしました。
- Linux nftables と FreeBSD pf の NAT44 生成を、インターフェース単位のルールへ寄せました。
- 3-role のファイアウォールモデルを Linux と FreeBSD で確認し、service hole を広い zone 全体ではなく、所有する受信インターフェースへ束縛しました。
- FreeBSD pf で `PathMTUPolicy` の TCP MSS clamp を生成できるようにし、Linux nftables とそろえました。
- dnsmasq の RA 生成で、IPv6 RA MTU option により path MTU を配布できるようにしました。

### 修正

- FreeBSD pf で DHCPv6、WireGuard、VXLAN の service hole が `wan` zone の全インターフェースへ広がる問題を修正しました。
- FreeBSD の NAT artifact を nftables ではなく `pf.anchor/routerd_nat` として報告するようにしました。
- NAT 生成の前に、PPPoE のリソース名を実 OS インターフェース名へ解決するようにしました。

## 0.4.0

### 追加

- nftables の暗黙拒否ログを `routerd-firewall-logger` で取り込み、`firewall-logs.db` に保存するようになりました。Linux では `nfnetlink` を直接読み取り、FreeBSD では `pflog` を `tcpdump` 経由で取り込みます。
- Web 管理画面に Connections タブ (リアルタイムの conntrack / pf state)、Clients タブ (DHCP リース + トラフィック統合)、Firewall タブ (拒否ランキング + 時系列テーブル) を追加しました。
- `TailscaleNode` で Tailscale の exit node と subnet router を広告できるようにしました。生成した systemd ユニットで `tailscale up` を実行します。NixOS 向け生成では `services.tailscale` を有効化し、ユニットの `path` も設定します。
- `WebConsole.spec.listenAddressFrom` と `DNSResolver` 系のリスニングアドレスを `Interface/<name>.status.ipv4Addresses` から導出できるようにしました。即値の代わりに参照で書けます。
- conntrack accounting (`net.netfilter.nf_conntrack_acct=1`) を `SysctlProfile/router-linux` 既定値に追加し、`TrafficFlowLog` で `bytesOut` / `bytesIn` を集計できるようにしました。

### 変更

- リアルタイムのコネクション表示の API / CLI を `connections` に統一しました (旧称 `conntrack-snapshot`)。`/api/v1/connections`、`routerctl connections` を使います。IPv6 を含む全ファミリを同じ表で扱います。
- NixOS 向けの宣言型レンダリングを拡張しました。`Package` (NixOS パッケージ宣言)、`SysctlProfile`、`NetworkAdoption`、`generated service artifacts` を `routerd render nixos` の出力に統合します。NixOS 上の `Package` は実行時に導入せず、生成された NixOS 設定で管理します。
- `generated service artifacts` から FreeBSD `rc.d` スクリプトを生成できるようになりました (`routerd render freebsd --out-dir`)。

### 修正

- `IPv6DelegatedAddress` controller が `Link/<name>` の status が空のとき、PD 由来アドレスをホストインターフェースに付与しない問題を修正しました。
- `generated service artifacts` controller が変更のない active unit を毎回再起動する問題を修正しました。

## 0.3.0

### 追加

- 宣言型な OS bootstrap リソースとして `Package` と `SysctlProfile` を追加しました。apt、dnf、nix、pkg のパッケージ宣言と、ルーター用途向けの sysctl 推奨値 (`nf_conntrack_max`、socket buffer、TCP/UDP timeout、`ip_forward` など) を 1 つのリソースで適用します。
- `NetworkAdoption` で systemd-networkd の DHCP / RA を YAML から無効化できます。`generated service artifacts` で routerd 自身が unit を render + install + enable できます。
- `routerctl events --limit N --topic X --resource K/N -o json` で sqlite3 不要に bus event を確認できます。
- `routerd plan --diff` で apply 前差分を表示します。
- `DNSResolver` に bootstrap forwarder (RFC1918 内部 DNS を優先しつつ public DNS を予備にする) を追加しました。

### 変更

- 設定ファイル中の `${...status.field}` 文字列参照を、型付きの `*From` フィールドへ整理しました (`addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`dependsOn`)。互換別名はありません。
- controller chain を pure event-loop 型に再構築しました。共通 `framework.FuncController` (Subscriptions + Bootstrap + PeriodicFunc) と `eventedStore` で、状態保存時に必ず `routerd.resource.status.changed` を発行し、下流が再評価する設計です。
- bus event を `slog` 経由で systemd journal へ出力します (`journalctl -u routerd.service -f | grep "routerd event"` で controller の意思決定を追跡できます)。高頻度イベントは debug レベルです。
- 全バイナリを静的ビルドにしました (`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`)。OS 別の依存パッケージ (`dnsmasq-base`、`nftables`、`conntrack`、`iproute2`、`ppp`、`wireguard-tools`、`strongswan-swanctl`、`radvd`、`tcpdump` など) を Ubuntu / NixOS / FreeBSD ごとに整理しました。
- `HealthCheck.sourceInterface` を YAML 上ではリソース名で書き、実行時に OS の interface 名に解決します。

### 修正

- `generated service artifacts` 同士の `RuntimeDirectory` 競合で再起動時に socket が消える問題を、`runtimeDirectoryPreserve` で declarative に解消しました。
- `generated service artifacts` の `state: absent` を正しく Drifted として検出し、unit 削除を plan に含めるようにしました。
- `SysctlProfile` の observe で型ゆらぎによる不要な drift を抑えました。

## 0.2.0

### 追加

- Stateful firewall を導入しました。`FirewallZone`、`FirewallPolicy`、`FirewallRule` で nftables の `inet routerd_filter` table を生成します。
- `EgressRoutePolicy` (旧 `WANEgressPolicy`) に `destinationCIDRs`、`gateway`、`gatewaySource` を追加しました。`HealthCheck` は `via`、`sourceInterface`、`sourceAddress` で probe の送信経路を指定できます。
- DNS サブシステムを再構成しました。`DNSZone` (権威ゾーン定義) と `DNSResolver` (フォワーダー / キャッシュ) に分離し、ローカルゾーン、条件付き転送、DoH / DoT / DoQ、平文 UDP DNS をサポートします。dnsmasq は DHCPv4 / DHCPv6 / RA / 中継に専念します。
- DS-Lite (`DSLiteTunnel`)、PPPoE (`PPPoESession`、`routerd-pppoe-client`)、DHCPv4 client (`routerd-dhcpv4-client`、`DHCPv4Client`) を追加しました。
- NAT44 (`NAT44Rule`) と conntrack 観測を追加しました。`/proc/net/nf_conntrack` がない環境では sysctl 由来の集計に縮退します。

### 変更

- `WANEgressPolicy` を `EgressRoutePolicy` に改名しました。互換別名はありません。
- DHCP 関連 Kind とバイナリ名を RFC 表記に統一しました (`routerd-dhcpv4-client`、`routerd-dhcpv6-client`)。旧名の互換別名はありません。

## 0.1.0

最初の v1alpha1 実装です。

- DHCPv6-PD クライアント、daemon contract、event bus、controller framework を導入しました。
- DHCPv6-PD → LAN アドレス導出 → DNS 応答までの controller chain を実装しました。
- DHCPv6 情報要求、DS-Lite (試作)、IPv4 経路、RA、DHCPv6 サーバー、`HealthCheck`、`EventRule`、`DerivedEvent` を追加しました。

このバージョン以降、出荷前の整理として API 名や実装方針に大きな変更が入っています。最新の利用方法は `Unreleased` の項目と `examples/` を参照してください。
