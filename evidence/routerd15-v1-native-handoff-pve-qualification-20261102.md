# routerd 1.0 ネイティブ状態移管 PVE 実通信検証

実施日: 2026-11-02

## 対象

- ソース改訂: `cc952492e3b4`
- 配布物: `routerd-v20261027.1620-linux-amd64.tar.gz`
- SHA-256: `8b2c6cee064caac2433c9cc50adc6d00865c786722f3014b1581a95c796417a5`
- ルーター VM: PVE VM 219
- 独立クライアント VM: PVE VM 220
- 分離ネットワーク: `br-rd15-lab` の VLAN 3131、3132、3133

## 修正対象

routerd 1.0 (`e3b49dbb`) が作成した IPv4 静的アドレスと IPv4 経路は、
1.5 に所有証跡がないため、更新直後に外部設定として保護されていました。

インストーラーは、固定した 1.0 識別子と現行設定の SHA-256 を持つ移管記録を作成します。
常駐実行は、記録と完全に一致する設定に限り、存在済みのネイティブ状態を所有証跡へ移管します。
設定を変更した場合、移管は許可されません。

## 実施結果

1. VM 219 で `e3b49dbb` を導入し、同一設定を保持したまま `cc952492` へ更新。
2. インストーラーが `/var/lib/routerd/migrations/routerd-v1-native-handoff.json` を 0600 で作成。
3. `IPv4StaticAddress/lan-v4` は `Applied`。
4. `IPv4Route/default-v4` はネイティブ所有証跡付き `Installed`。
5. 全体状態は `Healthy`、対象コントローラーの現在エラーはなし。
6. VM 220 から DHCP、DNS の UDP/TCP、NAT、管理到達性を確認。
   `PASS: live lease, DNS UDP/TCP, NAT and management positive control`
7. VM 220 から WAN/LAN/管理/DNS の VLAN 分離を確認。
   `PASS: WAN/LAN/management and DNS isolation`
8. VM 219 の `routerd.service` 再起動後、3 から 7 を再確認。

## 更新後の再検証

`0e7d27eb` の更新試験では、移管済みアドレスの所有証跡を保存する前に再照合する不具合を検出しました。この版は実機合格にしていません。

`58b66728` で、ハッシュ束縛済み移管を採用した直後に所有証跡を保存するよう修正し、再現単体試験を追加しました。専用ビルドの SHA-256 は `997af0127a5ced70ae446285c5d541e276861bfa54edd75cb47795495aa26bc6` です。

VM 219 を `58b66728` へ更新後、次を確認しました。

1. `IPv4StaticAddress/lan-v4` に `nativeReceipt` が保存され、`pending: false` でした。
2. `routerd.service` の再起動後、21資源、`Healthy`、現在エラーなしでした。
3. VM 220 の `probe` は DHCP、DNS の UDP/TCP、NAT、管理到達性で合格しました。
4. VM 220 の `isolation` は WAN/LAN/管理/DNS の VLAN 分離で合格しました。
5. `GOMAXPROCS=2 go test -p 2 -count=1 ./pkg/controller/chain` は 103.683秒で合格しました。

## 判定範囲

この記録は、routerd 1.0 から 1.5 への IPv4 静的アドレス・IPv4 経路の移管と、
この構成における独立クライアント実通信の合格証跡です。routerd 1.0 の全機能受入完了を示すものではありません。
