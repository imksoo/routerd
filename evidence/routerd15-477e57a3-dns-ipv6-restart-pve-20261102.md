# routerd 1.5 Linux IPv6 DNS 再起動・撤回 PVE 実通信試験

実施日: 2026-11-02（JST）

## 結果

`477e57a346b3df60ad49d2fef5b833fd51a44b3c` の Linux パッケージで、独立 VLAN の
IPv6 DNS 通信、DNS 常駐プロセス再起動、空構成への撤回、再適用を確認しました。
空構成時に残る `inet routerd_filter` は、routerd 所有の既定遮断表です。今回の修正後は
孤立資源の警告になりませんでした。

## 対象と構成

- パッケージ SHA-256: `7655b14d6649b54353f9118b71dbc90071d95f728f5d116acdd04052c669a745`
- VM219: 空の状態 DB から開始した Linux 候補。VLAN 3131 の `2001:db8:15::1:53` で待受
- VM220: VLAN 3132 の独立 `dnsmasq` 上流 `2001:db8:16::2:53`
- VM224: VLAN 3131 の独立 Linux DNS クライアント `2001:db8:15::10`
- 新規構成: `evidence/pve07-dns-ipv6-restart.yaml`
- 撤回構成: `evidence/pve07-dns-ipv6-restart-withdraw.yaml`

## 観測

1. VM224 は UDP/TCP とも `router.home.test` のローカル AAAA 応答
   `2001:db8:15::1` を受けました。
2. VM224 は UDP/TCP とも `host.upstream.test` を VM219 経由で VM220 へ照会し、
   AAAA 応答 `2001:db8:16::99` を受けました。
3. `routerd-dns-resolver@lan.service` の再起動で常駐プロセスは PID `3623` から
   `3687` へ替わりました。再起動後も二つの照会は UDP/TCP とも成功しました。
4. 空構成の `routerctl apply --replace` は世代 2、`HEALTHY`、警告なしでした。
   DNS 常駐プロセスと IPv4/IPv6 の UDP/TCP 待受は消えました。
5. 保存した新規構成の明示的な再適用は世代 3、`HEALTHY` となりました。

## 復元

VM219、VM220、VM224 を試験前スナップショットへ戻しました。試験 ISO、CD-ROM 接続、一時配置、
スナップショットを削除しました。VM219 と VM220 は基準 `routerd` を稼働させ、VM224 は停止状態です。

## 範囲外

DNSSEC、DoT、DoH、名前空間封じ込め、キャッシュ失効、DHCP リース由来名、否定応答の全方針、
物理回線、FreeBSD、DNS 全体、Linux 全体、置換可能品質全体は未確認です。

