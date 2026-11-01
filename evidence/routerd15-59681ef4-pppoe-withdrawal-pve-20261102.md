# PPPoE 撤去と基準経路復帰の PVE 再試験

実施日: 2026-11-02（JST）
対象ソースリビジョン: `59681ef4a10e0da9d4d7ba587c19cfe32b85460f`
試験用 `routerd` SHA-256: `907615b4d7b6ce30182d6020a8cf96f8971a72bdd2a0bdc9c7dd67939a8c2f7f`
試験用 `routerd-pppoe-client` SHA-256: `49c1b8977229a15390116bdcfd880aee5fe21f5ab4863c454d73947d7838c7a0`

## 結果

有効な PPPoE 接続から VM219 の基準設定を一回だけ適用し、世代 46、21 リソース
すべて `Healthy` になりました。`ppp-lab` と PPPoE 常駐プロセスは消え、既定経路は
`default via 198.18.16.1 dev rd15-wan` へ戻りました。従来の一回目だけ `Drifted` に
なる経路所有権の競合は、この実行リビジョンでは再現しませんでした。

## 修正内容

厳密な世代置換時は、経路制御より先に現在の PPPoE 常駐プロセスを通常の監督経路で照合
して停止します。これにより、削除予定の PPP 既定経路を次の `IPv4Route/default-v4` が
外部の経路として誤認しません。PPPoE 現在状態の取得では `--ifname` を読み取り、設定
済みインターフェース名と照合します。

## 構成と観測

- VM219: VLAN 3131 の LAN、VLAN 3132 の PPPoE 下位回線、基準設定は 21 リソース
- VM220: VLAN 3132 の独立 `pppoe-server` と PAP 認証
- 試験開始時: 世代 45、`Healthy`、`ppp-lab` は `100.64.0.2`、既定経路は
  `default dev ppp-lab`
- 刺激: VM219 で `/var/tmp/routerd15-bfd-pve/base.json` を `routerctl apply --replace`
  により一回適用
- 観測: 世代 46、21 リソースすべて `Healthy`、`ppp-lab` 不在、基準既定経路復帰

この再試験は、最初の PPPoE 試験で見つかった撤去順序だけを対象にします。IPv6 PPP、
CHAP、認証失敗、MTU 境界、複数同時セッションおよび異なる実装のアクセスコンセントレーター
は検証していません。

## 復帰と独立確認

VM219 の試験用バイナリを試験前の `/usr/local/sbin/` 配置へ戻し、routerd の稼働、
`ppp-lab` 不在、基準既定経路を確認しました。復元済み `routerd` と
`routerd-pppoe-client` の SHA-256 は、それぞれ
`a1cbdd287e777d9eb090512ec7b7fb69f2340f0f12a71ed5567a8e2dcd2626f8` と
`689e5ea589d0e4e43b0a74c3edbfac31d26e363c4c9b4b5d3799936f4f266246` です。

VM220 では PPPoE アクセスコンセントレーター用 VLAN と一時設定が不在で、routerd は
稼働中です。VM224 は停止中です。VM219 の試験用 CD-ROM は切り離し、試験用 ISO と
作業ディレクトリは削除済みです。
