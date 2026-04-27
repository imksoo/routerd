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

- `routerctl show pd` を追加し、DUID、IAID、プレフィックス、寿命、T1/T2、
  最後に見えた時刻、最後に消えた時刻、クライアント状態、警告を表示する。
- 内部に `PDLease` 相当の状態モデルを追加し、現在の
  `ipv6PrefixDelegation.<name>.*` 変数をそのモデルへ移していく。
- OS ごとの安全な更新フックを調査する。
  - systemd-networkd: DBus、`networkctl`、サービス再読み込みのどれで
    Release なしの更新を促せるか確認する。
  - FreeBSD/KAME `dhcp6c`: 制御ソケットの有無と、SIGUSR1 以外に安全な
    更新手段があるか確認する。
- `releasePolicy` または `sendRelease` 設定を追加し、NTT 系ホーム
  ゲートウェイ向けプロファイルでは保守的な既定値にする。
- プロファイルに、プレフィックスヒント、IA_NA と IA_PD を同じ要求に
  入れるかどうか、DUID 型、IAID、Release 抑止、収束待ち時間、
  ファイアウォールの応答許可条件を持たせる。
- プレフィックス委譲が消えた時、アドレス削除や RA/DHCPv6 サービス削除の
  前に、下流へ古いプレフィックスの廃止を伝える挙動を追加する。
