# routerd 1.5 DS-Lite、NAT、再起動、撤回の分離 PVE 実通信検証

## 結論

Linux 候補 `7fbafbbc` は、分離 VLAN の独立クライアントから独立 AFTR への DS-Lite IPv4 HTTP を転送しました。NAT44、TCP MSS 制御、候補 `routerd` の再起動、構成撤回、保存した新規構成の再適用を確認しました。

これは静的 IPv6 を使う限定構成の証跡です。委譲 IPv6 送信元の失効・再取得、AFTR 冗長化、FreeBSD の PF は未確認です。

## 実行対象

| 項目 | 値 |
| --- | --- |
| 実行リビジョン | `7fbafbbc3a2e3ee2ac89243091094865c0352721` |
| 候補アーカイブ SHA-256 | `0c204dcc1b8a5e99526b0eb8ee59320ae2b32b5792f64f9f78b67b850a7df9df` |
| 候補ルーター | PVE VM219 |
| 独立 AFTR と HTTP 端点 | PVE VM220、VLAN 3132 |
| 独立クライアント | PVE VM224、VLAN 3131 |
| 候補 LAN | `198.18.15.1/24` |
| クライアント | `198.18.15.10/24` |
| DS-Lite 外側 | VM219 `2001:db8:16::2`、VM220 `2001:db8:16::1` |
| DS-Lite 内側 | VM219 `192.0.0.2`、VM220 `192.0.0.1` |
| HTTP 宛先 | `192.0.2.1:18080` |

候補の状態領域は試験用に新設しました。既存の `rd15-wan` の未管理 IPv4 既定経路は、DS-Lite の同一経路キーと競合したため、試験用スナップショット内でのみ撤去しました。旧構成の読込みや変換は行っていません。

## 観測結果

1. `pve07-dslite-nat-through-traffic.yaml` の7資源を適用しました。`DSLiteTunnel/isolated-aftr`、`NAT44Rule/lan-to-dslite`、二つのゾーン、および防火壁方針は全て `Healthy` でした。
2. VM219 は `rd15-dslite` を `ipip6`、MTU 1454、外側のローカル `2001:db8:16::2`、AFTR `2001:db8:16::1` として作成しました。IPv4 既定経路は `default via 192.0.0.1 dev rd15-dslite` でした。
3. VM219 の NAT44 規則は `oifname "rd15-dslite" ip saddr 198.18.15.0/24 masquerade` でした。TCP MSS 制御は VLAN 3131 から `rd15-dslite` への IPv4 SYN を1414へ制限しました。
4. VM224 の HTTP 要求は本文 `routerd15 dslite nat through traffic` を受信しました。VM220 の HTTP 記録は送信元 `192.0.0.2` を示しました。VM219 の接続追跡記録は、元の `198.18.15.10` と変換後の `192.0.0.2` を同一フローとして示しました。
5. VM220 の独立 AFTR で捕捉した TCP SYN は `192.0.0.2` から `192.0.2.1:18080` 宛てで、MSS は1414でした。これは候補の NAT44 と TCP MSS 制御を通った実通信です。
6. `systemctl restart routerd` 後も、同じ DS-Lite、既定経路、候補サービスの `Healthy` 状態を確認しました。VM224 の新規 HTTP 要求も成功しました。
7. 空の新規構成を適用すると、`rd15-dslite`、DS-Lite 既定経路、DS-Lite 宛て NAT44 規則は消えました。VM224 の新規 HTTP は接続不能でした。直後の一回目は削除処理中の `routerd_mss` を一時的に孤立資源として警告しました。表の削除後に同じ空構成を再適用すると、警告なしで `Healthy` でした。
8. 保存した新規構成を明示的に再適用すると、世代4、7資源で `Healthy` となりました。`rd15-dslite`、NAT44、TCP MSS 制御、既定経路が戻り、VM224 の HTTP も成功しました。

## 復元

VM219、VM220、VM224 を試験前スナップショットへ戻しました。試験用 ISO、CD-ROM 接続、一時アーカイブ、スナップショットを削除しました。VM219 と VM220 は基準 `routerd` を稼働させ、VM224 は停止状態です。

## 範囲外

- DHCPv6-PD による DS-Lite 送信元、送信元失効、再取得、AFTR 名解決、AFTR 冗長化
- IPv6 クライアント転送、複数 DS-Lite 経路、負荷・性能、物理ネットワーク
- FreeBSD の PF、Linux 以外のプラットフォーム、routerd 1.0 全機能の完了判定
