# routerd 1.1 簡素化の実装状況

基準: `1cd7cfc7a65603e9026e7683a46d524d1e7bfa23`。2026-11-14、使い捨てKubernetes Pod内の公式exec-serverを利用した。全計画は実装・実機確認の途中であり、完了宣言ではない。

## 第一段階

- applyの文字列キー選択器4関数と到達不能分岐を撤去し、具体型のフィールドを参照する。PDクライアント選択の未使用引数と架空のOS/profile試験軸を除去した。
- 未使用JSON秘匿化処理と、その唯一の利用元だったAST・手書き台帳テストを除去した。本番YAML秘匿化は維持する。
- daemon APIの同一型JSON往復とversion文字列の実装写しを撤去した。実際の操作文字列契約を守るTestCommandVocabularyは維持する。
- version確認はrouterd単体ビルドに絞り、通常daemonビルドでも既存のフラグを共用する。
- MTUの反射・フィールド名推測を現在対応する具体型へ置換した。既定値、Enabled、underlay、明示MTU付きBridge/VXLANを保持する。
- SAM取得時のRRSetと任意PeerGroupを一度具体化して後続へ渡す。不正direct peerで有効RRSetを捨てない。
- Runnerの保存先をSQLiteへ固定し、能力不明時の無処理成功を除去した。状態遷移イベント構築を共用し、履歴要否を宣言設定世代で保持する。状態・イベントの既存トランザクションと公開順序は維持する。
- EventSubscriptionの実装と異なる将来互換コメントと、生成schemaの説明を修正した。

## 実施した確認

Pod内でLinuxのrouterd・routerctl、保存層利用helperのrouterd-eventdをビルドし成功。version確認スクリプトで実バイナリへのversion/revision注入確認が成功した。

既存テストのみを実行した。PDクライアント選択・既知NG組合せ・操作コマンド語彙、renderのMSS/RA関連3件、SQLiteの一覧失敗・Merge保持・世代とイベント保存が成功した。テスト追加・増補はしていない。schema再生成の差分は集約窓/待機の説明2箇所のみで、Webサイト側との一致も確認した。

実機は読取りのみ。homert02/03とk8s-rt01/02・pve-rt01〜08の設定を確認し、旧IPsec綴りの使用はなかった。稼働unitに旧serve互換フラグもなかった。homertはCLI 6ac8fbdb、coreはv20261108.1523、leafはv20261110.0354で、今回の改修版ではない。

## 残件

PD保存の単一正本化、IPIP観測統合、旧互換処理の判断と整理を継続中。改修版による実設定比較、実apply・削除、通常更新、秘密値非露出、実EventRuleの発火、復帰と通信は未実施。Cloud SAMの実稼働先は特定できていない。過去の記録やビルド成功を今回の実動作証拠として流用しない。

Podと共有キャッシュは他項目の実装中のため稼働を継続している。成果物の保全と公開後に、一時環境と作業用データを整理する。
