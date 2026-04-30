# 設計メモ

この文書は、まだ安定したリソース定義に入れていない設計判断と検証結果を
記録します。公開リポジトリに置くため、検証環境固有のプレフィックス、
MAC アドレス、DUID、宅内アドレスはドキュメント用の値に差し替えています。

## 用語の使い分け

本文では、根拠の強さを次の語で分けます。

| 語 | 意味 |
| --- | --- |
| assert | routerd として採用する設計判断。実装方針を示す。 |
| believe | 間接的な根拠に基づく推測。後で覆る可能性を残す。 |
| observe | ある時点で見えた挙動。再現性や一般性は別に扱う。 |
| measure | tcpdump、ログ、状態表などで数値やフィールドとして確認した値。 |
| cite | RFC、公式仕様、公開文書からの引用または要約。 |

この分類に落とせない文は、未検証事項として扱うか、本文から外します。

## 1. 検証済み事実

### 1.0 真実の元と操作経路

assert: routerd の真実の元 (source of truth) は、YAML 設定と `routerd apply` が
書き込む状態・所有台帳です。ルーターの挙動を変えるときは、ファイルを変更して
apply することを基本にします。生成済みファイルを手で直したり、apply の外から
デーモンだけを動かしたりすると、意図が git の履歴、差分、apply の結果、
ローカルデータベースに残らず、後から追跡できなくなります。

assert: ホスト上のサービスは、OS のサービス管理機構を通して扱います。systemd の
ホストでは `systemctl`、FreeBSD では `service` / rc.d を使います。通常の制御経路
として、長く動くデーモンへ場当たり的にシグナルを送ることは避けます。短時間の
切り分けで直接シグナルを使った場合でも、最後の確認は必ず `routerd apply --once`
の経路でやり直します。

assert: レンダラの変更は、本番と同じ apply 経路を通るまでテスト完了とは見なしません。
`routerd render` の出力確認は有用ですが、あくまで事前確認です。所有台帳、
サービス管理機構、依存順序、OS 自身の診断を通すには `routerd apply` が必要です。

### 1.1 RFC と公開仕様から分かること

- cite: RFC 8415 の通常の DHCPv6 取得手順は Solicit、Advertise、
  Request、Reply です。Rapid Commit が使われる場合は短縮されますが、
  routerd の NTT 向け既定では Rapid Commit を前提にしません。
- cite: RFC 8415 では、クライアントは UDP 546、サーバーとリレーは
  UDP 547 を待ち受けます。これは待受ポートの規定であり、受信する
  Advertise/Reply の送信元ポートが常に 547 であることを意味しません。
- cite: RFC 8415 では、IA_PD の中に IA Prefix を入れて希望プレフィックスや
  希望長を示せます。サーバーはそれをヒントとして扱います。
- cite: RFC 8415 では、Renew は元のサーバーへの更新、Rebind は Renew が
  成立しない場合の再束縛、Solicit は新規取得です。Confirm はアドレス確認用で、
  委譲プレフィックスの復旧手段としては扱いません。
- cite: NTT 東日本と NTT 西日本のフレッツ系公開インターフェース資料では、
  端末側 DUID は DUID-LL または DUID-LLT とされ、MAC アドレス由来であることが
  求められています。DUID-EN や UUID 由来 DUID は、この公開資料の端末モデルには
  入っていません。
- cite: 同資料では、DHCPv6 で 128 ビットのアドレスを取得しない構成が
  説明されています。NTT 向けプロファイルの既定で IA_NA を IA_PD と一緒に
  要求する根拠は、現時点ではありません。
- cite: 同資料の DHCPv6 オプション表では Rapid Commit が「この仕様では使わない」
  項目として扱われています。NTT 向け既定では Rapid Commit を送らない方針にします。
- cite: Sorah's Diary の 2017 年の実機報告は、DUID-LL 以外の Solicit が
  黙って無視されたと述べています。ただし、これは公式仕様ではなく経験報告です。
  routerd では、DUID-LL を NTT 向けプロファイルの厳しめの既定値として扱い、
  DHCPv6 全般の規則にはしません。
- cite: NEC IX の公開設定例では、ひかり電話ありの環境で DHCPv6-PD を使い、
  委譲されたプレフィックスを下流へ広告する構成が示されています。
