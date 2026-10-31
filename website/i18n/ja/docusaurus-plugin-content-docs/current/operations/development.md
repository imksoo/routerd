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

## Linux の WAN DHCPv4 ライフサイクル VM 試験

`scripts/linux-wan-dhcpv4-lifecycle-smoke.sh` は `make dist` で導入した製品を
専用の使い捨て Linux VM で検査します。空の router 設定と独立した管理経路が必要です。
ゲストの設定・試験用インターフェース・IPv4 forwarding を変更します。
iproute2、Python 3、curl、jq、dig、tcpdump、dnsmasq、Kea DHCPv4 と依存ライブラリを
用意し、systemd-resolved の stub resolv.conf を使用します。既存 Kea や `/run/kea`
との衝突時は開始しません。fixture は Ubuntu 24.04 用 Kea 2.4.1 を使用します。

```sh
python3 scripts/verify-dhcpv4-rebind_test.py
sudo bash scripts/linux-wan-dhcpv4-lifecycle-smoke.sh --disposable-vm \
  "$PWD/tests/fixtures/linux-wan-dhcpv4-lifecycle.json" /opt/routerd-wan-peer
```

パッケージルートには `usr/sbin/kea-dhcp4` と `usr/lib/x86_64-linux-gnu` を配置します。
上流と LAN クライアントは同一 VM 内の独立 namespace です。別実機間の試験ではありません。
T1/T2/失効は20/40/90秒です。実アドレス、metric 付き default route、resolved の
DNS/domain、LAN の UDP/TCP DNS 応答、HTTP peer が観測した NAT 送信元を合格条件とします。

