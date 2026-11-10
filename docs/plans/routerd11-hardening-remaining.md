# routerd 1.1 堅牢化の残作業

2026-11-10。基準は main `ce55cfc4` と公開版 `v20261110.0354`。
1.5で目指した改善のうち、現在の1.1に残る重複処理と、実証していない復元・切替・復帰を扱う。
この文書の追加では製品コードや実機を変更しない。

## 方針と範囲

[AGENTS.md](../../AGENTS.md)、[1.1 MVP](../design/routerd11-mvp.md)、[MVP2の後続指示](../design/routerd11-mvp2.md)と、所有者の最新指示に従う。現リポジトリを少しずつ改善する。1.5は再開せず、2.0リポジトリはアーカイブのまま扱う。

対象は homert02・03、pve-rt01〜08、k8s-rt01・02と実際の相手機器。Cloud SAMは対象外。各確認には、その機能と切替構成が実際に使われている対象を選ぶ。全機で全機能の組合せを試す計画にはしない。

コンフィグを正本として適用し、OS基盤と有効なDHCP/PD/BGP/SAM等の動的状態を維持する。所有権・作成履歴・設定前の集合・自動importは追加しない。SAMの削除済み所有履歴も復活させない。FreeBSDのOS管理DHCPは設定外アドレス・経路の照合対象に含めない。

コード変更は重複と不要な分岐の削減を基本とし、確認した問題の修正に絞る。新しいDB形式、イベント形式、汎用復旧エンジン、テストコード、モック、模擬環境、専用検証基盤は追加しない。

## 確認済みの出発点

[実施記録](routerd11-shape-up.md)では、homert両機の通信・DNSと同一設定のapply、pve-rt全8台の公開ISO起動・BGP・対向通信、k8s-rt両機の公開版適用・BGPを確認した。動的設定のYAMLコピー、不要な所有履歴や注入口の削除、型の整理も実施済みである。

これらは通常運用・更新・再起動の確認であり、手動変更からの復元や意図的な上流断・VRRP/SAM切替の実証とは区別する。DHCP/PDの更新には過去の観測があるが、今回の変更後の切替・復帰まで確認したものではない。

## 作業順序

### 1. Health判定の重複を減らす — 実施済み

[経路選択](../../pkg/egressroute/selection.go)と[PBR](../../pkg/controller/chain/policyroute.go)で重複しているHealth観測の読み取り、失敗閾値、有効期限の判定を小さく共通化する。現在のStore/API境界を使い、内部で必要な具体型へ一度変換する。同じ観測と基準時刻なら同じ判定になるようにし、既存の閾値・期限の意味を維持する。置き換えた解析・判定処理を削除する。

経路・nftablesの適用成功後に結果を公開する現在の順序と、OS操作・保存失敗の報告を維持する。新しい状態保存や再試行の仕組みは作らない。

完了条件は、重複した判断がなくなり、対象のビルドが通り、使用中のhomertで経路選択・カーネルの経路/nftables・実クライアント通信が整合すること。障害時の確認は第3段階で続ける。

### 2. 同じ設定のapplyによる復元を確かめる — 使用中の対象で確認

現在使用中の対象で、設定された実体の変更・削除、設定外の追加、生成ファイルの変更からの復元を、一つずつ確認する。アドレス・経路・policy rule、Bridge、WireGuard、VXLAN、トンネル、SAM proxy設定は、実設定で使用するものを選ぶ。WireGuardを使用する対象では、設定と鍵ファイルを変えずにカーネルの鍵が変わった場合も含める。未使用機能を試すための構成は追加しない。

完全な設定ファイルに対する `routerctl apply -f <設定ファイル> --replace` の後、実体が設定と有効な動的状態に一致し、対向通信・DNS・管理接続が使えることを確認する。設定から対象を削除した際の撤去も、実運用の対象で確認する。現在のコンフィグに従う集合管理を使い、以前の設定の保存や所有記録は加えない。

完了条件は、確認した相違がapplyで解消され、必要な動的状態が残ること。Healthy表示だけで完了としない。失敗を見つけたら、その適用経路を小さく直して同じ実機で再確認する。

### 3. 上流断・復帰とHealth helperの再起動を確かめる — 一部実機確認

homertの実際のWANと実クライアントを使い、一方の上流を止めた時の代替経路への切替と、戻した時の設定どおりの選択を確認する。Health helperの再起動も別に行い、古い観測だけで経路を選び続けず、新しい観測に基づく通常動作へ戻ることを確認する。

完了条件は、Health観測、選択結果、カーネルの経路/nftables、実通信が一致すること。設定・プロトコルにある期限で判断し、独自の短時間合格条件は設けない。

### 4. homertのVRRP切替と動的設定の復帰を確かめる — 一部実機確認

homert02・03の現在のmaster/backupを確認し、master側を停止してbackup側への引き継ぎを確認する。復帰後の役割は現在の優先度・preempt設定に従う。VIP、経路、DNS、実クライアント通信と、役割に依存するDHCP/PDの取得・通常更新を確認する。VRRP遷移では旧masterのRA router lifetimeを0にして既定ルーター広告を撤去する一方、現在の委譲global addressはdown状態のVMACにstagedとして残す実装である。これはPD prefix変更による旧prefixのdeprecated広告とは別の処理である。prefix変動を人為的に発生させる試験は、所有者指示により今回の対象外とする。自然に観測できた状態は記録するが、変動発生を待つことは完了条件にしない。

