# DNS の PVE 分離 VLAN 実通信記録

対象リビジョンは `5d71ee8c05ca9b66cd2dbde216b9575daefa2e96` です。Linux の PVE 分離 VLAN で、ローカル DNS、独立上流への転送、上流消失時の応答、上流復帰、`routerd` 再起動後の解決を確認しました。

## 構成

- VM219: `routerd`。VLAN 3131 は LAN、VLAN 3132 は WAN。
- VM220: VLAN 3132 の `198.18.16.1:53` で待ち受ける独立 `dnsmasq`。`*.upstream.test` を `198.18.92.2` へ応答する。
- VM224: VLAN 3131 の独立クライアント。`198.18.15.100/24` を使用する。
- VM219 の `DNSResolver/lan`: `198.18.15.1:53`。`DNSZone/local` は `router.home.arpa`、`DNSForwarder/upstream` は `DNSUpstream/peer` を参照する。

## 観測結果

1. VM224 から `router.home.arpa` を問い合わせると、UDP で `198.18.15.1` を取得しました。
2. VM224 から `upstream.test` を問い合わせると、UDP と TCP の両方で VM220 の独立上流から `198.18.92.2` を取得しました。
3. VM220 の上流 `dnsmasq` を停止した後、新しい上流名 `withdraw.upstream.test` は `SERVFAIL` となりました。同時に `router.home.arpa` は引き続き `198.18.15.1` を返しました。VM219 の DNS 状態は上流 `Down` を示しました。
4. 上流 `dnsmasq` を再起動すると、新しい上流名 `recovered.upstream.test` は UDP と TCP の両方で `198.18.92.2` を返しました。
5. `systemctl restart routerd` 後も、VM224 からローカル名は `198.18.15.1`、新しい上流名は UDP/TCP とも `198.18.92.2` を返しました。実行中の `routerd version` は `5d71ee8c` でした。

## 範囲外

- 全てのクエリ方針、私設名前空間の封じ込め、DNS-over-TLS/HTTPS などの転送方式は未確認です。
- DHCP リース由来レコードの更新・削除、否定応答、設定資源の撤回は未確認です。
- この記録は上記リビジョンと PVE 構成に限定されます。DNS 機能全体または製品全体の合格を示すものではありません。
