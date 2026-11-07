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


## routerd 1.5 のネイティブパッケージ試験

1.5 はクリーン OS へ apt/pkg で導入し、その後の更新もパッケージ間で行います。
更新時は利用者の設定と永続状態を保持します。ソースからの導入、`make install`、
手動コピー、tar 展開からの移行・引き取りは対象外です。
release workflow は native 基本パッケージを生成します。Linux の native libnDPI は本体と同じ版の[追加パッケージ](./ndpi-agent-libndpi.md)を使います。
ここでは公開 apt/pkg リポジトリの提供やリリースを宣言していません。

Linux は `make dist-deb PACKAGE_VERSION=<native-version>` で生成します。
共通の配布物一覧から `/usr` 配下のファイルと vendor systemd unit を梱包します。
実際の `/etc/routerd/router.yaml` と状態 DB はパッケージに含めません。
例は `/usr/share/doc/routerd/examples` に配置し、自動適用しません。
初回導入ではサービスを起動せず、1.5 用の設定を用意してから明示的に有効化・起動します。
パッケージの削除・purge 後も利用者の設定と DB を保持します。

以下は root、apt 接続、クリーンな使い捨て systemd VM、新旧2つのパッケージが必要な
明示実行の試験です。バージョンは新しい方が大きく、daemon binary 自体も異なるものを
用意します。依存パッケージとサービスを操作するため、作業端末や既存ルーターでは実行しません。

```sh
sudo sh scripts/linux-deb-lifecycle-smoke.sh --isolated-vm /tmp/prior.deb /tmp/current.deb
```

合格条件は、初回の未起動、CLI・実行ファイルのパッケージ所有、独立した UDP/TCP DNS 応答、
更新による実行ファイル交換と旧 daemon・helper の終了、設定の不変、実 SQLite DB 内の値の保持、
削除後の設定・DB 保持と生成 unit の撤去です。
`go test ./tests/nativepackage` は配置・所有権、異なるアーキテクチャの拒否、停止失敗時の更新拒否、
以前稼働していたサービスの復帰、生成ツール失敗時に不完全な成果物を公開しないことを検査します。
サービスの単体試験は実際のパッケージスクリプトを隔離した偽コマンドで実行します。

実行側は試験後に VM とディスクを削除します。失敗時は限定した apt/journal 診断を表示し、
ローカルで再現・修正してから再試験します。恒久的な実行ログではなく、手順・判定・簡潔な結果を残します。

FreeBSD は native 環境で `gmake dist-pkg ROUTERD_OS=freebsd` を使います。
Python、`pkg`、`dnsmasq`、`ca_root_nss`、`ndpi`、C compiler、`pkgconf`、`readelf` が必要です。
FreeBSD の標準 pkg に native libnDPI agent を含め、実 ELF の SONAME を
`shlibs_required` に記録します。新旧両方で実ライブラリの TLS/SNI 分類も確認します。
依存解決の初回導入試験では、成果物生成と native 単体試験の後に、使い捨てビルドゲストの
ndpi SDK/runtime パッケージを削除し、routerd の pkg 導入が OS リポジトリから
再導入することを確認してください。共有ホストでは行いません。使い捨て FreeBSD 14 ゲストに
`dig` と Python SQLite を用意し、次を実行します。

```sh
sudo sh scripts/freebsd-pkg-lifecycle-smoke.sh --isolated-vm /tmp/prior.pkg /tmp/current.pkg
```

設定・DB 保持、独立 DNS 疎通、実行ファイル交換と旧プロセス終了の基準は共通です。
試験では `pkg -o HANDLE_RC_SCRIPTS=true` を使い、hook の子孫プロセスの後処理後に
pkg が有効な rc.d サービスを起動し、main rc.d が自分の補助サービスを復帰させます。
グローバルな pkg ポリシーは変更しません。
さらに停止失敗を注入し、旧 daemon が DNS 応答を維持したまま pkg が置換を拒否することを
確認します。注入を解除して通常更新・削除を試験します。
native hook の単体試験では pkg の `POSIXLY_CORRECT` 環境を再現します。
rc.d 試験は所有者不明の拒否と、PID ファイル確認・シグナル操作中の正常終了を確認します。
Linux の更新・削除では、名前だけが routerd で始まる無関係なサービスの継続も確認します。

## 試験・ハーネス設計の必須規範

単体試験、オフライン fixture、runtime smoke、再利用可能な helper に適用します。製品要件、再現可能な試験手順、直接的な合否判定を残してください。診断出力が揃ったかではなく、必要な動作が機能するかを確認します。

- 製品 PASS／FAIL は完了済みで対象へ帰属できる観測で判断します。API／SSH の揺らぎ、未完了 command、入力欠落、watchdog timeout は、製品挙動を直接確認するまで観測／インフラの結果です。
- 製品契約に含まれる期限は維持します。有界 watchdog は停止用に使い、latency 条件へ読み替えません。
- 通常の試験が成功したら、一時 log、capture、生成状態を削除します。再利用するのは手順、期待動作、簡潔な結果であり、成功時の出力一式ではありません。
- 失敗時は未解決の原因を再現・調査するために必要な最小限の一時出力だけを保持します。ローカル再現と製品／試験の修正を行い、該当経路を再試験します。解消後は一時出力を削除し、単発 artifact を恒久的な合否条件にしません。
- 実挙動に影響する場合は実際の入口と選択ソースを確認します。candidate や mock の試験はその範囲だけを確認し、実転送や packaged service の証明とは扱いません。
- 過去の失敗を証明するためだけに log を恒久保存しません。修正後の動作は回帰試験で固定し、将来の運用者が必要とする手順や未解決の制約だけを残します。

機能要件、安全性、所有権、費用制限を弱める規範ではありません。直接観測の欠落は PASS の根拠になりません。たとえば WireGuard の取得失敗から禁止 AllowedIP がないとは言えず、同一スタックの ping 成功からトンネル転送が機能するとも言えません。

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

## Linux DHCPv6 server IA_NA 試験

iproute2、dnsmasq、dhcpcd、Go、network namespace 機能を備えた隔離 Linux 環境で
`sudo ./tests/netns/dhcpv6-stateful.sh` を実行します。現在の `DHCPv6Server` 設定を render し、
rendered dnsmasq を起動して、独立した dhcpcd client と交換します。`stateful` と `both` の両 mode で
Solicit/Advertise/Request/Reply が完了し、pool 内の address が割り当てられることを確認します。
成功時は2件の結果を表示し、一時 namespace、state、log を削除します。この試験は RA service を起動しません。
そのため ping や default route、forwarding を合格条件にせず、これらは別の RA/client lifecycle 試験で確認します。

## Linux の DHCPv4 reservation・dynamic pool 試験

iproute2、dnsmasq、BusyBox、Go と network namespace 機能を備えた隔離 Linux 環境で、
`sudo ./tests/netns/dhcpv4-reservation.sh` を実行します。現在の1.5 DHCPv4設定を実際の
renderer で生成し、private router namespace 内の dnsmasq と独立した BusyBox client 2台で検査します。
予約 client には指定アドレス、もう一方には設定 pool 内のアドレスが割り当てられること、
両 client が router と DNS option を適用し、dnsmasq の再起動後も同じ address を renewal することを
確認します。renewal の待機時間は停止用の上限であり、性能要件ではありません。成功時は一時 state と
log を削除し、失敗時は原因調査用の一時 directory を残します。package install、service manager の監督、
lease expiry、address conflict、DNS 応答、NAT、FreeBSD の検査は含みません。

## Linux DHCPv4 relay の実パケット試験

iproute2、dnsmasq、BusyBox `udhcpc`、tcpdump、Go と network namespace 機能を備えた隔離 Linux 環境で
`sudo ./tests/netns/dhcpv4-relay.sh` を実行します。現在の DHCPv4 relay 設定を生成し、別々の namespace に
実 dnsmasq relay と上流 DHCP server を起動します。1個の DHCPv4Relay resource に異なる2つの LAN subnet を指定し、
各 LAN の独立した BusyBox client から通信します。それぞれの pool の address と subnet ごとの gateway・DNS、
設定済み server ID を受け取ること、上流 server 停止中に lease が出ないこと、server 復旧後に
再取得できること、relay 撤去中に lease が出ず、relay 再起動後に再取得できることを合格条件にします。
成功時は1行の結果を表示し、一時 state と packet capture を削除します。失敗時は再現に必要な範囲に絞った
一時診断情報を残します。この試験は renderer と dnsmasq の packet path を確認します。routerd service の監督、
firewall policy、package install、再起動復旧、FreeBSD は対象外です。

試験 VM に Go がない場合は、Go がある build host で static renderer を作り、試験 script と一緒に VM に転送します。

```sh
CGO_ENABLED=0 go build -o /tmp/routerd-dhcpv4-relay-render ./tests/netns/dhcpv4-relay-render
sudo env ROUTERD_DHCPV4_RELAY_RENDERER=/tmp/routerd-dhcpv4-relay-render \
  ./tests/netns/dhcpv4-relay.sh
```

## Linux DHCPv4 relay daemon / systemd 試験

製品経路全体は、候補 revision の Linux `make dist` package を展開し、systemd、iproute2、dnsmasq、BusyBox
`udhcpc`、network namespace を利用できる使い捨て Ubuntu VM で次を実行します。

```sh
sudo scripts/linux-dhcpv4-relay-daemon-smoke.sh --disposable-vm \
  /path/to/extracted/linux-amd64-package
```