更新成功後に Kea を停止し、次の T1 試行を観測してから T2 前に再開します。
一時 capture で unicast Renew、ciaddr 付き broadcast Rebind、transaction ID と
client MAC が一致する ACK を要求し、lease 時刻の更新も確認します。
[RFC 2131 の再バインド](https://www.rfc-editor.org/rfc/rfc2131#section-4.4.5)と新規取得を区別します。
再度停止して実期限でアドレス・経路・DNS/domain・転送が失われること、Kea の復帰だけで
製品再起動なしに復旧すること、空設定への置換が古いエラーを残さず Healthy になることも検査します。
必要な交換が保存済みファイルに揃ったことを確認してから capture を停止します。
daemon の ACK 受信だけでは libpcap のカーネルバッファから保存済みとは判断できません。

成功時の生 capture・出力は削除し、namespace を撤去して元の forwarding 値へ戻します。
失敗時は製品と peer を停止し、当該 fixture をローカル再現・修正まで保持します。
物理 WAN 機器、別 DHCP サーバー、VM 再起動、FreeBSD はこの試験の保証範囲に含みません。

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

独立した上流用とクライアント用のネットワーク名前空間（netns）を使います。
Kea の /60 委譲、Linux の SLAAC、ルーター広告による既定経路を確認します。
ローカル・転送先の AAAA 応答は UDP/TCP の両方で確認し、対向で IPv6 の送信元を観測します。
Renew/Reply の実交換により、リースの時刻が更新されることも確認します。
Kea のアドレスプールを `2001:db8:1500::/60` から `2001:db8:1501::/60` に変更し、SIGHUP で再読込みします。

製品設定は変更せず、ルーターの常駐処理と DHCP サーバーのプロセス番号が維持されることを確認します。
新しいプレフィックス情報オプション（PIO）、クライアントの送信元、AAAA 応答への追随を必須とします。
ルーターの旧アドレスが撤去され、旧 DNS 接続先が応答しなくなることも必須です。
旧 SLAAC アドレスがクライアントに残る場合は、非推奨の状態（deprecated）であることを確認します。
RFC 4862 の2時間規則により、変更前の短い有効期間が残る場合があります。

続いてサービスの再起動を確認します。T1/T2 は20/40秒、推奨期間と有効期間は60/90秒です。
上流停止後に期限切れとなり、ルーターのアドレスとクライアントの既定経路が撤去されることを確認します。
製品が動いたまま通信が失敗することも必須です。
独立したサーバーを起動し、プレフィックス委譲、SLAAC、既定経路、DNS、通信が自動回復することを確認します。
経路だけでなく、クライアントアドレスの DAD 完了と利用可能性も待ちます。
最後に上流を再度停止し、期限切れを待って空設定を適用します。routerd を再起動せずに、
資源数ゼロ・資源の phase 問題なし・現在のコントローラーエラーなしで Healthy になることを確認します。
削除した PD/DNS の status は担当処理の停止確認後に削除し、ネイティブ撤去に失敗した所有記録は残します。
その後、対向処理と名前空間を撤去し、転送設定を復元します。

アドレス追加直後の DAD 待ちは、ルーター広告の `Pending` とし、設定適用を失敗させません。
DAD 完了後に広告を開始します。DAD 失敗・所有権不明は引き続きエラーです。
資源 `DNSForwarder` の評価は設定順のため、ローカルゾーンを既定の上流サーバーより先に宣言します。

成功時の生出力は自動削除します。改訂番号・判定、手順・試験用構成・回帰試験を残します。
失敗時は製品と対向処理を停止し、表示した試験用構成のパスを診断用に保持します。
製品の失敗はローカルで再現・修正してから再検証し、解消後は一時出力を削除します。
WAN の中継経路と戻り経路は試験用の事前設定です。
WAN でのルーター広告、DHCPv6 の IA_NA、複数プレフィックスの重複期間を伴う変更は、別の検証対象です。
再起動・異常終了からの復旧、ファイアウォール、FreeBSD、routerd 1.0 全機能の合格とも区別します。

## Linux の DHCPv6 アドレス・プレフィックス client 試験

iproute2、util-linux、curl、jq、Python 3、展開済み Kea 2.4.1 package を用意した
専用の破棄可能な Linux VM で実行します。checksum を固定した `make dist` archive の
helper を使用し、client と peer を network namespace で隔離します。
導入済み routerd サービスと設定は置き換えません。

```sh
sudo bash scripts/linux-dhcpv6-association-smoke.sh --disposable-vm \
  /var/tmp/routerd-CANDIDATE-linux-amd64.tar.gz ARCHIVE_SHA256 /opt/kea-package
```

先に link-local 疎通、Kea 設定、UDP/547 socket を環境認証します。
続いて `--request-mode address` と `--request-mode pd-address` の両方を検査します。

- 取得した IA_NA アドレス・IA_PD プレフィックスを Kea のリース DB と照合します。
- helper を再起動し、取得時刻を延長せず復元できることを確認します。
- 自然な T1 Renew を観測後、peer を停止状態にして Renew・T2 Rebind・実際の45秒の
  有効期限切れを確認します。公開状態と保存 snapshot の両方から失効したリースが消えることが必要です。
- peer だけを再開し、自動で再取得できることを確認します。Release によるローカルの
  リース権限撤去、helper 終了、fixture の後片付けも合格条件です。

残すのは archive・source・script の hash と対象範囲付きの判定です。成功時の生ログは
削除し、失敗時はプロセスを止めてローカル再現用の診断情報を保持します。
この試験は DHCPv6 helper のプロトコル検証です。カーネルへのアドレス適用、DAD/Decline、
WAN 経路、転送、routerd 全体の監督、FreeBSD、`DHCPv6Address` リソース全体の合格は示しません。

1.5 の監督処理は `DHCPv6Address` と `DHCPv6PrefixDelegation` の参照先を OS の
インターフェース名に解決します。同じインターフェースにそれぞれ一つを宣言した場合は、
異なる別名から参照していても一つの `pd-address` worker で処理します。同じ種類の重複宣言は
拒否します。`DHCPv6Address.spec.client` は省略するか `routerd-dhcpv6-client` にします。
外部クライアントを追加の socket 所有者として起動する設定は受け付けません。
アドレス単独の worker は内部識別子 `DHCPv6Address/NAME` を使い、リソースの状態と
アドレスのイベントには宣言名を使います。プロトコルの `Bound` は観測値であり、
OS への適用状態を `Applied` にする根拠にはなりません。宣言・監督のローカルテストだけで
上記の実機所有権検証を代替しないでください。

## Linux の DHCPv6 アドレス実適用試験

同じ専用 VM の前提と checksum を固定した `make dist` archive を使い、
`scripts/linux-dhcpv6-native-smoke.sh --disposable-vm ARCHIVE SHA256 KEA_PACKAGE_ROOT`
を実行します。routerd 本体・監督処理・アドレスコントローラーを動かし、固定の runtime/state
保存先も fixture 内に隔離します。

合格には、独立した Kea のリース、有限の `/128`、DAD 完了、peer との双方向疎通、
SQLite の取得時刻を変えない停止・再起動、自然な Renew、Kea 停止中の実カーネル失効、
自動再取得、設定削除による撤去が必要です。OS で撤去を確認してから所有記録を削除します。
独立したホストが取得アドレスを使う場合は、DAD 失敗、サーバーでの Decline 受信、
失敗したローカルアドレスの撤去を確認します。

helper はプロトコル、アドレスコントローラーは OS への適用を担当します。helper の状態が
不明でも、既知の有効リースを延長したり即時撤去したりしません。DAD 完了済みで期限内の
アドレスだけを適用済みとして表示します。OS 操作の応答が失われた場合は pending の所有記録を
維持し、存在する他者のアドレスを引き取りません。サービス停止時は既存の有限期限を維持し、
宣言の削除時は明示的に撤去します。手順と範囲付き判定を残し、成功した生ログは削除します。
この試験だけで WAN 経路、通過トラフィックの転送、FreeBSD、routerd 1.0 全機能を合格としません。

## Linux の DHCP・firewall 撤去試験

空設定と独立した管理経路を持つ専用 VM に正式パッケージを導入して実行します。
systemd、conntrack、dnsmasq、nftables、iproute2、udhcpc を含む BusyBox、
Python 3、curl、jq、dig を事前確認してください。router 設定を置換します。
最後の引数はゲストの管理インターフェースです。

```sh
sudo scripts/linux-dhcp-firewall-lifecycle-smoke.sh --disposable-vm \
  "$PWD/tests/fixtures/linux-dhcp-firewall-lifecycle.json" eth0
```

LAN・WAN・管理 zone の独立した peer で、最初に HTTP の正常到達を確認します。
DHCP の gateway/DNS option、2分 lease のタイマー更新、ローカル・転送 DNS の
UDP/TCP、NAT 送信元、service 再起動後の維持が合格条件です。
zone 適用後は WAN→LAN、LAN→管理、WAN→router DNS を遮断します。
HTTP の遮断はクライアントの失敗だけでなく、対向ログに届いていないことと
nft chain counter で判定します。管理→LAN の正常通信も必要です。
明示的な許可ルールで LAN→管理が通り、ルール撤去で再び遮断されること、
firewall 全撤去で `inet routerd_filter` が消え、元の正常通信に戻ることを検査します。
最後に空設定で管理アドレスを撤去します。成功時の生出力は削除し、失敗時だけ
ローカル再現・修正まで保持します。VM 再起動、WAN DHCP client、rebind・expiry、
実 WAN の挙動は別の試験です。

ローカルの撤去回帰テストは通常の `go test` で skip し、明示的な隔離 netns で実行します。

```sh
go test -c -o /tmp/firewall.test ./pkg/controller/firewall
sudo unshare --net env ROUTERD_RUN_NETNS=1 /tmp/firewall.test \
  -test.run '^TestFirewallNativeWithdrawal$' -test.v
rm /tmp/firewall.test
```

nft の所有 comment は最初のテーブル作成時に指定します。後から table block に
書いてもカーネルの comment は更新されません。撤去時は実マーカーと handle を確認し、
読み取り・削除失敗時は再試行用の状態を保持します。旧ビルドのマーカーなしテーブルは
自動削除しません。隔離環境の準備段階で明示的に処理し、手動撤去を製品の合格結果に含めません。

## バージョン間の停止切替・切戻し

下記の2台構成で、固定した1.0 baselineと候補版をそれぞれ`make dist`で配布し、
archiveのchecksumを検証します。peer VMとDHCPプロセスは動かし続けます。
計画停止とリース再取得を伴う試験です。1.0のDBを1.5へ流用したり、
バイナリだけ戻せば復旧できると仮定したりしません。

1. 1.0で設定をvalidateし、Healthy・controller errorなしとpeerの`probe`・`isolation`
   を確認します。バージョン、両VMのboot ID、DHCP PID、アドレス、実際の更新を記録します。
2. `routerd`、続いて`routerd-dnsmasq`・`routerd-dns-resolver@lan`を停止します。
   停止中の`/var/lib/routerd`、`/usr/local/etc/routerd`、生成済みの
   `/etc/systemd/system/routerd.service`と隣接する
   `routerd.service.routerd-activation.json`を一組の復旧用データとして保存します。
   fixtureの起動unitと管理経路のoverrideは維持します。
3. 新規の状態ディレクトリと検証済み1.5設定を選び、候補archive内から
   `sudo ./install.sh --no-install-deps --no-restart --no-config-update`を実行します。
   起動前にunitの再生成、`CAP_SYS_PTRACE`・`CAP_KILL`、サービス停止を確認して再起動します。
4. 期待する版、routerのboot ID変更、systemd `running`、Healthy・controller errorなしを
   確認します。peerの両試験と、再取得後の実際のDHCPタイマー更新も必要です。
   peerのboot ID・DHCP PIDが変わっていないこと、現在のbootでhelperの所有・権限エラーが
   ないことを確認します。通信が通るだけでは合格にしません。
5. 同じサービスを停止して1.5の復旧用データを保存し、1.0の設定・状態を復元します。
   元の1.0 archiveを同じ停止オプションで導入し、保存したunitとactivation receiptも
   復元します。`sudo systemctl daemon-reload`後に再起動し、手順4を1.0で繰り返します。
   正本設定が保存ファイルと一致することも確認します。
6. 保存した1.5の設定・状態と対応する候補packageへ戻し、起動・peer試験を繰り返します。
   全段階の合格後、fixtureのcleanupを実行し、一時archive・snapshot・生ログを削除します。
   残すのはソース版、再現手順、合否です。

失敗時はその段階で止め、復旧用データと原因のローカル再現に必要な情報を保持します。
修正packageで失敗した試験をやり直し、unit権限の手修正を合格に数えません。
この構成の対象はIPv4 DHCP/DNS・NAT・ゾーン隔離です。IPv6、WAN接続方式、
その他の1.0機能の切替試験は別途必要です。

## 2台の VM による再起動・適用中断試験

`scripts/linux-router-reboot-fixture.sh` は、独立した router VM と peer VM を使います。
隔離 Ethernet segment が VLAN 3131〜3133 を通すこと、他試験と ID が衝突しないことを
事前確認してください。router は正式パッケージ、peer は BusyBox udhcpc、dnsmasq、
Python HTTP server、curl、dig を使います。両方に iproute2、jq、systemd と独立した
管理経路が必要です。ゲストの試験ポート用 boot unit を作成します。hypervisor の
ネットワーク設定は変更しません。

両 VM の `/home/ubuntu/` に shell script と
`tests/fixtures/linux-dhcp-firewall-lifecycle.json` をコピーします。
router には `scripts/linux-interrupted-apply-smoke.py` もコピーします。
以下の SSH alias は鍵を確認して operator が設定したもの、eth0 は隔離 trunk です。

```sh
ssh router-fixture 'sudo /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-router eth0 /home/ubuntu/linux-dhcp-firewall-lifecycle.json'
ssh peer-fixture 'sudo /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-peer eth0'
ssh router-fixture 'sudo /usr/local/sbin/routerctl apply -f /var/tmp/rd15-reboot/candidate.json --replace'
ssh peer-fixture 'sudo /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm probe'
ssh peer-fixture 'sudo /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm isolation'
```

両 VM の `/proc/sys/kernel/random/boot_id`、routerd の MainPID、peer の
`rd15-dhcp-client` の MainPID を記録します。peer の `/var/tmp/rd15-reboot/leases`
から bound address と renew を確認し、試験中に peer や DHCP process を再起動しません。

1. router で `sudo systemctl reboot`。90秒以内に boot ID が変わり、routerd active、
   systemd running、`routerctl get status -o json` の controller currentError がないこと。
2. peer の `probe` と `isolation` を再実行。peer の boot ID・DHCP PID は不変、
   同じ address で実タイマー更新が続くこと。router の nft chain
   `wan_to_lan`・`lan_to_admin`・`wan_to_self` の counter も確認します。
3. router で `sudo systemctl kill --kill-whom=main --signal=SIGKILL routerd`。
   60秒以内に別 PID で自動復旧し、NRestarts が増加、controller error がないこと。
   router の boot ID は不変のまま、peer の全チェックを繰り返します。
4. 正常性を確認した後、router で次を実行します。

```sh
sudo /home/ubuntu/linux-interrupted-apply-smoke.py --disposable-vm
```

helper は `recovery.home.arpa` を追加し、canonical file の置換後、apply client が
応答待ちの間に元の製品 process を pidfd で停止します。client が既に成功した場合は
この試験の成立と見なしません。停止時点で directory fsync が完了したかは断定しません。
apply は結果未確認の失敗を返す必要があります。60秒以内の自動復旧後、peer の LAN
namespace から `dig +short @198.18.15.1 recovery.home.arpa A` が `198.18.15.1` を返すこと、
probe・isolation・同じ peer identity・DHCP 更新を再確認してください。
helper の正常終了だけでは復旧の合格になりません。

probe はローカル・転送 DNS UDP/TCP、HTTP 対向が観測した NAT 送信元、管理→LAN の
正常通信を検査します。isolation は client 失敗と対向 HTTP ログへの非到達、WAN DNS
遮断を検査します。手動で製品状態を直して合格にしません。fixture 準備の失敗は
環境準備として扱い、製品の合格数には含めません。

成功後は同じ flag で router の `cleanup-router`、peer の `cleanup-peer` を実行します。
空設定、firewall・管理アドレスの撤去を確認してから fixture の port・unit・namespace を
削除します。一時出力は削除されます。残すのは手順・revision・判定です。
電源断の永続性、全中断位置、IPv6 再起動、異なる版への切戻しは別の試験です。


## Linux WAN RA/SLAAC ライフサイクル試験

`scripts/linux-wan-ra-lifecycle-smoke.sh --disposable-vm` は、独立した管理経路があり、
`make dist` パッケージを導入済みの空の専用 Linux VM だけで実行します。
正規設定は resources が空の Router JSON とします。dnsmasq、Python 3、curl、jq、
iproute2、systemd-networkd を事前認証し、独立した RA デコーダー rdisc6 を
`/opt/routerd15-fixtures/kea/usr/bin/rdisc6` に用意します。試験中に環境を修復しません。
既存の rd15-ra、rd15-ra-upstream、同名 networkd/sysctl 成果物があれば開始しません。

networkd が管理する WAN の RA 受信を無効にした状態から、別 namespace の dnsmasq
RA-only peer を使います。router lifetime は15秒、prefix の preferred/valid lifetime は
120秒。別サブネットの HTTP サーバーへの到達には、RA で取得した default route が必要です。
製品 apply 前に rdisc6 で RA を復号し、peer 内から HTTP が使えることを確認します。

合格条件:

- 新規 managed:false 宣言で RA 受信設定や networkd adoption を変更せず、default route を取得しない。
- managed:true に変更すると、手動の WAN アドレス・default route 追加なしで SLAAC を取得し、
  proto ra の経路を使って HTTP に到達する。peer が観測した送信元を照合する。
- service の通常再起動と、main process の SIGKILL を別々に実施する。新 PID の Healthy な
  daemon が同じ2 resource を読み、正規設定と peer を変えずに HTTP が使えること。
  SIGKILL 後は systemd による自動再起動がちょうど1回で、手動起動や再 apply を行わない。
- RA 送信プロセスだけを SIGSTOP で停止し、shutdown RA を送らせない。
  router lifetime 後は経路検索と HTTP が失敗し、valid lifetime 後は SLAAC アドレスも消える。
  Linux は GC 前の期限切れ行を expires -Nsec として一覧に残すことがあるため、
  行の存在と転送への利用を混同しない。
  [Linux v6.8 の経路選択](https://github.com/torvalds/linux/blob/v6.8/net/ipv6/route.c)も期限切れを除外する。
- peer だけを再開すると取得・通信が復帰し、routerd PID は不変、controller error はゼロ。

最後に空設定を適用し、fixture 自身の networkd/sysctl ファイルと interface を削除します。
この明示的な後片付けは、管理解除時の自動ロールバックの証明ではありません。
失敗時は製品と peer を停止して診断用出力を保持し、原因をローカルで再現してから再試験します。
成功ログは削除し、リビジョン・手順・合否基準を残します。RDNSS/DNSSL、DHCPv6 IA_NA、
LAN 転送、ホスト再起動・カーネル状態喪失・ストレージ障害、他 OS は未検証です。
service 再起動と SIGKILL の試験ではカーネル状態が維持されるため、これらを区別します。
