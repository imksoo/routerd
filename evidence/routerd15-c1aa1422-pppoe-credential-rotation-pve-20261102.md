# routerd 1.5 Linux PPPoE 資格情報変更 PVE 実通信試験

実施日: 2026-11-02（JST）

## 結果

空の状態領域から、VM219 の PPPoE 資格情報を変更する新規 1.5 構成を適用しました。独立 VM220
のアクセスコンセントレーターを同じ新資格情報へ切り替えると、旧セッションは 3 秒以内の観測で
消え、候補は `ERROR` を公開しました。`routerctl apply --replace` による新構成の適用後、
`ppp-v6`、IPv6CP、ICMPv6 は 3/3 で復旧しました。

この試験で見つかった空の `ip routerd_nat` 表の誤った孤立警告は、`c1aa1422` で修正しました。
修正版パッケージで同じ新資格情報の構成を適用すると、`HEALTHY`、警告なし、PPP 常駐プロセス
各一つ、IPv6CP、ICMPv6 3/3 を確認しました。空構成への撤回と新構成の再適用も同じパッケージで
確認しました。

## 実行対象

- PPPoE 資格情報変更の実通信: `ea892a6bc0e9b62cb1c8ac292509d50f380921f5`
- 孤立警告の修正: `c1aa14222e72a041cab390e4abe06196d13b9179`
- 修正版パッケージ SHA-256: `963d570586aa40d02628ea82350efb254b28657e17e354783b3a3685e78a6545`
- PPPoE の実装経路 `pkg/controller/pppoesession`、`pkg/pppoeclient`、
  `cmd/routerd-pppoe-client`、`pkg/render/pppoe.go` には、二つの改訂間で変更がありません。
- VM219: 候補ルーター。VLAN 3132 の `rd15-wan` と `ppp-v6`
- VM220: VLAN 3132 の独立 `rp-pppoe` アクセスコンセントレーター。PAP、IPCP、IPv6CP
- 新規構成: `evidence/pve07-pppoe-ipv6.yaml`、`evidence/pve07-pppoe-ipv6-rotated.yaml`
- 撤回構成: `evidence/pve07-pppoe-ipv6-withdraw.yaml`

## 空状態と観測

1. スナップショットで保護した VM219 で `routerd` を停止し、既存の `/var/lib/routerd` を試験用の
   一時配置へ退避しました。新しい状態 DB は 4 KiB から開始しました。
2. 初期資格情報で、VM219 は IPv6CP の対向リンクローカルアドレスを取得し、ICMPv6 は 3/3 応答しました。
3. VM220 の PAP 資格情報を変更してアクセスコンセントレーターを起動し直すと、旧 `ppp-v6` は 3 秒以内の
   観測で消え、候補の状態は `ERROR` でした。
4. VM219 で `routerctl apply -f ... --replace` により変更後の新規構成を適用すると、世代 1、
   `CONNECTED`、資源数 6 になりました。IPv6CP は 3 秒の観測で再確立し、ICMPv6 は 3/3 応答しました。
5. 初回適用時の空 `ip routerd_nat` 表は、当時の孤立判定で警告になりました。実行時の PPP 常駐プロセスは
   新しい一組だけでした。警告を黙殺せず、所有意図を追加しました。
6. 修正版 `c1aa1422` を同じ空状態へ導入後、変更後の資格情報だけを持つアクセスコンセントレーターへ
   IPv6CP を確立しました。同じ構成の `routerctl apply --replace` は警告なしで `HEALTHY` となり、
   ICMPv6 は 3/3 応答しました。
7. 空構成への置換で `ppp-v6` と PPP 用 ICMPv6 規則は消えました。再適用後は世代 3、`CONNECTED`、
   IPv6CP、ICMPv6 3/3 に復帰しました。

## 防火壁境界

候補の既定入力方針は遮断です。試験構成は PPP 側を非信頼ゾーンへ置き、候補自身への `icmpv6`
`echo-request` だけを許可しました。空構成ではその規則を撤回し、空の既定遮断表を残しました。

## 復元

VM219 と VM220 を試験前スナップショットへ戻しました。二つの試験 ISO、一時配置、状態退避、
スナップショットを削除しました。VM219/220 の既存 `routerd` は `active`、VM224 は停止状態です。

## 未確認範囲

この結果は PAP の資格情報変更、IPv6CP リンクローカル通信、空構成の撤回・再適用に限ります。
IPv6 プレフィックスの配布・LAN 転送、別実装のアクセスコンセントレーター、CHAP、認証失敗の理由別処理、
MTU 境界、複数同時セッション、長時間切断、物理回線、FreeBSD、PPPoE 全体、Linux 全体、
置換可能品質全体は未確認です。
