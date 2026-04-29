# 設計メモ

この文書は、まだ安定したリソース定義に入れていない設計判断と検証結果を
記録します。公開リポジトリに置くため、検証環境固有のプレフィックス、
MAC アドレス、DUID、宅内アドレスは文書用の値に置き換えています。

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
- cite: Yamaha RT シリーズ、Cisco IOS XE、Juniper Junos、MikroTik RouterOS、
  pfSense、OPNsense、OpenWrt は、DUID、IAID、リース状態、委譲プレフィックス、
  下流広告を運用対象として扱います。routerd もこれらを状態として表示できる
  必要があります。

### 1.2 PR-400NE 検証環境で測定したこと

この節の値は文書用に置き換えています。

| 項目 | 測定結果 |
| --- | --- |
| DHCPv6-PD サーバー機能 | measure: PR-400NE の LAN 側は下流ルーターへ DHCPv6-PD を配りました。動作中ルーターと routerd 検証機が同じリース表に出ました。 |
| リース表 | observe: 画面上の払い出し上限は 15 件でした。 |
| 応答ポート | measure: Advertise/Reply は宛先 UDP 546 へ届き、送信元は 547 ではない一時ポートでした。したがって取得確認の tcpdump は `udp port 546 or udp port 547` にします。 |
| サーバー識別子 | measure: Server Identifier は DUID-LL でした。文書では `<HGW-DUID>` とします。 |
| 寿命 | measure: Reply の T1 は 7200 秒、T2 は 12600 秒、優先寿命と有効寿命は 14400 秒でした。 |
| 委譲長 | measure: 下流ルーターには `/60` が配られました。これは HGW が上流側で受けたより大きなプレフィックスを分割したものと考えます。 |
| ヒント | measure: 正確なプレフィックスヒント付きの Solicit でも取得できました。したがって、PR-400NE がすべてのヒントを拒否しているわけではありません。ただし、動作中ルーターの初回 Solicit にはヒントが無かったため、NTT 系プロファイルでは既定でヒントを出しません。 |

検証で使った文書用の対応表:

| 検証対象 | 文書用プレフィックス | 文書用 MAC | 文書用 DUID |
| --- | --- | --- | --- |
| FreeBSD/KAME 検証機 | `2001:db8:0:1220::/60` | `02:00:00:00:01:01` | `<DUID-LAB-FREEBSD>` |
| NixOS 検証機 | `2001:db8:0:1230::/60` | `02:00:00:00:01:02` | `<DUID-LAB-NIXOS>` |
| Ubuntu 検証機 | `2001:db8:0:1240::/60` | `02:00:00:00:01:03` | `<DUID-LAB-UBUNTU>` |
| PR-400NE | - | `02:00:00:00:00:01` | `<HGW-DUID>` |
| 動作中の商用ルーター | `2001:db8:0:1210::/60` | `00:00:5e:00:53:cf` | `<DUID-COMMERCIAL-ROUTER>` |

### 1.3 Solicit パケット比較

observe: PR-400NE が委譲している動作中ルーターと、routerd 検証機の Solicit を
比較しました。値は文書用に置き換えています。

| 項目 | 動作中ルーター | FreeBSD/KAME | Ubuntu/systemd-networkd |
| --- | --- | --- | --- |
| DUID 種別 | DUID-LL | DUID-LL | DUID-LL |
| IA_PD IAID | `<COMMERCIAL-IAID>` | `0` | `<NETWORKD-IAID>` |
| プレフィックスヒント | 無し | 整理後は無し | 整理後は無し |
| ヒント寿命 | 無し | 整理後は無し | 整理後は無し |
| 要求オプション | 無し | DNS のみ | DNS、SNTP、NTP など |
| 再設定許可 | あり | 無し | 無し |
| クライアント FQDN | 無し | 無し | あり |
| Rapid Commit | 無し | 無し | 無し |

