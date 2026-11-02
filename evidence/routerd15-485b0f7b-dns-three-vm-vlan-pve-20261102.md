# DNS の PVE 分離 VLAN 実通信記録

対象リビジョンは `485b0f7b3afc3d7926a771325c42a2c2ab970ea2` です。VM219 の `routerd` は SHA-256 `22824983fe92f43e489e111ae44661a48ba6755de04597b7a4d9e8cce4177ade` でした。

## 構成

- VM219: 候補 `routerd`。VLAN 3131 を LAN、VLAN 3132 を WAN として使用。
- VM220: VLAN 3132 の `198.18.16.1:53` で待ち受ける独立 `dnsmasq`。`*.upstream.test` を `198.18.92.2` に応答。
- VM224: VLAN 3131 の独立クライアント。`198.18.15.100/24` を使用。
- VM219: `DNSResolver/lan` は `198.18.15.1:53`、`DNSZone/local` は `home.test`、`DNSUpstream/peer` は `Interface/wan` を参照。

## 観測結果

1. VM224 から `router.home.test` を UDP で問い合わせ、`198.18.15.1` を取得しました。
2. VM224 から `fresh.upstream.test` を問い合わせ、UDP と TCP の両方で `198.18.92.2` を取得しました。
3. VM220 の上流 `dnsmasq` を停止した後、未キャッシュの `withdraw.upstream.test` は `SERVFAIL` でした。
4. 上流を復旧した後、`recovery.upstream.test` は `198.18.92.2` を取得しました。
5. VM219 の `routerd` を再起動した後、`restart.upstream.test` は `198.18.92.2` を取得しました。
6. 空構成を適用すると `routerd-dns-resolver@lan.service` は `not-found/inactive` となり、`198.18.15.1:53` の UDP/TCP リスナーが消えました。VM224 の `removed.upstream.test` はタイムアウトしました。
7. DNS 構成を再適用すると、`restore.upstream.test` と `final.upstream.test` は `198.18.92.2` を取得しました。最終 `routerctl get status` は `Healthy` でした。

## 範囲と留意点

- 空構成適用時に `1 orphaned managed artifacts found` の警告がありました。観測対象は残留する `inet routerd_filter` で、入力・転送の既定方針は `drop` でした。DNS リスナーは残留していません。この安全側の残留を警告対象とする妥当性は、DNS とは別に確認が必要です。
- 全ての問い合わせ方針、私設名前空間の封じ込め、DoT/DoH、DHCP リース由来レコードの更新・削除、否定応答は未確認です。
- この記録は上記リビジョン、PVE、VLAN 3131/3132、および三台の VM 構成に限定されます。DNS 全機能または製品全体の合格を示すものではありません。