- cite: 商用ルーターや公開されているルーター実装は、DUID、IAID、リース状態、
  委譲プレフィックス、下流広告を運用対象として扱います。routerd もこれらを
  状態として表示できる必要があります。

### 1.2 NTT ホームゲートウェイ向けプロファイルの形

measure: NTT ホームゲートウェイの LAN 側でプレフィックス委譲を受ける構成では、
成功したクライアントは DUID-LL、IA_PD、Rapid Commit 無効、`/60`
委譲を使いました。DHCPv6 の Advertise/Reply は UDP 宛先 546 に届くため、
送信元ポート 547 固定を前提にしてはいけません。

measure: ホームゲートウェイを再起動した直後は、LAN 側の DHCPv6-PD サーバーが
応答を始めるまで数分かかる場合がありました。その間、検証ルーターは妥当な
Solicit を送っていても Advertise を受け取りませんでした。サーバーが準備できた後は、
同じ DUID-LL クライアントがすぐ Advertise/Reply を受け取りました。そのため
routerd では「HGW 再起動直後に数十秒返事がない」ことを、クライアントの形が
間違っている証拠ではなく、上流側の準備待ちとして扱います。

observe: 既に委譲を受けたことがある FreeBSD 検証ルーターで、routerd の生成設定を
`routerd apply --once` で反映した後、rc.d の `service dhcp6c stop/start` だけで
最小形の fresh Solicit を送り直しました。Solicit は DUID-LL、IA_PD、DNS の
要求だけを含み、過去に成功した時とほぼ同じ形でしたが、ホームゲートウェイ通常稼働中の
短時間観測では Advertise/Reply を受け取りませんでした。この結果は、特定の
systemd-networkd のパケット形だけが原因という説明を弱め、ホームゲートウェイ側の
時刻または内部状態に依存する取得期間の仮説を強めます。

observe: ホームゲートウェイの払い出し表を直接取得できる補助ツールで確認したところ、
動作中の商用ルーターはホームゲートウェイ再起動から約 2 時間後にもリース残時間を
回復していました。一方、検証ルーター 3 台は再起動直後に取得したリースの残時間が
減るだけで、同じ時刻帯に更新された形跡がありませんでした。これは、ホームゲートウェイが
通常稼働中の既存 binding の Renew/Request には応答できること、そして検証側の
主要課題が fresh Solicit の形ではなく、取得済みリースの Renew/Rebind を
維持できていない点にあることを示します。

measure: 同じ時刻帯のパケット取得では、動作中ルーターの Renew は Server ID を含み、
IA_PD に `T1=7200`、`T2=12600`、IA Prefix の preferred lifetime と valid
lifetime に `14400` を入れていました。検証 Linux ルーターの systemd-networkd は
Renew/Rebind を送っていましたが、IA_PD と IA Prefix の寿命はいずれも `0` でした。
この差は、ホームゲートウェイが通常稼働中の Renew を受け付ける一方で、検証側の
Renew/Rebind が成功しない理由を調べるための最優先の観測点です。routerd はこの
種類の差分を `routerctl describe ipv6pd/<name>` で見えるようにする必要があります。

measure: 動作中ルーターの初回 Solicit にはプレフィックスヒントがありませんでした。
そのため routerd は `ntt-ngn-direct-hikari-denwa` と `ntt-hgw-lan-pd` では、
正確なヒントも長さだけのヒントも既定では送りません。正確なヒントが常に悪いとは
扱いませんが、既定の形からは外します。

assert: NTT 向けプロファイルでは DUID-LL を既定の識別子の形にします。
routerd は、運用者が `spec.iaid` を明示した場合を除き、IAID を作らず
出力もしません。systemd-networkd は再設定をまたいで IAID 状態を保持することがあり、
routerd 側で既定値として出しても効果が分かりにくく、複雑さに見合う利点が
ありませんでした。NTT 向けプロファイルでは、OS クライアントの上に追加の
要求オプション調整を重ねず、設定を最小に保ちます。SOL_MAX_RT のような
プロトコル維持用の項目は networkd が送る場合があるため、OS クライアントを
置き換えない限り、routerd が networkd の Solicit をバイト単位で完全制御できるとは
扱いません。

assert: `ntt-ngn-direct-hikari-denwa` と `ntt-hgw-lan-pd` では、正確なヒントも
長さだけのヒントも既定では送りません。`prefixLength` は routerd が期待する形を
表す値として残しますが、systemd-networkd へは `PrefixDelegationHint=` を出力しません。

