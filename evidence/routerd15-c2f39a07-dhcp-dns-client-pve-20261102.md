# DHCPv4・DNS の独立クライアント PVE 試験

実施日: 2026-11-02
対象リビジョン: `c2f39a07fccd91c8eb2ce13193caec9aba6f26e2`

## 結果

VM220 を独立クライアントとして VLAN 3131 へ接続し、VM219 の routerd 1.5 から DHCPv4 リースを取得しました。同じクライアントから DNS の UDP/TCP 問い合わせが成功しました。

## 観測

- VM220 は `198.18.15.107/24` を DHCPv4 で取得
- DHCPv4 提供元とゲートウェイ: `198.18.15.1`
- `router.home.arpa` の A レコード: `198.18.15.1`
- UDP と TCP の DNS 問い合わせはともに `NOERROR`、回答数 1
- `networkctl renew` 後も DHCPv4 の提供元が `198.18.15.1` であることを確認

DHCP クライアントには VM220 の既存 `systemd-networkd` を使用しました。試験用 VLAN と実行時 `.network` 定義は撤去済みです。VM220 は 13 資源の `Healthy` 構成へ復帰しています。

## 未検証範囲

IPv4 NAT の上流到達性、IPv6 の委任・RA・転送、上流喪失と復帰、DHCP リース期限切れ、物理装置の相互接続はこの試験に含みません。