完了条件は、設定に従う役割で必要な通信と動的更新が再開し、不要になったVIP・経路が各機能の規則に従って撤去されること。動的な取得値を静的YAMLへ書き込まない。

### 5. pve-rt・k8s-rtのSAM切替と復帰を確かめる — 片系で実機確認済み

現在のSAM構成から、対象のアドレス、転送経路、切替先と実クライアントを確定する。該当する稼働ノードまたは実経路を一つ停止し、復帰させる。既存の構成で必要な切替について、BGP広告、トンネル、proxy neighbor、選択されたアドレスへの実通信を前後で確認する。

完了条件は、設定と現在有効なSAM状態に従って通信先が切り替わり、復帰後に不要な広告・経路・proxy設定が残らないこと。Cloud SAMの確認や新しい切替方式は含めない。

## 進め方と完了判定

各段階は小さく変更・ビルドし、既存の実対象へ進む。実機の停止や設定変更は一度に一対象とし、管理接続とPVEコンソール等の復旧経路を確認して行う。使用中の設定、鍵、必要なリース・動的状態を保つ。

結果はこの文書に対象・revision・操作・実通信の結果・未確認事項を短く追記する。修正は差分を確認してcommit・publishし、公開後に作業用バイナリ、ログ、一時ファイルと作業用キャッシュを削除する。生ログをソースへ追加しない。

上記の実装整理と、使用中の構成に必要な復元・切替・復帰の確認が終われば、この残作業を完了とする。実施できなかった確認は理由とともに未確認として残す。1.5の旧計画全項目の移植や、新しい機能・網羅的試験を完了条件に加えない。

## 実施状況

