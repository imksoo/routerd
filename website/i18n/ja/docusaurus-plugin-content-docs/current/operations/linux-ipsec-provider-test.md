# Linux IPsec のパッケージ・サービス検証

明示的に割り当てられ、独立した管理接続を持つ専用 Ubuntu 24.04 VM で実行します。
実パッケージを導入・削除し、新規導入した provider を強制終了する試験です。
既存の strongSwan 設定・パッケージ、空でない routerd 設定、XFRM state/policy、
正常でない既存 routerd がある場合は開始しません。
Python 3、jq、iproute2、namespace/mount 用ツール、AppArmor とその parser、
Ubuntu archive keyring が必要です。

root 所有の新規作業ディレクトリ、SHA256 を固定した `make dist` archive、
対向 peer 用の同版 strongSwan の展開物を用意します。`APT_CONFIG` で署名検証付き
Ubuntu repository と private list/cache を指定します。外向き経路のない VM では
loopback の SSH reverse forward を利用でき、ホストの NAT や経路変更は不要です。
署名検証は無効化しません。

`apt-get -o APT::Update::Error-Mode=any update` の終了コードを確認し、
`apt-get -s install charon-systemd strongswan-swanctl` が新規 6 package、
既存 package の更新・削除なしとなることを確認します。実際の導入は製品の
PackageController に任せます。リポジトリルートから実行してください。

```sh
sudo timeout -k 15 900 bash scripts/linux-ipsec-provider-smoke.sh \
  --disposable-vm /root/private-work /root/routerd.tar.gz EXPECTED_SHA256 \
  /root/private-peer/extracted /root/private-work/apt.conf
```

試験ごとに新規 work directory を作成し、失敗結果を上書きしません。
既存 routerd の unit・実行ファイル・設定・状態ファイルを記録し、計画停止中に
設定と DB の backup/hash を取得します。試験バイナリと DB は別パスに置きます。
元の binary・設定・DB を置換せず、生成物として新規 IPsec receipt を使用します。

package controller による導入後、製品が接続を取得する前に、試験用の管理外接続を
`/etc/swanctl/` から読み込み `--list-conns` で確認します。Ubuntu の AppArmor が
許可する場所を使います。`--load-conns` は入力にない接続を削除し得るため、
製品接続の取得後には使いません。また終了 0 だけを設定読込成功と判断しません。

合格条件:

- 実 PackageController による依存導入、実 strongswan.service 起動、VICI 応答、所有 receipt の確定。
- 独立した peer との IKE/ESP と双方向通信。
- 再適用・撤去・再取得中に provider PID と systemd invocation が変わらず、管理外接続を保持すること。変更なしの再適用では ESP SPI と通信も保持すること。
- 新規 provider への SIGKILL 後、systemd と実行中 routerd が自動復旧すること。手動 restart/reconcile/SA initiate を行わず、通常通信で新たな SA が確立すること。
- 子 process の終了確認、所有効果の撤去、新規 package だけの削除。module/link/route/rule/AppArmor の復元、停止した自 provider の stale socket の撤去。
- 元 binary・設定・unit・停止中 DB の hash 一致と、旧 routerd の Healthy 復帰。

失敗時は stage、provider journal、receipt、cleanup 結果を確認してから再試験します。
説明できない writer がいる DB を backup で上書きせず、pending receipt を削除しません。
PSK・algorithm・peer を限定した試験であり、IPsec 全 mode やリリースの合格ではありません。
source/archive/script の hash と限定した判定を残し、レビューと復元確認後は
解決済み生出力・重複 binary・backup を片付けます。手順と回帰検査を長期成果とします。
