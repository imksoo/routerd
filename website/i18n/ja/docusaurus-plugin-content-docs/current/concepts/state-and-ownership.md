---
title: 状態と所有
slug: /concepts/state-and-ownership
sidebar_position: 5
---

# 状態と所有

routerd は、宣言した意図と観測した状態を分けて扱います。
YAML は、利用者が管理する意図です。
SQLite、lease ファイル、events.jsonl は、routerd と専用デーモンが観測した状態です。

![effective config、ownership ledger、object status、host inventory から dry-run 可能な GC planner と teardown registry へ進む lifecycle GC 図](/img/diagrams/lifecycle-gc.png)

## 状態の置き場所

リリース版のインストールでは、正本の設定を `/usr/local/etc/routerd/router.yaml`
に置きます。
routerd の実行ファイルは `/usr/local/sbin` に置きます。

Linux での状態の保存先は次の通りです。

| 種類 | 例 |
| --- | --- |
| routerd 状態データベース | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD リース | `/var/lib/routerd/dhcpv6-client/wan-pd/lease.json` |
| DHCPv4 リース | `/var/lib/routerd/dhcpv4-client/wan/lease.json` |
| PPPoE 状態 | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| ヘルスチェック状態 | `/var/lib/routerd/healthcheck/<name>/state.json` |
| 実行時ソケット | `/run/routerd/.../*.sock` |

FreeBSD でも、設定と実行ファイルは `/usr/local` 配下に置きます。
実行時ソケットは `/var/run/routerd` に置きます。
永続状態は `/var/db/routerd` に置きます。

## 所有の考え方

routerd が作るホスト側の構成物には、それぞれ所有元のリソースがあります。
たとえば dnsmasq 設定は DHCP と RA の各リソースから、`routerd-dns-resolver` の設定は `DNSResolver` と `DNSZone` から、nftables の NAT テーブルは `NAT44Rule` から作られます。
複数のトンネルから集約される TCP MSS clamp テーブルは、最上位の `Router` が所有します。

所有元が分かると、次の判断ができます。

- この構成物を routerd が変更してよいか。
- YAML からリソースを消したとき、ホスト側も消してよいか。
- 既存の設定を取り込むだけか、それとも routerd が新しく作るのか。

所有キーは `apiVersion/kind/name` です。
適用世代は識別に含めません。
リソースの status には所有元とライフサイクル情報を含めるため、古くなった構成物の削除時にも、routerd が管理するリソースと、引き継いだものや外部のオブジェクトを区別できます。

## ライフサイクル GC

routerd は、具体的なホスト成果物の所有台帳と、リソースごとの解体に必要なオブジェクト状態を保存します。
適用時、serve 起動時、削除フローでは、汎用の GC プランナーがこれらの記録を、適用と同じ有効設定と比較します。
有効設定には、`when` フィルター適用後の起動 YAML、動的設定、生成済み SAM リソースが含まれます。

GC の計画は、所有する成果物の削除、リソースの解体、台帳行の忘却、古い status 行の削除、イベント記録、破壊的な削除前の状態バックアップを表せます。
未対応 OS の統合はスキップし、引き継いだものや外部管理の status はそのまま残します。

リソースごとの成果物対応表と解体の契約は、[リソース所有](../resource-ownership.md) を参照してください。

## 古くなった状態を使わない

リースや観測値は便利ですが、古くなった値を使い続けるのは危険です。
特に DHCPv6-PD のプレフィックスは、Bound であることを確認できるときだけ下流へ展開します。
確認できないときは、AAAA、RA、DHCPv6 サーバー、LAN IPv6 アドレスの適用を止めます。

## イベント

routerd と専用デーモンは、状態の変化をイベントとして記録します。
イベントは、SQLite の `events` テーブルやデーモンごとの `events.jsonl` に残ります。
EventRule と DerivedEvent は、このイベントや状態を使って仮想的な状態変化を作り出します。

## 適用世代

status に出る `generation` は、最後に完了した適用世代です。
この値は、`routerctl apply` がホスト側の意図を更新し、SQLite に適用の完了を記録したときに増えます。
調整ループの回数ではありません。
dry-run の計画、デーモンイベント、ヘルスチェック、controller chain の定期調整では増えません。
新しい適用世代には、そのとき適用した YAML のスナップショットを保存します。
Web 管理画面は、このスナップショットを使い、読み取り専用の世代履歴と、世代間の差分（unified diff）を表示します。
YAML 保存を導入する前の行は履歴として残りますが、差分表示の対象にはできません。

## 状態を持つパケットフィルター

Linux では、routerd は nftables の管理テーブルを 1 回の `nft -f` トランザクションで更新します。
生成したルールセットは、必要なら管理テーブルを作成します。
その後、同じ nftables バッチの中でテーブルを空にし、新しいチェーンを読み込みます。
firewall zone のインターフェース set や client-policy の MAC set のように
routerd が所有する named set は、再定義の前に管理対象の set だけを削除します。
これにより、削除した set 要素が再読み込み後に残るのを防ぎます。
稼働中の NAT テーブルやフィルターテーブルを、削除してから作り直すことはしません。
そのため、routerd の再起動や通常の設定変更を行っても、既存の conntrack エントリーはカーネルの状態テーブルに残ります。

FreeBSD では、routerd は生成した pf ルールを `pfctl -f` で読み込みます。
pf は、状態を明示的に消さない限り、ルールの再読み込み時にも既存の状態テーブルを保持します。
routerd の通常の適用処理では、pf の状態を消しません。
