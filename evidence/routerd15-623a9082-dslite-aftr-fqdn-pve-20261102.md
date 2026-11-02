# DS-Lite AFTR FQDN 応答変更の PVE 実通信

## 実施条件

| 項目 | 値 |
| --- | --- |
| 実行リビジョン | `623a9082a08dd5e48a1a1e25e8c60e2ba637bc25` |
| Linux 配布物 SHA-256 | `c77354adef38df5aa1eeef2ac1c9339c79e409e4f19dab413b8ce187d634e4f0` |
| 構成 SHA-256 | `8f29f207efee041dd290b2d65cef44ab171fed4bfde717cf7261c892019781d4` |
| 候補ルーター | PVE VM219。新規状態領域 |
| 独立 WAN ピア | PVE VM220。Kea DHCPv6-PD、dnsmasq、AFTR、HTTP端点。VLAN 3132 |
| 独立クライアント | PVE VM224。VLAN 3131。`198.18.15.10` |

候補の `DSLiteTunnel/isolated-aftr` は `aftr.test` を参照し、候補自身の
`DNSResolver/aftr-local` を `127.0.0.1:53` で使用します。Kea は
`2001:db8:1500::/60` を委任します。候補はそこから
`2001:db8:1500::100` を DS-Lite の外側送信元として導出します。

## 観測結果

1. Kea の実際の DHCPv6 Solicit、Advertise、Request、Reply を VM220 で捕捉しました。候補は委任プレフィックスを `Bound` とし、`2001:db8:1500::1/64` と `::100/128` を LAN 側へ設定しました。
2. VM220 の独立 DNS が `aftr.test AAAA = 2001:db8:16::1` を返すと、候補は `rd15-dslite` を同じ外側宛先で生成しました。VM224 の HTTP は独立 HTTP 端点から本文 `routerd15 AFTR FQDN answer recovery` を受信しました。
3. 2026-11-02T11:47:38Z に、独立 DNS の AAAA 応答を `2001:db8:16::2` だけへ変更し、旧 `::1` AFTR を撤去しました。直後の VM224 新規 HTTP は接続不能でした。
4. `routerd` を再起動せず、2026-11-02T11:47:50Z に候補は `rd15-dslite` を外側宛先 `2001:db8:16::2` として再生成しました。切替開始から観測された復旧までは約12秒です。この値は試験での観測値であり、目標値ではありません。
5. 再生成後、VM224 の同じ HTTP は成功し、VM220 の新 AFTR インターフェース `rd15-aftr-b` の受信カウンターも増加しました。

## 範囲と復元

この証跡は、候補ローカル DNS を経由する AFTR FQDN の AAAA 応答変更、旧端点の撤去、DS-Lite の再生成、独立クライアントの新規 HTTP 復旧を対象とします。複数 DS-Lite の同時経路選択、正常性監視による能動待機、IPv6 クライアント通信、性能、物理回線、FreeBSD PF は未確認です。

試験終了後は VM219、VM220、VM224 を試験前スナップショットへ戻し、試験用 ISO、CD-ROM 接続、一時資産、スナップショットを削除します。
