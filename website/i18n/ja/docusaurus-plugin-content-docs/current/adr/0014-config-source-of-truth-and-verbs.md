# ADR 0014: 設定の正本と CLI verb

## ステータス

提案（2026-06-07）。

設定の永続化モデル、candidate/commit ライフサイクル、`routerd` / `routerctl` の
コマンドサーフェスを定義する。`routerd` の場当たり的な verb 増殖を置き換え、
削除、履歴、ロールバックを既存の SQLite generations と整合させる。

## 背景

routerd はディスク上の `router.yaml` をオペレーター入力と起動時に reconcile される
状態の両方として扱っている。この混同が具体的な欠陥を生んだ：実行時にリソースを削除しても
再起動後に残らない。

- `routerctl delete` はホスト成果物、所有権台帳エントリ、オブジェクトステータスを削除するが、
  `router.yaml` は**編集しない**。
- `routerd serve` は起動時に `router.yaml` を読み込み、desired state として reconcile する。
- apply/serve の孤立 GC は `router.yaml` で宣言されたリソースと比較するため、
  ファイルに残っているものは「desired」であり再作成される。

したがって、起動設定にまだ存在するリソースの `delete` は、次回の起動や apply で元に戻る。

2 つの業界モデルを検討した：

- **DB を正本とする（Cisco running-config、Kubernetes etcd）。** mutation はストアに
  入り、ファイルは入力。これにより命令的 delete が永続化されるが、routerd にとっては、
  プロダクトの中核であるプレーンテキスト、コメント付き、バージョン管理可能、ポータブルな
  設定を犠牲にする（`cat` による監査、1 ファイルコピーによる災害復旧、
  アップグレード時のスキーマ書き直し、ディスクレス USB 永続化）。
  startup-config/running-config の分離も必要になる。
- **ファイルを正本とする、candidate/commit（VyOS/Junos）。** 人間が読める設定が
  永続化された真実。`set`/`delete`/`commit` で candidate を構築し、
  `commit` がアトミックにバリデーションと適用を行い、履歴/ロールバックが組み込まれる。

プレーン GitOps はターゲットとして却下した：Git が名目上は正本だが、apply に失敗した
ファイルでも Git 上では宣言された状態として残るため、記録の真実と現実が黙って乖離する。
採用したモデルは、正本を「最後に正常に apply された設定」とし、
トランザクショナルな commit でゲートすることでこれを修正する。

CLI サーフェスも意図ではなく実装によって成長していた：

- `routerd` は 11 の verb を持っていた（validate / check / observe / plan / adopt /
  render / apply / rollback / delete / serve / run）。「適用せずに見る」verb が
  5 つ重複し、未実装の `run` スタブがあり、`apply` に必須の `--once` がオプショナルに
  見えた。
- `routerctl` は約 28 の verb を持っていた。4 つの重複する検査 verb
  （get / status / show / describe）がデータソース（設定ファイル / status ソケット /
  状態ストア）のみ異なり、6 つのトップレベルランタイムデータテーブルダンプ、
  2 つの診断 verb（doctor / diagnose）があった。

## 決定

### 1. 正本

単一の正本は 1 つの人間が読める正規の `router.yaml` ファイルである。routerd は
真実を不透明なデータベースに移さない。

- 正本は**最後に正常に apply された**設定。バリデーションまたは reconcile に
  失敗した設定は正本にならない。
- コメントと順序は、コメント保持 YAML ラウンドトリップ（yaml.v3 `Node`）を使って
  マシン mutation を通じて保持される。
- 各成功 apply はアトミックに正規ファイルを書き込み（temp + fsync + rename）、
  generation のスナップショットを取る。履歴とロールバックは既存の SQLite generations を
  再利用する。新しい履歴メカニズムは導入しない。
- 起動時、`serve` は正規設定を読み込む。バリデーションに失敗した場合、serve は
  last-good のコミット済み generation を reconcile し、起動拒否や壊れたファイルを
  正本とするのではなく、大きな警告を出す。

### 2. バイナリ分離

- **`routerd` は daemon/エンジン。** systemd unit は `routerd serve` のみを実行する。
  `serve` は単一の converge-and-exit を実行する
  （起動テスト、CI、ドリフト修復）。ブートストラップとリカバリは
  `routerd serve --config <initial.yaml>` で正規ファイルをシードする。
- **`routerctl` はオペレーター CLI**（kubectl 相当）。設定ライフサイクルと検査 verb を
  所有する。mutation verb は制御ソケット経由で稼働中の daemon と通信する。
  daemon が特権的な正規書き込み、reconcile、generation スナップショットを実行する。

### 3. 設定ライフサイクル verb（`routerctl` 上）

