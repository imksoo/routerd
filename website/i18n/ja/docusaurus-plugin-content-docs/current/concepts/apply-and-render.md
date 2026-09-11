---
title: 適用と生成
slug: /concepts/apply-and-render
sidebar_position: 4
---

# 適用と生成

![routerd で設定を検証し、dry-run し、サービスを起動して routerctl で状態を見る流れ](/img/diagrams/concept-apply-and-render.png)

routerd には、設定を書く段階で使う `routerd` と、動いている daemon
（起動し続けるプログラム）に話しかける
`routerctl` があります。最初は、この順番を守ると安全です。

```text
YAML を書く
    ↓
routerd validate
    ↓
routerd apply --once --dry-run
    ↓
安全を確認して routerd.service を起動
    ↓
routerctl get status
```

## 1. 検証する

`routerd validate` は YAML の書き方を確認します。Kind 名、必須の項目、値の範囲、
分かりやすい参照ミスを見つけます。daemon はまだ必要なく、ホストのネットワークも
変更しません。

```sh
sudo routerd validate --config ./router.yaml
```

初回に `routerctl validate` を使わない理由は、`routerctl` が動いている
`routerd.service` のローカルソケットに接続するからです。

## 2. dry-run する

dry-run は、読み込んだリソースの順番と生成する内容を確認する予行演習です。
初回は常設の状態ファイルを使わず、使い捨ての場所を明示します。

```sh
LAB_DIR="$(mktemp -d)"
sudo routerd apply --config ./router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
sed -n "1,120p" "$LAB_DIR/status.json"
```

- **state** は、routerd が見た状態を保存するデータベースです。
- **ledger** は、routerd が所有する成果物の記録です。
- **status** は、今回の結果を書いた JSON ファイルです。

`--dry-run` がある間はネットワークを本当に変更しません。それでも、設定の内容は
正しく読む必要があります。最初は隔離した Ubuntu Server VM とコンソールで行います。

## 3. 適用する

`routerd apply --once` から `--dry-run` を外すと、本当にホストを変更できます。
これは WAN、LAN、経路、NAT、サービスを変える可能性がある操作です。
最初の VM では、いきなり一回だけの live apply を勧めません。

代わりに、dry-run が安全だと確認してから設定を
`/usr/local/etc/routerd/router.yaml` に置き、`routerd.service` を起動します。
常駐するサービスは、設定と実際の状態の差を見つけて必要な処理を続けます。

```sh
sudo systemctl enable --now routerd.service
sudo systemctl is-active routerd.service
```

## 4. サービス起動後に routerctl を使う

サービスが作るローカルソケットに接続できるようになったら、`routerctl` を使います。
状態を読む例は次のとおりです。

```sh
sudo routerctl get status
sudo routerctl get events --limit 20
```

動いている routerd に候補 YAML を渡して検証や計画を見る場合も、この後です。

```sh
sudo routerctl validate -f candidate.yaml --replace
sudo routerctl plan -f candidate.yaml --replace
```

この 2 つは host の状態を変えませんが、稼働中の daemon が必要です。

## 適用途中で失敗した場合

エラーが返っても、canonical 設定ファイルは既に置換されている場合があります。
エラーと daemon ログの `stage`、`generation`、`canonical`、
`durabilityConfirmed`、世代完了記録の試行・成功フラグ、確定した runtime epoch を確認します。
`Healthy` はデータプレーンの観測結果であり、設定保存の完了を証明しません。

| 失敗箇所 | canonical 設定 | runtime と完了記録 |
| --- | --- | --- |
| 置換前 | 旧設定 | 切替済みなら復元を試みます。復元失敗時は実際の fallback または稼働世代なしを記録します。 |
| rename 後の chmod／directory sync | 新設定が可視 | 新 runtime を維持し、耐久性は未確認として返します。 |
| 世代完了の UPDATE | 新設定 | 新 runtime を維持し、通常の成功結果を出力せず失敗を返します。 |
| 結果や status の出力 | 新設定 | 完了済み世代は完了のまま、出力の失敗を返します。 |

SQLite に保存できない間も、daemon は今回の失敗診断をメモリーに保持します。
定期実行で正常な状態を観測しただけでは診断を消さず、次の設定操作で確認します。
このメモリーは再起動を跨ぎません。再試行前にエラー、canonical YAML、世代履歴、
controller status を照合してください。OS 全体の巻戻しや全ファイルシステムでの
電源断耐久性を保証するものではありません。

supervisor が runtime 切替要求を受け付けた後は、要求の context が期限切れになっても
完了の確認応答まで mutation の排他責任を維持します。準備処理がキャンセルに応じない
場合は新しい変更を拒否して serve の停止を通知します。停止しない builder の完了時間に
厳密な上限はありません。確定した稼働世代は serve に属し、HTTP 要求終了だけでは停止しません。

`--no-reconcile` は設定だけを commit して稼働世代を変更しません。
Standby の既存 HA 動作は維持し、canonical commit とホスト変更を行いません。
dry-run は本番の世代記録や canonical を変更しません。

dry-run は object status、状態変数、dynamic desired part を一つの SQLite read transaction
からコピーし、観測時刻・期限・source・世代・digest・撤去意図を保持します。
debug ログの入力 manifest には候補の hash と評価時刻を含め、payload は含めません。
job、event cursor、event／action journal、federation 履歴、世代履歴はコピー対象外です。
ホスト観測と controller の評価全体を凍結する仕組みではなく、実行時には TTL と ownership を
再評価します。manifest 自体は将来の apply を許可する証拠にはなりません。

## 生成とは

「生成」は、routerd が dnsmasq の設定、nftables の設定、systemd の unit など、
ホスト向けのファイルを組み立てることです。生成しただけで、必ずホストが変わる
わけではありません。dry-run では生成内容を確認し、live の適用やサービス起動で
必要な変更を反映します。

現在の routerd では、dnsmasq 向けには DHCPv4、DHCPv6、中継、RA の設定を
生成します。DNS の待ち受け、ローカルゾーン、条件付き転送、暗号化 DNS は
`DNSResolver` が扱います。

## プラットフォームの範囲

このページの初回手順は Ubuntu Server を対象にしています。netplan、systemd、
nftables のような Linux 固有の生成は、対応する機能がある環境だけで使います。
FreeBSD と NixOS の導入・サービス連携は土台がありますが、Ubuntu と同じ
レンダラーの対応を意味しません。
