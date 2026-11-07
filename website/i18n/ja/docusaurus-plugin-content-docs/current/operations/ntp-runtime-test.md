# NTP の実機試験

管理対象の NTPClient / NTPServer は生成した設定 fragment を所有します。
resource・provider・path・marker・digest の receipt で確認し、未取得の既存ファイルや
外部で差し替わったファイルは保持します。共有する provider は NTPServer が所有し、
有効な NTPClient の upstream を含めます。Linux の timesyncd 要求を chrony server に
委ねた場合、client は実効 provider を別途表示します。条件が false の server は
client の upstream を残して downstream への応答を止めます。

## 隔離した controller 試験

Linux では `make check-routerd15-ntp-native-linux` を使います。
Go、chronyd/chronyc、iproute2、unshare、mount と user/network/mount namespace が必要です。
`chronyd -x` によって共有時計を変更せず、独立した UDP caller で同期済み応答、許可・拒否、
条件変更、process restart、SQLite reopen と撤去を確認します。単なる SKIP は合格ではありません。

FreeBSD 14.3 では専用 VM に正確な source から作った chain test binary を置き、
`make check-routerd15-ntp-native-freebsd NTP_TEST_BINARY=/absolute/path/chain.test` を root で実行します。
VNET jail 内で時計変更権限が拒否されることを事前確認し、共有時計・rc.d 設定を変更せず
実 ntpd と UDP caller を使います。共有サービスの制御や時計の精度を証明する試験ではありません。

成功時は必要な cleanup の後で一時出力を削除します。失敗時だけ原因特定に必要な範囲を保持し、
再現条件・判定は手順と PR/台帳へ戻します。

## 正式パッケージと OS サービスによる時刻同期

独立した管理経路と隔離リンクを持つ、専用 Ubuntu VM 2台を使います。
router=`198.18.97.1/24`、reference=`198.18.97.2/24`、reference の caller 用 alias を
`198.18.97.10` / `198.18.97.20` とします。共有ホスト、container、network namespace、
FreeBSD jail で時計変更の手順を実行しないでください。

製品起動前に OS を準備します。router の時計サービスを停止し、chrony の既定の drift・
makestep 設定と `confdir /etc/chrony/conf.d` を残して、この fixture の既定の外部 pool を除きます。
reference では隔離 address、subnet allowlist、`local stratum 8` の別 chronyd を起動します。
reference は `-x` でも構いませんが、製品が管理する router の chrony service は通常どおり動かします。
reference が有効な NTP 応答を返すことを確認してから router の時計を変更します。

`make dist` の checksum を確認して展開先の `bin/` を含むディレクトリへ移動し、
`sudo ./install.sh --prefix /usr/local` で導入します。専用 router のみに
測定済みの10秒の時計差を作り、外部の dummy link `ntp-gate` を up にしてから
`tests/fixtures/linux-ntp-packaged.json` で routerd を起動します。
この link の観測 phase を server 条件に使い、upstream と管理通信は実リンクに残します。

2分以内に NTPServer の設定確認、NTPClient の共有 chrony 経由の Applied、reference 選択、
chronyc tracking の通常 leap 状態と、作った時計差の実際の補正を確認します。
これは準備した fixture の時間上限で、任意の時刻サーバーに対する精度・収束 SLO ではありません。
client の timesyncd 要求と実効 chrony provider を確認し、競合サービスを起動させません。

```sh
python3 scripts/ntp-peer-query.py --server 198.18.97.1 --source 198.18.97.10
python3 scripts/ntp-peer-query.py --server 198.18.97.1 --source 198.18.97.20 --expect silence
```

拒否検査の後にも許可検査を行い、provider 停止を拒否の根拠にしません。caller は時計を変更せず、
originate timestamp の一致、同期済み mode-4 応答、stratum を確認します。
dummy gate を down にして WhenFalse と応答停止、client の upstream 選択維持を確認し、
up に戻して応答復帰を確認します。routerd restart 後にも再確認します。
その後 chrony の NTPClient のみへ設定を置き換え、server fragment・downstream 応答の撤去と
同期維持を確認し、最後に空設定・uninstall・VM/disk/media/一時出力の削除まで行います。

失敗時は service・設定・packet の実状態を確認してローカル再現します。
この試験は timesyncd 単独、FreeBSD rc.d、storage crash、物理時計の精度の合格には広げません。


