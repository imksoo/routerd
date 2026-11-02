# routerd 1.5 Linux ファイアウォールの PVE VLAN 実通信検証（2026-11-02）

## 対象

- 実行バイナリ: `routerd v20261027.1620 (edffd8a0)`
- 配布物 SHA-256: `e44cf91f10ff3ff46755cd57aa8656d711985eca5b3ce5ed3b6433147e8aafdc`
- 構成: VM219 候補ルーター、VLAN 3131 の VM224 独立 LAN クライアント、VLAN 3132 の VM220 独立 WAN ピア。管理 VLAN 3133 は変更していない。
- 入力分離: `routerd-original.json`、遮断規則を加えた `routerd-deny-input.json`、routerd が管理する `routerd-canonical.json` を別ファイルにした。

## 観測

1. VM224 から VM220 の `198.18.16.1:8080` は `peer-ok` で成功した。
2. VM224 から作成した TCP 接続は ESTABLISHED のまま維持された。
3. `test-deny-wan-http` を適用すると、新規 LAN→WAN HTTP は時間切れとなり、規則カウンターは 2 パケット増加した。
4. VM220 から VM224 の `198.18.15.104:8081` への新規 HTTP も時間切れとなり、`wan_to_lan` の拒否カウンターは 2 パケット増加した。
5. 不変の `routerd-original.json` を適用すると `lan_to_wan` から遮断規則が消え、VM224 の新規 HTTP は再び `peer-ok` で成功した。

## 結論

この観測は、分離 VLAN でのゾーン許可、WAN 起点新規接続の拒否、確立済み接続の維持、選択規則の追加・撤回・復旧を示す。試験後は VM219/220 を起動、VM224 を停止した基準状態へ戻し、ISO、作業領域、スナップショットを削除した。

## 範囲外

IPv6 のファイアウォール選択子、ポート転送、レート制限、FreeBSD、および全製品機能の完了はこの証跡の対象外である。
