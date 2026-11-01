# PPPoE の PVE 独立アクセスコンセントレーター試験

実施日: 2026-11-02（JST）  
対象ソースリビジョン: `6f67e4fb860eca93ee1b76c3fce22447279bcb6f`  
試験用 `routerd` SHA-256: `55075e013fb09411376a2672c403e4713e5a96c61c716ca604e326139786224d`  
試験用 `routerd-pppoe-client` SHA-256: `49c1b8977229a15390116bdcfd880aee5fe21f5ab4863c454d73947d7838c7a0`

## 結果

VM219 の routerd は、VM220 の独立 PPPoE アクセスコンセントレーターへ PAP で
接続し、IPCP により IPv4 アドレスと既定経路を取得しました。VLAN 3131 の独立
クライアント VM224 から、VM219 を経由して VM220 の HTTP サーバーへ到達しました。
VM220 の HTTP ログは、クライアントの `198.18.15.100` ではなく VM219 の PPP
アドレスを送信元として記録しました。したがって、VLAN 分離、PPPoE、IPv4 NAT を
通る通信を確認できています。

アクセスコンセントレーター停止時には、VM219 の PPP インターフェースと既定経路が
消え、失敗イベントが記録されました。アクセスコンセントレーター再開後と routerd
再起動後には、接続・既定経路・独立クライアント通信が復旧しました。試験で観測した
待機時間を製品目標値としては扱いません。

## 実装した修正

`PPPoESession.spec.ifname` と `defaultRoute` は定義されていましたが、常駐
`routerd-pppoe-client` の起動引数へ渡されていませんでした。Linux の PPP peer
設定にも既定経路の指示がありませんでした。

- 監督側は `--ifname` と `--default-route` を PPPoE クライアントへ渡す
- PPPoE クライアントは指定した PPP インターフェース名を使用する
- `defaultRoute: true` の Linux peer 設定へ `defaultroute` と
  `replacedefaultroute` を出力する
- BFD 修正で追加した `CAP_DAC_OVERRIDE` の systemd 能力列を既存の unit 契約順へ
  揃える

`pkg/render`、`pkg/pppoeclient`、`cmd/routerd-pppoe-client`、
`pkg/controller/chain` の対象試験と `pkg/controller/chain` 全試験は合格しました。

## 構成

- ルーター: VM219、LAN は VLAN 3131 の `rd15-lan`、PPPoE 下位回線は VLAN 3132 の
  `rd15-wan`
- 独立アクセスコンセントレーター: VM220、`pppoe-server`、PAP、サービス名
  `rd15-service`、アクセスコンセントレーター名 `rd15-ac`
- 独立クライアント: VM224、VLAN 3131 の `rd15-client-lan`、一時アドレス
  `198.18.15.100/24`、既定ゲートウェイ `198.18.15.1`
- PPPoE 設定: PPP インターフェース `ppp-lab`、MTU/MRU 1492、`defaultRoute: true`
- 到達先: VM220 の `100.64.0.1:18090` に束縛した一時 HTTP サーバー

VM220 の既定の入力方針は PPP 側からの通信を拒否します。このため、試験中だけ
実在した PPP インターフェースと ICMP echo-request、TCP/18090 に限定した入力許可を
追加しました。routerd の設定を広く緩和していません。

## 観測

- 常駐クライアントの実行引数に `--ifname ppp-lab --default-route` を確認
- 生成した Linux peer 設定に `ifname ppp-lab`、`defaultroute`、
  `replacedefaultroute` を確認
- PAP 成功後、VM219 の `ppp-lab` は `100.64.0.5`、既定経路は
  `default dev ppp-lab` になった
- VM224 からの ICMP と HTTP は成功し、VM220 の HTTP ログは送信元
  `100.64.0.5` を記録
- アクセスコンセントレーター停止後、VM219 は PPPoE `Failed` を記録し、
  `ppp-lab` と PPP の既定経路が消えた
- アクセスコンセントレーター再開後、VM219 は `100.64.0.2` を取得して既定経路を
  復元し、VM220 の HTTP ログは送信元 `100.64.0.2` を記録
- routerd 再起動後、VM219 は `100.64.0.3` と `default dev ppp-lab` を復元し、
  VM224 の HTTP は成功、VM220 は送信元 `100.64.0.3` を記録

PPPoE 設定を基準設定へ置換した最初の適用は、正本置換後に
`IPv4Route/default-v4` の所有権不一致を理由として `Drifted` を返しました。
その後、PPPoE 常駐プロセスと `ppp-lab` は停止し、`rd15-wan` の基準既定経路は
復元されました。同じ基準設定をもう一度適用すると、generation 44、21 リソース
すべて `Healthy` になりました。この初回置換の失敗は
`routerd15-open-findings.md` に未解決事項として記録しています。

## 復帰

VM219 は元の `routerd` と `routerd-pppoe-client` の SHA-256 へ戻し、基準設定を
generation 44、21 リソースすべて `Healthy` で再適用しました。PPPoE プロセス、
`ppp-lab`、PPP の既定経路は残っていません。

VM220 は PAP secrets、routerd サービス、入力規則、VLAN、PPPoE
アクセスコンセントレーターを基準状態へ戻しました。VM224 は停止しました。試験用
CD-ROM、ISO、作業ディレクトリを削除しました。

## 後続の置換修正

この試験で記録した初回置換の `Drifted` は、`59681ef4` で修正済みです。厳密な設定
置換では、経路制御を開始する前に不要になった PPPoE 常駐プロセスを停止するように
しました。さらに、`--ifname` の値を PPPoE の現在状態へ反映します。

修正後は、有効な PPPoE 接続から同じ基準設定を一回だけ適用し、世代 46、21
リソースすべて `Healthy` を確認しました。`ppp-lab` は消え、既定経路は
`default via 198.18.16.1 dev rd15-wan` へ戻りました。復元後の VM219 では
`/usr/local/sbin/routerd` の SHA-256 が
`a1cbdd287e777d9eb090512ec7b7fb69f2340f0f12a71ed5567a8e2dcd2626f8`、
`/usr/local/sbin/routerd-pppoe-client` の SHA-256 が
`689e5ea589d0e4e43b0a74c3edbfac31d26e363c4c9b4b5d3799936f4f266246` であり、
routerd は稼働中でした。詳細は
`evidence/routerd15-59681ef4-pppoe-withdrawal-pve-20261102.md` に記録します。

## 未検証範囲

- IPv6 PPP、CHAP、物理回線、異なる実装のアクセスコンセントレーター
- PPPoE の認証失敗、MTU 境界、複数同時セッション、長時間の更新・リース挙動