assert: DUID-LL は必須に近い前提として扱います。一方で、ヒント、要求オプション、
クライアント FQDN の有無は、単独では成功/失敗を説明しません。FreeBSD/KAME と
Ubuntu/systemd-networkd は異なる Solicit でも取得できました。

assert: `ntt-ngn-direct-hikari-denwa` と `ntt-hgw-lan-pd` では、正確なヒントも
長さだけのヒントも既定では送りません。`prefixLength` は routerd が期待する形を
表す値として残しますが、systemd-networkd へは `PrefixDelegationHint=` を出力しません。

assert: odhcp6c 実験は main へ採用しません。新しい枝で明確に有利な結果が
出るまでは、Linux の基本経路は systemd-networkd のままとします。

### 1.4 OS クライアント実装から分かること

| クライアント | 測定または引用した挙動 | routerd での扱い |
| --- | --- | --- |
| systemd-networkd | cite/measure: `DUIDType=link-layer`、`IAID`、`PrefixDelegationHint`、`WithoutRA` を設定できます。Renew/Rebind の IA Prefix 寿命は 0 です。 | Linux の基本経路として残します。通知や詳細状態の見え方は弱いため、routerd 側で観測を補います。 |
| KAME/WIDE `dhcp6c` | cite/measure: DUID はファイル、IAID と IA_PD は設定で扱います。ヒント付き Solicit では IA Prefix 寿命を出力できます。 | FreeBSD の DHCPv6-PD 経路として残します。NTT 向けでは DUID-LL ファイルを routerd 管理対象にします。 |
| dhcpcd | cite: Linux と FreeBSD の両方で使え、DUID、IAID、フック、IA_PD を扱えます。 | FreeBSD の DHCPv6-PD には今は採用しません。IPv4 DHCP では、プラットフォームが選ぶ場合のクライアント候補として扱います。 |
| dnsmasq | cite/assert: LAN 側の DNS、DHCPv4、DHCPv6、RA には有用です。WAN 側の PD クライアントの正にはしません。 | LAN サービスに限定して使います。 |

assert: NTT 系プロファイルでは、実際の MAC アドレスから作った DUID-LL を既定にします。`duidRawData` は、高可用構成の切り替え、ルータ交換、移行のために明示的に使う上書き設定であり、通常の復旧経路では使いません。

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
- [rixwwd: PR-400NE / Dream Router の DHCPv6 パケット観測](https://rixwwd.hatenablog.jp/entry/2023/04/09/211529)
- [SEIL: NGN IPv6 ネイティブ IPoE 接続例](https://www.seil.jp/blog/10.html)
- [OpenWrt odhcp6c README](https://github.com/openwrt/odhcp6c)
- [OpenWrt odhcpd README](https://github.com/openwrt/odhcpd)
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
- observe: systemd-networkd の `networkctl renew` は、今回使った版では
  手動更新手段として十分に見えませんでした。版依存の可能性があるため、
  routerd の恒久仕様にはしません。
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

- `IPv6PrefixDelegation` の状態表示を強化し、現在値、最後に見えた値、
  DUID、IAID、T1/T2、寿命、最後の更新試行、警告を明確に出します。
- DUID と IAID は期待値と観測値を分けて表示します。
- NTT 向けプロファイルでは DUID-LL、IA_PD のみ、Rapid Commit 無効を既定にします。
- DHCPv6 応答を受けるファイアウォールは、UDP 宛先 546 を送信元ポート制限なしで
  許可します。
- 委譲プレフィックスが消えた時は、LAN 側 RA/DHCPv6 から古い情報を
  どう撤回するかを別途設計します。
- プレフィックス委譲がない場合でも、WAN 側 IPv6 到達性で DS-Lite を使う
  構成は設計候補として残します。ただし LAN 側 IPv6 をブリッジまたは通過させる
  構成は所有境界とファイアウォール設計が変わるため、別途検証します。
