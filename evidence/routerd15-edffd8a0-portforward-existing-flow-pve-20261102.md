# routerd 1.5 Linux PortForward の PVE 既存接続・撤回・復旧検証（2026-11-02）

## 対象

- 実行バイナリ: `routerd v20261027.1620 (edffd8a0)`
- 配布物 SHA-256: `e44cf91f10ff3ff46755cd57aa8656d711985eca5b3ce5ed3b6433147e8aafdc`
- VM219 は候補、VM220 は VLAN 3132 の WAN 要求元、VM224 は VLAN 3131 の LAN 応答器。
- 元構成、撤回入力、routerd の正本を別ファイルにした。

## 観測

1. VM220 `198.18.53.2` から VM219 `198.18.53.1:18080` への TCP は、VM224 `198.18.51.10:8080` から `READY 198.18.53.2` を受信した。
2. PortForward を除く構成を適用すると、`routerd_nat` は空の `prerouting` と `postrouting` の NAT フックだけを保持した。
3. 接続追跡は `ESTABLISHED`、`ASSURED` を維持し、既存ソケットは `ECHO old-flow` を受信した。
4. 同じ撤回状態の新規 WAN TCP 接続は時間切れとなった。
5. 不変の元構成を再適用すると DNAT 規則が戻り、新規 WAN TCP 接続は再び `READY 198.18.53.2` を受信した。

## 復元

試験後に VM219/220 は起動、VM224 は停止の基準状態へ戻した。ISO、作業領域、スナップショットを削除した。

## 範囲外

UDP、IPv6 NAT、NAT44FlowDNATPinhole、NAT44SessionSync、LocalServiceRedirect、再起動後の既存接続、および FreeBSD は対象外である。
