# routerd 1.1 型整理・簡素化計画

対象は homert、k8s-rt、pve-rt、Cloud SAM の実設定と、その動的生成、更新、復旧、撤去に必要な経路とする。以下を順に進める。不要な汎用機構、将来要件、新規テストは追加しない。各段階で呼び出し元を追い、小さく変更し、ビルドと実際の設定・対象で可能な確認を行う。製品の配布・実機入れ替えは別途の許可を要する。

1. BGP peer の観測と経路数集計を型付きのまま処理し、外部表示に必要な変換を境界に寄せる。実際の BGP/SAM の経路と状態表示を維持する。
2. 設定の読み込み後に型を崩して再変換する経路について、YAML、動的設定、plugin とその利用元を確認し、不要な往復を削る。利用中の設定の意味を維持する。
3. DHCP/PD、BGP、SAM など実際に内部参照する状態を対象に、汎用 map の往復を必要な型付き表現に整理する。CLI、when、既存の保存状態と動的更新・復旧の意味を維持する。
4. 実際の生成元・接続・停止・復旧をたどり、実装を切り替える必要のないインターフェースとテスト専用注入口を除去する。実運用の依存は維持する。
5. 残ったテストを内容と呼び出し経路に照らして吟味し、モック、実装追従、不要な条件と専用補助コードを除去する。残すテストのための新規基盤や追加テストは作らない。

各段階の結果と未確認事項はコミットに記す。計画外で対応したいことは、以下の残件に記載して最後に説明し、所有者の承認前には着手しない。

## 実施状況

- 1: BGP 動的 peer の観測・経路数を型付きで集計し、保存 status の項目を維持。過去 peer は履歴に必要な項目だけを復元する。
- 2: 動的設定合成時の YAML 往復を除去。SAM の JSON API は境界で map を受け取る実経路があるため、必要な型変換を維持。
- 3: egress 候補と SQLite 状態値を型付きにし、SAM 適用済み状態の復号先も型で固定。使用する数値・文字列の保存形式を保持。
- 4: BGP・SAM・LAN PD・VRRP・DNS の未使用の注入口や interface を除去。複数 OS の FIB や稼働中の observer 接続は残す。
- 5: 実装追従、偽装時刻・NIC、仮定した旧 DB、名前走査のテストと専用補助を削除。計画開始時比で Go テスト 1,363 行削除、追加なし。

変更対象の BGP、BFD、SAM、chain、dynamicconfig、egress、state 利用側、HealthCheck、plugin、control API と対象 command のチェック、および対象 Linux build と DNS の FreeBSD build は成功。homert の変更後 daemon は両機で起動した。SAM 更新・PD 更新・VRRP handoff は未確認である。

## 実機確認の記録

- 2026-11-09、`ec183691` の `routerctl` を homert02 で読み取り実行した。実設定・状態から生成した動的設定は稼働版 CLI と一致し、routerd は active、管理ネットワークのゲートウェイへの ping は成功した。変更後 daemon の適用や WireGuard の鍵復元は確認していない。homert02 の `ens20` 管理アドレスは routerd の DHCPv4Client ではなく既存の netplan/networkd の DHCP から得ている。DS-Lite は各トンネルに設定の静的アドレスに加え point-to-point アドレスを持つ。IPv4 余剰撤去では実際の動的生成元を考慮し、管理 NIC の引き継ぎが決まるまで通信を損なう適用を行わない。

- pve-rt07 の実状態 DB にある SAM transport・RR の計8リソースについて、変更後の `routerctl dynamic diff` と稼働版の出力が一致した。読み取り確認であり、変更後 daemon の BGP 通信・VRRP フェイルオーバーを確認したものではない。

- pve-rt07 の設定全体の JSON 表示では、YAML コピーの削除によって空の SAMPeerGroup の `nodes` が `[]` から `null` に変わる回帰を発見し、修正した。同じ実状態 DB で修正後の表示全体は稼働版と一致した。

- k8s-rt02 の実状態 DB に対する変更後 `routerctl dynamic diff` は、稼働版と33行すべて一致した。稼働版 BGP は Established、14 peer だった。こちらも daemon の入れ替えは伴わない。

- Cloud SAM の稼働先は未特定。`routerd-labs/cloudedge-mobility/terraform/envs` 内の AWS・Azure・OCI の state はいずれも資源が空だったが、他の稼働先の有無を示すものではない。Cloud SAM で変更後の実動作は未確認。

- homert03 の実設定と状態 DB に対する変更後・稼働版 `routerctl dynamic render -o json` は SHA-256 が一致した。2026-11-09 に homert03、次いで homert02 の daemon と変更対象 helper を `6ac8fbdb` に交換した。両機 Healthy、BGP Established、homert02 master・homert03 backup を確認し、実クライアント chatty からの名前解決と外部への ping が成功した。強制的な VRRP 切り替えや DHCP/PD 更新は確認していない。

- k8s-rt02 の実設定と動的状態から `routerctl dynamic render -o json` で得た設定全体も、変更後 CLI と稼働版の SHA-256 が一致した。

## 残件

- FreeBSD で DHCPv6 client を supervisor を介さず `--lease-file` なしで手動起動すると、既定の lease パスと controller の読み取りパスが異なる。supervisor は正しいパスを渡す。実運用で手動起動が必要か未確認であり、調査・修正には別途承認を要する。
