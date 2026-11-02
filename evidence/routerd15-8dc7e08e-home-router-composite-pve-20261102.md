# Linux ホームルーター統合実通信試験

- 実行リビジョン: `8dc7e08ec3f286ace9cc1e30cf408971e0506735`
- Linux 配布物 SHA-256: `d945433abfbb86485e3c0e7a42672d3858ecd8b8458ba24ee1b976de0f531023`
- 対象: VM219 の routerd 1.5 候補、VM220 の独立 Kea DHCPv6・DNS・HTTP ピア、VM224 の独立クライアント
- 分離経路: VLAN 3131 が LAN、VLAN 3132 が WAN、VLAN 3133 が管理
- 構成: `examples/routerd15-pve-home-router-composite.json`

## 観測

1. VM224 は VLAN 3131 で DHCPv4 アドレス `198.18.15.104/24` を取得し、既定経路を候補の `198.18.15.1` としました。
2. VM224 の `router.home.test A` と `AAAA` は候補のローカルゾーンから応答しました。`v4.test A` と `info.test AAAA` は、それぞれ独立した IPv4 と IPv6 の WAN DNS ピアへ転送されました。
3. VM224 から VM220 の IPv4 HTTP 応答器へ到達した送信元は `198.18.16.2` でした。NAT44 の変換後アドレスを独立ピアが返しました。
4. VM224 は候補のルーター広告から `2001:db8:1500::/64` の SLAAC アドレス、RDNSS、既定経路を取得しました。IPv6 HTTP 応答器が返した送信元は、この委譲済みアドレスでした。
5. Kea の DNS オプションを空にして候補の DHCPv6 クライアントを再取得させると、`DHCPv6Information/wan-info` の DNS は空になりました。DNS リゾルバー実行時設定から `info.test` の IPv6 上流だけが消え、VM224 の同じ問い合わせは `NXDOMAIN` になりました。
6. この撤回中も、`v4.test`、NAT44 の IPv4 HTTP、IPv6 HTTP は継続しました。3 台すべての QGA 応答も継続しました。
7. Kea の DNS オプションを復元すると、DNS リゾルバー設定は `udp://[2001:db8:17::53]:53` を再び持ち、VM224 の `info.test AAAA` は `2001:db8:17::53` に復旧しました。

## 試験上の条件と範囲

- WAN の固定 IPv6 到達性は、隔離ピアへ戻る経路を持つ試験用の `2001:db8:17::/64` で与えました。旧構成の設定移行は使用していません。
- ピアとクライアントに残っていた常駐 `routerd` は停止しました。独立ピアの旧入力規則には VLAN 3132 だけを受け入れる一時規則を加えました。
- すべての変更は `rd15-composite-8dc7e08e-pre` スナップショットへ復元後、試験用 ISO とスナップショットを削除しました。復元後は VM219 と VM220 が起動、VM224 が停止、VM219 の追加 ISO は未接続です。
- この証跡は Linux のホームルーター統合一構成に限ります。PPPoE、経路制御の全条件、冗長化、全セキュリティ規則、FreeBSD、および routerd 1.0 全機能の置換可否は未確認です。
