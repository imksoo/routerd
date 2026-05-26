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
| バージョン | **v20260526.2241** |
| 位置づけ | 推奨安定版（v20260526.1607 を置き換え） |
| 稼働実績 | 本番ルーター（homert02）で **2 回連続の in-place アップグレード**（1607 → 2152 → 2241）を検証済み: routerd 再起動のたびに `routerd-bgp` は触られず（MainPID 不変）、BGP は 2/2 Established を維持、uptime は再起動ごとに途切れず伸び続け（1h19m → 1h27m → 2h0m → 2h15m）、2-way ECMP（.38/.53）も kernel に残ったまま、`routerctl doctor dslite` は pass=12 warn=0、Web Console Gateway Health 画面は 180s / 90 samples で good=90 / bad=0 |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）、CI と Release ワークフローを通過 |

## v20260526.2241 を推奨する理由

推奨の理由は**新機能の追加ではなく運用上の成熟**です。v20260526.2241 は
v20260526.1607 の本番安全特性（Web Console secrets redaction、`gatewayHealth`
集約、機械可読 `routerctl doctor`、`ManagementAccess` apply ガード）をすべて
受け継ぎ、本番ルーター（homert02）で観測された 5 つの運用契約を追加して
います:

- **routerd 本体のバイナリ更新で BGP セッションが落ちなくなりました。** BGP
  コントローラーが reconcile 入口で applied 済みポリシー状態を hydrate する
  ようになり、routerd 再起動時に同一内容の import-policy 割り当てを再 PUT
  しなくなりました。homert02 の **2 回連続の routerd 再起動**で検証済み
  （PID 3368318 → 3407972 → 3428160）: BGP は終始 2/2 Established を維持、
  uptime は再起動ごとに途切れず伸び続け、2-way ECMP（.38/.53）も再導入なしで
  kernel に残りました。
- **`routerctl doctor dslite` が実体と一致するようになりました。** Doctor は
  DSLiteTunnel `phase=Up` を健全と判定し、EgressRoutePolicy の選択を
  `status.selectedSource = "DSLiteTunnel/<name>"` 経由でも認識します（旧来の
  `selectedCandidate` 名一致も併用）。homert02 のように `dslite-pd-balanced`
  といった集約候補名を使う本番構成でも、`gatewayHealth=ok` の DSLiteTunnel
  が WARN にならなくなりました。検証結果: warn=4 → pass=12 warn=0。
- **Gateway Health UI が独立画面になり、表示が安定しました。** Web Console は
  Gateway Health を Overview から独立画面に分離し（Connections / Clients と
  同じ構成）、`selectedPath` / `preferredPath` / `fallbackReason` /
  `failedProbes` / `lastTransition` を含む完全な evidence を表示します。
  Overview には集約カードのみを残します。partial refresh 中に
  `Components 0 / Unknown` と瞬時に表示される flap も解消しました。
  `reconcileSummary` は空 components の薄い snapshot で前回値を上書き
  しなくなりました。検証結果: **180s / 90 samples で good=90 / bad=0、
  26 components 確認**。
- **`install.sh` は silent no-op にならなくなりました。** これまでは
  release tree の外から起動すると（`cd /tmp/release && ./pkg/install.sh ...`
  など）cwd 相対の `bin/*` glob が 1 度も展開されず、`--with-ndpi-archive`
  payload だけが反映されるのに exit 0 + `routerd upgrade completed` の成功
  表示だけが出ていました。cwd に `bin/routerd` payload が無い場合は明示
  メッセージとともに `exit 2` で即座に失敗するようになりました。CI には
  両ケース（payload 欠落・正規 cwd）を再現する smoke
  (`scripts/install-sh-cwd-smoke.sh`) を組み込んでいます。homert02 検証:
  cwd-mismatch の antipattern は **rc=2 で即 fail**、正規の
  cd-into-package-dir パターンは rc=0。

