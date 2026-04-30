# DHCPv6-PD クライアントの実装と選び方

routerd は WAN 側の DHCPv6 Prefix Delegation を OS の DHCPv6 クライアントに任せます。
このページは、ラボの実機観測（NTT フレッツ光ネクスト + PR-400NE HGW配下）から得られた
クライアント実装ごとの挙動の差と、なぜ routerd の NTT 系プロファイルでは KAME/WIDE `dhcp6c` を
推奨するかをまとめます。

## 主要クライアントの比較

| 実装 | ライセンス | 上流の状況 | NTT NGN 配下での挙動 |
| --- | --- | --- | --- |
| KAME/WIDE `dhcp6c` (`wide-dhcpv6-client` / `net/dhcp6`) | BSD 系 | 上流の `wide-dhcpv6` は 2008-06-15 で停止。各ディストリビューションがパッチ適用で延命中。OPNsense fork (`opnsense/dhcp6c`) は BSD-3 で active | Renew/Rebind の IA Prefix lifetime を直前の Reply から保持し、再送信できる。NEC IX 系の商用ルータと同じ形のパケットを送れる |
| systemd-networkd | LGPL-2.1+ | active | Renew/Rebind の IA Prefix lifetime を 0/0 で送る場合がある（systemd issue #16356）。NTT HGW はこれを「prefix 不要」のシグナルとして扱い黙殺する |
| dhcpcd | BSD 2-clause | active | 単独で IPv4/RA/DHCPv6/PD を扱える。ただし NTT HGW 配下の検証では、Vendor-Class option や ORO の opt_82/opt_83 を含むため、最小形の Solicit ではない |
| odhcp6c | GPL-2.0 | OpenWrt プロジェクト配下で active。OpenWrt 以外の distro パッケージは少ない | OpenWrt 系で広く使われるが、NTT 10G ひかり（フレッツ クロス）で 8 時間ごとに切断する報告がある（OpenWrt issue #13454）。NTT HGW 向けは追加検証が必要 |

## NTT HGW（PR-400NE 系）の挙動の要点

ラボでの観測と公開資料から、以下が確からしいモデルです。

1. **再起動直後の取得 window**：HGW を再起動すると、しばらく LAN 側の DHCPv6 サーバーが
   新規 Solicit に応答しない時間がある。準備が整うと、最小形の Solicit でも Advertise/Reply を返す。
2. **通常稼働中は Renew/Request だけ受け付ける**：Server ID と既存の IA_PD Prefix を付けて
   投げる Renew には、HGW は時刻に依らず応答する。これは NEC IX 系の商用ルータが
   T1 の周期で繰り返し成功している事実から確認した。
3. **`pltime=0 vltime=0` の IA Prefix を含む Renew/Rebind は黙殺される**：
   これは「クライアントが prefix を不要としている」シグナルとして扱われていると見られる。
   systemd-networkd のラボ観測では、Renew 時に IA Prefix の preferred/valid lifetime を
   `0` で送り、HGW から無応答だった。
4. **応答の送信元 UDP ポート**：HGW からの Advertise/Reply は `546` 宛に届くが、
   送信元はエフェメラルポート（観測例: `49153`）になる。`udp port 547` だけのキャプチャでは
   応答を取り逃がす。

## 失敗のパターンと判別

| 症状 | 観測点 | 推定原因 |
| --- | --- | --- |
| HGW 再起動から数分は Solicit 無応答 | 取得 window 開放前 | HGW 側準備中。最大数分待って再評価する |
| 通常稼働中の新規 Solicit に応答なし | Server ID なしの Solicit のみ送っている | クライアントが内部 lease 状態を失っている。HGW 再起動を待つか、acquisition window を再開させる |
| 通常稼働中の Renew に応答なし | Server ID 付き Renew を送っているが Reply なし | IA Prefix の `pltime`/`vltime` が `0` の可能性。tcpdump の `IA_PD-prefix ... pltime:0 vltime:0` を確認する |
| HGW 払い出し表に MAC が出ているが LAN 側に prefix なし | クライアントの lease 取り込みが落ちている | 例: ログに `Could not set DHCP-PD address: Invalid argument`。systemd-networkd と netplan の組み合わせで観測した |

## routerd での扱い

- 既定の Linux 経路は `systemd-networkd`。ただし NTT 系プロファイル（`ntt-ngn-direct-hikari-denwa`、
  `ntt-hgw-lan-pd`）では `IPv6PrefixDelegation.spec.client: dhcp6c` を選んで KAME/WIDE `dhcp6c` を使うのが推奨。
- FreeBSD は `dhcp6c`（ports `net/dhcp6`）が既定。base の `dhclient` は IPv4 のみで PD 不可。
- routerd の `apply` は OS DHCPv6 クライアントの内部 lease 状態を温存することを最優先にする。
  rc.conf や drop-in に変更がない限りサービスを restart しない。
- 観測ロジックは「LAN 側の派生アドレスが残っている」ことを「PD 有効」とは扱わない。
  最後の Reply 観測時刻、OS クライアントの lease ファイル、`routerctl describe ipv6pd/<name>` の状態
  などを根拠にする方針を進めている。

## 参考リンク

- systemd issue #16356 — DHCPv6 Renew が valid/preferred lifetime をリセットしない問題
- OpenWrt issue #13454 — NTT 10G ひかりで odhcp6c が 8 時間ごとに切断する症例
- OpenWrt forum: Server Unicast option causes ISP to ignore Renew packages — 関連の Server Unicast 黙殺問題
- OPNsense fork `opnsense/dhcp6c` — BSD-3 ライセンスで active な KAME 系メンテナンス例
- NEC UNIVERGE IX フレッツ IPv6 IPoE 設定例 — 商用ルータの NTT NGN 接続実例