script は package 内の binary で新しい routerd 1.5 relay 設定を検証し、実 daemon と systemd 管理下の dnsmasq を
独立した上流 server / client と接続します。設定済み lease と option、上流停止中および relay 撤去中に lease が
出ないこと、上流復旧後の再取得、relay 再適用、managed dnsmasq 再起動後の lease を合格条件にします。
続いて静的 address 宣言を追加せず、外部所有 LAN の address を変更します。生成された relay 送信元と
所有 dnsmasq PID の両方が変わった後、独立 client が上流 server の option を保持した lease を
取得することを確認します。設定ファイルの変更だけでは稼働準備完了としません。
終了時は製品による unit・PID の撤去完了を待ってから fixture を片付けます。全体は180秒で
打ち切ります。既存 routerd state / service がある環境では実行せず、1.0 設定・state の import や upgrade は行いません。
成功時は一時 state と log を削除し、失敗時は再現に必要な上限付き診断情報だけ残します。この試験は relay controller と
dnsmasq service lifecycle を対象にします。package install、routerd boot service、firewall policy、reboot recovery、FreeBSD は
含みません。

## Linux DNS resolver runtime 試験

UDP 応答はソケットの送信期限を共有します。同時応答は、待機と実送信を
合わせた既存の2秒の送信期限内で処理し、短い競合だけでは即座に破棄しません。
既存の同時問い合わせ上限128は維持します。
`go test ./cmd/routerd-dns-resolver -run TestUDPReply` で競合時の実 UDP 応答と、
送信処理が占有されたままの場合の期限終了を確認できます。

iproute2、dnsmasq、`dig`、curl、Go と network namespace 機能を備えた隔離 Linux 環境で
`sudo ./tests/netns/dns-resolver-runtime.sh` を実行します。現在の1.5 runtime 設定を用いて
実際の `routerd-dns-resolver` を起動し、独立した `dig` client から local zone と条件付き upstream
forwarding を UDP/TCP の両方で確認します。設定を変更して SIGHUP を送った後に新しい zone answer が返ること、
worker 再起動後に local answer と TCP forwarding が復旧することも確認します。成功時は一時 state と log を削除し、
失敗時は調査用 directory を残します。この試験は resolver process のみを対象とし、routerd controller や
service manager、DHCP lease 由来の更新・tombstone、negative reply、DoT/DoH、FreeBSD runtime は含みません。

## Linux PortForward DNAT と hairpin 試験

iproute2、nftables、curl、Python 3、Go と network namespace 機能を備えた隔離 Linux 環境で
`sudo ./tests/netns/nat44-portforward.sh` を実行します。用意済みの routerd 1.5 YAML を validate し、
private router namespace 内で実際の NAT44 controller を動かします。独立した WAN、LAN server、LAN client の namespace から、
WAN DNAT、LAN hairpin の送信元変換、直接 LAN 通信の送信元保持を確認します。ルール撤去後は既存 WAN TCP 接続が reset され、
routerd NAT table が消え、新しい WAN 接続が backend に届かないことを確認します。成功時は一時 state と log を削除し、
失敗時は調査用 directory を残します。1.0 設定や state の import、package install、service manager、pinhole、session 同期、
複数 backend の ingress、FreeBSD は対象外です。

## Linux NAT44 source SNAT selector 試験

iproute2、nftables、curl、Python 3、Go と network namespace 機能を備えた隔離 Linux 環境で
`sudo ./tests/netns/nat44-source-snat.sh` を実行します。 `outboundInterface/sourceCIDRs/translation` 形式でも変換先アドレスと TCP 送信元ポート範囲を独立 HTTP peer で確認します。 プール試験では非連続の 3 アドレスすべての通信と、指定外アドレスを使わないことを確認します。プールは範囲の両端ではなく個々のアドレスの列です。Linux は重複を除いた列から新規フローを順番に割り当て、既存フローの変換は conntrack が維持します。VM では `ROUTERD_TEST_BINARY` に正式配布バイナリを指定できます。`ROUTERD_NAT44_TEST_DRIVER` に事前ビルドした `tests/netns/nat44-portforward-runtime` の試験用実行ファイルを指定すると、VM に Go ツールチェーンは不要です。用意済みの routerd 1.5 設定を validate してから、
private router namespace 内で実際の NAT44 controller と nftables を動かします。独立した WAN 側 HTTP peer が転送 packet の source address を観測します。
指定した source / destination CIDR だけに SNAT が適用され、除外 destination と source range 外の client は元の source address を保つことを確認します。
`snatAddressFrom` の参照先を変更すると新しい接続の source が切り替わり、rule 撤去後は NAT table が消えて元の source に戻ることも確認します。
成功時は一時出力を削除し、失敗時は今回の診断 directory だけを残します。

## Linux LocalServiceRedirect DNS 振り分け試験

iproute2、nftables、dnsmasq、BIND `dig`、Python 3、Go と IPv6 対応カーネルを備えた隔離 Linux 環境で
`sudo ./tests/netns/local-service-redirect.sh` を実行します。用意済みの routerd 1.5 設定を validate し、
private router namespace 内で実際の NAT controller と nftables を動かします。独立した LAN client と WAN 側 DNS server を使い、
宛先 address set・port・受信 interface に一致する IPv4/IPv6 の TCP/UDP query だけが local DNS に届き、
一致しない宛先・port・interface は upstream に届くことを確認します。選択する LAN interface を設定変更で切り替え、
redirect 撤去後に upstream 応答へ戻り、両方の NAT table が消えることも確認します。1.5 設定と新しい state から開始し、
1.0 設定や state は取り込みません。成功時は一時出力を削除し、失敗時は再現用の今回の診断 directory だけを残します。

## Linux の packaged DHCPv6 server lifecycle VM 試験

`make dist` で candidate を build し、専用の破棄可能な systemd Linux VM に展開してから実行します。

```sh
sudo scripts/linux-dhcpv6-server-lifecycle-smoke.sh --disposable-vm \
  /path/to/extracted/package
```

展開先には `bin/routerd` と `bin/routerctl` が必要です。VM には resource がない Healthy な
`routerd.service`、導入済み `/usr/local/sbin/routerctl`、`ip`、`jq`、`dhcpcd`、`tcpdump`、
`timeout`、`journalctl` と systemd tools が必要です。試験は一時的な routerd 1.5 設定を validate し、
package 内の `routerd serve`、systemd 管理の dnsmasq、独立した dhcpcd client を private veth と
client namespace で動かします。client の待機は最大150秒、renewal 観測は最大90秒です。
pool 内の IA_NA address、client の DAD 完了、同じ address の DHCPv6 renewal reply、RA Managed flag、
RA で設定された default route を合格条件にします。既存の router 設定と state は変更せず、
candidate 終了後に元の service を再開します。成功時は一時出力を削除します。失敗時は candidate と
peer を停止し、元の service を再開したうえで、ローカル再現に必要な今回の fixture だけを保持します。
この gate は DNS 応答、IPv4 NAT、forwarding、reboot recovery、package の clean install、
router 全体の qualification を証明しません。

## Linux DHCPv4 checksum offload 回帰試験

Linux packet socket は `PACKET_AUXDATA` を読みます。仮想リンクでは UDP checksum の
計算完了前の OFFER が `TP_STATUS_CSUMNOTREADY` 付きで届くため、capture 上の不一致だけで
破損 packet と判断しません。通常の frame の UDP checksum と、両経路の IP checksum・
長さ・fragment・UDP port の検証は維持します。

リポジトリのルートから通常ユーザーで `go test -c -o /tmp/routerd-dhcp-offload.test ./cmd/routerd-dhcpv4-client`
を実行し、明示的な隔離環境で次を実行します。

```sh
sudo unshare --net --mount bash -c '
  set -eu
  mount --make-rprivate /
  mkdir -p /run/netns
  mount -t tmpfs tmpfs /run/netns
  ROUTERD_DHCPV4_OFFLOAD_TEST=1 /tmp/routerd-dhcp-offload.test -test.run=^TestLinuxReceivesChecksumOffloadedOffer$ -test.v -test.timeout=30s
'
rm -- /tmp/routerd-dhcp-offload.test
```

独立 UDP peer が veth 経由で OFFER を送り、kernel の checksum 未完了属性と実 transport の
受信を確認します。属性の観測と PASS を要求し、SKIP は合格としません。通常の unit test では
skip し、opt-in 時もホストの初期 network namespace なら実行を拒否します。DHCP 全交換、
VM 再起動、FreeBSD の合格を示す試験ではありません。

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

## Linux ホームルーターのクリーン導入ライフサイクル試験

専用 Linux VM で `scripts/linux-home-router-lifecycle-smoke.sh --disposable-vm` を実行します。
引数は、事前に用意した routerd 1.5 JSON 設定、Linux `make dist` パッケージの展開先、
Kea/ndisc6 パッケージの展開先、独立した管理 interface 名です。fixture の management
interface を実環境の名前に置換してから設定を検証します。管理 interface は trust zone に
入るため、WAN/LAN の firewall 試験中も管理接続を維持します。VM には独立した管理経路と、`ip`、`ss`、systemd、
`nft`、`dnsmasq`、BusyBox、`dhcpcd`、`curl`、`dig`、`jq`、Python 3、`timeout`、
`/proc/sys/net/netfilter/nf_conntrack_count` が必要です。

routerd の既存バイナリ、正規設定、`/var/lib/routerd`、service、試験用 interface/netns が
残っていれば開始しません。パッケージ内の routerd で設定を検証し、新しい 1.5 パッケージを
導入して実 systemd service で同じ設定を試験します。旧設定・DBは読み込まず、routerd 1.0 の
アップグレード試験もしません。成功時はパッケージ、状態DB、一時出力を削除します。
失敗時は停止前に空設定を適用してrouterd管理の状態を撤去し、fixture を片付けます。
原因を再現できる上限付き診断情報と設定だけを残します。

```sh
sudo scripts/linux-home-router-lifecycle-smoke.sh --disposable-vm \
  tests/fixtures/linux-home-router-lifecycle.json \
  /path/to/extracted/routerd-package /opt/routerd15-fixtures/kea eth0
```