- 2026-11-10、`dda28462` でHealth判定を共通化し、`routerd`・`routerctl` をビルドした。homert02に投入し、backup維持、同一設定apply Healthy、IX2215・HGWのHealth、待機側の経路・rule・nftablesを確認。chattyからのHTTPS・DNSはmasterのhomert03経由で成功した。変更版による実転送と切替は未確認。
- 同日、homert02で `internet-via-ix2215` のHealth helperを再起動し、再起動後の新しい成功観測とHealthyへの復帰を確認した。待機側候補はこのHealthCheckを直接参照せず、Healthに応じた経路切替の確認ではない。
- 同日、pve-rt08でSAM proxy neighbor、IPIPのMTU、endpoint経路を変更または削除し、同一設定apply後に復元を確認。k8s-rt02でもIPIPのMTUがapply後に復元し、BGP 14/14、IPIP 8本、API VIPのHTTP 200を確認した。常時reconcileとapplyの寄与は分離していない。
- 同日、pve-rt08の片系IPIP停止中にhomert02→03の実経路がもう一方へ移り、ICMPは往復とも成功。復帰後はBGP 2/2、2経路、両IPIPとproxy neighborが戻った。連続監視ではなく無停止を示さない。既存のSAM `.0/32` 到達性WARNは残る。
- 同日、homert03を停止し、変更版を使うhomert02へのVRRP切替を確認した。homert02でVIP・PD Bound・RA/DHCP Applied、DS-LiteのHealth・経路・nftablesを確認し、chattyからHTTPS・DNSが成功した。homert03復帰後は設定のpreempt無効に従いhomert02 master・homert03 backup。切替直後は管理SSHが一時タイムアウトし、通信断の長さやDHCP/PDの更新自体は確認できなかった。
- 同日、homert03にも `dda28462` を投入し、backup維持と同一設定apply Healthyを確認した。masterのhomert02で使用中のDS-Lite Health helperを再起動し、新しい成功観測を確認した。
- 同日、homert02のDS-Lite一経路の実パケットを一時遮断した。HealthはUnhealthyとなり、新規フロー向けnftablesの振り分けから対象マークが外れ、他の2経路とchattyのHTTPS・DNSは動作した。遮断解除後はHealthがHealthyに戻り、3経路の振り分けが復帰した。上流機器そのものの停止は行っていない。
- 同日、homertの実設定履歴 `6f0ba0b` を確認した。`internet-via-ix2215` はHealth監視用route policyにあり、`ipv4-default` の `ix2215-static` からは意図的に参照を外している。この通常候補はHealth失敗による選択解除を行わないため、上流停止時の自動切替を実証した扱いにはしない。現設定の変更は行っていない。
- 同日、k8s-rt02のSAMトンネルの動的IPv4アドレスを削除し、同一設定のapply後に復元と対向通信を確認。pve-rt08ではMSS生成ファイルを変更し、apply後に元の内容とカーネルの規則を確認した。BGPは各々14/14・2/2、SAM経路・proxy、端末間通信、API VIPも確認した。常時reconcileとapplyの寄与は分離していない。
- 同日、待機側homert03の静的IPv4アドレス、Health監視用の経路・policy ruleをそれぞれ削除し、同一設定apply後に復元を確認。sysctl生成ファイルの変更も元に戻り、実行時sysctl、Health、主従とchattyからの通信は正常だった。通常reconcileとapplyの寄与は分離していない。
- 同日、homert03に設定外のIPv4アドレス、main経路、fwmark policy ruleをそれぞれ一時追加し、同一設定apply後に撤去を確認。既存アドレス・経路・rule、管理DHCP、VPN、主従とchattyの通信は維持された。通常reconcileとapplyの寄与は分離していない。
- 同日、homert03に送信元prefix指定の余剰ruleを追加し、apply後に撤去を確認。待機側のNTP向け静的経路を完全設定から一時的に除き、`--replace` applyで撤去された後、元の完全設定を再applyして経路を復元した。両applyはHealthy。
- 同日、homert02のDHCPv6-PD helperの通常renewを実行し、Reply・更新イベント、同じprefixのBound、委譲アドレス・DS-Lite経路とchattyの通信を確認した。chattyのDHCPv4は再度のrenew時刻とサーバー側のREQUEST・ACKが一致し、アドレスと通信を維持。lease期限の延長は確認できなかった。pve-rt03ではsvnet側DHCPv4の通常renewで送信・lease Boundを確認し、アドレス、SAM経路・BGPと対向通信は維持された。k8s-rt両台でも公開版daemon投入後の自然更新イベントを確認したが、稼働中helperと公開版のビルド一致は未確認。
- 最終確認時、homert02はmaster、homert03はbackupで、IPv4 VIPはmasterのみ。backupのDOWN状態のVMACには現在有効なprefixのIPv6アドレスがstagedとして残り、RAはPendingで、該当IPv6経路はなかった。backupのPD leaseはBoundかつ未期限切れで、実装は有効なleaseから待機側へ事前設定する。異なる旧prefixは観測されなかった。prefix変更や期限切れ後の撤去は実機未確認。
- 追加のソース調査で、PD prefix変更時にDS-Lite用 `/128` が旧leaseの期限付き処理から除外され、旧アドレスが無期限に残り得る経路を確認した。`c179c950` で除外条件を削り、有効な旧leaseの期限に基づく既存の処理を使う。homert03・02へ順次投入し、両機の同一設定apply Healthy、主従、PD、Health、経路、chattyのHTTPS・DNSを確認した。実際のprefix変更・失効は発生しておらず、その条件での撤去は実機未確認。
- 同日、homert02で非選択の `ds-lite-ra` のMTUを1450に変更し、完全設定のapply後に設定値1454への復元を確認した。通常reconcileとapplyの寄与は分離していない。主従、PD、4経路のHealth、chattyのHTTPS・DNSは正常だった。homert両機とpve-rt・k8s-rt全10台の現行設定と実体には、routerd管理のBridge・WireGuard・VXLANは確認されなかった。
- 同日、実機12台に旧networkd drop-inと旧単独eventd unitがないことを確認し、`ef2edb58` でその移行処理を削除した。homert両機に適用し、主従、現行adoption、PD、Health、HTTPS・DNSを確認。pve-rt03では変更版を一時起動し、EventGroupの7 peerへの配信、BGP 2/2、SAM経路・proxyと対向通信を確認後、公開版へ戻した。既存のSAM `.0/32` WARNは残る。
- 2026-11-10 22:56 UTC、homert担当Codexが現行設定・実体を再確認。両機は `f4da703f` ビルド、homert02 master・homert03 backup、BGP各2対向 Established。`dslite-pd-balanced` のHealthと経路表110～112・nftables 3 mark・実クライアント通信は一致。DS-Lite経路は同じWAN物理側を共有し、IX2215の通常候補はHealthを参照しない。上流機器停止と再VRRP切替は独立した復旧経路を確認できず実施しなかった。前回の一経路パケット遮断によるHealth切替・復帰は既に確認済みだが、上流機器そのものの停止は未確認。現在のPD prefixは `2409:10:3d60:1220::/60`、previousPrefixesは空。prefix変動試験は所有者指示により今回の対象外とし、変動・失効後の撤去を確認する試験は行わない。VRRP直後の管理断時間も未測定。今回の状態確認のみではこれらの条件を実証していない。
- 2026-11-10 23:04 UTC、既存 `~/k8s` Codexがpve07 / VM 153上のk8s-rt-02を約56秒一時停止し、PVEからの自動復帰を確認。停止中は全8 leafがBGP 1/2へ収束し、対象経路・proxyとhomert02↔03のICMP 3/3・TCP、API VIP HTTP 200を確認。復帰後、全8 leafがBGP 2/2、k8s-rt-02が一時10/14から14/14へ自然収束し、両IPIP・対象2経路・proxy・端末通信も復帰した。停止に伴う約55秒のゲスト時計遅れはPVE時刻で補正し、chrony Normalを確認。設定ファイルのSHA-256は前後一致。コード・テスト・模擬環境は変更していない。SAM `.0/32` WARNは残り、core完全停止や全対象アドレスの網羅確認は未実施。
