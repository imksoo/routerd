# IPsec 提供元再起動復旧の PVE 実通信試験

実施日: 2026-11-02
対象リビジョン: `8b80a174892f470ab4b5f66cabaea605b5985984`
実行バイナリ SHA-256: `a1cbdd287e777d9eb090512ec7b7fb69f2340f0f12a71ed5567a8e2dcd2626f8`

## 結果

Linux の strongSwan 提供元を強制終了した後、VM219 上で稼働する routerd 1.5 が旧 ESP SA を撤去し、新しい提供元との IKEv2/ESP 通信を再確立しました。独立した PVE VM 間の ICMP は、復旧前後とも双方で 2/2 応答でした。

## 構成

- VM219: routerd 1.5 の隔離ルーター
- VM220: strongSwan を実行する独立対向ルーター
- 接続網: VLAN 3132
- 保護対象: `10.254.96.1/32` と `10.254.96.2/32`
- 提供元: Ubuntu の `strongswan.service`

VM219 は通常の 21 資源構成へ、IPsec 接続、保護アドレス、対向から自身への ICMP だけを許可する一時規則を追加しました。VM220 は外部管理の VLAN インターフェースと信頼領域を一時宣言しました。

## 実施内容と観測

1. 初回の IKEv2 と CHILD SA を確立しました。VM219 から VM220 への ICMP は 2/2 応答、往復時間は 0.215–0.250 ms でした。ESP の各方向は 2 パケットでした。
2. IPsec 受領記録に VICI ソケット世代 `25:1995` が保存されていることを確認しました。
3. VM219 の `strongswan.service` 主プロセスへ `SIGKILL` を送信しました。systemd は提供元を再起動し、`MainPID` は `4668` から `6544`、`NRestarts` は 1 から 2、VICI ソケット世代は `25:1995` から `25:2200` へ変化しました。
4. 同一の routerd 1.5 構成を再適用しました。旧 SPI 集合との共通要素がない新しい ESP SPI 2 件だけを VM219 で確認しました。
5. 新しい IKEv2/ESP 通信で VM219 から VM220 へ 2/2 応答、VM220 から VM219 へも 2/2 応答でした。後者の往復時間は 0.247–0.277 ms でした。各方向の新しい ESP SA は 4 パケットまで増加しました。
6. VM219 を 21 資源、VM220 を 13 資源の基準構成へ戻しました。`routerd.service` と `strongswan.service` の状態は、両方で `active` です。試験用 VLAN、保護アドレス、接続定義、VM219 の XFRM SA、追加 ISO は不在です。

## 実装と局所確認

`8b80a174` は、`/run/charon.vici` のデバイス番号と inode 番号を受領記録へ保存します。記録済みの値と異なる場合だけ、所有確認済み SA を撤去して接続を読み込み直します。ソケット世代が同じ場合は、提供元の継続を推測せず、SA を撤去しません。

- `go test ./pkg/ipsec -count=1`: 成功
- `go test ./cmd/routerd -run "^$" -count=1`: 成功
- `GOOS=freebsd GOARCH=amd64 go test -c ./pkg/ipsec`: 成功
- `GOOS=freebsd GOARCH=amd64 go test -c ./cmd/routerd`: 成功
- `CGO_ENABLED=0 go build ./cmd/routerd`: 成功

## 未検証範囲

ソケットの inode が変わらない提供元置換、自然な期限切れと再鍵交換、複数対向、証明書認証、外部事業者装置との相互接続、FreeBSD の実行はこの試験に含みません。