管理 NIC に DHCP の default route がある場合、ゲストの network manager で、その
metric を fixture WAN の 4242 より大きくします（例: 65000）。管理サブネットへの経路は
維持します。試験は WAN DHCP 取得後、クライアント通信を始める前に試験宛先が隔離 WAN を
選ぶことを確認します。管理側 default を選んだ場合は fixture の準備エラーです。

Ubuntu にインストールした Kea は AppArmor profile により一時設定ファイルの読み取りを
拒否する場合があります。存在するファイルで `Unable to open file` となったら、kernel の
監査ログで拒否を確認します。対応するパッケージを root で専用 fixture ディレクトリへ
展開し、routerd 導入・試験前の環境準備で、実際の peer namespace 内から設定を
読み取れることを確認してください。

Kea 展開先には `usr/sbin/kea-dhcp6`、`usr/bin/rdisc6`、
`usr/lib/x86_64-linux-gnu` が必要です。`rdisc6` は root 所有にしてください。
この同一設定の統合試験は、DHCPv4 取得・更新、実際の委譲 LAN prefix からの DHCPv6 IA_NA
取得、IPv4/IPv6 クライアントからの DNS A/AAAA UDP/TCP、IPv4 NAT の対向送信元確認、WAN
から LAN への未要求通信拒否、DHCPv6-PD の更新・prefix 変更・期限切れ・再取得、RA/SLAAC、
委譲 IPv6 を使った転送、service 再起動、WAN 断・復帰、managed resource の撤去を確認します。
物理 ISP との相互接続、WAN 発見・
PPPoE・DS-Lite、再起動復旧、FreeBSD 全機能、routerd 1.0 の全機能維持を証明する試験ではありません。

fixture の上流 IPv6 default route は metric 50 とし、管理 NIC の metric 100 の RA default
より優先します。製品導入前に試験宛先が隔離 WAN を選ぶことを確認し、終了時には
その fixture 所有 route を撤去します。

上流 IPv6 の default route は、上流ルーターから学習済みの状態を模した disposable WAN fixture が
与えます。この試験はその経路を通る IPv6 転送を確認しますが、`IPv6StaticRoute` の設定レンダリングや
WAN Router Advertisement の取得までは検証しません。

## Linux の PD 由来 DS-Lite 送信元・パッケージ試験

`scripts/linux-dslite-pd-smoke.sh` は、独立した Kea から /60 を取得し、
4つの管理された WAN /128 を4本の DS-Lite が共用する実装経路を検査します。
委譲 LAN アドレス、送信元ハッシュの主系と低優先度の予備を含みます。
apt で導入したサービスと `/usr`・`/etc/routerd`・`/var/lib/routerd` の
標準パスを使い、旧導入状態の取り込みや設定移行は行いません。

独立した管理経路を持つクリーンな専用 Ubuntu VM に正式な `.deb` を apt で
導入し、サービスを起動・設定する前に実行します。OS 準備を済ませ、iproute2、
nftables、jq、curl、Python 3、conntrack、tcpdump、`kea-common` を導入します。
対応する `kea-dhcp6-server` を root 所有の試験ディレクトリへ展開し、`ldd` の
不足がなく `kea-dhcp6 -v` が動くことを確認してください。独立相手の実行ファイルを
別のパスに置くのは、通常サーバー用の制限と一時設定の競合を避けるためです。
製品バイナリの差し替えは行いません。VM 試験前のローカル準備では実 DHCP 取得を
確認し、VM 側では設定・待受・IPv6 経路・DAD・各 HTTP 相手を確認してから
routerd を起動します。

```sh
sudo bash scripts/linux-dslite-pd-smoke.sh --disposable-vm \
  tests/fixtures/linux-dslite-pd.json /opt/routerd15-fixtures/kea eth0
```

新規導入時にメイン unit の再起動が既に予約されている場合は、実際のタイマー・
サービスの完了を観測し、準備完了を再確認してから定常状態の番号変更試験へ
進みます。固定時間の待機や、起動時の予約を DHCP による再起動と見なす判定は
使いません。

合格条件は、有限の期限を持つ /128 と一致するトンネル送信元、独立相手が
観測する64送信元の安定した転送、自然な Renew での取得時刻更新です。
Kea のプールと相手 AFTR の戻り先だけを変更した場合、両デーモンを再起動せずに
4送信元を置換し、失効した旧プレフィックスを除去することを確認します。
routerd 再起動後も復旧し、Kea だけの停止では自然失効によって4送信元・
トンネル・LAN アドレス・転送が撤回されること、相手だけの再起動で自動復旧する
ことを要求します。空設定への置換は、送信元とトンネルを撤回し、リソース0・
コントローラーの現在エラーなしの Healthy に収束することを確認します。

宣言から削除したトンネルを、撤回処理が Disabled の表示行として復活させては
いけません。実体の消失を確認した後、所有記録を手放す前に公開状態を削除し、
削除に失敗した場合は再開可能な記録を残します。条件で一時無効になった宣言の
状態は保持します。リソース数0は既存の control API の JSON では省略される場合が
あるため、スキーマに沿って省略された0を扱います。

リンク観測側も、宣言から消えた Interface の観測行だけを削除します。
条件で一時無効の宣言や他の種類・API は保持し、列挙・削除の失敗は再試行のため
エラーとして返します。表示状態の整理で実インターフェースは操作しません。

相手側の戻り先は、新しい PD を通知する前に文書用プレフィックスへ準備します。
製品状態の手動修復ではありません。成功した生ログと名前空間は削除し、失敗時は
限定したパケット・状態を残して試験を終了してからローカルで再現します。
使い終わった VM は削除します。ISP の AFTR/DNS 発見、BGP/HA、リース・
セッション同期は別の検証です。手順が存在するだけでは合格とせず、実行した
製品版・手順版・結果を qualification ledger に記録します。

## Linux の複数 DS-Lite 経路・パッケージ試験