2026-11-03 の限定検証では製品 package `1d012587`、fixture/probe `5f7275ec`、
Ubuntu 24.04 / Linux、chrony 4.5 の専用 PVE VM 2台で上記を確認しました。
通常の systemd chronyd が作った時計差を -9.997554 秒補正し、独立した stratum-8 reference
を選択して通常 leap 状態になりました。許可・拒否・許可、WhenFalse 時の同期維持と応答停止、
WhenTrue 復帰、routerd restart/SQLite recovery、chrony client のみへの変更、空設定での
fragment・参照元撤去、uninstall が通り、VM・disk・snippet を削除しました。
reference のみ `-x` を使い、製品 service には使っていません。

NTPClient/NTPServer は Applied、NTP controller error はありません。全体 status は
未ロードの nf_conntrack による自動生成 ConntrackObserver の Unavailable を表示しており、
これを手修正したりルーター全体の正常と読み替えたりしていません。
相対的な時計差補正と上記経路の検証で、外部 UTC 精度、timesyncd 単独、provider package 交換、
FreeBSD rc.d の合格には広げません。FreeBSD wrapper の成功時出力削除は構文・lint 確認のみで、
この campaign では native 実行を再実施していません。


依存パッケージは管理対象providerに従います。Linuxのtimesyncd単独clientは
`systemd-timesyncd`、chrony所有者は`chrony`を要求し、共有chrony serverが所有する場合は
競合するtimesyncdを要求しません。管理対象外のNTP宣言はproviderを導入せず、FreeBSDの
標準ntpdにも追加packageは不要です。timesyncd単独試験ではapply後もtimesyncdが導入済みで、
chronyへ勝手に置換されていないことを確認します。


## Linux パッケージの timesyncd 単独試験

独立した管理経路を持つ専用 Ubuntu VM を使う。準備時に元の時刻サービスの
パッケージ版・有効化・稼働状態を記録し、復元用の同じ版の配布パッケージを確保する。
systemd-timesyncd の導入による chrony の削除は準備段階で確認する。
`make dist-deb dist-ndpi-agent-libndpi-deb` の正式パッケージを checksum 確認後に導入する。

隔離リンクでルーターを `198.19.86.1`、独立 chronyd 参照を `.2` と `.3` に置く。
参照は `-x`、local stratum 8、ルーターの許可リストを使用する。製品起動前に
`scripts/ntp-peer-query.py` で各アドレスから相関の取れた有効な応答を 3 回確認する。
参照を network namespace に置く場合も VM の時計は共有されるため、時計差を注入しない。
これは実同期通信の試験であり、外部 UTC 精度や注入した時計差の補正の証明ではない。

1. managed NTPClient のみを宣言し、`provider: systemd-timesyncd`、
   `source: static`、`servers: [198.19.86.2]` とする。validate 後、通常の
   パッケージ版 routerd に `routerctl apply --timeout 90s -f <config> --replace` を実行する。
2. 120 秒以内に Applied、正しい provider/server、timesyncd 稼働を要求する。
   `dpkg-query -W -f='${Status}' systemd-timesyncd` が `install ok installed`、
   `timedatectl show-timesync -p ServerAddress --value` が指定アドレス、
   `NTPSynchronized=yes`、`LC_ALL=C timedatectl timesync-status` の leap が normal、
   packet count が正数であること。Applied や以前の同期フラグだけでは合格にしない。
3. `.3` に変更して apply し、同じ判定と routerd MainPID 維持を確認する。
   systemd で routerd を再起動し、追加 apply なしで再度同期を確認する。
4. 空の設定を apply し、Healthy、resource/helper の撤去、status に記録した所有
   fragment の消失を確認する。試験サービスを停止し namespace/link/staging を撤去、
   元の時刻パッケージと有効化・稼働状態を復元し、専用 VM を停止する。

失敗時は依存パッケージの実行コマンド、dpkg 状態、provider unit、所有 fragment、
参照応答を確認し、ローカル回帰試験へ落とす。試験中にパッケージを手直しして合格にしない。

2026-11-06、正式製品 `59373f17`、パッケージ
`1.5.0~20261106212625+g59373f17` で両アドレス・動的 apply・再起動・撤去を通過。
各同期観測は normal leap、stratum 8、packet count 1。元の chrony を復元し VM を停止した。
DHCP 由来・インターフェース別・fallback の実機切替は別の残件。
ローカルの source 消失・復帰テストのサービス実行は fake である。
