# routerd 1.5 DS-Lite 分離 PVE 実通信検証（2026-11-02）

## 結論

routerd 1.5 の Linux ビルド `58b66728` が、PVE の分離 VLAN 3132 上で DS-Lite の IPv4-in-IPv6 トンネルを作成しました。独立 AFTR 側で復号した IPv4 宛先への ICMP は 3/3 応答しました。

## 実行対象

| 項目 | 値 |
| --- | --- |
| Linux ビルド | `58b66728` |
| パッケージ SHA-256 | `997af0127a5ced70ae446285c5d541e276861bfa54edd75cb47795495aa26bc6` |
| ルーター | PVE VM219 `routerd15-cutover-router` |
| 独立 AFTR 側 | PVE VM220 の `rd15-wan-peer` 名前空間 |
| 分離ネットワーク | `br-rd15-lab` の VLAN 3132 |
| ルーター側 IPv6 | `2001:db8:3132::2/64` |
| AFTR 側 IPv6 | `2001:db8:3132::1/64` |
| 内側 IPv4 宛先 | `192.0.2.1` |

## 観測結果

1. `DSLiteTunnel/isolated-aftr` を現行の21資源構成へ一時追加しました。`routerctl apply --replace` は `Healthy` で完了しました。
2. routerd は `rd15-dslite@rd15-wan` を作成しました。種別は `ip6tnl ipip6`、ローカル IPv6 は `2001:db8:3132::2`、AFTR は `2001:db8:3132::1`、MTU は1454でした。
3. AFTR 側の独立した IPv4-in-IPv6 トンネルで `192.0.2.1` を終端し、ルーターからの ICMP は 3/3 応答しました。
4. ルーター側・AFTR 側のトンネル統計はともに送受信3パケット、252バイトでした。AFTR 側の送信ドロップは、PVE 分離用の受信制限によるもので、ルーター側の応答受信は 3/3 成功しました。
5. 一時資源を除いた元の構成へ戻すと、`rd15-dslite` は削除されました。AFTR 側のトンネルと IPv6アドレスも削除しました。VM219 は21資源、`Healthy`、コントローラーエラーなしへ復帰しました。

## 範囲

この検証は、指定 AFTR 宛て DS-Lite の生成、独立終端とのカプセル化・復号、および撤去を確認します。実 ISP の DHCPv6 情報要求、プレフィックス委譲、AFTR の冗長切替、全ての NAT 方針を代替するものではありません。
