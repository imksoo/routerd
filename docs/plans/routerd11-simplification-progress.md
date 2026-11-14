# routerd 1.1 簡素化の実装状況

基準: `1cd7cfc7a65603e9026e7683a46d524d1e7bfa23`。2026-11-14、使い捨てKubernetes Pod内の公式exec-serverを利用した。全実装・ビルドは使い捨てPodで行い、待機系への改修版導入・再起動・切戻しはhomert担当が行った。全計画は実装・実機確認の途中であり、未確認事項は未完了として扱う。

`a1c22a8`、`9b5aaff`、`38ab89d`、最終版`5e4bd58`は、`simplify/routerd11-all`へ公開済み。以下の段階別記録は当時の確認範囲であり、最終版の検証結果とは区別する。

## 第一段階

- applyの文字列キー選択器4関数と到達不能分岐を撤去し、具体型のフィールドを参照する。PDクライアント選択の未使用引数と架空のOS/profile試験軸を除去した。
- 未使用JSON秘匿化処理と、その唯一の利用元だったAST・手書き台帳テストを除去した。本番YAML秘匿化は維持する。
- daemon APIの同一型JSON往復とversion文字列の実装写しを撤去した。実際の操作文字列契約を守るTestCommandVocabularyは維持する。
- version確認はrouterd単体ビルドに絞り、通常daemonビルドでも既存のフラグを共用する。
- MTUの反射・フィールド名推測を現在対応する具体型へ置換した。既定値、Enabled、underlay、明示MTU付きBridge/VXLANを保持する。
- SAM取得時のRRSetと任意PeerGroupを一度具体化して後続へ渡す。不正direct peerで有効RRSetを捨てない。
- Runnerの保存先をSQLiteへ固定し、能力不明時の無処理成功を除去した。状態遷移イベント構築を共用し、履歴要否を宣言設定世代で保持する。状態・イベントの既存トランザクションと公開順序は維持する。
- EventSubscriptionの実装と異なる将来互換コメントと、生成schemaの説明を修正した。

### 第一段階で実施した確認

Pod内でLinuxのrouterd・routerctl、保存層利用helperのrouterd-eventdをビルドし成功。version確認スクリプトで実バイナリへのversion/revision注入確認が成功した。

既存テストのみを実行した。PDクライアント選択・既知NG組合せ・操作コマンド語彙、renderのMSS/RA関連3件、SQLiteの一覧失敗・Merge保持・世代とイベント保存が成功した。テスト追加・増補はしていない。schema再生成の差分は集約窓/待機の説明2箇所のみで、Webサイト側との一致も確認した。

第一段階の実機確認は読取りのみ。homert02/03とk8s-rt01/02・pve-rt01〜08の設定を確認し、旧IPsec綴りの使用はなかった。稼働unitに旧serve互換フラグもなかった。当時のhomertはCLI 6ac8fbdb、coreはv20261108.1523、leafはv20261110.0354で、第一段階の改修版ではない。

## 後続の実装

PDリース本体はobjects.statusトップレベルを単一正本とし、型付きの取得・保存と表示生成へ集約した。_leaseは時刻・状態等のメタデータのみ。通常保存と原子的イベント保存の両経路で同じ正規化を使い、読取り・保存の不正や失敗を隠さない。現在の版が生成し得る旧二表現は照合してから対象行の書込み時に旧コピーを除去する。実機の移行確認前に旧読取り枝まで削除はしない。

Linux IPIPのendpoint/TTL/MTU/アドレスの希望状態、実体観測、一致判定を共用した。変更操作はcontrollerに残す。未対応・観測失敗をHealthyに丸めず、GRE/FOU/GUEやFreeBSDの残る経路は実利用未確定のため一括削除していない。

旧serveの無視フラグ登録・警告と、IPsec旧綴りの正規化・専用ケースを撤去した。現行表記のIPsecと、生成・更新・撤去の本体は維持する。OS・事業者対応は、Cloudの実利用を確定せず削除しない。

### 後続実装時点の確認

この段階のLinux主要バイナリとFreeBSD向けrouterd/routerctl/DHCPv6 helperのクロスビルド、既存state関連テスト、IPsecの既存確認が成功した。取得済み実設定12台のvalidate、homert両機とk8s-rt02の実環境で一時バイナリによるcheckも成功した。checkはdry-runであり、稼働daemonの置換・実apply・リース移行の実証ではない。この実機check後に加えたPD不正メタデータ検査等の当時の最終差分はビルドと既存テストで確認したが、その時点では同じバイナリの実機確認は未実施だった。最終版の確認範囲は後述する。

## A04の完了監査で追加した整理

controller側に残るStoreの一覧・削除・状態参照能力の推測も撤去し、必要なメソッドを既存chain.Storeに明記した。保持期限の削除はeventedStoreからSQLiteへ直接委譲し、能力不足として省略される分岐をなくした。24時間の既存保持期間とdry-runの早期returnは維持する。新しい万能Storeやテストは作っていない。

### A04追加整理時点の確認

既存の実SQLiteによる保持期限テストと、Linux/FreeBSDのrouterd・routerctl・DHCPv6 helper・eventdビルドが成功。監査前の`9b5aaff`（当時の最終バイナリ）では、実設定12台のvalidateとhomert両機・k8s-rt02の実checkが成功した。この時点では追加整理後の稼働daemonへの反映と実機の保持期限削除は未実施だった。保持期限削除の実機確認は現在も残件である。

## 公開済み最終版と確認範囲

