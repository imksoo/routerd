# DHCPv6-PD クライアントの実装と選び方

routerd は WAN 側の DHCPv6 Prefix Delegation を OS の DHCPv6 クライアントに任せます。
このページは、ラボの実機観測（NTT フレッツ光ネクスト + PR-400NE HGW配下）から得られた
クライアント実装ごとの挙動の差と、なぜ routerd の Linux 向け NTT 系プロファイルでは
`dhcpcd` を既定にし、FreeBSD では KAME/WIDE `dhcp6c` を使い続けるかをまとめます。

## 主要クライアントの比較

| 実装 | ライセンス | 上流の状況 | NTT NGN 配下での挙動 |
| --- | --- | --- | --- |
| KAME/WIDE `dhcp6c` (`wide-dhcpv6-client` / `net/dhcp6`) | BSD 系 | 上流の `wide-dhcpv6` は 2008-06-15 で停止。各ディストリビューションがパッチ適用で延命中。OPNsense fork (`opnsense/dhcp6c`) は BSD-3 で active | Renew/Rebind の IA Prefix lifetime を直前の Reply から保持し、再送信できる。NEC IX 系の商用ルータと同じ形のパケットを送れる。FreeBSD では既定、Linux では明示した場合だけ使う代替経路として残す |
| systemd-networkd | LGPL-2.1+ | active | Renew/Rebind の IA Prefix lifetime を 0/0 で送る場合がある（systemd issue #16356）。NTT HGW はこれを「prefix 不要」のシグナルとして扱い黙殺する |
| dhcpcd | BSD 2-clause | active | 単独で IPv4/RA/DHCPv6/PD を扱える。Linux では有力な経路として動作確認を進めており、T1 の長時間 Renew 観測は Phase 3 に残す。Linux の NTT 系プロファイルでは既定にする |
| odhcp6c | GPL-2.0 | OpenWrt プロジェクト配下で active。OpenWrt 以外の distro パッケージは少ない | OpenWrt 系で広く使われるが、NTT 10G ひかり（フレッツ クロス）で 8 時間ごとに切断する報告がある（OpenWrt issue #13454）。NTT HGW 向けは追加検証が必要 |

## routerd の選択表

routerd は複数のクライアント経路を残します。OS とプロファイルによって
自然な既定値が違うためです。`spec.client` は明示できます。空の場合は、
`routerd apply` が現在のホストに合わせて既定値を選びます。ラボ検証では
`--override-client` と `--override-profile` で、その 1 回の apply だけ
上書きできます。

| OS | クライアント | NTT プロファイルでの扱い | 備考 |
| --- | --- | --- | --- |
| FreeBSD | `dhcp6c` | 検証済みの既定 | FreeBSD の現行本線。KAME/WIDE `dhcp6c` を使う。routerd は不要なサービス変更を避け、クライアント状態を保つ。 |
| FreeBSD | `dhcpcd` | 既知の問題として警告 | DUID ファイル削除時に DUID-LLT を作り、DUID-LL を強制しても HGW から応答がなかった。取得メモの Section L を参照。 |
| Ubuntu/Linux | `dhcpcd` | `ntt-*` の既定 | Linux の NTT プロファイル本線。Ubuntu と NixOS で同じ経路を使い、systemd-networkd の DHCPv6-PD に依存しない。T1 の長時間 Renew 観測は Phase 3 に残す。 |
| Ubuntu/Linux | `dhcp6c` | 明示した場合だけ使う代替経路 | 移行や比較検証のために明示指定できる。systemd-networkd の Renew/Rebind 寿命 0/0 問題は避けられるが、既定値ではなく、新しい例では使わない。 |
| Ubuntu/Linux | `networkd` | `ntt-*` では既知の問題として警告 | 一般的な Linux PD では有用。ただし NTT HGW では IA Prefix 寿命 0/0 の Renew/Rebind を観測した。 |
| NixOS | `dhcpcd` | `ntt-*` の既定 | Linux の NTT プロファイルと同じ既定。nixpkgs で WIDE `dhcp6c` を素直に使う経路がないことも理由のひとつ。 |
| NixOS | `networkd` | `ntt-*` では既知の問題として警告 | 他の Linux と同じく networkd Renew/Rebind 寿命問題を避ける。 |

既知の問題がある組み合わせは検証エラーではありません。routerd は apply 警告と
`KnownNGCombination` イベントを出し、そのまま続行します。意図した切り分けを
妨げず、危険だけを見えるようにするためです。

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

- 一般 Linux の既定経路は `systemd-networkd`。ただし NTT 系プロファイル（`ntt-ngn-direct-hikari-denwa`、
  `ntt-hgw-lan-pd`）では `IPv6PrefixDelegation.spec.client: dhcpcd` を既定にする。
