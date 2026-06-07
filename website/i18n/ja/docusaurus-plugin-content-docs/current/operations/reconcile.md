---
title: 調整（リコンサイル）と削除
---

# 調整（リコンサイル）と削除

![Diagram showing reconcile and removal from validate, plan, and dry-run preflight through effective desired view construction to owner-reference GC planner cleanup with backup and event recording](/img/diagrams/operations-reconcile.png)

routerd は、YAML が宣言する意図とホストの現在状態を比べます。
差があれば計画（plan）を作り、必要なら dry-run で確認してから適用します。

## 標準シーケンス

```bash
routerd validate --config router.yaml
routerd plan     --config router.yaml
routerd apply    --config router.yaml --once --dry-run
routerd apply    --config router.yaml --once
```

遠隔のルーターでは、本番の `apply` を実行する前に、管理経路（SSH、コンソール、ハイパーバイザーのコンソール）が変更後も残ることを確認してください。

稼働中のルーターでは、`plan`、`observe`、dry-run apply は状態データベースを一時的な snapshot として読みます。デーモンが SQLite WAL に同時書き込みしている場合、その snapshot はわずかに古い可能性があるため、rollback 記録ではなく事前確認用の参考入力として扱ってください。

## 常駐モード

```bash
routerd serve --config router.yaml
```

serve モードでは、バス上のイベントに反応し、影響範囲のリソースだけを再評価します。
入力となるのは、DHCPv6-PD のリース更新、ヘルスチェックの結果、派生イベント、inotify による設定変更の検知などです。

コントローラーの dry-run フラグは、所有範囲ごとに効きます。
`--controller runtime-dry-run-ingress=false` は、IngressService コントローラーによるヘルスの実選択と、IngressService 由来の nftables DNAT/hairpin ルールの実適用を意味します。
独立した `NAT44Rule` と `LocalServiceRedirect` は、引き続き
`--controller runtime-dry-run-nat=false` で別に制御します。

`IngressService`、`PortForward`、NAT、BGP、静的経路・ポリシー経路など、転送を伴う
リソースがある場合、routerd は必要なランタイム sysctl を導出します。
`routerd apply --once` は、派生する設定を観測・計画・生成しますが、ホストには反映しません。
反映は、`routerd serve` がコントローラーの調整（リコンサイル）の中で収束させます。
これにより、一度きりの apply は設定の検証と成果物の生成にとどまり、
デーモンとランタイムのカーネルのライフサイクルは、長時間動くコントローラーが所有します。

## drift の確認

routerd は、状態データベースだけを唯一の正として扱いません。
状態ストアには前回の apply で観測した内容を記録しますが、各コントローラーは
処理を省く前に、自分が管理する実機の状態も確認します。
たとえば、systemd ユニットの enabled/active 状態、dnsmasq が期待どおりの設定ファイルで
動いているか、DHCPv4 リースのアドレスがインターフェース上に残っているか、
管理対象の nftables テーブルが実機に存在するか、といった点です。

これは、再起動の後、手作業の変更に失敗した後、upgrade が途中で止まった後に重要になります。
状態データベース上は Applied のままでも、OS 側の状態がずれていることがあるためです。
コントローラーは、前回の status 行をそのまま信じるのではなく、宣言された YAML へ
OS の状態を収束させます。

## 派生リソース

一部のホストオブジェクトは、YAML に直接書かせず、より高レベルの意図から生成します。
たとえば `routerd.service`、`routerd-healthcheck@*.service`、firewall log デーモン、
DPI helper サービスは、派生したサービスユニットです。生成されたリソースは次で確認できます。

```bash
routerctl show derived-resources
```

既定では、現在の設定から派生するものだけを表示します。
現在の設定に由来しない古い status 行は、稼働中のリソースに見えないよう
非表示にします。古い状態データベースを掃除するときは、`--include-stale` で確認できます。

削除済み、または未対応のリソース Kind が YAML に残っている場合、routerd はそれを黙って
無視せず、設定の読み込みを失敗させます。

## 管理対象の掃除

YAML からリソースが消えた場合、所有元のコントローラーは、自分が所有する構成物だけを
削除または無効化します。
対応する `HealthCheck` がなくなった `routerd-healthcheck@*.service` は、
無効化して削除します。
NAT44 ルールが 0 件になった場合は、管理対象の `routerd_nat` テーブルまたは
pf anchor を空にします。
`state: absent` の `generated service artifacts` は、生成済みのユニットを削除し、ユニットが存在するか
まだ enabled/active のときだけ停止します。

古い status 行が、現在のスキーマにないリソース Kind に属している場合は、
`routerctl delete --force <kind>/<name>` で削除します。同じ kind/name が複数の
API グループにある場合は、routerd が推測で消さないよう `--api-version <version>` を指定します。

ファイアウォールの生成では、管理対象の nftables テーブルを維持したまま、1 回の
`nft -f` バッチで再読み込みします。
firewall zone のインターフェース set や client-policy の MAC set のような named set は、
再定義の前に routerd が管理対象の set だけを削除します。
これにより、削除済みの要素が残らず、通常の apply でフィルターテーブル全体を
削除して作り直すこともありません。

## 削除

routerd が削除するのは、所有を確認できる構成物（routerd が以前に作成したもの、または明示的に取り込んだもの）だけです。
第三者の構成や手作業の変更には触れません。

世代単位のロールバックに対応しています。`routerctl rollback --list` で過去の apply が記録した世代一覧を表示し、`routerctl rollback --to <generation>` で保存済みの Router YAML を通常の apply 経路で再適用できます。ロールバックは宣言済み config と routerd 管理下の artifact を再適用するもので、conntrack や kernel の一時状態、デーモンのランタイム状態、routerd の ledger 外で行ったホスト変更までは**復元しません**。削除を含む変更では、必ず `routerd plan` と `routerd apply --dry-run` で削除リストを確認してから適用してください。

## 関連項目

- [状態と所有権](../concepts/state-and-ownership.md)
- [Apply と render](../concepts/apply-and-render.md)
- [トラブルシューティング](../how-to/troubleshooting.md)
