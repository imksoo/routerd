# DS-Lite 二経路優先切替の PVE 実通信記録

## 結論

Linux 候補 `81444257` は、同じ委任済み IPv6 送信元を使う二つの DS-Lite トンネルを同時に維持しました。独立 AFTR-A の撤去では健全な AFTR-B へ切り替え、AFTR-A の復旧では高い重みの経路へ戻りました。各段階で、分離 VLAN の独立クライアントから HTTP 応答を取得し、NAT 後の送信元と接続追跡マークを確認しました。

## 実行対象

| 項目 | 値 |
| --- | --- |
| 実行リビジョン | `81444257bd89a9f3f25ed9fd7e7f1023c6fa18c9` |
| Linux 配布物 SHA-256 | `a07047cfc3bc3fc9131fc2bbda7e052bc736f0e52cc1deb7229fafa5cdb039a7` |
| 構成 SHA-256 | `fab3c33a6fc516f43b67b881132ff695ca45cc27bc27126825aab33e53e2991e` |
| 候補ルーター | PVE VM219 |
| 独立 AFTR、Kea、HTTP端点 | PVE VM220、VLAN 3132 |
| 独立 IPv4 クライアント | PVE VM224、VLAN 3131 |
| 委任プレフィックス | `2001:db8:1500::/60` |
| 共有外側送信元 | `2001:db8:1500::100` |
| AFTR-A、内側 IPv4 | `2001:db8:16::1`、候補 `192.0.0.2`、AFTR `192.0.0.1` |
| AFTR-B、内側 IPv4 | `2001:db8:16::2`、候補 `192.0.0.3`、AFTR `192.0.0.1` |
| 優先度 | A: 重み200、マーク `0x110`、経路表110。B: 重み100、マーク `0x111`、経路表111 |

構成は新規の routerd 1.5 形式であり、旧設定、移行処理、旧状態データベースは使用していません。

## 実装変更

1. 同じ委任済み IPv6 送信元を複数の DS-Lite 資源が参照できるようにし、各資源の内側 IPv4 を明示します。
2. `priority` 方式で候補が一つの宛先だけを持つ場合、nftables はハッシュを要求せず、その宛先の固定マークを接続追跡へ保存します。
3. 再発試験 `TestNftablesEgressRoutePolicyPrioritySingleTargetDoesNotRequireHash` を追加しました。`go test ./pkg/render -run TestNftablesEgressRoutePolicyPrioritySingleTargetDoesNotRequireHash -count=1`、関連する経路選択試験、設定検証は通過しました。

## 観測結果

1. VM219 で `rd15-dsla` と `rd15-dslb` はともに `Up`、対応する HealthCheck は `Healthy` になりました。各トンネルから VM220 の `192.0.2.1:18080` へ直接 HTTP を実行し、応答本文を受信しました。
2. 優先 EgressRoutePolicy は `dslite-a`、重み200を選択しました。`table ip routerd_policy` は LAN の新規接続へ固定マーク `0x110` を設定しました。
3. VLAN 3131 の VM224 から `192.0.2.1:18080` への新規 HTTP は成功しました。VM220 の HTTP記録は送信元 `192.0.0.2`、VM219 の接続追跡は元の `198.18.15.10`、変換後の `192.0.0.2`、マーク272を記録しました。
4. VM220 で `rd15-aftr-a` だけを削除しました。`through-dslite-a` は `Unhealthy`、`lastResult: timeout` へ変化し、`through-dslite-b` は `Healthy` を維持しました。EgressRoutePolicy は `dslite-b`、重み100、固定マーク `0x111` を選択しました。
5. 撤去中の VM224 の新規 HTTP は成功しました。VM220 は送信元 `192.0.0.3`、VM219 の接続追跡は元の `198.18.15.10`、変換後の `192.0.0.3`、マーク273を記録しました。
6. VM220 で AFTR-A を同じ外側宛先・内側 IPv4 で再作成しました。候補プロセスの再起動も設定再適用もせず、`through-dslite-a` は `Healthy`、EgressRoutePolicy は `dslite-a`、重み200、固定マーク `0x110` に戻りました。VM224 の新規 HTTP は成功し、VM220 は送信元 `192.0.0.2`、VM219 はマーク272を記録しました。

固定の切替時間は、この試験の要件にしていません。観測した状態遷移と新規通信のみを記録します。

## 復元

VM219、VM220、VM224 を `rd15-mul-119` の試験前スナップショットへ戻しました。試験用 ISO、CD-ROM接続、PVE一時資材、スナップショットを削除しました。VM219 と VM220 は起動、VM224 は停止の基準状態へ戻っています。

## 範囲外

- AFTR の FQDN 応答変更と二経路優先切替の組合せ、能動待機、同時多重障害、性能、物理ネットワーク
- IPv6 クライアント転送、PPPoE、全経路制御条件、全防火壁方針、FreeBSD PF
- routerd 1.0 全機能の完了判定
