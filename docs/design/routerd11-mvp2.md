# routerd 1.1 MVP 監査指摘と作業順序

2026-11-09、main `c8a06314c93d43fbb4c3a4b4aad0083b72c0fd04` を対象とした監査。MVP導入時 `259d960` との差分と現在の呼び出し経路を調査した。指摘はソースから条件と処理経路を確認したもので、監査時に実機障害を再現したものではない。1.1 MVPとAGENTS.mdを実現済みとは判定できない。

追記: 所有者は監査後、関係ない別の設定は手動追加も含めて残し、コンフィグをレンダーして設定するよう指示した。設定された対象の修復と、設定から削除した対象の撤去は引き続き必要である。作成元や所有権の判定・記録は不要であり、データベース互換を理由に残さない。DHCPリースやSAMの動作に必要な状態は保持する。以下の設定外の対象を実体から一律撤去する指摘はこの指示に置き換え、そうした処理を全リソースタイプで取り除く。Cloud SAMの実機確認は当面の対象外とする。

## 1. 禁止された試験と専用コードの除去

- `webconsole/scripts/screenshot.mjs` は固定応答の模擬APIと偽時計を使う。試験、専用のCI処理、Makeターゲット、依存を削除する。必要なWeb Consoleの確認には使用中のrouterdを使う。
- inventoryの架空環境試験とGOOS・コマンド・ファイル操作の注入フィールドを削除し、通常入口 `inventory.Collect()` を残す。
- canonical configの人工エラー試験と専用の `atomicFileOps` を削除する。実際のatomic write、同期、エラー報告は維持する。
- Linux経路検索の固定JSON応答試験と可変の `routeLookupCommand` を削除し、実コマンドを直接呼ぶ。
- FreeBSD SAMの製品から到達しない旧published-ARP作成とrouting socket処理、専用試験を削除する。現在のproxyall、PF、GARP等は維持する。
- この一覧は残存箇所の完全一覧ではない。残るGoテストを内容を見ずに一括で禁止対象としない。

## 2. 設定への復元

- WireGuardは設定ハッシュと実体の照合で再適用を省く際、設定の秘密鍵から導いた公開鍵とカーネルの公開鍵も比較する。鍵ファイルが正しくカーネル側だけ変わった場合に修復する。所有記録や適用証明は追加しない。
- 静的IPv4アドレスは、過去のstatusだけでなく現在の実体を列挙し、設定から導けない余剰を撤去する。
- IPv4経路は、過去のstatusにない別prefixの余剰もmain表で扱う。
- IPv4 policy ruleは、`from <prefix> lookup <table>` 等、現在の限定的な解析から漏れる余剰も扱う。余剰ruleによって不要なtableが残る問題も扱う。
- GRE、IPIP等のTunnelInterfaceは、過去のObjectStatusにない手動作成物も実体から見つけて扱う。
- SAMの永久proxy neighborと `proxy_arp` は、記録の欠落や消失後も不要な状態を実体から扱う。通常のARP/NDキャッシュとは区別する。

設定、現在有効な動的状態、OS基盤から必要な集合を導く。DHCP、PD、BGP、SAMの正当な状態を一括削除しない。通常の同一設定applyでは、NetworkAdoptionは生成ファイルの変更や前回エラーの再試行がなければ `networkctl reconfigure` を呼ばない。対象機能を使う実機で同じ設定への復元を確認する。

## 3. SAMとPPPoEの復旧

- PPPoEは子プロセス終了後の撤去確認に一度失敗すると `teardownFailed` が後続開始を拒否する。再接続時に現在のインターフェースを再確認し、撤去済みなら失敗を解除する。実際に存在する場合や観測エラーは隠さない。製品側の5秒は、根拠のない試験タイムアウトとは区別する。現在の4系統でPPPoEが有効かは未確認。
- SAMは一件の不正な適用履歴が `reconcileState` 全体を止め、正常なcaptureやpoolの適用も妨げる。不正記録を報告し、扱えない撤去対象を未完了としながら、設定から確定できる正常対象を進める。履歴が存在しないだけの場合は現在も拒否していない。全状態DBの初期化や新しい復旧フレームワークは不要。

