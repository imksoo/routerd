---
title: 開発時の確認
---

# 開発時の確認

![Diagram showing development checks split between local pre-commit tests, CI pull request validation, and release workflow archive publishing](/img/diagrams/operations-development.png)

routerd は 2 つの自動化経路を分けています。

- CI ワークフローは通常の push と pull request を検証します。
- Release ワークフローはリリースタグを push した後にリリースアーカイブを生成します。

Release ワークフローは複数の OS と CPU アーキテクチャーを対象にします。
また、GitHub Release の成果物も公開します。
そのため、通常の CI とは分けています。


## 試験・ハーネス設計の必須規範

単体試験、オフライン fixture、runtime smoke、release qualification、再利用可能な旧 helper・テンプレート・コピー元にも適用します。現在の caller がないコードも対象です。変更時は製品要件、必要な直接証拠、観測の限界を明示しなければなりません。

- 観測・診断ログは残し、製品合否から分離します。具体的な製品要件なしに journal、進展カウンタ、latency 統計、ホスト全体の診断を合否条件へ結合してはいけません。
- PASS／製品 FAIL は完了済みで対象へ帰属できる直接証拠で判断します。API／SSH の揺らぎ、採取不能、欠落・壊れた入力、guest command 未完了、timeout は観測／インフラの結果です。ゼロ、違反なし、拒否成功、実測製品違反へ変換してはいけません。
- 無根拠な速度条件、重複 probe・gate を削減します。契約上の期限と停止用の有界 watchdog は維持し、製品性能の実測と区別します。
- 一時データを削除する前に、失敗した生の試行、ソース識別、exit、時刻、cleanup 結果を保持します。今回所有する資源だけを cleanup し、失敗を隠したり製品証拠を消したりしてはいけません。
- 原因調査せず失敗した実試験を反復してはいけません。既存 retry・時間・費用予算に達したら停止し、失敗・不確実性・残予算・修正案を報告してから次へ進みます。根拠のある失敗／欠落観測だけを有界に再取得し、各試行を保存します。
- 実際の入口、選択ソースのパスと SHA、依存関係、最終結果への伝播を監査します。候補や mock の検証はその範囲の証拠です。稼働入口が候補を選ぶことや実転送の証明とは扱えません。
- 実装、準備候補、稼働コード、保存原本の再解析、意図的に変更した fixture、未実証を区別します。原本の負例を保持し、過去の失敗を PASS に書き換えてはいけません。
- 未接続・旧コードの再利用にも同じ規範を適用します。封印原本は不変とし、再利用されるソースを直します。現役 caller がないことを理由に欠陥を無視してはいけません。

真の機能要件、安全・所有権の確認、費用制限を弱める規範ではありません。必要証拠の欠落は PASS の根拠になりません。正常通信と直接違反の証明は維持し、診断との無根拠な結合だけを外します。

良い例: curl exit 0／HTTP 200 と必要な body・経路証拠があれば、任意の latency 欠落は診断として記録し PASS にできます。悪い例: WireGuard 取得失敗を「禁止 AllowedIP なし」と扱う、invalid apply の timeout／強制終了を拒否成功に数える、同一スタックの ping 成功をトンネル転送の証明にする。

## CI ワークフロー

`.github/workflows/ci.yaml` はブランチへの push と pull request で動きます。
Ubuntu のランナーを使い、レビュー前に緑に保つべき範囲を確認します。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

`webconsole/`、チェックイン済みの静的 asset、共通 quality workflow、または Makefile を
変更した場合、CI は Web Console 専用 gate も実行します。`npm ci`、全依存と本番依存の
high severity audit、TypeScript typecheck、本番 build、生成 asset の差分検査を行います。

CI ワークフローはリリース成果物を公開しません。
リリースアーカイブは、日付ベースのタグで `Release` ワークフローが生成します。

## pre-commit フック

リポジトリには任意で使える pre-commit フックを含めています。

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

有効にすると、`git commit` の前に次の確認を実行します。

```sh
go test ./...
make check-schema
```

どちらかが失敗するとコミットは止まります。
スキーマの差分やテスト失敗を CI の前に検出できます。

緊急でローカルコミットが必要な場合は、次の環境変数を指定します。

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

後続の修正が明確な場合だけ使ってください。
ブランチを push した後は CI が実行されます。


## 実カーネルの ICMPv6 受信テスト

通常の `go test ./...` は非特権で実行します。raw socket を使う受信試験は
`routerd_native` build tag で分離し、Linux CI の独立 network namespace と
FreeBSD native VM のゲートで明示的に実行します。有効化した試験の権限エラーは失敗です。

隔離 Linux VM では通常ユーザーでコンパイルし、対象の試験だけを特権で実行します。