メインの遅延再起動タイマーには
[`StopPropagatedFrom=routerd.service`](https://www.freedesktop.org/software/systemd/man/249/systemd.unit.html#PropagatesStopTo=)
を設定し、明示的な停止で予約も取り消します。通常の自己再起動でタイマーまで
再起動して繰り返さないこと、notify 型サービスの起動待ち中にも予約できることを
確認します。必要な systemd 249 以降は Debian パッケージの依存関係で宣言します。

次の任意試験は、一般ユーザーの systemd と一時 unit ファイルだけを使います。
停止後の復活を再現し、修正後の停止維持・一度の再起動・起動待ち中の予約を
検査します。本番サービスやホストのネットワークは操作しません。

```sh
ROUTERD_RUN_USER_SYSTEMD=1 go test ./pkg/controller/chain \
  -run '^TestSelfRestartUserSystemdLifecycle$' -count=1 -v
```

メインサービス内の Package コントローラーは、apt のダウンロード用ユーザーが
所有するキャッシュの権限変更に `CAP_FOWNER`、非公開ディレクトリからの取得済み
ファイルの移動に `CAP_DAC_OVERRIDE` を必要とします。制限された権限セットでは
UID 0 だけでは足りません。生成 unit と配布用 unit の両方を同期してください。
次の回帰試験は、一時ディレクトリで実際の chmod・rename を実行します。
パッケージ導入やホストのネットワーク変更は行いません。

```sh
go test -c -o /tmp/routerd-package-caps.test ./pkg/render
sudo env ROUTERD_RUN_PACKAGE_CAPS=1 /tmp/routerd-package-caps.test \
  -test.run '^TestRouterdPackageCachePermissionsNative$' -test.v
```

既存通信の回帰試験は、本番の nft 生成処理と単一の UDP ソケットを使います。
Linux、root、`unshare`、`nsenter`、iproute2、nftables、conntrack、Python 3 が必要です。
外側の名前空間の隔離を検査し、ホストの `/run/netns` へ登録しません。
ネットワーク変更は隔離した名前空間の終了で消えます。

```sh
work=$(mktemp -d)
CGO_ENABLED=0 go test -c -o "$work/render.test" ./pkg/render
sudo env ROUTERD_RUN_NETNS=1 ROUTERD_HOST_NETNS="$(readlink /proc/self/ns/net)" \
  unshare -n "$work/render.test" -test.run '^TestEgressExistingFlowNative$' \
  -test.v -test.timeout=45s
rm "$work/render.test"
rmdir "$work"
```

合格条件は、同じ UDP 通信の識別値が故障回線から正常回線へ移ること、
正常な間は維持されること、主系全断で予備へ移り、主系復旧時も正常な予備の
通信を維持することです。通常の単体試験では異常判定・インターフェース消失の
除外と、設定を変更しないことも検査します。実カーネルの経路識別値の試験であり、
ISP 側 NAT が変わる場合の相手 TCP セッション継続を保証するものではありません。

`scripts/linux-multi-dslite-smoke.sh` は、専用の使い捨て Ubuntu VM に apt で
新規導入し、4つの独立したカーネル AFTR と LAN クライアントを別の名前空間で
動かします。3本を送信元アドレスで分散し、低優先度の4本目へ退避します。
設定例は文書・ベンチマーク用アドレスを使い、実ホストの設定は含みません。
候補パッケージへの更新後には、64組の同じ UDP 通信情報を DS-Lite・NAT の
相手へ送ります。正常経路の通信は conntrack ID と相手経路を維持し、
切替対象は1回のクライアント再送までで復旧することを合格条件とします。
相手は正常な主系・予備・引き続き正常な予備の順に受信し、UDP 相手の応答は
環境認証時にも確認します。これは実際の UDP 転送の検証で、ISP 側 NAT 変更時の
TCP セッション継続保証ではありません。

Linux の MASQUERADE は送出インターフェースが変わると古い項目を削除し、
そのパケットを落とします。次の単独試験で、同じ UDP ソケットを維持したまま
古い項目の削除を観測し、次の送信が新しい変換元で相手へ届くことを確認します。
routerd や製品状態への手動修復は使いません。

```sh
sudo env ROUTERD_RUN_NETNS=1 ROUTERD_HOST_NETNS="$(readlink /proc/self/ns/net)" \
  unshare -n python3 tests/netns/masquerade-oif.py
```

根拠は Linux 6.8 の
[送出インターフェース変更判定](https://github.com/torvalds/linux/blob/v6.8/include/net/netfilter/nf_nat.h#L51-L63)
と[破棄・削除処理](https://github.com/torvalds/linux/blob/v6.8/net/netfilter/nf_nat_core.c#L802-L809)です。
全通信の ID 維持という試験条件は不適切です。正常な経路と主系復旧後の
正常な予備通信については、ID 維持を引き続き検査します。


事前に OS 初期化・再起動を済ませ、`iproute2`、`nftables`、`jq`、`curl`、
`python3`、`conntrack`、`tcpdump`、`dnsmasq-base` を導入してください。
試験用と独立した管理 NIC を使い、routerd・設定・DB や試験名と競合する
インターフェースがないクリーンな VM を用意します。版の異なる2つの候補を
`make dist-deb` でビルドし、新しい方のパッケージ版が大きいことを確認します。
開発用バイナリの手動配置で代用しません。

```sh
go run ./cmd/routerd validate --config tests/fixtures/linux-multi-dslite.json
bash -n scripts/linux-multi-dslite-smoke.sh
# スクリプト・設定・両パッケージを専用 VM に転送して実行します。
sudo bash scripts/linux-multi-dslite-smoke.sh --disposable-vm \
  tests/fixtures/linux-multi-dslite.json /tmp/prior.deb /tmp/current.deb eth0
```

IPv6 リンクと独立 HTTP 受信を環境認証した後に routerd を導入します。
経路変更時はヘルス結果だけで判断せず、カーネルの nft マーク規則の更新を
待ちます。90秒は試験の観測上限であり、製品の切替時間要件ではありません。

64送信元それぞれの接続先が安定し、3本すべてで通信し、AFTR が変換後の
内部送信元を観測することを要求します。1本断、主系3本断、復帰、実サービス
再起動、apt 更新、設定保持・旧プロセス終了・パッケージ整合性、空設定での
トンネル撤回・通信停止まで確認します。製品状態の手動修復は合格にしません。
撤回後はローカル宛ての Telemetry 設定で実際のメイン unit を変更し、予約タイマー
を観測してから停止します。10秒の遅延を過ぎても停止が維持されることを要求します。
この設定は再起動の起点であり、コレクターへの配送試験ではありません。

終了後は呼出側が VM とディスクを片付けます。成功ログ・キャプチャは削除し、
失敗時だけ診断と最大2個の1MBキャプチャを残してローカル再現に使います。
PD・RA 送信元の期限切れ、実 ISP、ホスト再起動、DHCP/DNS、BGP、HA・セッション
同期や、実ホスト全体の置き換え品質をこの試験だけで証明するものではありません。

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

## Linux native Health to PBR gate

`make dist` で正式archiveを作り、SHA256を固定します。iproute2、nftables、
Python 3、curl、jqとnamespace/mount権限を備えたLinux試験ホストで実行します。

```sh
sudo make check-routerd15-health-pbr-native-linux \
  HEALTH_PBR_ARCHIVE=/absolute/path/to/routerd-archive.tar.gz \
  HEALTH_PBR_ARCHIVE_SHA256=EXPECTED_SHA256 \
  HEALTH_PBR_WORK_PARENT=/absolute/path/to/existing/private/work-directory
```

専用network/mount namespace、独立した二つのHTTP上流とLAN clientを用意し、
両経路を環境認証してから認証用routeを撤去します。既存の `/run/routerd` と
`/var/lib/routerd` はprivate mountに置き換え、ホストのroute/firewall/serviceは変更しません。

合格条件は正式packageのHealth worker→IPC→routerdの選択→実HTTP転送、
優先上流の停止による代替への切替、上流だけの再開による自動復帰です。
監視worker停止後は古い成功判定が失効し、155秒以内に代替へ切り替わることを要求します
（鮮度期限の最小2分＋通常poll最大30秒＋実行余裕）。worker再開とrouterd再起動後も転送でき、設定撤去後は
所有rule/route/default mark tableが消え、client転送が止まる必要があります。
全上流停止時はPending/NoReadyCandidatesとなりmark tableを撤去し、上流再開だけで
転送が復帰すること、無関係なrule/routeを保持することも確認します。

Health helperの起動はfixtureが担当するため、systemd起動、package更新、NAT、
全policy/source mode、VRRP、FreeBSDの認証は含みません。source/archive/scriptの
識別子と限定した判定を残し、成功時の生出力は削除します。失敗時は所有processを止め、
指定work directory内の診断を保持して再現に使います。

## Linux IPsec の依存導入と provider 試験

[IPsec のパッケージ・サービス検証手順](linux-ipsec-provider-test.md)に従い、
割り当て済みの専用 Ubuntu VM で実際の依存導入、systemd/VICI、接続の所有権、
強制終了後の復旧を検査します。既存 routerd の設定・DB の保持と復元も合格条件です。
IPsec 全機能の維持を認定する試験ではありません。

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

## FreeBSD の native DHCPv6 アドレス試験

`scripts/freebsd-dhcpv6-native-smoke.sh` は FreeBSD 14.3 用の所有権検証手順です。
スクリプトの追加やクロスコンパイルだけでは、実機合格になりません。独立した管理経路のある
使い捨て VM で、root、VNET/epair、nullfs、devfs/fdescfs、`/usr` 配下の Kea、
sqlite3、jq、Python 3、curl を先に準備してください。環境準備を完了してから製品を検証します。

`make dist ROUTERD_OS=freebsd GOARCH=amd64 VERSION=... GIT_COMMIT=...` で正式 archive を
作成し、SHA256 とスクリプトを VM へ転送します。VM のリポジトリディレクトリから、
既存の private 作業親ディレクトリを指定して実行します。

```sh
sudo timeout -k 30 600 sh scripts/freebsd-dhcpv6-native-smoke.sh \
  --disposable-vm /absolute/routerd-freebsd-amd64.tar.gz ARCHIVE_SHA256 \
  /usr/local/sbin/kea-dhcp6 /root/routerd-tests
```

作業親は一時領域と `/usr` の外に置きます。各 jail は独立した root と `/var` を使い、
既存サービスの socket、SQLite、helper 状態と分離します。最初に scoped IPv6 の往復経路、
有限 `/128`、独立した重複アドレス、Kea の設定と listener を認証します。
ここでの失敗を製品不良と判定したり、製品検証中に環境修復したりしません。

合格には Kea リースとの一致、有限 `/128` と DAD 完了、双方向各 3 パケットの全受信、
停止・再起動時の取得時刻維持、自然な Renew と native 寿命の更新、実失効、peer だけの再開からの
自動再取得、設定撤去、対象アドレスの `DHCP6_DECLINE_LEASE` と native 撤去、PD/NA 共用 worker
での取得が必要です。所有する子プロセス、jail、epair、マウントの後片付けも合格条件です。
成功時は生出力を削除し、失敗時は作業親配下に診断を残します。失敗段階と内容を確認してから
ローカル再現を組み立てます。全 source mode、WAN 転送、routerd 1.0 全機能の維持は別の判定です。

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

## routerd 1.5 の新規導入

バージョン間の移行・切戻しは受入条件に含めません。導入前に目的の設定を1.5形式へ
変換して1.5で検証し、新しい1.5 stateを使って導入します。1.0の設定・DBは読み込みません。

## 2台の VM による再起動・適用中断試験

`scripts/linux-router-reboot-fixture.sh` は、独立した router VM と peer VM を使います。
隔離 Ethernet segment が VLAN 3131〜3133 を通すこと、他試験と ID が衝突しないことを
事前確認してください。router は正式パッケージ、peer は BusyBox udhcpc、dnsmasq、
Python HTTP server、curl、dig を使います。両方に iproute2、jq、systemd と独立した
管理経路が必要です。ゲストの試験ポート用 boot unit を作成します。hypervisor の
ネットワーク設定は変更しません。

管理 NIC と隔離 trunk が別の場合、`setup-router` と `setup-peer` には trunk NIC を
渡します。ゲストの network manager で trunk を起動時に up にし、親 NIC には試験用
サブネットのアドレスを付けません。初回 apply 前に生成された `candidate.json` の
`Interface/management.spec.ifname` を実際の管理 NIC に設定し、再度 validate します。
生成済みの admin zone はこの resource を参照するため、管理経路と試験 VLAN を
それぞれ適切な NIC に割り当てられます。

dual-stack mode の `tests/fixtures/linux-dualstack-reboot.json` は静的 WAN IPv4 を
DHCPv4 に置き換え、WAN RA 受信、PD、LAN の委譲 address・DHCPv6/RA、dual-stack DNS を
組み合わせた設定で、実 CLI の validate が通っています。router 側 fixture は WAN の
networkd RA 受信を無効にした初期状態を作り、製品 resource が受信を有効にします。
`setup-router` の設定パスの次の引数には、別の管理 NIC を指定できます。
独立 radvd peer はローカルで SLAAC・学習 default route・HTTP 対向の送信元観測を
確認しました。dual-stack peer には radvd と、root で専用ディレクトリへ展開した
同一 OS 用 Kea DHCPv6（共有 library を含む）が必要です。router setup 後の `rd15-wan` の
DAD 完了済み link-local address を読み、次のように渡します。

```sh
ssh router-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-router ens19 /home/ubuntu/linux-dualstack-reboot.json eth0'
ssh peer-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-peer ens19 <router-wan-link-local> /opt/routerd15-fixtures/kea'
```

この link-local は PD pool の戻り next hop とし、router 再起動後も同一であることを確認します。
peer setup は WAN link-local の DAD 完了後に Kea を起動し、RA/PD/DNS unit、
DHCP socket、IPv6 HTTP/DNS の正対照を確認します。Kea の active だけでは準備完了としません。
通常の `probe` と `isolation` が IPv4/IPv6 の DNS A/AAAA UDP/TCP、委譲 prefix の
SLAAC/default route、対向が観測した IPv6 送信元、IPv6 WAN-to-LAN/DNS 遮断も検査します。
LAN IPv6 client は kernel SLAAC で、DHCPv6 IA_NA の更新・再起動はこの probe では未確認です。
統合した2 VM 再起動試験は製品 `6977c66e`・fixture `96e00032` で合格しました。
旧 IPv4 の結果だけをこの経路の合格とは扱いません。

上流 DNS の問い合わせ期限は既定で3秒です。`dig` は5秒待ち、resolver が許容された
期限内に応答する前に試験クライアントが諦めることを防ぎます。応答時間の目標値では
ありません。正対照は timeout・SERVFAIL・空応答・誤ったアドレスで失敗し、WAN からの
遮断試験は DNS 応答があれば失敗します。

router VM は routerd の binary・設定・DB がない clean state とし、1.5 形式に整えた設定を
用意します。現在の Linux `make dist` パッケージを展開して
`sudo ./install.sh --no-install-deps --enable-service` で導入します。
両 VM の `/home/ubuntu/` に shell script と
`tests/fixtures/linux-dhcp-firewall-lifecycle.json` をコピーします。
router には `scripts/linux-interrupted-apply-smoke.py` もコピーします。
以下の SSH alias は鍵を確認して operator が設定したもの、eth0 は隔離 trunk です。

```sh
ssh router-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-router eth0 /home/ubuntu/linux-dhcp-firewall-lifecycle.json'
ssh peer-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-peer eth0'
ssh router-fixture 'sudo install -m 0600 /var/tmp/rd15-reboot/candidate.json /usr/local/etc/routerd/router.yaml && sudo systemctl start routerd'
ssh router-fixture 'sudo /usr/local/sbin/routerctl apply -f /var/tmp/rd15-reboot/candidate.json --replace'
ssh router-fixture 'sudo /usr/local/sbin/routerctl get status -o json | jq -e ".status.phase==\"Healthy\" and ([.status.controllers[] | select(.currentError==true)] | length==0)"'
ssh peer-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm probe'
ssh peer-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm isolation'
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
sudo python3 /home/ubuntu/linux-interrupted-apply-smoke.py --disposable-vm
```

helper は `recovery.home.arpa` を追加し、canonical file の置換後、apply client が
応答待ちの間に元の製品 process を pidfd で停止します。client が既に成功した場合は
この試験の成立と見なしません。停止時点で directory fsync が完了したかは断定しません。
apply は結果未確認の失敗を返す必要があります。60秒以内の自動復旧後、peer の LAN
namespace から `dig +short @198.18.15.1 recovery.home.arpa A` が `198.18.15.1` を返すこと、
probe・isolation・同じ peer identity・DHCP 更新を再確認してください。
helper の正常終了だけでは復旧の合格になりません。

probe はローカル・転送 DNS UDP/TCP、HTTP 対向が観測した NAT 送信元、管理→LAN の
正常通信を検査します。probe と isolation は最初の DHCP lease を最大45秒待ちます。
isolation は client 失敗と対向 HTTP ログへの非到達、WAN DNS 遮断を検査します。
手動で製品状態を直して合格にしません。fixture 準備の失敗は
環境準備として扱い、製品の合格数には含めません。

成功後は同じ flag で router の `cleanup-router`、peer の `cleanup-peer` を実行します。
空設定、firewall・管理アドレスの撤去を確認してから fixture の port・unit・namespace を
削除します。router setup は開始時の `net.ipv4.ip_forward` を記録し、cleanup で
fixture を撤去してから同じ値に戻します。一時出力は削除されます。残すのは手順・revision・判定です。
電源断の永続性、全中断位置、IPv6 再起動はこの試験の対象外です。


直近の限定検証では Linux 正式パッケージ `603126ea073f6d8e4ce3729975a2e97411d36850`
の新規導入、独立クライアントの DNS A（UDP/TCP）、NAT、管理経路と遮断を確認しました。
router 再起動、main process の SIGKILL、設定ファイル置換後かつ応答前の apply 中断の
3経路とも自動復旧し、peer の boot ID・DHCP PID は同一のまま、同じ lease の実更新を
確認しました。中断 apply は失敗応答となり、復旧後は追加 DNS record が引けました。
再起動後の最初の上流問い合わせは client query・upstream reply・client response を
capture で対応付けました。過去の1秒 probe 失敗の原因、IPv6 再起動、1.0 全機能維持を
証明する結果ではありません。

2 VM dual-stack 限定検証（2026-11-03）では正式 Linux パッケージ
`6977c66e9b3eb31074aaac502d3618f8ff159a57`、archive SHA-256
`c10fa0115dc98ba028e722d357c836b711bd40f7727d7d855ef3649f33887575`、
fixture `96e0003240341d4638ef5837cdfc430f645660f9` を使いました。
Ubuntu 24.04 router は再起動後 Linux 6.8.0-147 で動作し、独立 peer は動かし続けました。
新規導入・router 再起動・SIGKILL・設定適用中断の各段階で、WAN DHCPv4、WAN RA による
アドレスと default route、PD、LAN SLAAC、両 family の DNS A/AAAA UDP/TCP、
対向で確認した NAT と IPv6 転送、管理経路、IPv4/IPv6 遮断が通りました。
peer の boot ID・DHCP PID・上流 RA/PD/DNS PID は同一で、各中断後も同じ DHCPv4
アドレスの更新を確認しました。SIGKILL と適用中断はそれぞれ systemd の自動再起動1回で
復旧しました。中断 apply は結果未確認の失敗を返し、復旧後に追加 DNS record を
両 family・UDP/TCP で確認しました。WAN link-local は同一、native deny counter も非ゼロです。

初回は checksum offload metadata を捨てたため独立 dnsmasq の OFFER を受信できない
製品回帰がありました。実 kernel 回帰試験の修正前 FAIL・修正後 PASS を確認し、
旧 package を撤去して修正版を新規導入してから上記を確認しました。
LAN DHCPv6 IA_NA の更新・再起動、電源断、他 OS、1.0 全機能維持はこの結果の対象外です。
成功後は管理状態の撤去と package uninstall を確認し、専用 VM 2台・disk・一時 media も削除しました。

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

## IPv6 静的ルートの既定 metric

Linux では `IPv6StaticRoute.metric` の省略またはゼロ指定を実効値 1024 として扱います。
所有情報の `routeOwner.route.metric` とカーネル出力を照合してください。設定値のゼロを
そのままカーネル値と比較しません。正の明示指定値はそのまま使います。
[ルート読戻し手順](../design/routerd15-ipv6-route-readback.md)は適用・クライアント転送・
撤去を検査します。ローカル回帰試験は次のとおりです。

```sh
go test ./pkg/controller/chain -run '^TestIPv4RouteControllerAppliesAndWithdrawsIPv6StaticRoute$' -count=1
```


## パッケージ版 IngressService の切り替えと drain

正式パッケージを入れた専用 Linux VM、空の canonical 設定、試験用リンクと独立した
管理経路を使います。本番では実行しません。`tests/netns/ingress-conntrack-survive.sh`
は手書き nft ルールの試験であり、IngressService の製品経路の証明には使いません。

独立クライアントと2つの TCP echo backend を別々の namespace/veth サブネットに置き、
ルーター経由の戻り経路を用意します。外部所有 Interface 3件、実行時の forwarding
Sysctl、IngressService を宣言します。client 側 TCP listener、2つの名前付き backend、
failover、hairpin off、TCP 検査 timeout 500ms、正常・異常しきい値1を使います。
直接の backend 応答を確認してから通常 daemon の API で適用します。

1. status の選択と独立クライアントの実応答が backend1 で一致することを確認します。
2. TCP 接続を1本維持し、200msごとに backend1 の echo 応答を検査します。再接続は禁止です。
3. `routerctl ingress drain ingress/service backend=backend1` 後、45秒以内に新規接続が
   backend2 へ向かい、元の接続は backend1 のまま進行することを確認します。
4. main daemon を再起動し、drain、backend2 への新規接続、元の接続の継続を確認します。
5. `routerctl ingress undrain ingress/service backend=backend1` 後、新規接続が backend1
   に戻ることを確認し、元のクライアントを正常終了します。
6. backend1 を停止して backend2 への自動切り替え、復帰後の backend1 への自動復帰を
   status と実際の新規 TCP 応答で確認します。
7. 空設定で Healthy、所有 NAT table・helper の撤去を確認します。peer と client の終了、
   namespace/veth・一時ファイル削除、fixture の forwarding 設定復元後に VM を停止します。

失敗時は status、所有 nft table、クライアント結果を確認して終了し、製品状態を手修正して
通しません。全 backend 障害時の方針、sourceHash/random、各種アドレスソース、guest reboot
はこの試験の証明範囲外です。


## Linux 動的 NAT ピンホール試験

nftables、iproute2、Python、Go を備えた隔離 Linux VM で
`sudo ./tests/netns/nat44-flow-dnat-pinhole.sh` を実行します。独立した router/client/WAN
namespace、製品 NAT controller、送信元ポートを変えて応答する独立 UDP peer を使います。
source-SNAT 試験と同じ `ROUTERD_TEST_BINARY` と `ROUTERD_NAT44_TEST_DRIVER` を指定すると、
正式 validator と同じソースの試験 driver を使い、guest の Go は不要になります。
実行上限は `timeout 180`。手書き nft ルールに置き換えず、本番では実行しません。

許可通信の応答、送信元・宛先・送受信ポート・受信 interface の対象外拒否、5秒の期限切れ、
撤去後の拒否、再追加時の古い学習状態の不復活を確認します。許可対象を変えた後は、
新しい対象の実通信成功と以前の対象の拒否を両方確認します。拒否試験でも peer の送信要求
受信を確認し、外向き通信の失敗を成功扱いしません。終了時は namespace と一時ファイルを
撤去します。失敗時は peer の受信、応答の識別情報、所有 map を確認してから修正します。
通常 daemon の再起動や全てのポート衝突・変換の組み合わせは、この試験の証明範囲外です。

NAT・ポリシーの静的アドレス集合はルールと同じ nft トランザクションで置換し、削除した
アドレスを許可対象に残しません。DNS 解決で更新する集合は既存の address controller が
所有します。`TestNativeNATAddressSetReplacement` は実 nftables で IPv4/IPv6 の置換、
再適用、DNS 所有の要素の保持を検査します。NAT pool の構文試験と同じ隔離 user/network
namespace で `ROUTERD_RUN_NETNS=1`、親 namespace を `ROUTERD_HOST_NETNS` に指定します。


## パッケージ版のアドレス集合・転送先変更試験

正式 base/addon を導入した専用 Ubuntu VM、空の canonical 設定、通常 routerd service を
使います。iproute2・dnsmasq・dig を用意し、独立 client/WAN namespace を別々の veth で
VM へ接続します。IPv4/IPv6、client default route、WAN の戻り経路を設定し、元の
forwarding sysctl 値を保存します。本番ホストでは実行しません。

WAN DNS に各 family の2アドレスを持たせ、`probe.test` に `192.0.2.22` を応答させます。
VM LAN の独立 DNS は port 15353 で `192.0.2.11` を応答します。external Interface、
forwarding Sysctl、各 family の最初の WAN アドレスを含む IPAddressSet、LAN の
TCP/UDP port 53 を15353へ転送する LocalServiceRedirect を構成します。設定形式は
`tests/netns/local-service-redirect-runtime/config.yaml` を参照します。各入力を導入済み
routerd で validate し、fixture ディレクトリで
`routerctl apply --timeout 90s -f CONFIG --replace` を実行します。

各 apply 後45秒以内に Healthy を要求します。独立 client から全 WAN アドレスへ
`dig +short +time=2 +tries=1 @ADDRESS probe.test A` を、`+tcp` 有無の両方で実行します。
選択対象だけ local 応答、それ以外は WAN 応答を要求します。集合を2番目のアドレスへ
変更して apply し、main PID が不変のまま旧対象・新対象の両方が切り替わることを確認。
service 再起動後も同じ結果を要求します。集合と redirect を同時撤去すると、全てが
WAN 応答へ戻ることを確認します。各問い合わせ判定の収束待ちは15秒以内とし、
応答欠落を除外成功と扱いません。

最後に空設定で Healthy・resource 0・所有 NAT table/helper の不在を確認し、service と
独立 DNS を停止。namespace/veth/一時ファイルを撤去し、forwarding を元へ戻して VM を
停止します。失敗時は原因調査に必要な status・nft 集合/ルール・client 結果だけを保持。
この試験は通常 daemon の静的集合変更と再起動が対象です。DNS 解決集合の更新、
policy routing 通信、guest reboot、全 flow-pinhole 組み合わせの証明ではありません。


## パッケージ版 FirewallFlowPinhole の応答試験

正式パッケージ・空 canonical 設定の専用 Ubuntu VM で通常 service を使います。独立
LAN client/WAN peer namespace をルーティング接続し、元の forwarding 値を保存。
管理 interface は external Interface と `mgmt` FirewallZone に明示し、SSH の新規接続を
維持します。LAN は `trust`、WAN は `untrust`。NAT は構成しません。導入済み routerd で
validate し、`routerctl apply --timeout 90s -f CONFIG --replace` 後45秒以内に Healthy を要求。

独立 UDP peer は最初の WAN アドレスの32100で要求を受け、IDを記録してから32099で応答。
応答元は同じアドレスまたは許可 destination 範囲の別アドレスを選びます。client は試行毎に
新しい source port を使い、指定遅延+2秒待ちます。要求先と応答元の port を変えることで、
通常の established 許可で pinhole の欠落を隠さないようにします。FirewallFlowPinhole は
LAN→WAN、client source 範囲、WAN destination 範囲、outbound32100、inbound32099、timeout5秒。

`remoteAddress` で同じ応答元を許可し、別アドレスを拒否することを確認します。
`localEndpoint` へ apply すると別アドレスを許可し、port32098は拒否することを要求。
応答を6秒遅らせた場合は期限切れで拒否。main service 再起動の観測期限は停止期限を含め120秒。
Healthy 後は新しい許可通信を
確認します。zoneを残してpinholeだけ撤去すると、新しい応答が拒否されることを確認。
全拒否試験で peer の要求受信を確認し、送信不通を合格と扱いません。client port は再利用しません。

最後に空設定・Healthy・helperと所有filter tableの不在を確認。service/peer停止、
namespace/veth/一時ファイル撤去、forwarding復元、VM停止まで実施します。失敗時は
nft集合/counterと要求・応答identityを取得し、ローカル再現へ進みます。IPv4 firewallの
2相関方式・応答port・期限切れ・再起動・撤去が対象で、NAT応答map、全selector、guest reboot、
FreeBSDを確認したものではありません。


### DNS解決による集合更新の追加試験

通常daemonのredirect試験を、静的addressesから `names: [selected.test]` と
`refreshInterval: 1s` に変更します。独立WAN dnsmasqの追加hostsファイルに選択する
IPv4/IPv6を記載し、local TTLを1秒にします。daemonだけが参照するDNSは一時的な
runtime systemd drop-inの `BindReadOnlyPaths=FIXTURE_RESOLV_CONF:/etc/resolv.conf`
で指定し、daemon起動前にdaemon-reloadします。ホスト全体のresolverは変更しません。

最初の通信成功後、peerのhostsを2番目のアドレス対へ変更してHUPを送ります。routerdの
再applyやstatus編集はしません。既存controllerの30秒周期を考慮し、60秒以内に
IPAddressSet statusが新アドレス対だけになることを要求。旧/新対象・IPv4/IPv6・UDP/TCP
の8応答判定とmain PID不変を確認し、再起動後も維持、撤去後は外部応答に戻ることを確認。
daemon停止後はruntime drop-inを削除してdaemon-reloadし、一時ファイルを撤去します。
成功応答の変更を検証する試験であり、DNS障害、NXDOMAIN/空応答、TTL期限切れでの撤去
方針は未確認です。refresh間隔とTTL期限切れを混同しません。


## パッケージ版 WebConsole のブラウザ試験

専用Ubuntu VM・正式パッケージ・空canonical設定から、loopbackだけにbindするWebConsoleを
通常routerctl/service経由でapplyします。固有titleと未使用portを指定し、SSHでローカルの
loopback portへ転送。既存ChromeとPlaywright coreを使い、API差替えやSQLiteへの観測値注入は
しません。失敗時もブラウザとSSH tunnelを閉じます。

`/#resources` を開き、30秒以内に `#resources-table` にWebConsole resourceが表示されることを
要求。titleは `Resources - TITLE` です。ページから `/api/v1/config` にPOSTし、405/read-onlyを
確認します。CLIで未参照の静的IPAddressSetを追加し、ブラウザ再読込なしで45秒以内にrowが
現れ、GET `/api/v1/config` にも含まれることを確認。CLIで撤去後、rowが消えるか現在の設定に
宣言されていないと明記されることを要求します。保持された撤去statusを現行宣言と混同しません。

通常service再起動を120秒以内で観測し、ページ再読込後も残るresourceを表示、JavaScript
page errorが無いことを確認。空設定でHealthy・Web listener/helper不在を確認し、daemon停止、
browser/tunnel終了、入力削除、VM停止まで行います。全UI操作・admission・未観測表示の証明では
ありません。応答順序やunknown表示には既存の独立したfixtureブラウザ試験も使います。


### パッケージ版 DHCP リースから DNS への反映

正式 Debian パッケージ、空の設定、独立管理経路、busybox/dig を備えた専用 Ubuntu
VM を使う。隔離 veth のルーター側を `198.19.87.1/24`、クライアント側は固定 IPv4
なしとする。external Interface、dnsmasq DHCPv4Server の `.100`–`.110`/1 分 pool、
その named server を `dhcpDerived.sources` に指定する DNSZone `lease.test`、
正引き・逆引きで同 zone を選ぶ DNSResolver/Forwarder を宣言する。
validate 後に通常サービスを起動し、90 秒期限で apply する。

独立クライアント namespace で `busybox udhcpc -f -n -i eth0 -t 10 -T 2
-x hostname:lease-client -s <fixture-hook>` を期限付きサービスとして動かす。
bound/renew hook は提示アドレスを設定・記録し、deconfig はクライアントだけを撤去する。
60 秒以内に UDP/TCP の A が提示アドレス、PTR が完全なホスト名と一致すること。
dig は各回 `+time=2 +tries=1` を指定する。systemd で routerd を再起動（最大 120 秒）し、
リース注入や SQLite 編集なしで A を確認する。udhcpc に SIGUSR2 を送り DHCP RELEASE
させ、60 秒以内に両 transport の A/PTR が NXDOMAIN・ANSWER 0 になること。
応答なしを負応答と扱わない。

空設定を apply し Healthy・現在エラーなし・helper 撤去を確認する。クライアント停止、
namespace/veth/staging 撤去、VM 停止まで行う。失敗時は DHCP callback、DNS、実リース・
journal を観測しローカル再現へ落とす。取得・main 再起動・解放の試験であり、自然失効、
HA、DHCPv6 名、全 transport/policy の合格とはしない。


### パッケージ版 IngressService の分散と全停止

既存の隔離 client/backend 2 台の構成に `onNoHealthyBackends: reject` を指定する。
両 echo server の直接応答を先に確認する。独立 client に送信元アドレスを 17 個用意し、
通常 apply で `sourceHash` を選ぶ。45 秒以内に両 backend と effective selection を確認。
各送信元から新規 TCP 接続を 4 回開き、改行付き payload に対する backend 名付き応答を
照合する。同じ送信元の 4 回が同じ backend、標本全体で両 backend を使うこと。
これは同じ ruleset 内の affinity であり、設定置換をまたぐ対応の固定ではない。
`random` でも全応答が許可 backend に由来し、両方を使うことを確認する。
有限標本による均等性の保証ではない。connect/read は各 3 秒で打ち切る。

両 backend を停止し、45 秒以内に healthy 数 0 を確認する。新規接続が timeout や成功でなく
connection refused になること。backend2 のみ復旧して実応答、backend1 も復旧して
sourceHash を再確認する。空設定の Healthy、helper/NAT 撤去、peer 停止、namespace/link/
staging 撤去、forwarding 復元、VM 停止まで行う。動的アドレス、別の全停止ポリシー、
UDP、物理故障は別の試験とする。


### IPv6 preferred source 消失からの回復

Linux はアドレス消失時に policy default route を残して `src` だけを消すことがある。
所有 controller は rule と確認済み経路を撤去し、有効な送信元の復帰後に両方を復元する。
reconcile の間に消失・復帰した場合も対象。同一カーネル・interface identity の確認済み
所有記録と、src 以外が一致する経路に限って修復する。未所有・作成確認前の経路は採用せず、
新規作成は src を含む一致確認を維持する。

`make check-routerd15-system-native-linux` の
`TestPolicyIPv6NativeKernelSourceLossRecovery` は隔離 network/UTS/mount namespace、
実 veth、SQLite と製品 controller を使い、kernel の src 消失、route/rule 撤去、復帰、
最終撤去を確認する。通常 unit test では明示有効化がないため skip する。
正式パッケージのサービス・独立 payload 試験とは区別する。

policy の実効果の撤去成功後、削除済み EgressRoutePolicy の status も取り除く。
以前の PreferredSourceUnavailable/Pending を空設定の不健康判定に残さない。
宣言が残る無効条件、dry-run、撤去失敗の status は保持する。


パッケージ版の IPv6 host policy 試験は空設定・独立管理経路の専用 Ubuntu VM で実施する。
veth 上の source `2001:db8:88::1/64`、peer gateway `fe80::88`、peer loopback の
HTTP 宛先 `2001:db8:89::99/128` を準備する。製品 apply 前に peer の直結 HTTP 成功、
loopback 宛 HTTP 失敗を確認する。external WAN Interface と IPv6 priority
EgressRoutePolicy を宣言し、hostTraffic、preferredSource interface、static gateway、
table 128、priority 10128、metric 50 を指定する。90 秒期限で通常 daemon に apply。
60 秒以内に Applied、src 付き実経路、iif lo rule、loopback HTTP payload を要求する。

fixture の外部 source アドレスだけを削除し、PreferredSourceUnavailable/Pending、
経路・rule 不在、HTTP 失敗を確認する。source を戻し main PID 維持で通信復帰、
main 再起動（120 秒以内）後も通信を確認する。空設定で Healthy、古い status・経路・rule
不在、HTTP 失敗を確認。peer 停止・namespace 削除後、対向 veth 消滅を最大 5 秒待ち、
staging と VM を片付ける。host 自身の IPv6 通信の試験であり transit/hash 全般ではない。
シェルの否定判定は `if unexpected-command; then exit 1; fi` を使う。
単独の `! command` は `set -e` で失敗停止するとは限らない。


### namespace 試験の否定判定

`tests/netns/vxlan-l2-mss-clamp.sh` と
`tests/netns/vrrp-vmac-lan-address-lifecycle.sh` は、拒否対象の操作が成功した場合や
撤去対象が残る場合に明示的に終了する。後続コマンドのある単独 `! command` は
`set -e` だけでは assertion にならない。if/loop の条件や、戻り値を呼出元が確認する
関数は別なので、前後の制御構造を読んで判断する。

VXLAN gate は現ソースから通常ユーザーで作った試験 driver の絶対実行パスを
`ROUTERD_PATH_MTU_DRIVER` で受け取れる。明示 sudo で外側も mount/network namespace
に隔離して実行する。driver は隔離確認のため `/proc/1/ns/net` を読める必要がある。
controller と実カーネルの試験であり通常 daemon のパッケージ試験とは区別する。
VRRP gate は `ROUTERD_RUN_NETNS=1` と正式 package helper の
`ROUTERD_VRRP_VMAC_BIN` を指定し、内部で mount/network namespace に隔離する。
両 gate は timeout で上限を付け、終了 0 と後片付けを要求する。

## Linux の重複アドレスを使う VRF 分離試験

`make check-routerd15-native-parent-linux` は
`TestVRFNativeLinuxOverlappingPeerIsolation` を含みます。Python 3 と
カーネルの VRF 対応が必要です。専用ターゲットが network namespace と
sysfs を隔離します。ホストの network namespace で
`ROUTERD_RUN_NETNS=1` を設定して実行しないでください。

製品の VRF・アドレス controller に、同じローカル・対向 IPv4 アドレスを持つ
2 つの VRF を作らせます。各 VRF に、同じクライアントサブネットを持つ独立した
クライアント namespace も接続します。隔離したルーター namespace 内だけで
IPv4 転送を有効にし、各対向にはクライアントへの戻り経路を設定します。
独立した対向 namespace は異なる UDP 応答を返します。各検査を VRF に束縛した
ルーターのソケットと、ルーターを通過するクライアントの両方から実行します。
各 VRF から正しい応答を受信し、各対向専用アドレスへの通信が成功する一方、
別 VRF の対向専用アドレスへの通信はタイムアウトすることを合格条件とします。
SQLite 再オープン後も繰り返し、片側の VRF・アドレス削除後に他方の通信が
継続することを確認します。最後に両 VRF・所有アドレス・所有権記録の撤去を
確認します。実カーネルを使う controller 試験であり、パッケージ daemon の
再起動、委譲 IPv6・RA の合格を意味しません。

## Linux VRF 配下の委譲アドレスと RA

`make check-routerd15-native-parent-linux` は
`TestVRFNativeLinuxDelegatedRA` も実行します。`ip`、`sysctl`、`nsenter`、
`ping` と、前節と同じ明示的な namespace 隔離が必要です。
有限寿命の PD lease を SQLite に入力し、製品の VRF・委譲アドレス・RA
controller に実際の処理を行わせます。独立したクライアントのカーネルが
SLAAC アドレスと `proto ra` デフォルトルートを取得し、ルーターの委譲
IPv6 アドレスと、VRF を通過した先の独立した上流 namespace へ疎通できることを
確認します。転送設定・上流アドレス・戻り経路は隔離した試験環境の設定です。
保存した PD プレフィックスを変更し、新しい SLAAC アドレスを送信元として
上流へ通信でき、旧アドレスが非推奨化されることを確認します。設定撤去後は
ルーターの新旧所有アドレス、RA デフォルトルート、RA 所有権記録が消えることを
合格条件とします。DHCPv6 交換とパッケージ daemon の再起動は対象外です。
拡張後の初回に RA 送信の `sendmsg: invalid argument` が発生しました。
委譲アドレスの DAD 完了後もリンクローカル送信元が tentative だったためです。
Linux では送信元のカーネルフラグを確認し、DAD 中は `LinkLocalDADPending` として
待ち、controller の再評価で送信を開始します。同じターゲットに含まれる
`TestListenRejectsTentativeLinkLocal` は、この状態を実カーネルで再現して確認します。
既存 controller は status イベントと 30 秒周期で再評価します。この試験は
再評価を直接呼び出すため、パッケージ起動時の復帰時間の証明には使いません。

## パッケージ版 VRF の検証前提

隔離 Linux ルーター VM と独立した対向 VM を使用します。Ubuntu generic
カーネルでは設定適用前に `modinfo vrf` で VRF モジュールを確認します。
最小 OS イメージで不足していれば、環境準備中に次を実行します。

```sh
sudo apt-get install kmod "linux-modules-extra-$(uname -r)"
sudo modprobe vrf
```

パッケージは稼働カーネルに一致させ、環境確認をやり直します。製品の判定試験中に
環境を修復しません。今回の Ubuntu 24.04 最小ゲストは `CONFIG_NET_VRF=m` でも
`vrf.ko` がありませんでした。パッケージ版・ハッシュ、独立 Kea の待受、両側の
隔離 L2 経路も確認し、VM 起動時は routerd より先に試験用 NIC を復元します。
Kea 起動前に対向 NIC のリンクローカルアドレスが tentative／dadfailed でないことを
確認し、UDP 547 の実際の待受を必須にします。プロセスの稼働だけでは不十分です。
電源断試験前に fixture 設定を永続化し、終了時は通常の撤去と fixture 削除の後に
ゲストのファイルシステム書込みを完了させてから電源を切ります。

空の Healthy 状態から VRF の LAN/WAN、IPv4 アドレス、実 DHCPv6 PD、委譲
LAN/WAN アドレスと RA を適用します。対向の IPv4/IPv6 通過転送、daemon 再起動、
対向 VM・DHCP サーバープロセスを維持したルーター VM の再起動を確認します。
最後に空設定へ戻し、Healthy、所有 VRF・アドレス・helper の撤去、削除済み VRF の
health 行が残らないことを確認します。`Absent/OwnedGenerationWithdrawn` は設定と
所有権記録の両方から消えた時点で削除し、再起動後の残留状態も回収する必要があります。
OS 再起動後は旧 boot ID の所有記録を退役させ、新しい VRF を取得します。
同じ起動中の名前空間変更は拒否し、同名の他者所有インターフェースを取得・削除しません。
dry-run は旧記録を維持します。`TestVRFRebootRecovery` がこれらを検証します。

## Tailscale の広告解除と復旧

`ROUTERD_TEST_TAILSCALE=/absolute/path/to/tailscale make check-routerd15-tailscale-cli`
は、実 Tailscale CLI と専用 Unix LocalAPI fixture を使います。ホスト daemon や
tailnet には接続しません。

通常 daemon の検証では、独立した Linux ゲスト 2 台、試験用 Headscale/TLS、
認証済みの試験専用ノード、ルーター配下の HTTP 応答元を先に準備・確認します。
制御経路は試験するデータ経路から分離し、転送と宛先経路を Sysctl・IPv4Route
リソースで宣言します。

1. subnet と exit-node を広告する TailscaleNode を適用し、試験サーバーで承認します。
   独立クライアントから subnet 経由と、その subnet 外の宛先への exit-node 経由で
   期待する HTTP 本文を取得できることを確認します。
2. クライアントの exit-node 選択を解除してから、YAML の広告経路を削除し、
   `advertiseExitNode: false` を適用します。Running/Healthy、daemon の広告なし、
   Headscale の available routes なし、クライアントの経路なし、旧 HTTP 応答の不達を
   要求します。空の `available_routes` は省略される場合があります。approved routes
   が残っているだけで広告継続とは判定しません。
3. 再広告して両経路の通信を確認し、routerd 再起動後にも exit-node 通信を確認します。
   製品状態の手動修復や再認証で合格にしません。
4. 空構成を適用し、Healthy、管理ユニット・経路の撤去、`WantRunning: false` を
   確認します。独立 peer と制御サーバーを停止し、試験用転送設定・namespace・
   ファイルを復旧・撤去してゲストを停止します。

IPv6 exit 通信、認証期限切れ、制御サーバー断からの回復は別の検証項目です。

### Tailscale の認証期限切れ

上記の隔離環境で、通常 daemon の Running/Healthy と独立 HTTP 通信を確認します。
Headscale の `nodes expire --identifier <test-node-id>` で試験対象ノードだけを
失効させます。実 backend の NeedsLogin、既存 reconcile 周期内の
Pending/AuthenticationRequired、全体状態が Healthy でないことを確認します。
観測を強制する apply・再起動は行わず、main PID 不変と旧 HTTP 通信の不達も
要求します。

新しい試験専用の一回限りの認証キーをログに出さずに発行し、0600 の環境ファイルに
保存して通常の設定 API から `authKeyEnv`・`authKeyFile` を宣言します。
Running/Healthy、以前の reason/runtimeError の消去、独立 HTTP 通信の復旧を
確認します。最後に空構成へ撤去し、試験用認証ファイルも削除します。
これはノード認証の失効と明示的な認証更新の検証であり、自動キー発行や
制御サーバー断の検証ではありません。

### 両アドレス族の exit 通信と制御サーバー停止

独立した応答元に IPv6 を追加し、制御用 LAN の外にある宛先への
IPv6StaticRoute を宣言します。適用前には独立クライアントから到達できないこと、
exit-node 選択後には IPv4・IPv6 の期待する HTTP 本文が取得でき、IPv6 経路が
`tailscale0` を通ることを要求します。

制御サーバーの返信経路を exit 経路から分離してください。Headscale とクライアントを
同じゲストに置く場合は、クライアントの `--exit-node-allow-lan-access=true` を
使い、exit 選択後にサーバーからルーターへの LAN 経路と新しい HTTPS health 通信を
確認します。これがないと LAN から来た SYN に対する SYN-ACK が exit 経由になります。
以前からの接続が残っているだけでは環境確認になりません。systemd-run の一時
ユニットは停止後に回収される場合があるため、再開時は同じ起動コマンドを使います。

制御サーバーを停止して両ノードからの health 通信が失敗することを確認し、
両アドレス族で新規 TCP HTTP 通信を繰り返します。再認証せずにサーバーを再開し、
サーバー側で両ノードの online と両経路の通信復旧を確認します。routerd 再起動後も
IPv6 通信を確認し、空構成への適用で両宛先経路の撤去・切断・Healthy を要求します。
クライアントの exit 選択・LAN 例外を戻し、試験環境を撤去します。これは限定した
停止試験であり、認証期限を超えて通信できるという保証ではありません。

## 最新パッケージでの組合せライフサイクル

実機のリソース数・依存関係を維持し、試験専用の追加宣言を明示します。
予約クライアントと負荷クライアントのアドレスを重複させません。Tailscale は
認証済み TLS endpoint を入力生成時から使い、後の手動 URL 置換に依存しません。
永続 peer bridge 上で一時 AFTR link を再作成する前に、試験先アドレスの古い
近隣キャッシュだけを除去します。古い veth MAC 宛ての失敗を新 AFTR の異常と
見なさず、独立 peer、同版の正式パッケージ、空設定 Healthy、同期 SSH を確認してから
製品試験を開始します。受信ログも一時実行ファイルと同じ容量制限付き tmpfs に置き、
3 signal 分の余裕と root filesystem の空きを開始前に確認します。ファイル単位の
サイズ上限だけでは、空きが少ない filesystem を保護できません。

初回起動後は、管理された自己再起動に伴う API ソケット交換を含めて待ち、
予約済み再起動がなくなってからクライアント試験を始めます。集約 phase はそのまま
報告します。journal の独立した上限を超える保持期間は
`LogRetention/default: Degraded / EventJournalSafetyBound`、待機系は停止対象の
DS-Lite とその tunnel/VMAC の外部観測 Down を表示します。これらだけでは通信障害を
意味しません。他の Pending/Error/Degraded を隠したり、表示を緑にするために宣言した
保持期間を変えてはいけません。両台復旧後は期待する役割、controller エラーなし、
説明した観測以外の異常なしを要求します。意図的に相手を電源断した間の同期エラーは、
復帰後に解消する必要があります。

予約クライアント2台、全条件付き DNS と認証済み DoH、管理アクセスの許可・拒否、
DNS/NTP 転送、native TLS 分類、両台の Telemetry を確認します。同じ独立74通信を
自然な PD 更新、両方向の電源断・復帰、最新 lease/PD 同期、各方向の稼働 daemon
再起動まで維持します。応答内容、元の変換tuple、TCPの単一accept、復帰ノードの
共有アドレス主張を照合します。TCP接続が維持されても無停止と表現せず、最大応答間隔を
報告します。最後は両台の empty Healthy、実撤去、独立 peer の後片付けまで確認します。

### 新規導入時の lease 同期の前提

Ubuntu/Debian では `DHCPv4ServerLeaseSync`、`DHCPv6ServerLeaseSync`、
`DHCPv6PrefixDelegationLeaseSync` の宣言から runtime package に `rsync` と
`openssh-client` を生成します。過去の lab 準備や旧ルーターへの導入済みツールに
依存せず、`dpkg-query -W rsync openssh-client`、`command -v rsync`、
`command -v ssh` と APT の導入履歴を確認します。生成される
`Package/router-runtime` は内部の依存関係計画であり、`routerctl get` で
取得できる宣言済みリソースではありません。

パッケージ導入だけでは相手の本人確認・認証は準備されません。新ノードごとに
専用鍵を用意し、秘密鍵を保護して、相手には公開鍵だけを登録します。受信側にも
SSH server と rsync が必要です。信頼できる管理経路で相手の host key を確認し、
known_hosts 方針を設定してください。秘密鍵を YAML や Git に保存しません。
必要な両方向の無人認証に続けて、実際の最新 lease/PD の受信まで確認します。
SSH 成功だけで同期成功とは判定しません。1.5 の新規導入に旧秘密鍵や旧 DB の
コピーは不要です。

### PD 更新時の読取り順序

DS-Lite が旧 PD を読んだ直後に共有アドレスの有効期限更新が確定する場合が
あります。共有アドレスの所有記録は最新の PD と照合し、同一プレフィックスの
有効な延長でトンネルを撤去しません。失効、プレフィックス変更、現在の PD より
長いアドレス有効期限は引き続き撤去条件です。ローカル回帰テストでこの読取り順序と
削除が発生しないことを確認し、実機では独立 peer による更新を挟んだ ifindex と
元の通信フローの維持を確認します。

更新コマンド実行中だけでなく、実機の読み戻し後を含む全保存段階を検査します。
既に保持し、更新後も利用可能なアドレスは Applied を維持します。一瞬でも Pending を
公開すると、依存トンネルの撤去と既存通信の経路変更につながります。
`TestLANHeldRenewalKeepsAppliedDependencyAndConfirmedLifetime` で全保存を検査します。
新規取得や利用不能なアドレスの準備完了確認は引き続き必要です。

### 昇格通知前の conntrack mark

共有MACを記憶しているクライアントは、daemon が MASTER を認識する前でも
VMAC の起動直後から送信できます。待機時のルールから複製済みの宣言された mark を
保持し、VIP 公開が確認されるまで維持します。公開後は通常のヘルス判定による
経路変更に戻り、新しい未マーク通信は利用可能な経路を選びます。回帰テストでは
BACKUP、公開前 MASTER、公開済み MASTER を区別します。実機の合否は引き続き
両方向の切替で元の変換 tuple と TCP 接続を維持できることで判定します。

`TestHAPromotionNativePreservesImportedConntrackMark` は network・UTS・mount を
隔離し、実UDP通信と conntrack の mark を照合します。通常の `go test` では skip し、
実行には `ROUTERD_RUN_SYSTEM_POLICY_NATIVE=1` と外側の名前空間IDを明示します。
実行コマンドと後片付けは英語版の
[HA試験手順](https://routerd.net/docs/operations/linux-ha-pair-qualification)に記載しています。
Linux 6.8.0-149 で修正前は backup の mark が 272 から 275 に変わり失敗し、
修正後は backup・公開前の保持と公開後の通常変更が通過しました。
これは mark 保持の検証であり、HA全体や過去のTCPリセット原因の確定を意味しません。
