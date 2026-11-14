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

- 2026-11-09、k8s担当がk8s-rt02・pve-rt07のIPv4 main tableと設定・動的状態を読み取り比較した。kernel・DHCP・BGP以外の表示はそれぞれ9件・11件で、k8s側の8件とpve側の2件はSAM endpoint設定、pve側の8件はSAM capture構成、両機の既定経路はDHCP適用済み状態と宛先が一致した。単時点の比較であり、変更後daemonの復元・通信は未確認。

- 2026-11-09、pve-rt01〜08とk8s-rt01・02の有効設定とIPv4 policy ruleをPVEゲストエージェントで読み取り比較した。各機はOS標準の3 ruleで、現行の撤去条件に該当するruleはなかった。homert02の14 ruleも、設定とTailscaleの動的ruleを考慮すると撤去対象はなかった。変更後daemonでのrule・table撤去は未確認。

- 2026-11-09、PVEゲストエージェントからpve-rt01〜08の実行中設定とカーネルを読み取り、各機のIPIP TunnelInterface 2件と実リンク2本の名前が一致した。k8s-rt02も設定8件と実リンク8本が一致した。変更後daemonでの維持・撤去は未確認。

- 2026-11-09、pve-rt01〜06・08は全機VM稼働中、稼働版 `v20261108.1523` のrouterdはactive・Healthyで、各機のBGP 2 peerはともにEstablished。k8s-rt02からpve-rt01〜08の各IPIP対向への単発pingも成功した。変更後daemonの通信と切替・復旧は未確認。

- 同日、pve-rt07はVM稼働中だがゲストエージェントが断続的に応答せず、現行CLIの転送・比較を中断した。PVEホストのルート領域は使用率29%、inodeは3%だったが、ゲスト内の容量とエージェント不調の原因は未確認。作業用ファイルは削除した。

- 同日、所有者の指示でpve-rt07（VM 169）を強制リセットした。再起動後はゲストエージェントとrouterdがactive、routerdはHealthy、BGP 2 peerがEstablishedで、IPIP 2本の対向へのpingが成功した。ゲストの `/` 使用率は容量2%、inode 1%。その後、ゲストエージェントの読み取りが再びタイムアウトした。不調の原因は未確認。

- 同日、homert02の実設定・実状態DBを使い、`0ae7dc6d` のCLIと稼働版の `dynamic render` を比較した。削除済みの所有権フィールドを除く構造は一致し、稼働版の比較前後の出力も一致した。変更後daemonによるapply・復旧は未確認。一時バイナリは削除した。

- 2026-11-09、k8s-rt02の実設定・実状態DBを使い、`a8c87aca` のCLIと稼働版の `dynamic render` を比較した。削除済みの `Interface.spec.owner` と `metadata.ownerRefs` を除く構造差分は0件で、稼働版の比較前後の出力も一致した。変更後daemonの通信・復旧は未確認。一時ファイルは削除した。

- 同日、pve-rt07でも `a8c87aca` による比較を試みたが、ゲストエージェントの応答が不安定だったため転送前に中止した。以前の出力差の原因は未特定。

- 2026-11-09、chattyのk8s担当がk8s-rt-01/02の実行中canonicalと再起動時の永続設定を読み取り比較し、YAML構造が一致した。以前確認されたDNSリソースの差は解消済み。再起動・復旧の実行は未確認。

- 2026-11-09、chatty側でpve-rt07の実設定・状態を使い `d082d024` のCLIと稼働版CLIを比較した。種類別リソース件数は同じだが、動的設定出力は完全一致しなかった。稼働版の `Interface` 2件には現行版で削除した `owner` があり、差分を説明し得るが、他の差や逐次読取中の状態変化は未確認。ゲストエージェントが断続的に応答しなかったため、k8s-rt02の比較は未実施。一時ファイルは削除した。

- 2026-11-09、chatty側でDHCPv4の実状態を確認した。pve-rt07の `mgmt-dhcpv4` とk8s-rt02の `lan-dhcpv4` は自然更新後にBound。pve-rt07の `svnet1-source` もBoundだが、参照可能な更新イベントはなかった。両機とも稼働版は `v20261108.1523` で、変更後daemonは未確認。

- 2026-11-09、chatty側からpve-rt07の2本とk8s-rt02の8本のIPIPトンネルを読み取り確認した。有効設定と実体の種類・対向・トンネル内IPv4アドレスが一致し、全件UP。各トンネルに対応する対向へのpingが1回ずつ成功した。切替と変更後daemonでの復元は未確認。

- 2026-11-09、homert02の稼働版と `2bd6d70b` のCLIで、実設定と状態DBから生成した動的設定のSHA-256が一致した。稼働中daemonはactive、BGP peer 2件はEstablished、LANのVRRPはmaster。chattyからゲートウェイとhomert02/03へのping、外部名の解決が成功した。稼働版 `6ac8fbdb` では、起動後の2026-11-09 11:54 UTCにPD更新イベントがあり、その後もPD Bound、BGP 2 peer Established、VRRP master、chattyからのゲートウェイ到達と名前解決を確認した。変更後daemonの適用・設定復元・切替は未確認。

- 2026-11-09、`ec183691` の `routerctl` を homert02 で読み取り実行した。実設定・状態から生成した動的設定は稼働版 CLI と一致し、routerd は active、管理ネットワークのゲートウェイへの ping は成功した。変更後 daemon の適用や WireGuard の鍵復元は確認していない。homert02 の `ens20` 管理アドレスは routerd の DHCPv4Client ではなく既存の netplan/networkd の DHCP から得ている。DS-Lite は各トンネルに設定の静的アドレスに加え point-to-point アドレスを持つ。IPv4 余剰撤去では実際の動的生成元を考慮し、管理 NIC の引き継ぎが決まるまで通信を損なう適用を行わない。