### 1.3 OS クライアント実装から分かること

| クライアント | 測定または引用した挙動 | routerd での扱い |
| --- | --- | --- |
| systemd-networkd | cite/measure: `DUIDType=link-layer`、`IAID`、`PrefixDelegationHint`、`WithoutRA` を設定できます。Renew/Rebind の IA Prefix 寿命は 0 です。 | Linux の基本経路として残します。通知や詳細状態の見え方は弱いため、routerd 側で観測を補います。 |
| KAME/WIDE `dhcp6c` | cite/measure: DUID はファイル、IAID と IA_PD は設定で扱います。ヒント付き Solicit では IA Prefix 寿命を出力できます。 | FreeBSD の DHCPv6-PD 経路として残します。NTT 向けでは DUID-LL ファイルを routerd 管理対象にします。 |
| dnsmasq | cite/assert: LAN 側の DNS、DHCPv4、DHCPv6、RA には有用です。WAN 側の PD クライアントの正にはしません。 | LAN サービスに限定して使います。 |

assert: DHCPv6-PD の取得経路は意図的に絞ります。Linux は systemd-networkd、
FreeBSD は KAME/WIDE `dhcp6c` を使います。

assert: NTT 系プロファイルでは、実際の MAC アドレスから作った DUID-LL を既定にします。`duidRawData` と `iaid` は、高可用構成の切り替え、ルータ交換、移行のために明示的に使う上書き設定であり、通常の復旧経路では使いません。

assert: FreeBSD では、NTT 系プロファイルの `dhcp6c` を `-n` 付きで起動し、
サービス再起動時に DHCPv6 Release を送らないようにします。これは公開設定項目ではなく
プロファイル内部の挙動です。通常の Renew/Rebind の時刻管理は、引き続き OS 側の
DHCPv6 クライアントに任せます。

## 2. ラボ環境特有の問題

### 2.1 仮想環境のマルチキャスト透過性

observe: 検証機は Proxmox 上の仮想マシンでした。Linux bridge の
`multicast_snooping` が有効な状態では、RA や DHCPv6 のマルチキャスト交換が
見えない、または一部だけ見える状態になり得ます。

cite: Proxmox bridge の `multicast_snooping=0` が IPv6 RA/DHCPv6 の検証で
必要になる事例は、公開記事やフォーラムにも報告されています。

assert: routerd の検証で DHCPv6-PD を判断する前に、次を確認します。

- Proxmox bridge が IPv6 マルチキャストを通すこと。
- 経路上の L2 スイッチで MLD/IGMP snooping が検証を妨げていないこと。
- tcpdump は上流側インターフェースで `udp port 546 or udp port 547` と RA を
  別々に取ること。
- 「HGW が返していない」と結論する前に、同じ区間で動作中ルーターの Solicit や
  Request が見えるか確認すること。

### 2.2 L2 スイッチのマルチキャストスヌーピング

observe: 検証経路上の L2 スイッチで IGMP snooping が有効な場合、IPv6 RA や
DHCPv6 のマルチキャスト交換の一部が届かなくなることがありました。多くの実装で
IGMP snooping の有効/無効は MLD snooping と連動するため、IPv4 マルチキャストの
最適化を意図して入れた設定が IPv6 ND/DHCPv6 の検証を阻害する形で出ます。

assert: routerd の検証では、まず snooping を OFF にしてマルチキャストを flat に
流し、観測経路を素直にします。本番運用で snooping を維持したい場合は、別の
設計選択肢として次があります。

- 経路上に MLD Querier を立てて General Query を送り、各クライアントから
  Listener Report を引き出して snooping テーブルを維持する。
- routerd 配下に NDP/DHCPv6 multicast を完全に通す範囲だけ snooping を切り、
  他の VLAN は維持する分割設計にする。

believe: ラボ規模では snooping OFF が現実的です。フラッディング増は無視できる
範囲で、原因切り分けが速くなります。

observe: 物理 L2 スイッチの設定変更は機種ごとの管理画面・CLI に依存するため、
本文書では機種名や具体的な設定コマンドは扱いません。

## 3. 公開資料

主な参照先:

