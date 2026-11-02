# PVE WAN リンク断・復帰の実通信記録

## 結論

Linux 配布物 `81444257` は、PVE が VM219 の WAN-A 専用仮想 NIC へ設定したリンク断を検出しました。`HealthCheck` は WAN-A を `Unhealthy` にし、優先 `EgressRoutePolicy` は WAN-B を選択しました。独立 VM224 の新規 HTTP は継続し、独立 VM220 は WAN-B の NAT44 後送信元を記録しました。リンク復帰後は、候補機の再起動や設定再適用なしに WAN-A へ戻りました。

これは PVE の仮想 NIC リンク状態による WAN 障害試験です。物理ケーブル、回線終端装置、ISP 網を切断した試験ではありません。

## 実行対象

| 項目 | 値 |
| --- | --- |
| 実行リビジョン | `81444257bd89a9f3f25ed9fd7e7f1023c6fa18c9` |
| Linux 配布物 SHA-256 | `a07047cfc3bc3fc9131fc2bbda7e052bc736f0e52cc1deb7229fafa5cdb039a7` |
| 構成 SHA-256 | `d17fa3f31377c7873a6e90d5bc90ec559cd414b84372ffc847078d638f434fb0` |
| 候補ルーター | PVE VM219 |
| 独立上流 | PVE VM220。VLAN 3132 は WAN-A、VLAN 3133 は WAN-B |
| 独立クライアント | PVE VM224。VLAN 3131 |
| WAN-A | VM219 `ens19`、PVE の VLAN 3132 専用仮想 NIC、`198.18.16.2/24` |
| WAN-B | VM219 `rd15-admin`、VLAN 3133、`198.18.17.1/24` |

構成は新規の routerd 1.5 形式です。旧設定、移行処理、旧状態データベースは使用していません。

## 観測結果

1. `routerctl validate --replace -f /mnt/router.yaml` は有効でした。通常時は両方の HealthCheck が `Healthy` で、優先度の高い WAN-A が選択されました。VM224 の HTTP は VM220 へ到達し、VM220 は NAT44 後の送信元 `198.18.16.2` を記録しました。
2. PVE で VM219 の `net1` を `link_down=1` にしました。候補内の `ens19` は `NO-CARRIER` になりました。WAN-A の HealthCheck は `host_unreachable`、`Unhealthy` へ遷移しました。WAN-B は `Healthy` を維持しました。
3. 選択は WAN-B、重み100へ変わりました。VM224 の新規 HTTP は成功し、VM220 は NAT44 後の送信元 `198.18.17.1` を記録しました。
4. PVE で `net1` を `link_down=0` に戻すと、候補内の `ens19` は `LOWER_UP` になりました。WAN-A は `Healthy`、選択は WAN-A、重み200へ戻りました。VM224 の新規 HTTP は成功し、VM220 は再び `198.18.16.2` を記録しました。
5. 空の新規 1.5 構成へ置換すると、`EgressRoutePolicy` は存在しなくなり、VM224 の新規 HTTP は接続タイムアウトで失敗しました。同じ構成を再適用すると、両方の HealthCheck、WAN-A 選択、VM224 の HTTP、VM220 の送信元 `198.18.16.2` が復旧しました。

切替時間の目標値は設定していません。観測したリンク状態、健全性、選択結果、新規通信だけを記録します。

## 準備時の訂正

最初の投入は CD-ROM のホット接続で VM219 に `/dev/sr0` が現れず失敗しました。また、Windows 改行を含む補助スクリプトは実行できませんでした。VM219 を通常再起動して CD-ROM を認識させ、投入を Base64 化して改行を正規化しました。この時点まで HTTP、障害切替、または合格判定に使う通信は実行していません。

## 復元

VM219、VM220、VM224 を試験前スナップショットへ戻し、PVE の CD-ROM 接続、試験 ISO、一時配置、スナップショットを削除します。VM219 と VM220 は稼働、VM224 は停止の基準状態へ戻します。

## 範囲外

- 物理ケーブル、回線終端装置、ISP 網、PPPoE、IPv6 動的経路、ECMP、既存 TCP 接続、同時多重障害
- FreeBSD、および routerd 1.0 全機能の完了判定