### 38ab89dでの設定読込み・比較

`38ab89d`では、実設定12台のvalidateが成功した。homert03の旧新版checkは80リソースの全フィールドが一致したが、状態はUnknownであり、Healthyの確認ではない。これは同版のcheck結果であり、`5e4bd58`での再実施や実applyの実証ではない。

### 5e4bd58の実装・Pod内確認

最終`5e4bd58`では、specAsの受入れを`T`、非nilの`*T`、実JSON入力の`map[string]any`に限定し、誤った内部型をJSON往復で変換する処理を撤去した。Podで4コマンドのビルドと、既存の`pkg/api`、`pkg/config`、`pkg/dynamicconfig/codec`テストが成功した。過去段階のFreeBSDクロスビルド等を、この最終版の確認結果としては扱わない。

### 5e4bd58の待機homert03限定確認

`~/homert02`の既存Codexが、最終版の4本を現在待機系のhomert03だけへ転送し、SHA照合、起動、再起動、旧版への切戻しを実施した。設定SHAは不変。DB・鍵を上書きせず、切戻し時も最新DBを維持した。主系はmaster／PD Bound、待機系はbackup／Standby、110リソースで、既存のpublic-dns／ntpのResolveFailedを維持した。

通信確認は管理GWへのpingのみ。実apply・削除・PD更新・SAM通信・Web権限の確認は行っていない。起動・再起動・切戻しの確認を、これらの動作確認や主系への最終版投入と混同しない。

### 世代表示の観測と未解決点

`status.generation`は53534から0となり、旧版へ切り戻した後も0だった。一方、SQLiteの読取りで`generations`と`sqlite_sequence`の53534、および`config_yaml`と稼働設定の完全一致を確認した。原因は未断定であり、世代消失とはしない。表示と保存状態の差の原因確認は残件である。

## 現在の残件

Cloud SAMの実稼働先と他OSの対応方式の確定、PD移行・通常renew・when・有効leaseを保持した旧版切戻しは未完了。実apply・削除と設定への復元、実機の保持期限削除、Web権限・秘密値非露出・実EventRuleの発火・SAM動作・対向通信も未完了である。待機homert03で確認した起動・再起動・切戻しを超える復帰・通信は、確認済みとしない。

過去段階のビルド、validate、checkや今回の限定確認を、未実施の実動作確認へ流用しない。計画全体の完了宣言ではない。

## 成果物保全と後片付け

以前の開発Pod、キャッシュ、一時ログは成果物の保全後、すべて削除済み。文書更新に使用したPodも、成果物の保全後に削除する。

## 利用者依頼による秘匿化テスト

2026-11-14、利用者の「じゃあテストしてよ」という指示で、`TestConfigViewsRedactSecretsWithoutChangingSources` を追加した。使い捨てPodで本物のWebConsole handlerをHTTP提供し、実ファイルとSQLiteの2世代を用いて検証した。実装から期待値を生成せず、無害なpassword/privateKey/tokenの確認用文字列が現行設定・両世代・text/JSON差分に露出せず、設定中では `***REDACTED***` となること、公開項目と差分の方向が維持されること、元ファイルと保存YAMLがバイト単位で不変なことを確認した。

`go test -p 2 ./pkg/webconsole -run ^TestConfigViewsRedactSecretsWithoutChangingSources$ -count=1 -v` が成功。モック・偽のStore/応答・ルーターネットワークの代替環境は使っていない。本番設定・実際の秘密情報・実機の役割は変更していない。この結果は秘匿化の振る舞いの確認であり、本番の利用者別認可を検証したものではない。

## 再起動後のstatus世代0の修正

原因は、serve起動時に空のresultCacheへGeneration未設定のObserve結果を入れ、DBの適用済み設定世代を復元していなかったことだった。起動時に最新保存世代がHealthy/Appliedで、保存設定あり、現行の宣言設定と有効設定の既存ハッシュが一致するときだけ世代を復元する。過去の成功へ遡る処理や最新番号の無条件補完は追加せず、既存観測の世代も上書きしない。DB・設定読込み・ハッシュ計算のエラーは起動元へ返す。

利用者依頼に基づき、使い捨てPodで実SQLiteを閉じて再オープンし、実Observe・cache・status出力を使うTestInitialGenerationRestartを追加。正常復元、後続観測での保持、失敗/Committed/未完了世代、設定/有効設定不一致、元観測不変、実DB等のエラー伝搬の13ケースが成功。CGO無効のrouterd/routerctlビルドも成功。新たなDB/schemaやモック・疑似ネットワークは追加していない。実機の同一修正確認は別途記録する。

初回877d206は実待機側の起動・再起動で世代0のままであり、修正成功ではなかった。apply側はrouterWithIPv6PDClientOptionsによる既定値確定後に世代ハッシュを保存するが、初回の復元側とテストfixtureは確定前の設定を使っていた。fixtureを実applyと同じ経路へ直すと正常ケースが失敗し、不備を再現できた。復元側も設定原文確定後に既存の既定値処理を適用してからハッシュ照合するよう修正した。

既存13ケースとPD client未指定の実用ケースを含む14ケース、CGO無効ビルドが成功。埋込み秘密値が検出されなかった実機設定で一時診断を行い、既定値確定前のハッシュは保存53534と不一致、確定後は一致することも確認した。診断用の入力・コードは恒久テストや成果物へ含めず削除した。設定再apply、ハッシュ不一致の無視、新しい互換分岐で症状を消していない。
