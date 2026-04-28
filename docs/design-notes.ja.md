# 設計メモ

この文書には、まだ安定したリソース定義に入れていない初期の設計メモを
残します。

## プレフィックス委譲を受けられない場合の DS-Lite

参考資料:

- [佐藤広生氏 FreeBSD Workshop 2017 資料](https://people.allbsd.org/~hrs/sato-FBSDW20170825.pdf)

PR-400NE / NTT フレッツ環境の検証では、ホームゲートウェイ再起動直後に
DHCPv6 プレフィックス委譲を受けられるホストがある一方で、別のホストは
IA_PD の DHCPv6 Solicit を送り続けても Advertise / Reply を受け取れない
ことがあります。プレフィックス委譲を受けられない場合の逃げ道として、
次の構成を検討します。

- WAN 側では RA/SLAAC または DHCPv6 IA_NA で得た IPv6 アドレスを使う。
- その上流 IPv6 到達性を使って DS-Lite を確立する。
- LAN 側には DHCPv4 と DS-Lite で IPv4 到達性を提供する。
- LAN 側 IPv6 は委譲プレフィックスをルーティングせず、ブリッジまたは
  それに近い形で上流へ通す。

これは設計メモであり、まだリソース定義ではありません。IPv6 をブリッジ
する構成は所有境界が変わるため、実装前に別途検証が必要です。

- routerd は LAN 側のルーティング済み IPv6 プレフィックスを所有しない。
- RA、DHCPv6、ファイアウォール、近隣探索の挙動が上流のホームゲート
  ウェイに寄る可能性がある。
- IPv6 ブリッジ時に LAN ホストを意図せず外へ露出しないよう、ファイア
  ウォールの既定動作を慎重に決める必要がある。
- DS-Lite のトンネル送信元として、委譲LANプレフィックスから作った
  アドレスではなく、WAN 側の SLAAC / IA_NA アドレスも選べる必要がある。

将来のリソース候補:

- 「プレフィックス委譲はないが上流 IPv6 は使える」ことを表す WAN 状態。
- WAN 側 SLAAC / IA_NA アドレスを使える DS-Lite 送信元アドレス選択。
- 明示的なファイアウォール既定動作を持つ IPv6 ブリッジまたは通過用
  リソース。
- 過去に委譲されたプレフィックス、DUID、IAID、リース情報を記録し、
  ホームゲートウェイが新規要求に敏感な場合に更新に近い挙動を優先する
  再試行方針。

現在の足場:

- `IPv6PrefixDelegation` は、観測したプレフィックスの状態を routerd の
  状態保存領域に `ipv6PrefixDelegation.<name>.*` として記録する。
- 現在の下流プレフィックスが見えなくなっても、最後に見えた
  プレフィックスは残す。
- `IPv6PrefixDelegation.spec.convergenceTimeout` は、直前まで見えていた
  現在のプレフィックスを短時間だけ維持するための待ち時間です。OS 側の
  DHCPv6 クライアントがプレフィックス委譲を取り直している間に、すぐ
  「消えた」と判断しないために使います。この値は systemd-networkd や
  KAME `dhcp6c` のパケット再送間隔とは別物です。現時点の routerd は、
  それらのクライアント固有の再送間隔を調整しません。
- systemd-networkd では、実行時ファイルから読み取れる範囲で IAID/DUID
  の材料を記録する。NTT 系プロファイルでは、上流インターフェースの
  MAC アドレスから期待されるリンクレイヤ DUID も記録する。
- FreeBSD の `dhcp6c` では、IAID を設定値から、DUID を
  `/var/db/dhcp6c_duid` から記録する。ホームゲートウェイが DUID/IAID の
  組を覚えている場合、新規クライアントではなく既存リースの更新として
  扱わせるために重要になる。
- FreeBSD では、KAME `dhcp6c` が下流インターフェースに設定したアドレス
  から委譲プレフィックスを観測し、その後で設定された安定サフィックスの
  アドレスを二つ目のアドレスとして追加する。
- FreeBSD の `dhcp6c` は `-n` 付きで起動し、必要な再起動では SIGUSR1 で
  止めてから起動し直す。通常の停止では DHCPv6 Release を送るため、
  ホームゲートウェイ側に古いリースが残り、クライアント側は新規 Solicit
  に戻ってしまう状態を避けるためです。
- `IPv6PrefixDelegation.spec.hintFromState` は既定で有効です。routerd が
  最後に見えたプレフィックスを覚えており、その有効寿命が切れていなければ、
  systemd-networkd または KAME `dhcp6c` へプレフィックスヒントとして
  渡します。リースの記憶がない場合や古すぎる場合は、プレフィックス長だけの
  ヒントへ戻します。上流が古いリースを忘れていても、通常の要求として
  扱われるだけで済むようにします。
- PR-400NE の検証では、DHCPv6 の Advertise / Reply が UDP 宛先ポート
  546、かつ送信元ポート 547 以外で返ることを確認した。ファイアウォール
  方針では宛先ポート 546 を見ればよく、送信元ポート 547 を必須条件に
  してはいけない。
- PR-400NE の検証では、ホームゲートウェイ再起動後に複数の `/60`
  プレフィックス委譲が同時に見える一方で、再起動前の新規 Solicit は
  無応答に見えることがあった。当面は DUID/IAID と最後に見えた
  プレフィックスを記録し、不要な Release を避ける。更新に近い再試行は
  今後の別作業として扱う。

この段階では、routerd が DHCPv6 Renew/Rebind パケットを自前で生成する
わけではありません。その実装は、管理経路を落とさず、OS 側の DHCPv6
クライアントと競合しないよう、別の段階で慎重に進めます。

## 状態と所有台帳の保存

routerd は、ローカル状態と所有台帳を SQLite に保存します。既定の場所は
Linux では `/var/lib/routerd/routerd.db`、FreeBSD では
`/var/db/routerd/routerd.db` です。現在のスキーマは Kubernetes の保存方式を
参考にし、反映処理の世代、リソース単位の状態、イベントを分けて記録します。
「routerd が何を望み、何を観測し、それがいつ起きたのか」を、リソースごとに
追えるようにするためです。

- `generations` は、反映を試みた単位ごとに、結果、警告、使った設定のハッシュを
  記録します。
- `objects` は、リソース単位の状態 JSON を保存します。たとえば
  `IPv6PrefixDelegation/wan-pd` は、リース、DUID、IAID、時刻を一つの行に
  まとめて持ちます。
- `artifacts` は、routerd が管理しているホスト側構成物の所有台帳です。所有者は
  API バージョン、種類、名前に分けて保存します。
- `events` は、反映時の警告やプレフィックス委譲の観測を記録します。あとで
  詳細表示コマンドから使えるようにするための土台です。
- `access_logs` は、将来のローカル HTTP API 監査用です。今回は表だけを作り、
  まだ書き込みません。

JSON は文字列として保存し、SQLite の JSON1 機能で確認できます。

```sh
sqlite3 /var/lib/routerd/routerd.db \
  "select json_extract(status, '$.lastPrefix') from objects where kind = 'IPv6PrefixDelegation' and name = 'wan-pd';"
```

古い `state.json` と `artifacts.json` は、移行用の入力としてだけ扱います。
直前の二表だけの SQLite スキーマも移行元として扱います。routerd は `state`
の行を `objects` へ、古い `artifacts` 表を新しい所有者列へコピーし、移行後に
古い表を削除します。JSON ファイルは引き続き `.migrated` 付きの名前へ変えます。
プレリリース期間中は互換性よりも保存構造の整理を優先しつつ、初回起動の移行は
自動で行います。

routerd の実行に `sqlite3` コマンドは不要です。ただし、人が状態を調べる
ときには便利です。特に `json_extract` で JSON の一部だけを見る用途に向いて
います。`jq` は、信頼済みローカルプラグインが標準入出力で JSON を扱い、
シェル製プラグインが応答を組み立てる場面で必要になるため、引き続き残します。

### PR-400NE 実機観測: プレフィックスヒント

2026-04-28 に、FreeBSD と KAME `dhcp6c` を使う router01 を PR-400NE 配下で
検証しました。管理用インターフェースは別ネットワークに残し、変更したのは
WAN 側の DHCPv6 プレフィックス委譲クライアント設定だけです。検証前の
router01 には、ローカルに記録された `lastPrefix` はなく、安定した DUID と
IAID `0` だけが状態保存領域に残っていました。

観測結果:

- 状態からのプレフィックスヒントがない場合、router01 は UDP 546 から
  `ff02::1:2` の UDP 547 へ DHCPv6 Solicit を送った。IA_PD は IAID `0`、
  プレフィックス長だけのヒント `::/60` だった。40 秒の取得中、PR-400NE
  から Advertise / Reply は返らなかった。
- routerd の状態保存領域に、過去に見えていた
  `2409:10:3d60:1240::/60` を一時的に入れると、routerd は
  `dhcp6c.conf` に `prefix 2409:10:3d60:1240::/60 infinity;` を出力した。
  tcpdump でも、Solicit の IA_PD に IAID `0` と
  `2409:10:3d60:1240::/60` のプレフィックスヒントが入ることを確認した。
  ただし、この場合も 40 秒の取得中に PR-400NE から Advertise / Reply は
  返らなかった。
- 同じプレフィックスを一時的に入れたまま IAID だけを `1` に変えると、
  tcpdump では IAID `1` と同じプレフィックスヒントを持つ Solicit を確認
  できた。この場合も PR-400NE から Advertise / Reply は返らなかった。
  検証後、IAID は `0` に戻した。
- これらの取得中に DHCPv6 Release は観測されなかった。クライアントの
  再起動には、Release を避ける停止方法を使った。

解釈:

- KAME `dhcp6c` は、`dhcp6c.conf` の `prefix ... infinity;` を IA_PD の
  プレフィックスヒントとして実際にパケットへ入れる。したがって、routerd の
  FreeBSD 向け出力は意図した形で働いている。
- 今回の PR-400NE の状態では、プレフィックスヒントだけでは、見えなくなった
  委譲プレフィックスは復帰しなかった。この時点のホームゲートウェイには
  router01 向けの有効な紐付けがないか、紐付けがあっても新規 Solicit には
  応答していない可能性が高い。
- 次に意味がある検証は、リースがまだ有効なうちの Renew/Rebind 経路です。
  そのため、プレフィックスヒントの仕組みは残しつつ、OS 側クライアントと
  競合しない更新操作、または期限切れ前の Renew/Rebind を観測する仕組みを
  追加する必要があります。

### PR-400NE 実機観測: 更新フック

2026-04-28 に、router01、router02、router03 へ最新バイナリを入れ、
routerd サービスを再起動したうえで、OS 側クライアントへ更新を促す処理を
検証しました。管理経路は残し、検証後に状態保存領域は元に戻しました。

FreeBSD と KAME `dhcp6c` を使う router01 では、
`2409:10:3d60:1230::/60`、2 時間前の観測時刻、有効寿命 14400 秒を
構造化リースとして一時的に入れました。反映後、`lastRenewAttemptAt` が
記録されたため、routerd の更新フックは実行されています。`vtnet0` の
tcpdump では、次のように見えました。

- 新しいバイナリで生成設定が変わったため、最初の反映で `dhcp6c` 設定が
  修復され、まず `::/60` の長さだけのヒントを持つ Solicit が出た。
- その後、IA_PD の IAID `0` と、一時的に入れた
  `2409:10:3d60:1230::/60` のプレフィックスヒントを持つ Solicit が出た。
- 取得中に Renew、Rebind、PR-400NE からの Advertise / Reply は見えなかった。

この結果から、KAME `dhcp6c` では、更新フックとヒントの供給により、
ヒント付き Solicit までは出せることが分かりました。ただし今回の
PR-400NE の状態では、委譲プレフィックスは復帰しませんでした。また、
この検証は純粋な Renew 経路ではありません。クライアント側に見える有効な
委譲リースがなく、最初の反映で生成設定の修復も走ったためです。

systemd-networkd を使う router02 でも、同じ形で一時的なリース記録を
入れました。反映後、`lastRenewAttemptAt` が記録されたため、routerd は
`networkctl renew ens18` を呼んでいます。しかし `ens18` の tcpdump では、
検証時間中に DHCPv6 パケットは見えませんでした。この状態では、
`networkctl renew` は DHCPv6 プレフィックス委譲の Renew、Rebind、Solicit
を観測できる形では出していません。

設計上の結論:

- `lastRenewAttemptAt` は残す価値があります。routerd が、見失ったリースに
  対して一度だけ更新を促したことを確認できます。
- 現在の更新フックは、確実な復旧手段ではなく、できる範囲で OS 側
  クライアントを刺激する処理として扱います。
- KAME `dhcp6c` では、クライアントが有効なリースを覚えていない場合、
  Release なしの再起動とプレフィックスヒントが現実的な復旧操作になり得ます。
  ただし、それは新規 Solicit 経路であり、ホームゲートウェイが応答するかに
  依存します。
- systemd-networkd では、よりよい制御経路を探すか、networkd がまだ
  DHCPv6 状態を持っている有効期限前に定期確認する仕組みが必要です。
- 今後は、更新フックの呼び出しと結果をログに明示します。状態変化だけに
  頼らず、パケット取得と照合しやすくするためです。

## 他のルーターにおける DHCPv6 プレフィックス委譲の扱い

この節では、オープンソース実装と商用ルーターの情報を見比べ、routerd の
プレフィックス委譲設計に取り込める点を整理します。

参考資料:

- [RFC 9915: Dynamic Host Configuration Protocol for IPv6](https://datatracker.ietf.org/doc/html/rfc9915)
- [OpenWrt odhcp6c README](https://github.com/openwrt/odhcp6c)
- [OpenWrt odhcp6c ソース参照](https://lxr.openwrt.org/source/odhcp6c/src/dhcpv6.c)
- [OpenWrt odhcpd README](https://github.com/openwrt/odhcpd)
- [OpenWrt odhcpd ソース参照](https://lxr.openwrt.org/source/odhcpd/src/)
- [systemd.network マニュアル](https://www.freedesktop.org/software/systemd/man/254/systemd.network.html)
- [FreeBSD dhcp6c(8)](https://man.freebsd.org/cgi/man.cgi?manpath=freebsd-release-ports&query=dhcp6c&sektion=8)
- [FreeBSD dhcp6c.conf(5)](https://man.freebsd.org/cgi/man.cgi?query=dhcp6c.conf)
- [pfSense advanced networking documentation](https://docs.netgate.com/pfsense/en/latest/config/advanced-networking.html)
- [OPNsense DHCP documentation](https://docs.opnsense.org/manual/isc.html)
- [MikroTik RouterOS DHCP documentation](https://help.mikrotik.com/docs/display/ROS/DHCP)
- [MikroTik RouterOS IP pools documentation](https://help.mikrotik.com/docs/display/ROS/IP%2BPools)
- [Cisco IOS XE DHCPv6 Prefix Delegation](https://www.cisco.com/c/en/us/td/docs/ios-xml/ios/ipaddr_dhcp/configuration/xe-16-9/dhcp-xe-16-9-book/ip6-dhcp-prefix-xe.html)
- [Cisco DHCPv6 PD configuration example](https://www.cisco.com/c/en/us/support/docs/ip/ip-version-6-ipv6/113141-DHCPv6-00.html)
- [Juniper Junos IA_NA and Prefix Delegation](https://www.juniper.net/documentation/us/en/software/junos/subscriber-mgmt-sessions/topics/topic-map/dhcpv6-iana-prefix-delegation-addressing.html)
- [Juniper Junos subscriber LAN prefix delegation](https://www.juniper.net/documentation/us/en/software/junos/subscriber-mgmt-sessions/topics/topic-map/dhcpv6-prefix-delegation-lan-addressing.html)
- [Juniper Junos DHCPv6 client reference](https://www.juniper.net/documentation/us/en/software/junos/cli-reference/topics/ref/statement/dhcpv6-client-edit-interfaces.html)

### 観測した傾向

| 実装 | リースと寿命の扱い | 再起動後の扱い | 識別子の扱い | 更新・再束縛・新規要求・解放の方針 | 失われた時の扱い |
| --- | --- | --- | --- | --- | --- |
| OpenWrt `odhcp6c` | プレフィックス、優先寿命、有効寿命をスクリプトや ubus へ渡す。統計には Solicit、Request、Renew、Rebind、Reply、Release が出る。 | DHCPv6 の状態変化に合わせてスクリプトが走り、OS 側の設定を更新する。 | クライアント識別子を DHCPv6 option 1 として扱う。DHCPv6 のパケット状態はクライアントデーモンが所有する。 | 標準的な DHCPv6 の状態遷移を実装し、`bound`、`updated`、`rebound`、`unbound`、`stopped` を通知する。 | `unbound` は全 DHCPv6 サーバーを失った状態で、その後クライアントを再開する。 |
| OpenWrt `odhcpd` | サーバー側のリースをファイルへ保存する。プレフィックス委譲リースにはインターフェース、DUID、IAID、期限、割り当て、長さ、有効なプレフィックスが含まれる。 | プレフィックス変化に合わせて動的に再設定する。 | サーバー側の紐付けは DUID と IAID をキーにする。 | クライアント、サーバー、リレーの責務を分けている。 | 委譲プレフィックスがない時でも、RA、DHCPv6、近隣探索のリレー構成を取れる。 |
| systemd-networkd | DHCPv6 クライアントと下流プレフィックス割り当てを持つ。上流、下流、プレフィックスヒント、サブネット番号、RA 広告を設定できる。 | 設定ファイルと DHCPv6 交換から実行時状態を作る。routerd の唯一の正として使える安定リースデータベースは提供しない。 | `DUIDType`、`DUIDRawData`、`IAID` を設定できる。 | `SendRelease` がある。systemd の版によって既定動作が変わり得るため、Release を抑えたい時は明示するべき。 | 変化は networkd 内部で処理される。外部制御側は、結果としてできたアドレス、経路、ログを観測する必要がある。 |
| FreeBSD / KAME `dhcp6c` | クライアント DUID を `/var/db/dhcp6c_duid` に保存する。IA_PD と `prefix-interface` の設定で下流へアドレスを付ける。 | `SIGHUP` は再初期化、`SIGTERM` は停止。通常は Release を送る。`SIGUSR1` は Release なしで停止する。 | IAID は `dhcp6c.conf` に書き、DUID は DUID ファイルに保存される。 | `-n` は終了時の Release を抑止する。新規 Solicit に応答しにくいホームゲートウェイでは重要。 | 下流プレフィックスが残っているかどうかは別プロセスが観測する必要がある。 |
| pfSense / OPNsense | どちらも画面上で DHCPv6 プレフィックス委譲を扱う。pfSense は DUID 編集と Release 抑止の設定を文書化している。OPNsense は有効なプレフィックスリースと DUID による固定割り当てを文書化している。 | 設定は下位の DHCP 部品へ反映される。DUID を保存して再インストール後も同じ識別子を使える。 | pfSense は raw DUID、DUID-LLT、DUID-EN、DUID-LL、DUID-UUID を扱える。OPNsense のサーバー側固定割り当ては DUID を使う。 | pfSense は `dhcp6c` が既定で Release を送ることと、それを止める設定を明示している。 | DHCPv6 状態とログを画面から確認できる。 |
| MikroTik RouterOS | クライアントはプレフィックス、残り時間、状態を表示する。サーバー側の割り当ては DUID、IAID、寿命、期限、最終確認時刻を持つ。動的プールにも期限が付く。 | 受け取ったプレフィックスを動的プールへ入れる。プレフィックスを外す時は古いプレフィックスを寿命 0 で広告できる。 | クライアントは独自 DUID、インターフェース由来 DUID、IA_PD ID、IA_NA ID を指定できる。サーバー側は DUID と IAID を使う。 | 状態には searching、requesting、bound、renewing、rebinding、stopping がある。`renew` は更新を試み、失敗すると初期化する。`release` は明示的に解放して再開する。 | 状態とスクリプトで、現在のプレフィックスと有効性を外へ出せる。 |
| Cisco IOS XE | DUID は安定しており、最小番号のインターフェース MAC アドレスから作られる。同じ DUID でも IAID が違えば別クライアントとして扱われる。 | DHCPv6 クライアントで得たプレフィックスを general prefix として持ち、インターフェースから参照できる。 | DUID の安定性と IAID による分離が文書化されている。 | 通常は 4 メッセージ交換。Rapid Commit も有効化できる。表示コマンドでは IA_PD の T1/T2 が見える。 | クライアント状態、委譲プレフィックス、DNS、ドメイン、general prefix を表示できる。 |
| Juniper Junos | リース時間からアドレスやプレフィックスの寿命、更新時刻、再束縛時刻を決める。IA_NA と IA_PD は別々のリース時刻を持ち得る。 | 加入者向けの委譲プレフィックス保存では、ログアウト後もプレフィックスを保持し、再ログイン時に同じ委譲プレフィックスを割り当てられる。 | DHCPv6 クライアント識別子の DUID 型を設定できる。加入者管理系では紐付けとリースを追跡する。 | 加入者向けの一部構成では、IA_NA と IA_PD を一つの Solicit に入れる方針が文書化されている。 | 運用コマンドで DUID、リース時刻、紐付け状態が見える。 |
| VyOS | 現在の VyOS は生成設定で DHCPv6 クライアント挙動を作る。過去の議論や不具合では、WIDE `dhcp6c` 風のプレフィックス委譲、`sla-id`、下流インターフェース設定が見える。 | PPPoE や下流インターフェースの起動順が問題になりやすい。 | WIDE 風の DUID ファイルや IA_PD 設定がログや生成設定に出る。 | 実務上の教訓は、インターフェース起動のたびに盲目的にデーモンを再起動せず、識別子を安定させること。 | 下流のプレフィックス長やアドレス設定間違いとして問題が表れやすい。 |
| dnsmasq / WIDE DHCPv6 | dnsmasq は LAN 側 DHCPv6 サーバーや RA には有用だが、WAN 側プレフィックス委譲クライアントの正にはしない方がよい。WIDE/KAME `dhcp6c` はクライアント側プレフィックス委譲で重要。 | dnsmasq のリースはサーバー側の情報。WIDE/KAME のクライアント状態は DUID ファイル、設定、実行中デーモンの状態に依存する。 | dnsmasq はサーバー DUID を設定できる。WIDE/KAME はクライアント DUID ファイルを持つ。 | dnsmasq は LAN サービスに残し、WAN 側プレフィックス委譲の取得は担当させない。 | LAN 側 DHCP/RA は routerd が観測したプレフィックス状態に追従させる。 |

### routerd に取り込む方針

採用すべきもの:

- routerd の状態保存領域に、構造化されたプレフィックス委譲リースを持つ。
  リソース名、インターフェース、クライアント実装、DUID、IAID、観測できる
  場合はサーバー DUID、現在のプレフィックス、過去のプレフィックス、
  優先寿命、有効寿命、T1、T2、最後に見えた時刻、最後に消えた時刻、
  取得状態を保存する。
- 有効な過去プレフィックスを OS 側の DHCPv6 クライアントへヒントとして
  渡す。DUID、IAID、プレフィックスの組を覚えているホームゲートウェイでは
  効果が期待でき、上流がヒントを無視しても通常の要求へ落ちるだけで済む。
- DUID と IAID を明示的な期待状態として扱う。systemd-networkd と KAME
  `dhcp6c` で可能な範囲は設定として出力し、観測した識別子が期待値と違う
  場合は警告する。
- Release を送らない方針は明示する。FreeBSD/KAME では `-n` と SIGUSR1
  による停止を続ける。systemd-networkd では対象の版で使えるなら
  `SendRelease=no` を出力し、使えない場合の挙動を文書に残す。
- `renew` 操作の抽象を追加する。最初の実装では routerd が DHCPv6
  パケットを自作せず、OS 側クライアントに安全な更新手段がある場合だけ
  それを呼ぶ。
- プレフィックス委譲の状態を制御 API に出す。現在の状態、時刻、最後に
  見えた時刻、最後に消えた時刻、可能なら最後の状態遷移、警告を返す。
  RouterOS、Cisco、Junos、OpenWrt に近い運用上の見え方を目指す。
- 既存の収束待ち時間は残す。ただし、優先寿命と有効寿命が観測できるように
  なったら、そちらを主に使う。
- 下流プレフィックスが消えた時は、LAN 側の RA/DHCPv6 情報を意図して
  廃止または撤回する。MikroTik のように、古いプレフィックスを寿命 0 で
  広告する考え方は、将来の RA 出力に取り込む価値がある。
- DHCPv6 応答を受けるファイアウォールは広めに保つ。クライアント宛ての
  UDP 宛先ポート 546 を許可し、送信元ポート 547 を必須条件にしない。

見送るもの:

- 現時点では routerd 内で DHCPv6 Renew/Rebind パケットを自作しない。
  それには完全な DHCPv6 クライアント状態機械、サーバー識別子、
  再送タイマー、認証や再設定の扱い、OS 側クライアントとの競合回避が
  必要になる。
- dnsmasq を WAN 側プレフィックス委譲状態の正にしない。dnsmasq は
  LAN 側の DHCP、RA、DNS の部品として使う。
- すべてのホームゲートウェイがプレフィックスヒントや過去プレフィックスを
  標準どおりに扱うとは仮定しない。個別の癖はプロファイルに持たせるが、
  既定の考え方は DHCPv6 の寿命と OS クライアントの状態遷移に従う。
- 商用ルーターの加入者管理のような大きな仕組みは、まず一台のルーターでの
  プレフィックス委譲リース記録と制御 API が固まるまで入れない。

### バックログ

- `routerctl show ipv6pd` を拡張し、寿命、T1/T2、クライアント状態、警告も
  表示する。DUID、IAID、現在のプレフィックス、最後に見えた
  プレフィックス、観測時刻の表示はすでに入っている。
- 内部の `PDLease` 状態モデルを拡張し、OS 側クライアントから取得できる
  場合は、サーバー DUID、優先寿命、有効寿命、T1、T2、取得状態も保存する。
- OS ごとの安全な更新フックをさらに固める。
  - systemd-networkd: routerd は現在、ローカルのリース記録から前回リースが
    まだ有効と判断できる場合に `networkctl renew <link>` を使う。
  - FreeBSD/KAME `dhcp6c`: routerd は現在、同じ条件で実行中のクライアントへ
    SIGHUP を送る。より強い方針を入れる前に、制御ソケットの有無を調べる。
- `releasePolicy` または `sendRelease` 設定を追加し、NTT 系ホーム
  ゲートウェイ向けプロファイルでは保守的な既定値にする。
- プロファイルに、プレフィックスヒント、IA_NA と IA_PD を同じ要求に
  入れるかどうか、DUID 型、IAID、Release 抑止、収束待ち時間、
  ファイアウォールの応答許可条件を持たせる。
- プレフィックス委譲が消えた時、アドレス削除や RA/DHCPv6 サービス削除の
  前に、下流へ古いプレフィックスの廃止を伝える挙動を追加する。

## NTT フレッツ系 DHCPv6 プレフィックス委譲と PR-400NE 向け仮説

この節では、NTT フレッツ系のプレフィックス委譲について、公開仕様と
ルーター設定例から分かることを整理します。あわせて、検証環境の
PR-400NE が期待していそうなクライアント挙動を、推定として分けて
記録します。

参考資料:

- [RFC 8415: Dynamic Host Configuration Protocol for IPv6](https://www.rfc-editor.org/rfc/rfc8415.html)
- [NTT 東日本 技術参考資料](https://www.ntt-east.co.jp/gisanshi/)
- [NTT 東日本 IP 通信網サービスのインタフェース フレッツシリーズ 第三分冊](https://flets.com/pdf/ip-int-3.pdf)
- [NTT 西日本 IP 通信網サービスのインタフェース](https://www.ntt-west.co.jp/info/katsuyo/pdf/23/tenpu16-1.pdf)
- [NTT 西日本 NGN IPv6 ISP 接続用トンネル接続インタフェース](https://www.ntt-west.co.jp/open/ngn/pdf/ipv6_tunnel_uni.pdf)
- [Yamaha RT シリーズ DHCPv6 機能](https://www.rtpro.yamaha.co.jp/RT/docs/dhcpv6/index.html)
- [Yamaha IPv6 IPoE 機能](https://www.rtpro.yamaha.co.jp/RT/docs/ipoe/index.html)
- [NEC UNIVERGE IX フレッツ 光ネクスト IPv6 IPoE 設定例](https://jpn.nec.com/univerge/ix/Support/ipv6/native/ipv6-internet_dh.html)
- [NEC IX-R/IX-V DHCPv6 機能説明](https://support.necplatforms.co.jp/ix-nrv/manual/fd/02_router/14-1_dhcpv6.html)
- [インターネットマルチフィード transix DS-Lite サービス](https://www.mfeed.ad.jp/transix/dslite/)
- [インターネットマルチフィード transix 対応機器](https://www.mfeed.ad.jp/transix/dslite-models/)
- [インターネットマルチフィード transix 用語集](https://www.mfeed.ad.jp/transix/faq/glossary/)
- [Sorah's Diary: フレッツ光ネクスト ひかり電話あり環境の DHCPv6-PD 観測](https://diary.sorah.jp/2017/02/19/flets-ngn-hikaridenwa-kill-dhcpv6pd)
- [rixwwd: PR-400NE / Dream Router の DHCPv6 パケット観測](https://rixwwd.hatenablog.jp/entry/2023/04/09/211529)
- [SEIL: NGN IPv6 ネイティブ IPoE 接続例](https://www.seil.jp/blog/10.html)

### 公開資料から言えること

- RFC 8415 では、通常の取得手順は Solicit、Advertise、Request、Reply の
  4 メッセージです。Rapid Commit を使うと Solicit と Reply だけに
  短縮できますが、クライアント側は Rapid Commit が使われる前提に
  してはいけません。
- RFC 8415 では、クライアントの待受ポートは UDP 546、サーバーと
  リレーの待受ポートは UDP 547 です。したがって、受信時に送信元
  ポート 547 のパケットだけを許可する規則は実装上の近道であり、
  クライアント受信経路としては安全ではありません。
- RFC 8415 では、IA_PD の中に IA Prefix を入れて、希望する
  プレフィックスや長さをヒントとして送れます。プレフィックスを
  `::/length` とすることで、長さだけのヒントも送れます。
- RFC 8415 では、Renew はリースを出した元のサーバーへの更新要求、
  Rebind は Renew が失敗した後に任意のサーバーへ出す再束縛要求、
  Solicit は有効寿命がすべて切れた後の新規要求として扱われます。
- Confirm はアドレス向けの確認です。リンク上で割り当て済みアドレスが
  妥当か確認できますが、routerd が委譲プレフィックスの復旧手段として
  依存するものではありません。
- NTT 東日本と NTT 西日本の公開インターフェース資料は、主にフレッツ網側の
  IPv6 接続や DHCPv6、DHCPv6-PD 関連 RFC を示しています。一方で、
  PR-400NE の LAN 側での細かな実装差までは明記していません。
- 現在公開されている NTT 東日本と NTT 西日本のフレッツシリーズ向け
  インターフェース資料では、端末側 DUID は DUID-LL 方式または
  DUID-LLT 方式に従い、MAC アドレスから生成する必要があるとされています。
  そのため、DUID-EN、UUID 由来の DUID、machine-id 由来の DUID は、
  フレッツ端末として文書化された範囲から外れます。
- 同じ資料では、DHCPv6-PD を使うサービスで網から渡される
  プレフィックス長は 48 ビットまたは 56 ビットと説明されています。
  PR-400NE 配下で観測している /60 は、NGN から直接渡された長さではなく、
  ホームゲートウェイが下流向けに分割したものと考えるべきです。
- Sorah's Diary の 2017 年の実機報告は、現在の公開資料より強い内容です。
  そこでは、NGN の DHCPv6-PD は DUID-LL 以外の Solicit を黙って無視し、
  DUID-LLT や DUID-EN では通らないと報告されています。現在の公式資料は
  DUID-LLT も許しています。そのため routerd では、「DUID-LL のみ」を
  DHCPv6 全般の規則ではなく、厳しめの NTT 向けプロファイルの癖として
  扱います。
- NEC の IX 向け公式設定例では、フレッツ 光ネクストはひかり電話契約の
  有無で挙動が変わると説明されています。ひかり電話なしでは RA を使い、
  ひかり電話ありでは DHCPv6-PD を使います。同じ設定例では、DNS を
  DHCPv6 クライアントで受け取り、委譲されたプレフィックスを下流へ RA で
  広告し、DNS 配布のために RA の other-config flag を立てています。
- NEC の設定例では、DHCPv6 を送信元 547 から宛先 546、送信元 546 から
  宛先 547 という条件で通しています。これはベンダー設定例としては
  妥当ですが、PR-400NE の観測例では Advertise が一時ポートから出た
  記録があります。そのため routerd の NTT ホームゲートウェイ向け
  設定では、送信元ポートにかかわらず UDP 宛先 546 を受けるべきです。
- Yamaha の DHCPv6 資料では、RT シリーズが DHCPv6-PD、取得した
  プレフィックスの下流利用、フレッツ 光ネクストやフレッツ 光クロス向けの
  `ngn type` 設定を持つことが分かります。
- transix / インターネットマルチフィードの資料からは、DS-Lite の
  サービス構成と対応機器は分かります。ただし、PR-400NE が下流ルーターへ
  どのように DHCPv6-PD を返すかまでは定義していません。routerd では、
  transix の DNS や AFTR の扱いを、プレフィックス取得とは別の機能として
  扱うべきです。

### NTT 公式仕様の精読

ローカルに取得済みの NTT 東日本 `ip-int-3.pdf` と NTT 西日本
`tenpu16-1.pdf` を、DHCPv6 プレフィックス委譲クライアント側の細かい
要件に絞って読み直しました。以下のページ番号は PDF ビューアー上の通し番号
ではなく、本文に印字されているページ番号です。

| 確認項目 | 該当箇所 | routerd での読み取り |
| --- | --- | --- |
| IA_NA と IA_PD を同時に求めるか | NTT 東日本、フレッツ 光25G `2.4.1.1.2` pp. 5-6 は、端末が DHCPv6-PD でプレフィックスを受け、DHCPv6 では 128 ビットのアドレスを取得できないと説明しています。同じ資料の DHCPv6 オプション表 `2.4.1.1.5` pp. 7-8 では、IA_NA、IA_TA、IA Address、Rapid Commit に注記 2 が付き、その注記はこのインターフェース仕様では使わないという意味です。NTT 西日本、フレッツ 光クロス `2.4.2.1.2` pp. 21-22 も、プレフィックスを使い、DHCPv6 では 128 ビットのアドレスを取得できないという形です。 | NTT 向け既定で IA_NA を IA_PD と一緒に要求する根拠は見つかりません。むしろ、必要なのは IA_PD であり、IA_NA はこの範囲の端末モデルには入っていないと読むのが自然です。 |
| DHCPv6 の再送間隔 | NTT 東日本と NTT 西日本の本文から、SOL_TIMEOUT や REQ_TIMEOUT のような DHCPv6 クライアント再送定数は見つかりませんでした。見つかるタイマー記述は、別プロトコルやサービス動作に関するものです。 | 自前クライアントを作る場合は DHCPv6 の RFC に沿った再送を使います。PR-400NE や HGW の応答タイミングに備えて取得待ち時間はプロファイルで長くできますが、NTT PDF から独自の再送値を作るべきではありません。 |
| UDP 送信元ポート | 両 PDF は DHCPv6 と DHCPv6-PD について RFC3315/RFC3633 を参照していますが、クライアントの送信元ポートを追加で縛る記述は見つかりません。通常の DHCPv6 クライアント、サーバーのポートモデルを超える規則は確認できませんでした。 | routerd が自前でソケットを持つ場合は DHCPv6 クライアントポートから送るべきです。ただし、受信側ファイアウォールで応答の送信元ポート 547 を必須にしてはいけません。これは NTT PDF の特別規則ではなく、RFC の考え方と PR-400NE の実機観測に基づきます。 |
| RA の M/O フラグと開始タイミング | NTT 東日本、フレッツ 光クロス `4.4.2.1.2` p. 22 と NTT 西日本、フレッツ 光クロス `2.4.2.1.2` pp. 21-22 は、RA の O フラグと M フラグが 1 になる場合がある一方、Information-Request には対応しないと書いています。NTT 東日本と西日本のフレッツ 光ネクスト `2.4.2.1.2` pp. 60-61 / pp. 59-60 では、O=1 なら Information-Request、M=1 なら DHCPv6-PD を推奨するとしています。また、音声系サービスでは DHCPv6-PD を使い、48 ビットまたは 56 ビットのプレフィックスを受けると説明しています。 | M フラグは、仕様に沿った PD 開始の合図として扱えます。一方で Information-Request の扱いはサービス種別ごとにそろっていません。NTT HGW 向けプロファイルでは Information-Request を必須にせず、将来の自前クライアントでは RA 待ち開始と強制 Solicit の両方を選べるようにします。 |
| Rapid Commit | NTT 東日本 `2.4.1.1.5` pp. 7-8 などのオプション表には Rapid Commit が出ますが、IA_NA と同じく「この仕様では使わない」注記が付いています。2 メッセージの Rapid Commit 取得を使う、または必須にするという本文は見つかりませんでした。 | `ntt-flets-with-hikari-denwa` では Rapid Commit を要求せず、前提にもしません。サーバーが使ってきた場合だけ観測結果として記録します。 |
| Solicit にサーバー識別子が必要か | NTT 東日本 `2.4.1.1.4` p. 6 と NTT 西日本 `2.4.2.1.4` p. 22 などの DUID 節では、網側 DUID は安定していて MAC アドレス由来と説明されています。オプション表にも Server Identifier はありますが、初回 Solicit に Server Identifier を入れる必要があるという記述は見つかりません。 | Solicit にはクライアント識別子を入れ、サーバーが分かった後に Server Identifier を使います。Renew のためにサーバー識別子は保存しますが、新規 Solicit で存在しないサーバー識別子を作って送るべきではありません。 |
| Confirm メッセージ | NTT 東日本と NTT 西日本の本文検索では、Confirm の対応記述は見つかりませんでした。DHCPv6-PD の手順図では Solicit、Advertise、Request、Reply が示されています。 | Confirm は NTT 向け PD 復旧手段にはしません。有効なリースがある間は Renew/Rebind、失われた後はプレフィックスヒント付き Solicit で扱います。 |

IA_NA と IA_PD を同時に求める場合の実装案:

- NTT 向けプロファイルの既定は IA_PD のみにします。公式仕様は、DHCPv6 で
  128 ビットのアドレスを取得できないことを繰り返し説明しており、オプション表
  でも IA_NA は使わない項目に分類されています。
- もし今後、特定の HGW や上流経路で同時要求の方が安定することが分かれば、
  `requestNonTemporaryAddress: true` のような明示的なプロファイル項目を
  追加します。NTT 向けだから自動的に IA_NA を入れる、という動きにはしません。
- systemd-networkd では、その項目が有効な時だけ、プレフィックス委譲に加えて
  DHCPv6 アドレス利用も出力する案にします。現在の NTT 向け既定は
  プレフィックス委譲だけです。
- KAME `dhcp6c` では、既存の `send ia-pd` と `id-assoc pd` に加えて、
  `send ia-na <iaid>;` と `id-assoc na <iaid> { };` を出す案にします。
- 出力テストでは、NTT 既定では IA_PD だけが出ること、明示的な実験スイッチを
  入れた時だけ IA_NA と IA_PD が同時に出ることを確認します。

### 2026-04-29 HGW 再起動後のパケット比較

PR-400NE を再起動した後、HGW 画面では 3 台すべてに `/60` が払い出されました。

- router01: `2409:10:3d60:1220::/60`
- router02: `2409:10:3d60:1230::/60`
- router03: `2409:10:3d60:1240::/60`

router03 の WAN 側で、再起動中の DHCPv6 パケットを取得しました。この取得には、
IX2215 や Aterm と思われる別 DUID の Solicit は入りませんでした。そのため、
商用ルーターとのバイト単位比較はまだ未完了です。一方で、router03 と PR-400NE
の完全な取得手順は取れています。

| 項目 | router02 の比較用 Solicit | router03 の成功した取得 |
| --- | --- | --- |
| クライアント DUID | DUID-LL、MAC `bc:24:11:30:5d:76` | DUID-LL、MAC `bc:24:11:40:32:de` |
| UDP ポート | 送信元 546、宛先 547 | 送信元 546、宛先 547 |
| 要求内容 | IA_PD のみ。IA_NA、Rapid Commit、Vendor Class、Reconfigure Accept はなし。 | Solicit では IA_PD のみ。IA_NA、Rapid Commit、Vendor Class、Reconfigure Accept はなし。 |
| Solicit のオプション順 | IA_PD、Client FQDN、Option Request、Client Identifier、Elapsed Time | IA_PD、Client FQDN、Option Request、Client Identifier、Elapsed Time |
| 要求オプション | SIP ドメイン、SIP アドレス、DNS、SNTP、NTP、option 82、option 103、option 144 | DNS、SNTP、NTP、option 82、option 103 |
| プレフィックスヒント | `2409:10:3d60:1230::/60` | 最初は `2409:10:3d60:1220::/60`。HGW は `2409:10:3d60:1240::/60` を返した。 |
| サーバー応答 | 比較用取得では応答なし | Advertise と Reply はリンクローカル `fe80::1eb1:7fff:fe73:76d8`、UDP 送信元 49153、宛先 546 から返った。 |
| サーバー側オプション | 観測なし | Server Identifier は DUID-LL、MAC `1c:b1:7f:73:76:d8`。DNS、SNTP、IA_PD T1 7200、T2 12600、優先寿命 14400、有効寿命 14400。Reply には Reconfigure Accept も入っていた。 |

重要な観測結果:

- 成功した router03 の Solicit は、router02 の routerd 生成 networkd Solicit と
  ほぼ同じ構造です。どちらも DUID-LL、IA_PD のみ、UDP 送信元 546、主要な
  オプション順も同じです。
- PR-400NE は今回も UDP 送信元 49153 から応答しました。したがって、
  DHCPv6 クライアント向け受信は、送信元 547 ではなく宛先 546 で許可する
  方針を維持します。
- router03 の最初の Solicit は、状態から得た古い
  `2409:10:3d60:1220::/60` をヒントにしていました。しかし HGW は
  `2409:10:3d60:1240::/60` を委譲しました。正確なヒントはあくまで希望であり、
  違う `/60` が返っても routerd はそれを受け入れ、すぐ状態を更新する必要が
  あります。
- router02 は復帰後も `2409:10:3d60:1230::/60` でした。これは前回の
  `lastPrefix` と一致します。HGW が紐付けを覚えている、または同じ紐付けを
  作り直せる場合に、`hintFromState` が同一プレフィックス復帰を助け得る
  という裏付けになります。ただし、ヒントだけが原因だとまでは断定しません。
- キャプチャには、router03 が Reply 後に Release を送り、その直後に再度
  Solicit して成功する流れも入っていました。取得が成功している時でも、
  制御されていないクライアント再起動は HGW 側の紐付けを揺らす可能性が
  あります。systemd-networkd 経路では Release 抑止を引き続き重視します。

### 2026-04-29 PD 復活後の状態検証

同じ復旧タイミングで、routerd の状態と OS の状態を確認しました。

| ホスト | routerd の PD 状態 | LAN 側 IPv6 | dnsmasq | DS-Lite | IPv6 外部疎通 |
| --- | --- | --- | --- | --- | --- |
| router01 / FreeBSD | `routerctl describe ipv6pd/wan-pd` では、現在値と最後に見えた値が `2409:10:3d60:1220::/60`。 | `vtnet1` はリンクローカルのみ。期待する `2409:10:3d60:1220::1/64` はまだ付いていない。 | `routerd_dnsmasq` は存在せず、`dnsmasq` も動いていない。 | DS-Lite トンネルは見えない。 | `ping6 ipv6.google.com` は経路なしで失敗。 |
| router02 / NixOS | `routerctl describe ipv6pd/wan-pd` では、現在値と最後に見えた値が `2409:10:3d60:1230::/60`。 | `ens19` に `2409:10:3d60:1230::2/64` と、DS-Lite 送信元用の `::100`、`::101`、`::102` が付いている。 | 動作中。設定は `192.168.160.2` と `2409:10:3d60:1230::2` で待ち受け、`ens19` で RA を出し、DNS として `2409:10:3d60:1230::2` を配る。 | `ds-lite-a`、`ds-lite-b`、`ds-lite-c` が MTU 1454 で起動し、送信元は `::100`、`::101`、`::102`。 | `ping6 ipv6.google.com` は成功。 |
| router03 / Ubuntu | `routerctl describe ipv6pd/wan-pd` では、現在値と最後に見えた値が `2409:10:3d60:1240::/60`。 | `ens19` に `2409:10:3d60:1240::3/128` が付いており、委譲 `/64` の経路もある。 | 動作中。設定は `192.168.160.3` と `2409:10:3d60:1240::3` で待ち受け、`ens19` で RA を出し、DNS として `2409:10:3d60:1240::3` を配る。 | `ds-lite-a`、`ds-lite-b`、`ds-lite-c` が MTU 1454 で起動し、送信元は `2409:10:3d60:1240::3`。 | `ping6 ipv6.google.com` は成功。 |

今回の作業端末は、確認した LAN 側リンクでグローバル IPv6 アドレスと既定経路を
持っていませんでした。そのため、下流クライアントが SLAAC で `/64` を取れて
いるかの確認は、この作業端末からは完了できませんでした。

今後の更新観測メモ:

- HGW の応答では、T1 は 7200 秒、T2 は 12600 秒、優先寿命と有効寿命は
  14400 秒でした。
- 正常取得後、T1 の少し前から T2 を越えるまでパケットを取る観測枠を用意します。
  運用者側の cron または systemd timer で tcpdump を起動し、
  `/tmp/routerd-pd-renew-<host>-<timestamp>.pcap` のように保存すればよいです。
- 見るべき点は、OS 側クライアントが覚えているサーバーへ Renew を出すか、
  T2 後に Rebind へ進むか、routerd の `lastObservedAt` が新規 Solicit なしに
  更新されるか、HGW が同じ `/60` を維持するかです。
- FreeBSD の LAN 側への伝搬は、この観測の後で修正しました。router01 は
  保存済みのプレフィックス委譲状態から LAN 側アドレスを導出し、管理対象の
  dnsmasq rc.d サービスを動かし、IPv6 既定経路を得て、IPv6 通信できる
  状態になっています。

### 2026-04-28 の実機 DUID 確認

DUID 管理のコードを変える前に、検証機で実際の設定とパケットを確認しました。

| ホスト | クライアント | 出力された設定 | 観測した DUID | パケット上の結果 |
| --- | --- | --- | --- | --- |
| router02 / NixOS | systemd-networkd | `/etc/systemd/network/10-netplan-ens18.network.d/90-routerd-dhcp6-pd.conf` に `DUIDType=link-layer` と `2409:10:3d60:1230::/60` のヒントが出ていました。 | `networkctl status ens18` は `DUID-LL:0001bc2411305d76` を表示しました。tcpdump でも `client-ID hwaddr type 1 bc2411305d76` でした。 | networkd 向け出力は効いており、実際に DUID-LL が出ています。60 秒の再起動取得では Advertise / Reply は見えませんでした。 |
| router03 / Ubuntu | systemd-networkd | `/etc/systemd/network/10-netplan-ens18.network.d/90-routerd-dhcp6-pd.conf` に `DUIDType=link-layer` と `2409:10:3d60:1220::/60` のヒントが出ていました。 | `networkctl status ens18` は `DUID-LL:0001bc24114032de` を表示しました。tcpdump でも `client-ID hwaddr type 1 bc24114032de` でした。 | networkd 向け出力は効いており、実際に DUID-LL が出ています。60 秒の再起動取得では Advertise / Reply は見えませんでした。 |
| router01 / FreeBSD | KAME `dhcp6c` | `/usr/local/etc/dhcp6c.conf` は IA_PD と `::/60` のヒントを要求しています。 | `/var/db/dhcp6c_duid` は、長さ `0e 00` の後に DUID type `00 01` が続いており、DUID-LLT でした。tcpdump でも `client-ID hwaddr/time type 1 time 830607215 bc2411e3c238` でした。 | FreeBSD 側では、まだ DUID ファイルを routerd が管理していません。router01 は DUID-LLT を送っており、厳しめの NTT/HGW 挙動では弾かれる可能性があります。 |

結論:

- router02 と router03 については、systemd-networkd 向けの実装が発火して
  いないわけではありません。`DUIDType=link-layer` は出力され、
  networkd もそれを使い、パケット上でも DUID-LL でした。
- router02 と router03 が今回応答を得られなかった理由は、DUID だけでは
  説明できません。同じ DUID-LL で過去にプレフィックスを受け取っているため、
  ホームゲートウェイのリース状態、再起動直後のタイミング、Solicit と
  Renew の違いは引き続き有力な仮説です。
- FreeBSD/KAME 側には明確な未対応があります。router01 は生成済みの
  DUID-LLT ファイルを使い続け、Solicit でも DUID-LLT を送っています。
  次の実装では、NTT 向けプロファイルで `/var/db/dhcp6c_duid` を管理または
  取り込み対象にしてから、FreeBSD での HGW 挙動を再検証するべきです。

実装後の追跡結果:

- routerd は、FreeBSD KAME の DUID ファイルが DUID-LL でない場合に退避し、
  NTT 系リンクレイヤ DUID プロファイルで `dhcp6c` を起動する前に
  `0a 00 00 03 00 01 <上流MAC>` を書き込むようになりました。
- router01 では、`/var/db/dhcp6c_duid` が DUID-LLT から
  `0a 00 00 03 00 01 bc 24 11 e3 c2 38` に変わりました。以前のファイルは
  `/var/db/dhcp6c_duid.bak.20260428T094248Z` として残っています。
- 変更後の tcpdump では `client-ID hwaddr type 1 bc2411e3c238` を確認でき、
  パケット上でも DUID-LL になりました。その 60 秒の取得では
  Advertise / Reply は見えなかったため、ホームゲートウェイのリース状態や
  Solicit と Renew の違いは、FreeBSD の DUID 修正後も残る仮説です。

### 2026-04-29 手動 Renew 実機テスト

3 台の検証ルーターで、WAN 側インターフェースを 60 秒 tcpdump しながら、
OS の DHCPv6 クライアントに手動更新を促しました。

| ホスト | クライアントと操作 | パケット観測 | テスト後の状態 | 解釈 |
| --- | --- | --- | --- | --- |
| router01 / FreeBSD | KAME `dhcp6c`; `kill -HUP $(cat /var/run/dhcp6c.pid)` | `dhcp6c` は Renew を送りませんでした。UDP 送信元 546、DUID-LL `bc:24:11:e3:c2:38`、IA_PD IAID 0、`2409:10:3d60:1220::/60` の正確なヒントを持つ Solicit を `ff02::1:2` へ送り直しました。60 秒以内に Advertise / Reply は見えませんでした。 | routerd は current / last prefix ともに `2409:10:3d60:1220::/60` を観測し続けました。`lastObservedAt` は、ホスト上に残っているプレフィックスを見て更新されました。 | この構成での HUP は本当の Renew ではありません。プレフィックスヒント付き Solicit へ戻る、制御された再取得です。 |
| router02 / NixOS | systemd-networkd; `networkctl renew ens18` | `ens18` では DHCPv6 パケットが 1 つも見えませんでした。コマンドは成功終了し、systemd-networkd のログにも新しい行は出ませんでした。 | routerd は current / last prefix ともに `2409:10:3d60:1230::/60` を観測し続けました。`lastObservedAt` は、Reply 受信ではなくホスト状態の観測で更新されました。 | この systemd-networkd の版と設定では、`networkctl renew` は DHCPv6-PD の手動更新として働いていません。 |
| router03 / Ubuntu | systemd-networkd; `networkctl renew ens18` | `ens18` では DHCPv6 パケットが 1 つも見えませんでした。コマンドは成功終了し、systemd-networkd のログにも新しい行は出ませんでした。 | routerd は current / last prefix ともに `2409:10:3d60:1240::/60` を観測し続けました。`lastObservedAt` は、Reply 受信ではなくホスト状態の観測で更新されました。 | router02 と同じく、networkd 配下の routerd で信頼できる手動更新手段にはなっていません。 |

このテストでは、通常の T1/T2 による更新経路は確認できませんでした。一方で、
OS 任せの手動更新フックが弱いことは分かりました。

- FreeBSD `dhcp6c` は刺激できますが、その動きは Renew ではなく、
  プレフィックスヒント付き Solicit です。
- systemd-networkd は `networkctl renew` を成功扱いにしますが、観測窓の中で
  DHCPv6-PD パケットを出しませんでした。
- routerd の状態は、ホスト上に残っている委譲プレフィックスを観測することで
  新しく見えます。これはローカル状態としては有用ですが、上流リースが
  更新された証拠とは区別しなければなりません。

設計上の扱い:

- OS 側クライアントへの手動刺激は、あくまで復旧を促す補助として扱います。
- 可能な場合は、T1、T2、優先寿命、有効寿命、サーバー識別子、最後の
  DHCPv6 メッセージ遷移を保存します。これらがないと、上流リースの更新と
  ホスト上の残存プレフィックス観測を区別できません。
- 将来の routerd 内蔵 DHCPv6 クライアントでは、Renew と Rebind を自前で
  実装し、パケット単位でログに残すべきです。PR-400NE/HGW の調査には、
  これが一番見通しのよい経路です。
- `networkctl renew` に頼らず、T1 の時刻をまたいで tcpdump を取り続ける
  受動観測を残件に追加します。

### 2026-04-29 T1/T2 自然更新窓の受動観測

3 台の検証ルーターで、WAN 側インターフェースの DHCPv6 を 4 時間取得する
受動観測を `2026-04-28T15:50Z` ごろに開始しました。

| ホスト | WAN 側インターフェース | 開始時のプレフィックス | 取得ファイル |
| --- | --- | --- | --- |
| router01 / FreeBSD | `vtnet0` | `2409:10:3d60:1220::/60` | `/tmp/pd-renew-window.pcap`, `/tmp/pd-renew-window-routerctl.log` |
| router02 / NixOS | `ens18` | `2409:10:3d60:1230::/60` | `/tmp/pd-renew-window.pcap`, `/tmp/pd-renew-window-routerctl.log` |
| router03 / Ubuntu | `ens18` | `2409:10:3d60:1240::/60` | `/tmp/pd-renew-window.pcap`, `/tmp/pd-renew-window-routerctl.log` |

現在の routerd 状態には、T1、T2、優先寿命、有効寿命がまだ明示的には
保存されていません。そのため、この観測では検証環境で見えている
PR-400NE/IX2215 の値を使って、優先寿命 14400 秒、有効寿命 14400 秒、
T1 7200 秒、T2 12600 秒と仮定します。各プレフィックスは HGW 再起動後の
`2026-04-28T15:02Z` から `15:07Z` ごろに再取得されたため、想定時刻は
次の通りです。

| 事象 | 推定 UTC 時刻 | 見たいこと |
| --- | --- | --- |
| T1 | `2026-04-28T17:02Z` から `17:07Z` | 通常のクライアントなら、覚えているサーバーへ Renew を送るはずです。 |
| T2 | `2026-04-28T18:32Z` から `18:37Z` | Renew が成功していなければ、Rebind へ進むはずです。 |
| 有効寿命切れ | `2026-04-28T19:02Z` から `19:07Z` | 更新できなければ、プレフィックスは無効になるはずです。 |

開始直後の確認:

| ホスト | DHCPv6 メッセージ数 | 直後のメモ |
| --- | --- | --- |
| router01 | Solicit 8, Request 0, Renew 0, Rebind 0, Reply 0 | 先ほどの HUP テスト後、KAME `dhcp6c` が正確なヒント付き Solicit を送り続けていました。これはきれいな T1 Renew とは分けて扱います。 |
| router02 | Solicit 0, Request 0, Renew 0, Rebind 0, Reply 0 | まだ DHCPv6 パケットはありません。 |
| router03 | Solicit 0, Request 0, Renew 0, Rebind 0, Reply 0 | まだ DHCPv6 パケットはありません。 |

pcap は tcpdump 実行中でも途中集計できるよう、パケットごとに書き出す形で
取得しています。routerd の状態は 10 分ごとに
`routerctl describe ipv6pd/wan-pd` の先頭部分を記録します。

T2 前の暫定結果です。`2026-04-28T17:27Z` ごろに緊急確認しました。

| ホスト | ここまでの DHCPv6 数 | T1 窓のパケット詳細 | 現在のローカル状態 | 暫定判断 |
| --- | --- | --- | --- | --- |
| router01 | Solicit 582, Request 0, Renew 0, Rebind 0, Advertise 0, Reply 0 | `17:00Z` から `17:10Z` にかけて、KAME `dhcp6c` は DUID-LL `bc:24:11:e3:c2:38` と `2409:10:3d60:1220::/60` の正確なヒントを入れた Solicit を毎分送り続けました。 | プレフィックスはローカルではまだ観測でき、IPv6 既定経路もあります。 | router01 は通常の更新経路に乗っていません。ヒント付き Solicit を繰り返しており、応答は見えていません。 |
| router02 | Solicit 0, Request 0, Renew 9, Rebind 0, Advertise 0, Reply 0 | systemd-networkd は `17:01:01`, `17:03:09`, `17:07:19`, `17:15:24`, `17:24:35` に Renew を送りました。server-ID は `1c:b1:7f:73:76:d8`、クライアント DUID-LL は `bc:24:11:30:5d:76`、IA_PD は `2409:10:3d60:1230::/60` で、Renew パケット内の寿命は 0 と表示されています。 | LAN 側の委譲経路は残っていますが、カーネル上の残り時間は約 5700 秒まで減っています。systemd-networkd のログには新しい行は出ていません。 | WAN 側では DHCPv6 Renew が実際に出ています。tcpdump の条件で見落としているわけではありません。HGW からの Reply は見えていません。 |
| router03 | Solicit 0, Request 0, Renew 6, Rebind 9, Advertise 0, Reply 0 | `17:01:01` の時点で、systemd-networkd はすでに Rebind を送っていました。その後 `17:03:19`, `17:07:42`, `17:16:07`, `17:25:17` に `2409:10:3d60:1240::/60` で再送しています。 | LAN 側の委譲経路は残っていますが、カーネル上の残り時間は約 5800 秒まで減っています。systemd-networkd のログには新しい行は出ていません。 | router03 は粗い T1 推定時刻の時点で、すでに Renew を越えて Rebind に入っていたようです。それでも Reply は見えていません。 |

`udp port 546 or udp port 547` という取得条件が原因で見落としている可能性は
低いです。この条件で router02 と router03 の送信 Renew/Rebind は取れており、
UDP 送信元または宛先が 546/547 の Reply も取れるはずです。T2 前の時点で
見えている問題は、検証ルーターから Renew/Rebind が出ているにもかかわらず、
WAN 側で Advertise / Reply が一切見えていないことです。HGW 画面で
HGW/VoIP 側のリースは更新され、検証 3 台のリースは更新されていないという
観測とも合います。

追加で気になる点:

- systemd-networkd の Renew/Rebind では、既存 IA_PD プレフィックスを入れつつ、
  パケット上の優先寿命と有効寿命が 0 と表示されています。これは更新要求の
  表現として正常かもしれませんが、動作中の商用ルーターのパケットと比較する
  価値があります。
- router03 は、PR-400NE/IX2215 の表から粗く推定した T2 よりかなり早く
  Rebind に入っています。routerd は OS クライアントが実際に得た T1/T2 を
  保存しないと、この種の調査で時刻を正確に扱えません。

### PR-400NE の挙動に関する仮説

以下は検証環境向けプロファイル `ntt-flets-with-hikari-denwa` の作業仮説です。
公開資料と実機観測から推定したものであり、PR-400NE の LAN 側仕様として
明示されたものではありません。そのため、設定で変更できる形にしておきます。

| 場面 | 想定するクライアント手順 | そう考える理由 | routerd で再現すべきこと |
| --- | --- | --- | --- |
| HGW 再起動直後の新規取得 | /60 の IA_PD と、必要ならプレフィックスヒントを入れた Solicit を送り、Advertise、Request、Reply と進む。 | RFC 8415 の基本は 4 メッセージです。検証環境の HGW は再起動後に下流ルーターへ /60 を配ります。NEC や SEIL の設定例も、ひかり電話環境で DHCPv6-PD を使っています。 | 標準的な Solicit を送り、UDP 宛先 546 への Advertise/Reply を送信元ポートにかかわらず受ける。HGW 再起動直後の遅れに備えて、取得待ち時間を短くしすぎない。 |
| 既知リースの復帰 | DUID、IAID、`lastPrefix` がまだ有効なら、IA_PD に前回プレフィックスをヒントとして入れる。OS 側クライアントがサーバー状態を覚えているなら、Solicit へ戻る前に Renew を優先する。 | RFC 8415 はプレフィックスヒントを認めています。PR-400NE は短い再起動をまたいで DUID、IAID、プレフィックスの紐付けを覚えているように見えます。 | `PDLease` を保存し、有効寿命内ならヒントを出力する。復旧時に Renew、Rebind、Solicit のどれを使ったか見えるようにする。 |
| 通常の更新 | T1 で、リースを出したサーバーへ Renew を送る。T2 までに Reply がなければ、任意のサーバーへ Rebind を送る。 | RFC 8415 は IA_PD についても T1/T2 による Renew/Rebind を定義しています。HGW のリース表には有限の寿命が出ているため、更新できなければ最終的に委譲を失います。 | T1/T2 と寿命を保存する。プレフィックスが消えてからではなく、有効期限前に更新を促す。 |
| 期限切れまたは忘れられたリース | 有効寿命が切れた後は Solicit へ戻る。プロファイルが許す場合だけ過去プレフィックスをヒントにし、既定では `::/60` の長さだけのヒントにする。 | RFC 8415 では、有効寿命が切れたらその交換は終わります。古い正確なヒントも害は少ないと考えられますが、プロファイルで選べる方が安全です。 | 有効寿命切れ後は正確な過去ヒントを既定で使わない。NTT 向けには /60 の長さだけの要求を残す。 |
| Release 抑止 | デーモン再起動や制御された停止では、明示されない限り Release を送らない。 | ホームゲートウェイによっては、Release して新規取得を繰り返すより、既存の紐付けを更新または自然失効させる方が安定する可能性があります。pfSense や KAME `dhcp6c` でも Release 抑止の運用ができます。 | プロファイルに `sendRelease: false` 相当を持たせ、OS ごとの出力で守る。 |
| DUID の厳格化 | NTT 向けプロファイルでは DUID-LL を既定にする。DUID-EN や UUID 由来 DUID はフレッツ系プレフィックス委譲では無効扱いにする。DUID-LLT は、厳しめの挙動を緩める設定を明示した時だけ許す。 | 現在の NTT 公式資料は MAC アドレス由来の DUID-LL または DUID-LLT を許しています。一方で、Sorah の NGN 直結観測では DUID-LL 以外が黙って無視されたとされています。systemd-networkd の既定である DUID-EN は、少なくとも NTT 文書の範囲外です。 | NTT 向けでは `spec.duidType` の既定を `link-layer` にし、それを明示的に出力する。観測した DUID が type 3 の DUID-LL でない場合は警告する。 |
| IA_NA と IA_PD の同時要求 | 最初は IA_PD だけにする。IA_NA と IA_PD の同時要求はプロファイルで選べるようにする。 | 商用ルーターの加入者向け例では両方を同時に求めるものがありますが、検証環境では委譲プレフィックスだけが必要で、HGW は細かな差に敏感かもしれません。 | `routerd_dhcp6c_client` は両方を扱えるようにする。ただし、NTT ホームゲートウェイ向け既定値は、追加検証まで IA_PD のみにする。 |
| Rapid Commit | サーバーが使うなら対応するが、既定では要求も前提化もしない。 | RFC 8415 では Rapid Commit が使えますが、NTT や機器ベンダー例では今回の用途で必須とはされていません。 | `ntt-flets-with-hikari-denwa` では既定で Rapid Commit を無効にする。サーバーが使った場合は記録する。 |
| UDP 受信条件 | UDP 宛先ポート 546 の DHCPv6 応答を、送信元ポートにかかわらず受ける。 | RFC 8415 が定めるのは待受ポートです。PR-400NE のコミュニティ観測では、Advertise の送信元が 49153 だった例があります。 | ファイアウォールとパケット解析で、受信 DHCPv6 応答に送信元 547 を必須にしない。 |

### `routerd_dhcp6c_client` で再現すべきこと

将来 `insomniacslk/dhcp` を使って routerd 内に DHCPv6 クライアントを持つ
場合は、OS 側クライアントのよい部分を再現しつつ、NTT 向けプロファイルの
挙動を観測しやすくします。

- DUID と IAID を安定させる。NTT 向けプロファイルでは、現在の NTT 公式資料が
  DUID-LLT も許しているとしても、DUID-LL を既定にする。systemd-networkd の
  既定である DUID-EN を避け、DUID-LL 以外が黙って無視されるという実機報告にも
  対応するためです。
- `PDLease` には DUID、IAID、サーバー識別子、分かる場合はサーバーの
  リンクローカルアドレス、現在のプレフィックス、過去のプレフィックス、
  優先寿命、有効寿命、T1、T2、最後に見えた時刻、最後に消えた時刻、
  最後の DHCPv6 メッセージ遷移を保存する。
- Solicit には、クライアント識別子、経過時間、設定に応じた DNS/SNTP の
  要求、IA_PD を入れる。状態が有効なら前回の正確なプレフィックスを
  ヒントにし、そうでなければ NTT 向けプロファイルでは `::/60` の長さだけを
  ヒントにする。
- NTT 向けプロファイルでは、既定で IA_NA を入れない。IA_NA と IA_PD の
  同時要求は、コード変更なしに試せるようプロファイル設定にする。
- Advertise を受けたら、同じクライアント識別子と IA_PD を使って選択した
  サーバーへ Request を送る。後の Renew に使えるよう、サーバー識別子を保存する。
- T1 では、サーバー識別子と現在の委譲プレフィックスを含む IA_PD で
  Renew を送る。T2 までに更新できなければ、DHCPv6 のマルチキャスト
  アドレスへ Rebind を送る。
- 有効なサーバー識別子がない時は、Renew を装わない。その場合は
  プレフィックスヒント付き Solicit に戻し、新規取得の試行であることを
  ログに残す。
- Release は既定の停止動作にせず、管理者が明示した操作として扱う。
- 再送は RFC の考え方に従う。ただし、PR-400NE が検証環境で HGW 再起動中
  または直後にしか応答しないように見えるため、NTT 向けプロファイルでは
  初回取得の待ち時間を長めにできるようにする。
- パケット単位の遷移をすべてログに残す。メッセージ種別、トランザクション
  ID、DUID、IAID、要求したヒント、委譲されたプレフィックス、T1/T2、
  寿命、応答の送信元ポートが 547 以外だったかを記録する。
- プレフィックスの取得、更新、再束縛、喪失、要求したヒントと違う /60 が
  返された時にイベントを出す。

### この調査からのバックログ

- `ntt-flets-with-hikari-denwa` という DHCPv6 クライアントプロファイルを
  明示的に追加する。既定値は `/60`、安定 DUID-LL、安定 IAID、
  `hintFromState: true`、`sendRelease: false`、IA_PD のみ、
  Rapid Commit 無効、長めの初回取得待ち、受信 UDP 宛先 546 を
  送信元ポート制限なしで許可、とする。
- OS 側クライアントの DUID も routerd の管理対象にする。systemd-networkd では
  NTT 系プロファイルの既定として `DUIDType=link-layer` を出力する。
  FreeBSD の KAME `dhcp6c` では同じプロファイルで `/var/db/dhcp6c_duid` を
  管理し、DUID-LL 以外のファイルがあれば退避してから、上流インターフェースの
  MAC アドレスから作った DUID-LL を `dhcp6c` 起動前に書き込む。
- 正確なプレフィックスヒント付き Solicit、長さだけのヒント付き Solicit、
  現在の IA_PD を含む Renew、T2 後の Rebind、送信元ポート 547 以外からの
  Advertise を、パケット取得ベースの結合テストとして追加する。
- transix の AFTR 名解決はプレフィックス取得と独立させる。ただし、
  利用できる IPv6 上流接続があることを示す状態変数に、経路選択方針が
  依存できるようにする。
