# PortForward と IngressService の現行版 PVE 実行記録

対象リビジョンは `4aa5339e8f814dd638d258c1c8561ace5f8eb68d` です。PVE の隔離 VM219 で、候補バイナリと実際の `systemd` サービスを起動しました。候補の `routerd` は SHA-256 `6a5d1eb7c0149c97d30d6e8def8bb927551409461a5f00adcb0ca17ab887e500` です。

外部要求元、LAN クライアント、二つのバックエンド、管理到達確認は、VM219 内の別ネットワーク名前空間で稼働させました。これは現行版の隔離 VM 実通信であり、VM220・VM224 と VLAN 3131・3132 を使用した三 VM 試験ではありません。

## 構成

- WAN 側の外部要求元: `198.18.52.1/24`。
- ルーター WAN/LAN: `198.18.52.2/24`、`198.18.50.1/24`。
- LAN クライアント: `198.18.50.100/24`。
- `PortForward`: TCP `198.18.52.2:18080` を `198.18.50.10:8081` へ転送。LAN 側のヘアピン接続を有効化。
- `IngressService`: TCP `198.18.52.2:18081`、`backend-a` の `198.18.50.10:8080` と `backend-b` の `198.18.50.20:8080`、優先切替方式、TCP 健全性確認、LAN 側ヘアピン接続を設定。
- 防火壁: WAN は非信頼、LAN は信頼、管理経路は管理ゾーンとして設定。空設定の撤回後も `routerd_filter` の入力・転送は既定拒否。

## 観測結果

1. `routerd` は `PortForward` と `IngressService` の通常経路・ヘアピン経路を nftables へ生成しました。
2. 外部要求元と LAN クライアントの双方から `PortForward` へ接続できました。バックエンドが観測した送信元は、それぞれ `198.18.52.1` と `198.18.50.1` でした。
3. 同じ二経路から `IngressService` の `backend-a` へ接続できました。バックエンドが観測した送信元は、それぞれ外部要求元と LAN 側ルーターアドレスでした。
4. `backend-a` を停止すると、健全性確認が `backend-b` への切替を観測し、両経路の新規接続は `backend-b` へ到達しました。`backend-a` の復旧後は、優先切替方式に従い `backend-a` へ戻りました。
5. `routerctl ingress drain` で `backend-a` を排出すると `backend-b` が選択されました。`routerctl ingress undrain` の後は `backend-a` が再選択されました。
6. `routerd` の再起動後、`IngressService` は `backend-a` を再選択し、外部側からの新規接続は応答しました。
7. `PortForward` を含まない設定へ置換すると、既存 TCP 接続は継続しました。撤回後の新規 `PortForward` 接続は受け付けられませんでした。
8. 空の 1.5 設定へ置換すると、`routerd_nat` 表は消えました。`routerd_filter` は既定拒否のまま残り、新規 `IngressService` 接続は受け付けられませんでした。

実行スクリプトは `PASS: PortForward DNAT/hairpin/withdrawal and IngressService failover/drain/restart/withdrawal` を出力しました。

## 復元

VM219 は試験前スナップショット `rd15-current-ingress-pre` へ戻しました。基準 ISO、`/usr/local/sbin/routerd`、`/usr/local/sbin/routerctl`、`/usr/local/sbin/routerd-bgp` の SHA-256、`routerd` と `routerd-dnsmasq` の稼働を確認しました。確認した SHA-256 は順に `a1cbdd287e777d9eb090512ec7b7fb69f2340f0f12a71ed5567a8e2dcd2626f8`、`7de346416d215f842deaee95f615dad4c5bbb3c8d78f376d2a43ded55554b8f9`、`615e5b90d75c055ae43a12df0c4012b74ed5ab4a3f91bed8e295f93d4d858e99` です。その後、スナップショットと試験用 ISO を削除しました。

## 範囲外

- VM220・VM224 を使う VLAN 分離構成、物理回線、UDP、IPv6、複数ルーター間の状態同期は対象外です。
- この記録は静的ポート転送と二バックエンドの入口サービスに関する適用、切替、復旧、撤回を示します。Linux 全機能または製品全体の合格を示すものではありません。