- pve-rt07 の実状態 DB にある SAM transport・RR の計8リソースについて、変更後の `routerctl dynamic diff` と稼働版の出力が一致した。読み取り確認であり、変更後 daemon の BGP 通信・VRRP フェイルオーバーを確認したものではない。

- pve-rt07 の設定全体の JSON 表示では、YAML コピーの削除によって空の SAMPeerGroup の `nodes` が `[]` から `null` に変わる回帰を発見し、修正した。同じ実状態 DB で修正後の表示全体は稼働版と一致した。

- k8s-rt02 の実状態 DB に対する変更後 `routerctl dynamic diff` は、稼働版と33行すべて一致した。稼働版 BGP は Established、14 peer だった。こちらも daemon の入れ替えは伴わない。

- Cloud SAM の稼働先は未特定。`routerd-labs/cloudedge-mobility/terraform/envs` 内の AWS・Azure・OCI の state はいずれも資源が空だったが、他の稼働先の有無を示すものではない。Cloud SAM で変更後の実動作は未確認。

- homert03 の実設定と状態 DB に対する変更後・稼働版 `routerctl dynamic render -o json` は SHA-256 が一致した。2026-11-09 に homert03、次いで homert02 の daemon と変更対象 helper を `6ac8fbdb` に交換した。両機 Healthy、BGP Established、homert02 master・homert03 backup を確認し、実クライアント chatty からの名前解決と外部への ping が成功した。強制的な VRRP 切り替えや DHCP/PD 更新は確認していない。

- k8s-rt02 の実設定と動的状態から `routerctl dynamic render -o json` で得た設定全体も、変更後 CLI と稼働版の SHA-256 が一致した。

- 2026-11-10、homert02のsystemd-resolvedは `lain.local` のDNSルートがなく、routerd DNSでは解決できる `syslog.lain.local` がOS経由では失敗していた。ens20のnetworkd設定に `Domains=~lain.local` を追加し、OS経由の解決を確認した。daemonを `4e8f199c` に交換し、同じ設定の `apply --replace` は generation 15959・Healthy、80リソースにError/Failed/Degradedなし。BGP 2 peerはEstablished、homert02はVRRP backup、PDはWhenFalseでPending。chattyからVIPと外部IPへのping、名前解決が成功し、VIPとhomert03のARPは同じMACを示した。homert03へのSSHはホスト鍵不一致のため状態を直接確認していない。SAM切替と手動変更からの復元は未確認。

- 2026-11-10、homert03のSSH鍵をPVE07のVM 158のゲストエージェント経由で照合し、eth2のnetworkd設定に `Domains=~lain.local` を追加した。OS経由の名前解決を確認し、`1694b89d` のdaemonを投入、同じ設定の `apply --replace` はgeneration 53532・Healthy、111リソースにError/Failed/Degradedなし。homert03はVRRP master・PD Bound、homert02はbackup・PD WhenFalseでPending、両機BGP 2 peer Established。chattyからVIPと外部IPへのping、名前解決が成功した。SAM切替と手動変更からの復元は未確認。

- 2026-11-10、変更後daemonをpve-rt01〜08とk8s-rt01・02に順次投入し、実設定の `apply --replace` は全機Healthyだった。pve-rt各機はBGP 2 peer Established、IPIP 2本UP、proxy neighbor件数は投入前後で一致し、各BGP対向へのpingも成功。k8s-rt両機はBGP 14 peer Established、IPIP 8本UP。設定を手動で変更した後の復元とSAM切替は未確認。k8s担当Codexへの依頼は接続エラーで届かなかったため、両機とも本作業で適用した。

- 2026-11-10、UTCリリース `v20261110.0354` のISOをSHA-256照合し、pve-rt01〜08の既存設定ディスクを維持して順次起動した。各機は同版でactive、BGP 2 peer Established、各対向へのpingと同一設定のapplyがHealthy。01・02では稼働中のLive ISO差し替えによりSquashFS読み取りエラーが生じ、リセットして復旧した。03〜08は旧ISOを維持したまま正常停止してから差し替えた。k8s-rt01・02にも公開版daemonを適用し、両機apply Healthy・BGP 14 peer Established。全VM設定を確認し、未参照の旧Live ISO 49本を削除、他VMが参照する旧ISOと設定メディアは維持した。SAM切替や手動変更からの復元は未確認。

## 残件

- 2026-11-10時点のHealth判定整理と実機での復元・切替・復帰は、[堅牢化の残作業](routerd11-hardening-remaining.md)にまとめる。
- FreeBSD で DHCPv6 client を supervisor を介さず `--lease-file` なしで手動起動すると、既定の lease パスと controller の読み取りパスが異なる。supervisor は正しいパスを渡す。実運用で手動起動が必要か未確認であり、調査・修正には別途承認を要する。

## 2026-11-14 の追加整理

基準 revision `1cd7cfc7a65603e9026e7683a46d524d1e7bfa23` から、簡素化開発計画の A01・A02・A03・A10・A11・A13 を実装した。具体型の設定参照、PD 選択の未使用引数削除、未使用 JSON 秘匿化と AST 台帳・同一型往復テストの撤去、version 単体ビルド、EventSubscription の説明と生成 schema の修正を対象とする。今回の確認結果と残件は [実装記録](routerd11-simplification-progress.md) に記す。コミット・push・本番操作は行わず、実設定の読取り比較・PD 通常更新・Web Console の権限別表示・実通信は残件である。過去の実機記録を今回の検証結果には含めない。