- Linux の `client: dhcp6c` は、移行や比較検証のために明示した場合だけ使う代替経路として残す。既知の問題としては扱わないが、NTT プロファイルの新しい例では `dhcpcd` を使う。
- FreeBSD は `dhcp6c`（ports `net/dhcp6`）が既定。base の `dhclient` は IPv4 のみで PD 不可。
- routerd の `apply` は OS DHCPv6 クライアントの内部 lease 状態を温存することを最優先にする。
  rc.conf や drop-in に変更がない限りサービスを restart しない。
- 観測ロジックは「LAN 側の派生アドレスが残っている」ことを「PD 有効」とは扱わない。
  最後の Reply 観測時刻、OS クライアントの lease ファイル、`routerctl describe ipv6pd/<name>` の状態
  などを根拠にする方針を進めている。

## 2026-04-30 ラボ追加観測（PR-400NE 連続稼働中）

PR-400NE が長時間連続稼働している状態でラボ実機検証を行い、以下のエビデンス段階の所見を得た。
本ページの既存の節を補完するもので、bootstrap 経路の詳細は別ページ
NTT NGN HGW 配下での連続稼働中 PD 取得
にまとめている。

- **observe**：HGW を最近再起動していない状態で、34 種類の Solicit variants
  （OUI sweep、Vendor-Class、User-Class、hop-limit と flow-label の組み合わせ、
  prefix hint の有無、IA_NA との同梱、RS→RA→Solicit の順序、再送インターバルの調整など）
  を試したが、Advertise/Reply は 1 つも返ってこなかった。
- **observe**：同じ瞬間、同じ LAN 上の NEC IX2215 は T1 周期で既存 lease の更新に
  11/11 全成功していた。HGW の DHCPv6 サーバ自体は健全で、新規 binding 経路だけが封じられている。
- **believe**：HGW は「acquisition window（HGW 再起動直後の数分、新規 Solicit が受理される）」と
  「steady state（既知の Server Identifier を伴う Renew/Request/Confirm/Information-Request だけが受理される）」を
  区別している。steady state での新規 Solicit は silent drop される。
- **measure**：routerd の lab VM から HGW の `Server Identifier` と `IA_PD` claim を載せた
  `Request`（msg-type 3）を合成して送ると、HGW から即座に新規 /60 binding を含む `Reply` が返った。
  RFC 8415 §18.2.10.1 の INIT-REBOOT 系 Request は、Solicit 経路が封じられていても受理される。
  詳細なトランスクリプトは別ページに置く。
- **assert**：routerd の WAN プロファイルは Solicit/Request/Renew/Rebind/Release/Confirm/Information-Request を
  すべて能動制御できる必要がある。OS 標準の DHCPv6 client は canonical な Solicit→Advertise→Request 経路に
  固執しており、この状態から HGW 再起動なしで自力復旧できない。これは
  `docs/design-notes.md` Section 5.2 の active controller 経路の主要な動機の一つである。

## Renew 受理条件の仮説（2026-04-30）

3 系統の Renew をバイトレベルで並べて比較した結果、以下の仮説を得た。
反証されるまで routerd はこの contract に従う。

- **observe**：unicast Renew（HGW のグローバルアドレス宛 UDP 547）は常に応答が返る。
  しかしその応答は概ね 4ms 以内に返る `status-code 5 (UseMulticast)` の Reply である。
  WIDE `dhcp6c` は同じ `xid` のまま multicast `ff02::1:2` に再送するが、HGW は silent drop する
  （xid 重複サプレッションと推測）。
- **measure**：成功した multicast Renew（IX2215, 11/11）は次の特徴を持つ：
  `T1=7200`、`T2=12600`、bound prefix の `pltime`/`vltime` が非ゼロの `IA_PD`、
  `reconfigure-accept` option (20) あり、新規 `xid`。
- **measure**：失敗した multicast Renew（WIDE `dhcp6c` の UseMulticast 後）は
  T1/T2 と IA_PD lifetime は IX2215 と一致するが、`reconfigure-accept` option がなく
  `xid` が unicast 試行と同じ。
- **measure**：失敗した multicast Renew（routerd active controller の同セッション）は
  `T1=0`、`T2=0`、`reconfigure-accept` なし、`xid` は新規。
- **believe**：HGW の multicast Renew 受理条件は概ね
  「新規 xid AND 非ゼロ T1/T2 AND `reconfigure-accept` option あり」と推定する。
  どれか一つでも欠けると silent drop。要素別の ablation で再検証するまで、routerd は 3 つすべて満たして送る。

## DHCPv6 クライアント実装の落とし穴（2026-04-30）