- `validate [-f <file>]`：静的スキーマの妥当性。ホスト変更なし。
- `plan [-f <file>]`：差分のプレビュー。ホスト変更なし。
- `apply -f <file>`：正規ファイルを mutation し reconcile する。**入力必須。**
  - デフォルトは**部分 upsert**（入力内のリソースを追加または更新。他のリソースは
    そのまま）。部分 `delete` と対称。
  - `--replace` は正規ファイルを入力と完全に等しくする（存在しないリソースは prune）。
  - **`add` verb はない**：追加には本体が必要なので、フラグメントの `apply`。
    `delete` のみが独自 verb を必要とする。不在はドキュメントとして表現できないため。
  - `serve` が稼働中のとき、apply はデフォルトで即座に reconcile する。
    `--no-reconcile` は書き込みのみ。serve が稼働していないとき、`routerctl apply` は
    エラーで `routerd serve` を指示する。
- `delete <kind>/<name>`：正規ファイルからのアトミックな部分削除の後 reconcile。

入力規約：`-f <file>` はファイルを読み、`-f -` は stdin を読み、`-f` を省略すると
現在の正規ファイルを対象とする（`validate`/`plan` はライブの正本で動作）。
`apply` は明示的な入力を要求。`validate` と `plan` は非特権（読み取り）。
`apply` と `delete` は特権的で、制御ソケットアクセスでゲートされる。

### 4. 検査とランタイム verb（`routerctl` 上）

- `get` / `status` / `show` / `describe` を 2 つに統合：
  - `get [kind[/name]] [-o yaml|json|table]`：マシンリーダブル。spec と status を
    subject でマージ。
  - `describe <kind>/<name>`：人間が読める詳細（spec、status、conditions、
    最近のイベント、関連ランタイム）。
  - `status` と `show` は削除。それらのビューは `get`/`describe` に統合。
  - すべての検査は稼働中 daemon の制御 API に問い合わせ、verb ごとのデータソース
    切り替え（旧来の混乱の原因）を停止。
- 6 つのランタイムデータテーブルダンプ（`events`、`ledger`、`dns-queries`、
  `connections`、`traffic-flows`、`firewall-logs`）を `get <subject>` に統合。
- 診断を `doctor` に統合。アクティブプローブを `doctor --probe <subject>` に移動
  （`diagnose` を吸収）。
- ドメインサブツリー（`firewall`、`dynamic`、`mobility`、`plugin`、`action`、
  `federation`）は維持し、`get`/`describe` スタイルのサブ verb を使用。
  `wireguard` と `tailscale` は `vpn` サブツリーに移動。`firewall-logs` は
  `get firewall-logs` に。
- ランタイム制御：`drain`/`undrain` は `ingress` の下に移動。
  `restart-dns-resolver` は `restart <daemon>` に汎用化。`set-log-level` は
  `log-level` に。
- `version` と `help` は変更なし。

### 5. `routerd` からの削除または移動

`check`、`observe`、`render`、`adopt`、未実装の `run` は削除または統合
（`check`/`observe`/`render` は `plan` に、`adopt` は `routerctl` に）。
`apply` は必須の `--once` を失う。`rollback` は `routerctl` に移動。

### 6. パーミッション

正規の `router.yaml` は world-readable だが、書き込みは root/`routerd` のみ
（秘密は `SecretValueSource` 経由で外部に保持）。制御ソケットは `0660 root:routerd`
で、読み取り verb は任意のユーザーで動作し、mutation verb はソケットメンバーシップで
ゲートされ、特権 daemon が実行する。

## 結論

- `delete` と `apply` は、正規の正本をコミットで書き換えるため、構造的に
  再起動をまたいで永続化される。
- apply に失敗した設定は稼働中の正本にならない。起動時は last-good にフォールバック。
- verb サーフェスが縮小し、データソースによる重複が解消される。
- 制御 API に apply/plan/delete/validate の mutation を追加する必要がある。これが主な実装コストとなる。
- 破壊的変更は許容される（ユーザー 1 名、後方互換 shim なし、プロジェクトポリシーに従う）。
  設定は新しいモデルに書き直す。

## 実装計画（ゴール）

- **Phase 1：コミットコア。** daemon 内の正規ライター：yaml.v3 ラウンドトリップ
  （コメント/順序保持）、アトミック書き込み、成功 apply 時の generation スナップショット、
  `serve` の last-good 起動フォールバック。
- **Phase 2：制御 API mutation。** 制御ソケット API に apply/plan/delete/validate を
  追加。ソケットパーミッションモデル付き。
- **Phase 3：verb 移動。** `routerctl` が validate/plan/apply/delete
  （daemon 経由）を獲得。upsert デフォルト/`--replace`/入力必須付き。`serve`。
  `routerd` を serve 専用にトリム（check/observe/render/adopt/run の削除/移動、
  必須 `--once` の削除、rollback を routerctl に移動）。
- **Phase 4：検査統合。** get/status/show/describe を制御 API 上の
  `get`+`describe` にマージ。6 つのデータテーブルダンプを `get <subject>` に統合。
  `diagnose` を `doctor --probe` に吸収。
- **Phase 5：ドメインと制御の整理。** wireguard/tailscale の `vpn` サブツリー、
  `restart <daemon>`、`ingress drain/undrain`、`log-level`。
- **Phase 6：ドキュメントとマイグレーション。** チュートリアル/how-to/リファレンスと
  サンプル設定を新しいサーフェスに更新。非推奨 verb の削除。
