# HealthCheck と経路方針の配布物更新・再起動 PVE 実通信記録

## 結論

Linux 配布物 `81444257` を PVE VM219 へ導入した後、二つの DS-Lite 経路、HealthCheck、優先 EgressRoutePolicy、NAT44 は復旧しました。`routerd` の再起動で主プロセスPIDが交代した後も、独立クライアント通信と選択結果は維持されました。

## 実行対象

| 項目 | 値 |
| --- | --- |
| 実行リビジョン | `81444257bd89a9f3f25ed9fd7e7f1023c6fa18c9` |
| Linux 配布物 SHA-256 | `a07047cfc3bc3fc9131fc2bbda7e052bc736f0e52cc1deb7229fafa5cdb039a7` |
| 構成 SHA-256 | `fab3c33a6fc516f43b67b881132ff695ca45cc27bc27126825aab33e53e2991e` |
| 候補ルーター | PVE VM219 |
| 独立 Kea、AFTR、HTTP端点 | PVE VM220、VLAN 3132 |
| 独立 IPv4 クライアント | PVE VM224、VLAN 3131 |

構成は新規の routerd 1.5 形式であり、旧設定、移行処理、旧状態データベースは使用していません。

## 観測結果

1. チェックサムを確認した配布物を VM219 へ導入しました。`routerctl version` は `81444257` を報告しました。
2. `through-dslite-a` と `through-dslite-b` はともに `Healthy`、EgressRoutePolicy は `dslite-a`、重み200を選択しました。nftables は LAN の新規接続に固定マーク `0x110` を設定しました。
3. VM224 の新規 HTTP は成功し、VM220 の HTTP記録は変換後送信元 `192.0.0.2` を記録しました。
4. `systemctl restart routerd` の前後で主プロセスPIDは `5051` から `8062` へ交代しました。
5. 再起動後も `routerd` は `active`、両方の HealthCheck は `Healthy`、EgressRoutePolicy は `dslite-a`、重み200、固定マーク `0x110` でした。VM224 の新規 HTTP は成功し、VM220 は再び送信元 `192.0.0.2` を記録しました。

## 復元

VM219、VM220、VM224 を `rd15-health-814` の試験前スナップショットへ戻しました。試験用 ISO、CD-ROM接続、PVE一時資材、スナップショットを削除しました。VM219 と VM220 は起動、VM224 は停止の基準状態へ戻っています。

## 範囲外

- IPv6 方針ルーティング、ハッシュまたはマーク方式の全分岐、動的ゲートウェイまたは送信元、VRRP、物理 WAN 障害
- 同時多重障害、性能、物理ネットワーク、FreeBSD PF
- routerd 1.0 全機能の完了判定