- **WIDE `dhcp6c`** はサーバ link-local をキャッシュすると Renew を unicast で先に試す。
  結果、上記 UseMulticast bounce 経路に落ちる。routerd のプロファイルでは `dhcp6c` を可能な限り
  multicast Renew に寄せ、unicast Renew は deprecated 経路として扱う。
- **WIDE `dhcp6c`** は送信する Solicit/Renew に `reconfigure-accept` option を載せない。
  受信処理は実装されているが送信処理が無い。送信パッチを当てるか routerd の active controller を使う必要がある。
- **WIDE `dhcp6c`** の DUID は FreeBSD では `/var/db/dhcp6c_duid`、Ubuntu では `/var/lib/dhcp6/dhcp6c_duid` に保存される。
  `DUID-LL`（type 0x0003）を強制する場合、`hardware-type` は必ず `0x0001`（Ethernet）にする。
  それ以外は NTT NGN 上で HGW が要求する形に違反する。
- **systemd-networkd** は Renew/Rebind の IA Prefix を `pltime=0 vltime=0` で送る場合がある（systemd issue #16356）。
  HGW はこれを release 相当のシグナルとして silently drop し、binding が壊れたまま client は気づかない。
  routerd の Linux 向け NTT 系プロファイルは networkd を避けて `dhcpcd` 経路に切り替える。
- **Ubuntu 上の dhcpcd** は、routerd が管理するリソース単位の service と hook で動かせる。
  2026-04-30 の通常稼働中試験では、DUID-LL と IA_PD を含む Solicit を繰り返し送ったが、
  HGW から Advertise/Reply は返らなかった。これは dhcpcd 全般を否定するものではないが、
  その後のラボ作業で Linux の NTT 系既定にした。T1 Renew の長時間観測は Phase 3 の検証として残す。

## Server Identifier の RA からの導出

- **observe**：HGW の DHCPv6 `Server Identifier` は `DUID-LL` で、構造は
  `0003 0001`（DUID type 3、hardware type Ethernet）に HGW LAN 側 MAC を続けたもの
  （lab 値: `1c:b1:7f:73:76:d8`）。
- **observe**：同じ MAC は HGW が出す Router Advertisement の source link-local アドレスに
  modified EUI-64 で埋め込まれている。
- **assert**：routerd は RA を 1 通受信し、source link-local から modified-EUI-64 逆変換で MAC を復元、
  先頭に `0003 0001` を付けて期待される `Server Identifier` を導出する。リソース仕様で override 可能。
- **assert**：これにより routerd は過去の DHCPv6 トランザクション履歴がないコールドスタートからでも
  「RA 受信 → Server-ID 計算 → Request 送信 → Reply 受信」で復旧できる。Solicit を経ずに Request だけで
  HGW が応答する事実（NTT NGN HGW 配下での連続稼働中 PD 取得の検証記録を参照）と組み合わせた経路。

## Reconfigure key の扱い

- **observe**：新規 PD 取得の最初の Reply には `Authentication` option が含まれることがあり、
  `proto=reconfigure`、`algorithm=HMAC-MD5`、`RDM=mono`、16 バイトの `reconfig-key value` を持つ。
  これは将来 HGW が Reconfigure（RFC 8415 §18.2.10）を送るときの認証鍵。
- **observe**：同じ binding の Renew Reply には Authentication option が含まれないことがある。
- **assert**：routerd は最初の Reply で得た Reconfigure key を保持し、
  別の Reply で明示的にローテーションされるまでこれを authoritative として扱う。
  鍵は `objects.status._variables` の lease 状態と並びで保存し、公開リソースビューには露出しない。

## 参考リンク

- systemd issue #16356 — DHCPv6 Renew が valid/preferred lifetime をリセットしない問題
- OpenWrt issue #13454 — NTT 10G ひかりで odhcp6c が 8 時間ごとに切断する症例
- OpenWrt forum: Server Unicast option causes ISP to ignore Renew packages — 関連の Server Unicast 黙殺問題
- OPNsense fork `opnsense/dhcp6c` — BSD-3 ライセンスで active な KAME 系メンテナンス例
- NEC UNIVERGE IX フレッツ IPv6 IPoE 設定例 — 商用ルータの NTT NGN 接続実例
- RFC 8415 §18.2.1 (Solicit), §18.2.4 (Request), §18.2.5 (Confirm), §18.2.6 (Renew),
  §18.2.10 (Reconfigure), §18.2.10.1 (INIT-REBOOT considerations)
- rixwwd 公開ノート — PR-400NE の Reply 送信元 UDP ポートが ephemeral（観測例: 49153）の指摘
- sorah 公開ノート — PR-400NE の DUID-LL 構造の観測
