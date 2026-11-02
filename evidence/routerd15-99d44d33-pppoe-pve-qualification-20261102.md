# PPPoE・NAT44 の現行 PVE VLAN 実通信再試験

実施日: 2026-11-02（JST）
対象ソースリビジョン: `99d44d3329a68188ab5f3082213e55888399a88d`
実行物: `CGO_ENABLED=0 go build -trimpath` により同リビジョンから作成

| 実行物 | SHA-256 |
| --- | --- |
| `routerd` | `5baa537f279d3ef991296009df3190c081b5acdfc043774af912554bf735dab5` |
| `routerctl` | `c928c83fd47f474ffdd05b1fb17e5aa0aba3aea36d1714677ffad12d9f02b199` |
| `routerd-pppoe-client` | `427a36efe43d54dd576b3c30619fd92460b085199d5bda041d1f5ca9f2736810` |

## 結果

新規 routerd 1.5 設定で、VM219 は独立 VM220 の rp-pppoe アクセスコンセントレーターと
PAP/IPCP を確立しました。VLAN 3131 の独立クライアント VM224 は、VM219 の NAT44 を経由して
VM220 の HTTP サーバーへ到達しました。

アクセスコンセントレーター停止、再開、PPPoE 設定の撤回・再適用、routerd 再起動を実施しました。
各操作で PPP インターフェース、既定経路、独立クライアント通信は期待どおり撤回または復旧しました。
これは PAP、IPv4、単一 PPPoE セッション、単一 NAT44 規則に限定した実通信証跡です。

## 構成

- VM219: VLAN 3131 の `rd15-lan`、VLAN 3132 の `rd15-wan`、候補実行物と新規状態領域
- VM220: VLAN 3132 の `rd15-pppoe-wan`、rp-pppoe `pppoe-server`、PAP、`rd15-service`、`rd15-ac`
- VM224: VLAN 3131 の `rd15-client-lan`、`198.18.15.100/24`、既定ゲートウェイ `198.18.15.1`
- PPPoE: `ppp-lab`、MTU/MRU 1492、`defaultRoute: true`
- 到達先: VM220 の `100.64.0.1:18090` に束縛した一時 HTTP サーバー

VM220 の既存 routerd は試験中だけ停止し、その nftables テーブルを削除しました。
独立アクセスコンセントレーターの入力方針が試験通信を拒否するためです。
試験後に既存 routerd を再開し、テーブル再作成を確認しました。

## 観測

1. 新規 PPPoE 設定は世代 1、8 リソースすべて `Healthy` でした。
   PAP/IPCP 後、VM219 は `ppp-lab` に `100.64.0.10`、`default dev ppp-lab` を設定しました。
2. VM224 から `100.64.0.1` への ICMP は 3/3、HTTP は 200 でした。
   VM220 の HTTP ログは送信元として `100.64.0.10` を記録しました。
   nftables の `routerd_nat` は `oifname "ppp-lab" ip saddr 198.18.15.0/24 masquerade` を保持しました。
3. VM220 のアクセスコンセントレーターと PPP プロセスを停止しました。
   VM219 は PPPoE を `Failed` と公開し、`ppp-lab` を削除しました。
   既定経路は `default via 198.18.16.1 dev rd15-wan` に復帰しました。VM224 の HTTP は 000 でした。
4. VM220 のアクセスコンセントレーターを再開しました。
   VM219 は PAP/IPCP、`ppp-lab`、PPP の既定経路を再確立しました。
   VM224 の ICMP は 3/3、HTTP は 200 へ復旧しました。
5. PPPoE を含まない基準設定へ一回置換すると、世代 2、6 リソースは `Healthy` でした。
   `ppp-lab` と `routerd_nat` は不在となり、VM224 の HTTP は 000 でした。
   この適用は `2 orphaned managed artifacts found` の警告を返しました。
   同一 PPPoE 設定への再適用は世代 3、8 リソース `Healthy` となり、警告は再出力されませんでした。
6. 世代 3 の再適用後、VM219 は `100.64.0.11` を取得しました。
   VM224 の ICMP は 3/3、HTTP は 200 へ復旧しました。
   候補 `routerd` の systemd による再起動後も、`100.64.0.10`、PPP の既定経路、NAT44 を確認しました。
   VM224 の ICMP は 3/3、HTTP は 200 でした。

## 試験資材の補正

最初の試験用設定は外部所有の LAN アドレスを `IPv4StaticAddress` として再宣言しました。
このため、新規状態領域で所有権確認に失敗しました。最終設定では LAN アドレスを外部所有のままとしました。

独立アクセスコンセントレーターには当初、`/etc/ppp/pppoe-server-options` がありませんでした。
このため、PPPoE セッションを直後に終了しました。PAP を要求する最小設定を追加後に通信試験を実行しました。
これらは試験基盤の不備であり、製品合格としては数えていません。

## 復帰

VM219 は、試験前に保存した `routerd`、`routerctl`、`routerd-pppoe-client`、設定、状態、systemd の
ユニットへ戻しました。三つの復元済み実行物は保存時 SHA-256 と一致し、`routerd` と
`routerd-dnsmasq` は稼働中です。VM220 は PPPoE サーバー、PAP 設定、一時 HTTP サーバー、VLAN を
除去し、既存 routerd を稼働状態へ戻しました。VM224 は VLAN を除去して停止しました。

## 未検証範囲

- IPv6 PPP、CHAP、認証失敗、資格情報更新、MTU 境界、複数同時セッション、長時間障害
- 物理回線と異なる実装のアクセスコンセントレーター
- 全 PPPoE 機能、全 NAT44 機能、routerd 1.0 全機能の完了判定