## 4. release、文書、後片付け

- `scripts/release.sh` の `git commit -m "Release ${release_tag}"` は必須hookの4項目を満たさず停止する。実際の差分に対する説明を渡せるようにするか、コミットを担当者の明示操作に分ける。hookの無効化や空疎な説明の自動挿入はしない。
- 完全な候補ファイルを置き換えるMVPの例は、`plan` と `apply` の両方に `--replace` を明示する。既定の `apply -f` は部分upsertである。
- `internal/notes` の追跡済み生ログ5本（`iso-boot-test-20260510-1742.log`、`iso-live-router-functional-test-20260510.log`、`iso-router-test-20260510-1842.log`、`iso-soak-20260511` 系2本）を除去する。
- `docs/operations/development.md`、`docs/testing/provider-contract-harness.md` 等に残る模擬状態・ハーネスや生診断・過去証跡の保持を促す旧指示を現行規約に合わせて整理する。文書の存在だけで全ハーネスが実行中と判断しない。

## 5. 実対象で残る確認

Cloud SAMの実稼働先を特定し、変更後のSAM・BGP通信や切替・復旧を確認する。homertの強制VRRP切替、DHCP/PD更新、手動変更の復元、pve-rtとk8s-rtの変更後daemonの通信など、必要な更新・復旧を実対象で確認する。代替の模擬環境を作らない。4系統の現在の完全な実設定セットは監査時のcheckoutにはなく、削除した全Kind・fieldの不要性や設定互換性を包括的に認定できていない。

監査時点の記録では、homert02/03に `6ac8fbdb` のdaemonと対象helperを投入し、Healthy、BGP Established、master/backup、実クライアントからのDNSと外部pingを確認した。pve-rt07とk8s-rt02では実状態DB・設定を使うCLI出力の比較までであり、変更後daemonの通信は未確認。Cloud SAMは調べたTerraform stateが空で、稼働先を特定できていない。`6ac8fbdb` から監査対象HEADまでの変更は記録文書のみ。これらは既存記録であり、この監査での再接続・追認ではない。

## 監査で確認した改善と判定

Bridgeの実体照合・修復、WireGuard/VXLAN/DS-Liteの実インターフェースに基づく撤去、SAM proxy_arpの所有証明に依存しない一致判定、BGP観測・動的設定コピー・egress/SQLite状態の型整理は進んでいる。`259d960` から `c8a06314` にかけてGit管理ファイルは2,086から1,560、テスト以外のGoコードは173,904行から161,198行、Goテストファイルは355から178、Goテストコードは128,346行から36,065行に減った。`tests/netns/` と `tests/golden/` の追跡ファイルはそれぞれ27と123から0になった。全体差分は7,200行追加、172,347行削除。キャッシュや未追跡ファイルは集計対象外。

判定は「削減と実機移行は前進しているが、中核の設定復元と禁止コードの撤去は未完了」。1.5／2.0の廃棄条件である、1.1で必要な運用の成立は未確認。importの現行範囲は所有者の指示によるため変更対象としない。

監査の根拠: [対象MVP](https://github.com/imksoo/routerd/blob/c8a06314c93d43fbb4c3a4b4aad0083b72c0fd04/docs/design/routerd11-mvp.md)、[対象AGENTS.md](https://github.com/imksoo/routerd/blob/c8a06314c93d43fbb4c3a4b4aad0083b72c0fd04/AGENTS.md)、[対象差分](https://github.com/imksoo/routerd/compare/259d960ae3231286d7774747b6e6f1f5c51f30de...c8a06314c93d43fbb4c3a4b4aad0083b72c0fd04)、[実機確認の記録](https://github.com/imksoo/routerd/blob/c8a06314c93d43fbb4c3a4b4aad0083b72c0fd04/docs/plans/routerd11-shape-up.md)。
