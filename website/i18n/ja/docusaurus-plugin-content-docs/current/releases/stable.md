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
| バージョン | **v20260526.2335** |
| 位置づけ | 推奨安定版（v20260526.2241 を置き換え。ドキュメントと CI の整合性を取り直した追従対応で、ランタイムの挙動変更はありません） |
| 稼働実績 | 本番ルーター（homert02）で **3 回連続のインプレースアップグレード**（1607 → 2152 → 2241 → 2335）を検証済みです。routerd 再起動のたびに `routerd-bgp` には触れず（MainPID 2394269 が 4 回連続で不変）、BGP は 2/2 Established を維持、uptime は再起動ごとに途切れず伸び続け（1h19m → 1h27m → 2h0m → 2h15m → 3h7m → 3h10m）、2-way ECMP（.38 / .53）もカーネルに残ったまま、`routerctl doctor dslite` は pass=12 / warn=0、Web Console の Gateway Health 画面は 180 秒 / 90 サンプルで good=90 / bad=0、`install.sh` は正規のパッケージディレクトリに `cd` してから実行するパターンで終了コード 0 でした |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）、CI と Release ワークフローをすべて通過 |

## v20260526.2335 を推奨する理由

推奨の理由は**新機能の追加ではなく運用上の成熟**です。v20260526.2335 は
v20260526.2241 の本番安全特性をすべて受け継いでいます（v20260526.2241
自身が v20260526.1607 の Web Console シークレット伏字化、`gatewayHealth`
集約、機械可読な `routerctl doctor`、`ManagementAccess` の適用ガードを
継承しています）。その上にドキュメント / CI の堅牢化を 1 件追加して
います。

- **推奨安定版の表示が静かに乖離しなくなりました。** 新しい CI ガード
  (`scripts/check-active-stable.sh`) が `website/src/pages/index.tsx`
  の `STABLE_VERSION` を正本として、ホームページ冒頭のカード、各
  ロケールの導入ヒント、告知バー、`docusaurus.config.ts` が別の
  `vYYYYMMDD.HHmm` を指していた場合に CI を失敗させます。
  v20260526.2241 への昇格時にホームページのカードと 4 言語の導入
  ヒントが `v20260526.1607` のまま取り残されていた事象を、今後の昇格で
  再発させないための保険です。

v20260526.2241 から受け継ぎ、2335 の homert02 適用でも再検証された
5 つの運用契約は以下のとおりです。

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
