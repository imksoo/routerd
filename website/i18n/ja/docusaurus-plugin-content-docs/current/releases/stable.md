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
| バージョン | **v20260526.1607** |
| 位置づけ | 推奨安定版（v20260525.1631 を置き換え） |
| 稼働実績 | 本番ルーター（homert02）で検証済み: routerd 再起動・install 中も DNS は無瞬断（NG 0）、`/api/v1/config` の生 secrets 検出 0、`gatewayHealth` は 26 components で overall=ok、`routerctl doctor` は rc=0（pass=32 warn=4 fail=0 skip=1）、install 跨ぎで BGP 2/2 と 2-way ECMP を維持 |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）、CI と Release ワークフローを通過 |

## v20260526.1607 を推奨する理由

推奨の理由は**新機能の追加ではなく運用上の成熟**です。
v20260526.1607 は前推奨版の本番安全な DNS / BGP アップグレード挙動をそのまま受け継ぎ、
本番ルーター（homert02）で検証された 4 つの運用契約を加えています:

- **Web Console が secrets を漏らさなくなりました。** `/api/v1/config` と
  generation の config / diff エンドポイントは、シリアライズ前に
  WireGuard `privateKey` / `preSharedKey`、Tailscale `authKey`、
  BGP/PPPoE/IPsec `password`、WebConsole `initialPassword`、bearer/token 系を
  redact します。キーは残しマーカに置換するため UI は壊れません。homert02
  実トラフィック検証で **生 secrets 検出 0**。
- **`gatewayHealth` が出口経路全体を集約します。** `/api/v1/summary` は
  DNSResolver / DSLiteTunnel / DHCPv6PrefixDelegation に加えて
  EgressRoutePolicy / NAT44Rule / HealthCheck も束ねます。Web Console バナーは
  選択中 path と preferred の一致状態を表示し、fallback 候補使用中は警告を
  目立たせます。homert02 検証で **overall=ok / 26 components**。
- **`routerctl doctor` は機械可読な安定契約になりました。** `-o json` 出力は
  v1alpha1 の運用者向け契約として明文化（area・status enum・summary・終了コード）。
  fail で非0 終了するためスクリプトから扱えます。homert02 検証で
  **rc=0（pass=32 warn=4 fail=0 skip=1）**。
- **`ManagementAccess` による宣言的 apply ガード。** 管理 IF の欠落、firewall が
  SSH を遮断する状況、WebConsole の全アドレス bind を、apply 前 preflight で
  検出し（`--allow-mgmt-lockout` で上書き可）非 dry-run apply を中止します。
  同じチェックは `routerctl doctor mgmt` でも実行できます。

**継承事項（v20260525.1631 等から）:** DNS リゾルバが独立長寿命サービスユニット
として動き、routerd 再起動・アップグレードで DNS が中断しません（homert02 検証:
`routerd.service` restart 中・install 中とも DNS probe NG 0）。`install.sh` は
バイナリ更新時に `routerd-bgp` を自動再起動せず、eBGP セッション・ECMP を維持
します（homert02 検証: 2/2 Established・2-way ECMP・HTTP 200 を install 跨ぎで
維持）。完全な BGP 制御プレーン（FRR 不使用、#26 next-hop 書き換え、#28
OpenRC live ISO 起動）。`routerctl ledger` 保守（`integrity-check` / `vacuum` /
`backup` / `prune-events`、非 dry-run prune には監査イベントを発行）。

## 既知の観測（リリースを止めない事項）

- **DS-Lite の doctor WARN は egress が正常でも出ることがある。** AFTR の AAAA
  プローブまたは tunnel device 検出が間欠的に noisy なとき、doctor の `dslite`
  area が WARN を返すことがあります（`gatewayHealth=ok`、実際の egress も
  HTTP 200 で通る場合）。dataplane 障害ではなく保守的診断ノイズと扱います。
  次の調整で DS-Lite doctor の severity を `gatewayHealth` の selected-path
  証拠と整合させる予定です。
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