```sh
go test -tags routerd_native -c -o /tmp/routeradvertisement-native.test ./pkg/routeradvertisement
sudo unshare --net sh -ec 'ip link set lo up; exec /tmp/routeradvertisement-native.test -test.run="^TestSolicitationWildcardReceiverOnActualSocket$" -test.v'
```

隔離 FreeBSD VM でも同じ tag でコンパイルし、テストバイナリを root で実行します。
対象は実カーネルでの受信とパケットの付帯情報です。転送や独立した対向機との疎通の証明とは分けます。
終了後はテストバイナリを削除します。


## 子プロセスの所有確認と service の権限

Linux service は、dnsmasq が UID を落とした後も procfs で正確なプロセス identity を
確認するために `CAP_SYS_PTRACE`、確認済みの子へ pidfd でシグナルを送るために
`CAP_KILL` を必要とします。同梱 unit と runtime renderer の権限を揃えます。
所有確認を省略したり dnsmasq を root のまま動かしたりして権限不足を隠しません。
procfs のアクセス条件は [ptrace(2)](https://man7.org/linux/man-pages/man2/ptrace.2.html) を参照してください。

通常ユーザーでコンパイルし、専用の PID/mount namespace で実行します。試験は
実 service と同じ capability 制限で、別 UID の子の確認と停止を製品の owner 経由で検査します。
確認・停止の失敗はゲートの失敗です。CI も同じ試験を実行します。

```sh
go test -tags routerd_native -c -o /tmp/chain-native.test ./pkg/controller/chain
sudo unshare --mount --pid --fork --mount-proc /tmp/chain-native.test -test.run="^TestDnsmasqOwnerWithServiceCapabilities$" -test.v
```

DHCP の取得・更新、DNS、service 再起動は、別途隔離 VM と実クライアントで確認します。
終了後はテストバイナリを削除します。

## Linux の PD・RA ライフサイクル VM 試験

`scripts/linux-pd-ra-lifecycle-smoke.sh` は `make dist` から導入した製品を
実 systemd service で検査します。空の router 設定、独立した管理経路、
本番通信のない専用 Linux VM を使用してください。`--disposable-vm` が必須です。
ゲストの設定を置換し、試験用インターフェースと IPv6 forwarding を変更するため、
通常の unit test には含めません。

開始前に systemd の正常性と iproute2、Python 3、curl、jq、dig、dnsmasq を確認します。
Kea DHCPv6 と ndisc6、および共有ライブラリはゲストと同じディストリビューションの
パッケージから準備します。専用ディレクトリへ展開する場合も root で展開し、
setuid executable の `rdisc6` が一般ユーザー所有にならないようにします。
Kea は `ldd` で依存ライブラリを確認してください。実行済みの組合せは
Ubuntu 24.04.4 / Kea 2.4.1 です。既存の Kea や `/run/kea` と衝突したら試験を開始しません。

```sh
sudo scripts/linux-pd-ra-lifecycle-smoke.sh --disposable-vm \
  "$PWD/tests/fixtures/linux-pd-ra-lifecycle.json" /opt/routerd-pd-peer
```

最後の引数は `usr/sbin/kea-dhcp6`、`usr/bin/rdisc6`、
`usr/lib/x86_64-linux-gnu` を含むパッケージの展開先です。OS に導入済みなら `/` も
使えますが、既存 Kea の実行状態を変更する許可を意味しません。

独立した上流・クライアント netns を使い、Kea の /60 委譲、Linux の SLAAC と
RA default route、ローカル・転送 AAAA の UDP/TCP 応答、対向が観測した IPv6 送信元、
実 Renew/Reply による lease timestamp 更新、service 再起動を確認します。
T1/T2 は20/40秒、preferred/valid lifetime は60/90秒です。上流停止後に期限切れで
router のアドレスと client の default route が撤去され、製品は動いたまま通信が
失敗することも必須です。最後に空設定の適用、peer・netns 撤去、forwarding 復元を行います。

アドレス追加直後の DAD 待ちは RA の Pending とし、設定適用を失敗させません。
DAD 完了後に広告を開始します。DAD 失敗・所有権不明は引き続きエラーです。
DNSForwarder は設定順に評価するため、ローカル zone を default upstream より前に置きます。

成功時の生出力は自動削除します。残すのは revision・判定、手順・fixture・回帰テストです。
失敗時は製品と peer を停止し、表示した fixture パスを診断用に保持します。
製品の失敗はローカルで再現・修正してから再検証し、解消後は一時出力を削除します。
WAN の transit と戻り経路は試験用の事前設定です。WAN RA、DHCPv6 IA_NA、prefix 変更、
reboot/crash recovery、firewall、FreeBSD、routerd 1.0 全機能の合格とは区別します。