- [RFC 8415: Dynamic Host Configuration Protocol for IPv6](https://www.rfc-editor.org/rfc/rfc8415.html)
- [RFC 9915: Dynamic Host Configuration Protocol for IPv6](https://datatracker.ietf.org/doc/html/rfc9915)
- [NTT 東日本 技術参考資料](https://www.ntt-east.co.jp/gisanshi/)
- [NTT 東日本 IP 通信網サービスのインタフェース フレッツシリーズ 第三分冊](https://flets.com/pdf/ip-int-3.pdf)
- [NTT 西日本 IP 通信網サービスのインタフェース](https://www.ntt-west.co.jp/info/katsuyo/pdf/23/tenpu16-1.pdf)
- [Yamaha RT シリーズ DHCPv6 機能](https://www.rtpro.yamaha.co.jp/RT/docs/dhcpv6/index.html)
- [Yamaha IPv6 IPoE 機能](https://www.rtpro.yamaha.co.jp/RT/docs/ipoe/index.html)
- [NEC UNIVERGE IX フレッツ 光ネクスト IPv6 IPoE 設定例](https://jpn.nec.com/univerge/ix/Support/ipv6/native/ipv6-internet_dh.html)
- [NEC IX-R/IX-V DHCPv6 機能説明](https://support.necplatforms.co.jp/ix-nrv/manual/fd/02_router/14-1_dhcpv6.html)
- [Sorah's Diary: フレッツ光ネクスト ひかり電話あり環境の DHCPv6-PD 観測](https://diary.sorah.jp/2017/02/19/flets-ngn-hikaridenwa-kill-dhcpv6pd)
- [rixwwd: NTT ホームゲートウェイ配下の DHCPv6 パケット観測](https://rixwwd.hatenablog.jp/entry/2023/04/09/211529)
- [SEIL: NGN IPv6 ネイティブ IPoE 接続例](https://www.seil.jp/blog/10.html)
- [systemd.network マニュアル](https://www.freedesktop.org/software/systemd/man/254/systemd.network.html)
- [FreeBSD dhcp6c(8)](https://man.freebsd.org/cgi/man.cgi?manpath=freebsd-release-ports&query=dhcp6c&sektion=8)
- [FreeBSD dhcp6c.conf(5)](https://man.freebsd.org/cgi/man.cgi?query=dhcp6c.conf)
- [pfSense advanced networking documentation](https://docs.netgate.com/pfsense/en/latest/config/advanced-networking.html)
- [OPNsense DHCP documentation](https://docs.opnsense.org/manual/isc.html)
- [MikroTik RouterOS DHCP documentation](https://help.mikrotik.com/docs/display/ROS/DHCP)
- [Cisco IOS XE DHCPv6 Prefix Delegation](https://www.cisco.com/c/en/us/td/docs/ios-xml/ios/ipaddr_dhcp/configuration/xe-16-9/dhcp-xe-16-9-book/ip6-dhcp-prefix-xe.html)
- [Juniper Junos IA_NA and Prefix Delegation](https://www.juniper.net/documentation/us/en/software/junos/subscriber-mgmt-sessions/topics/topic-map/dhcpv6-iana-prefix-delegation-addressing.html)

## 4. 既知の制限と未検証事項

### 4.1 DHCPv6-PD

- believe: DUID-LLT が特定の NTT 経路で黙って無視される可能性はあります。
  ただし NTT 公式資料は DUID-LLT も許しているため、これは公式仕様ではなく
  実装差の可能性として扱います。
- assert: routerd 自前 DHCPv6 クライアントは、OS クライアントで得られる
  安定性と運用性を確認した後の選択肢です。先に DUID、IAID、リース、イベントの
  観測を固めます。

### 4.2 状態と所有台帳

routerd は、ローカル状態と所有台帳を SQLite に保存します。既定の場所は
Linux では `/var/lib/routerd/routerd.db`、FreeBSD では
`/var/db/routerd/routerd.db` です。

| 表 | 役割 |
| --- | --- |
| `generations` | 反映を試みた単位ごとの結果、警告、設定ハッシュを保存します。 |
| `objects` | リソース単位の状態 JSON を保存します。例: `IPv6PrefixDelegation/wan-pd` のリース、DUID、IAID、時刻。 |
| `artifacts` | routerd が管理するホスト側構成物の所有台帳です。 |
| `events` | 反映時の警告やプレフィックス観測を保存します。 |
| `access_logs` | 将来のローカル HTTP API 監査用です。 |

JSON は文字列として保存し、SQLite の JSON1 機能で確認できます。

```sh
sqlite3 /var/lib/routerd/routerd.db \
  "select json_extract(status, '$.lastPrefix') from objects where kind = 'IPv6PrefixDelegation' and name = 'wan-pd';"
```

routerd の実行に `sqlite3` コマンドは不要です。人が状態を調べる時には便利です。
`jq` は、信頼済みローカルプラグインが JSON を扱うために残します。

### 4.3 ホスト情報

routerd は反映処理の開始時に、観測したホスト情報を
`routerd.net/v1alpha1/Inventory/host` として保存します。状態 JSON には
Go の OS 名、`uname` から得たカーネル情報、仮想化の判定、取得できた DMI 情報、
サービス管理方式、`nft`、`pf`、`dnsmasq`、`dhcp6c`、`sysctl` などのコマンドが
使えるかどうかを記録します。

assert: Inventory は観測値であり、望ましい設定を表すリソースではありません。
通常の `spec.resources` には書かず、最初の実装ではレンダラも参照しません。
今ここで保存する理由は、後で物理機か仮想機か、systemd か rc.d か、ブリッジの
マルチキャスト設定のようなホスト前提を、レンダラごとの推測ではなく観測事実として
扱えるようにするためです。

確認には次を使います。

```sh
routerctl describe inventory/host
```

### 4.4 今後の設計作業

- 現在のプロファイル整理後に、Linux の systemd-networkd と FreeBSD の
  `dhcp6c` が自然に行う DHCPv6-PD の Renew/Rebind を観測します。routerd が
  クライアントのタイマーを管理せずに、状態として確実に表示できる範囲を確認します。
- OS クライアントから T1/T2 や寿命を取れる場合は、`IPv6PrefixDelegation` の
  状態表示をさらに強化します。現在値、最後に見えた値、観測した DUID/IAID、
  期待する DUID/IAID、最後に観測した時刻、警告は混ぜずに表示します。
- `IPv6PrefixDelegation` の観測根拠を見直します。LAN 側に過去の委譲アドレスが
  残っているだけでは、上流の DHCPv6-PD リースが現在も有効とは限りません。
  `routerctl describe ipv6pd/<name>` は、派生アドレスの存在、OS クライアントの
  リース状態、最後に観測した DHCPv6 Reply、T1/T2 や寿命を分けて表示し、
  根拠が派生アドレスだけの場合は警告します。
- `routerctl describe ipv6pd/<name>` を強化し、運用確認の最初の手段を
  生のシェルコマンドにしないようにします。詳細表示には、生成したクライアント設定の
  要約、OS サービスの状態、そのクライアントに触れた最後の apply 操作、取得できる
  場合は関連する直近ログ、将来 routerd が管理するパケット観測やイベントを持てるなら
  DHCPv6 のメッセージ種別ごとの数を出します。バイト単位の診断では引き続き
  パケット取得が必要ですが、最初の正常性確認は routerctl で答えられるようにします。
- FreeBSD のホスト情報でコマンド検出を直します。`/usr/local/sbin` に入る
  `dhcp6c` や `dnsmasq` は rc.d から使えても、routerd の PATH が狭いと
  見つからないことがあります。Inventory はプラットフォーム別の探索パスを使い、
  見つけたコマンドのパスも表示します。
- 古い委譲プレフィックスを LAN サービスから撤回する設計を仕上げます。反映処理は、
  委譲プレフィックスが変わったあと、routerd が作った LAN 側アドレスと DS-Lite の
  送信元アドレスのうち、管理対象の末尾アドレスを持つ古いものを削除します。
  委譲プレフィックスが消えた時に、LAN 側 RA/DHCPv6 から古い情報をすばやく
  撤回する設計はまだ残っています。
- `Inventory/host` を、仮想ブリッジのマルチキャスト透過性、RA 受信、
  サービス管理方式の違いといったホスト前提の助言や将来の出力に使うかを設計します。
- 残っているサンプルプラグインを `plugins/` 直下に置き続けるか、信頼済み
  ローカルプラグインの実例が増えた段階でテスト用 fixture へ寄せるかを決めます。
- プレフィックス委譲がない場合でも、WAN 側 IPv6 到達性で DS-Lite を使う
  構成は設計候補として残します。ただし LAN 側 IPv6 をブリッジまたは通過させる
  構成は所有境界とファイアウォール設計が変わるため、別途検証します。