**継承事項（v20260526.1607 等から）:** Web Console の `/api/v1/config` と
generation エンドポイントは WireGuard `privateKey` / `preSharedKey`、
Tailscale `authKey`、BGP/PPPoE/IPsec `password`、WebConsole
`initialPassword`、bearer/token 系を redact します。`/api/v1/summary` は
DNSResolver / DSLiteTunnel / DHCPv6PrefixDelegation / EgressRoutePolicy /
NAT44Rule / HealthCheck を `gatewayHealth` に集約します。`routerctl doctor`
は v1alpha1 の機械可読契約（`-o json`、明文化された area / status enum /
summary、fail で非 0 終了）。`ManagementAccess` apply preflight は
`--allow-mgmt-lockout` 無しでは lockout を防ぎます。DNS リゾルバは独立
長寿命サービスユニットとして動き、routerd 再起動・アップグレードで DNS は
中断しません。`install.sh` はバイナリ更新時に `routerd-bgp` を自動再起動
せず、eBGP セッション・ECMP は routerd バイナリ更新をまたいで残ります。
`routerctl ledger` 保守（`integrity-check` / `vacuum` / `backup` /
`prune-events`、非 dry-run prune には監査イベントを発行）。

## 既知の観測（リリースを止めない事項）

- **`install.sh` 後に `routerd-bgp` が旧 inode のままで動く場合がある。** これは
  意図どおりです。`install.sh` は upgrade 時に `routerd-bgp` を自動再起動せず、
  確立済み BGP セッションと ECMP が routerd バイナリ更新をまたいで残ります。
  運用者が Graceful Restart のタイミングで `systemctl restart routerd-bgp` を
  実行するまで、プロセスは旧 inode のバイナリを掴み続けます。
- **`ManagementAccess` 未宣言の構成では `routerctl doctor mgmt` が SKIP になる。**
  これは live config 側の選択であり、リリースの欠陥ではありません。apply の
  lockout ガードと doctor mgmt の判定を有効にしたいなら `ManagementAccess`
  リソースを宣言してください（[`examples/home-router-mgmt-protected.yaml`](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
  参照）。

:::warning アップグレード時の注意
- **`install.sh` は必ず展開した release ディレクトリに `cd` してから実行してください。** 別ディレクトリから（例: `cd /tmp && sudo ./routerd-release-vYYYYMMDD.HHmm/install.sh ...`）起動すると、`exit 2` で実行を拒否するようになりました。これは意図したものです。以前は同じ呼び出しで silent no-op が発生し、`--with-ndpi-archive` の payload だけが反映されていました。
- **v20260523.1542 以前から上げる場合:** `disabled:` フィールド（`enabled: false` を使用）と no-op の `--controller-chain*` / `--observe-interval` フラグが削除済みです。該当する設定とホストの service unit をアップグレード前に書き直してください。
- **DNS リゾルバのサービスユニット化:** リゾルバは `routerd-dns-resolver@<name>.service` として動くようになりました。この方式への初回アップグレード時だけ「子プロセス → ユニット」の切り替えで一度だけ短い DNS 瞬断が出ます。以降は routerd の再起動・アップグレードで DNS は中断しません。
:::

## 「安定版」の意味と注意点

:::warning API はまだ v1alpha1 です
「安定版マイルストーン」は、**この版が本番運用に堪える品質である**ことを示すもので、**API（リソーススキーマ）の後方互換を約束するものではありません**。
:::

- routerd のリソース API は現在 **v1alpha1** です。リリース間で**破壊的変更が入ることがあります**。
- バージョンを上げるときは、後方互換に頼らず、**新しいスキーマに合わせて設定（YAML）を書き直す**前提で進めてください。
- マイグレーション用の互換コードは持たない方針です。各版の変更点は [変更履歴（Changelog）](./changelog.md) を確認してください。

## 導入とアップグレード

導入手順は [導入とアップグレード](../install-and-upgrade.md) を参照してください。アップグレードは、推奨マイルストーン版を起点に行うことを勧めます。
