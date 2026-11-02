# routerd 1.5 proposal and implementation plan

Status: active on `work/routerd15-integrate-20261031`, derived from
`proposal/routerd15-kiss-runtime`; not a released product.
Baseline: routerd `e3b49dbb69c7de97bab9595884745eb7cbe93d92`.

## Product decision

Improve routerd in place on a separate branch. Preserve its useful networking
behavior while removing duplicate state, conversions, unused execution paths,
and unnecessary abstractions. Configuration and event/state database compatibility
are explicitly not required. Cutover uses rewritten config and a fresh chosen DB;
there is no old-format migration project. Existing features are not implicitly
removed. A new package/process/framework is justified only by the feature's needs.

## Completion and first live-use milestone

The final goal is preservation of **all existing routerd 1.0 functions**, with
routerd 1.5 ready for actual use. Compare against the pre-1.5 implementation at
the baseline above, including resource kinds, caller behavior, commands,
platform support, examples, tests and operational procedures. A renamed or
replaced implementation must demonstrate the same useful function. Configuration
and database format changes remain allowed; functional removal is not.

The first live-use configuration is a home/site main router with DHCP/DNS,
IPv4 NAT and IPv6. Its admission checks are:

- Clean package installation and startup through the actual service manager,
  with management access independent of the tested WAN/LAN.
- Independent client DHCP acquisition/renewal and DNS UDP/TCP answers, real
  IPv4 forwarding/NAT, IPv6 delegation/RA and client forwarding; test the
  upstream mode selected for the actual deployment.
- Upstream loss/return, lease renewal/expiry and prefix changes, process restart,
  host reboot and interrupted configuration changes without manual repair of
  the product's state. Check withdrawal and absence of traffic leaks as well as
  successful traffic.
- Prepare the target configuration in the 1.5 format before installation (manual
  or AI-assisted conversion is an operator preparation step), validate it with
  1.5, and install against fresh 1.5 state. Do not require an in-place 1.0
  upgrade, old-config reader, old-database import, or cross-version cutback test.
  Keep an independently restorable copy of the previous router setup for the
  trial's operational recovery plan; that copy is not input to routerd 1.5.
- Configuration rejection, replacement and withdrawal on 1.5. Run focused local
  checks for the changed behavior. Newly discovered failures stay open until
  reproduced, repaired and rechecked; a limited trial cannot reclassify other
  features as unnecessary.

Passing these checks permits a proposal for a bounded real-use trial, not a
claim that the whole revision is complete. Continue the full 1.0 feature audit,
implementation and qualification after that milestone. Production cutover,
release and main merge remain separate authorization boundaries. Work through
routine repairs and authorized isolated-VM checks without requesting a new task
for each failure. Preserve small checkpoints remotely, and keep reproducible
procedures and verdicts rather than permanent raw-log bundles.

## Current execution priority, 2026-10-31

Complete Linux implementation and qualification first, as requested by the
operator. Preserve published FreeBSD work and its open findings, but defer
FreeBSD-only implementation and native execution. Fix shared defects when they
affect Linux, including cancellation, storage and process ownership. A Linux
acceptance result does not establish whole-platform completion; retain the
remaining FreeBSD functions in the final 1.0 preservation scope.

## Current Linux execution checkpoint, 2026-11-02

The active branch records current PVE VLAN results rather than treating earlier
branch history as current qualification. DHCPv4 acquisition plus DNS UDP/TCP
answers, IPv4 NAT with a separate cloned client, RA/SLAAC, and DHCPv6-PD with
delegated-prefix forwarding, expiry and recovery are recorded at their executed
revisions. The IPsec provider-restart repair is also recorded at its executed
revision.

Linux 宅内ルーター統合の PVE 試験は `2a43a0e8` に記録しています。VM219 を候補ルーター、
VLAN 3131 の VM224 を独立クライアント、VLAN 3132 の VM220 を上流 HTTP 端点として、
DHCPv4、ローカル DNS、NAT44、IPv6 ルーター広告（Router Advertisement）を同時に構成しました。VM224 は
`198.18.15.104/24`、既定ゲートウェイ、DNS を取得し、`router.pve.test` を候補のローカル
ゾーンから解決しました。VM220 のパケット捕捉では HTTP の送信元が VM219 WAN 側の
`198.18.16.2` となり、NAT44 を確認しました。

VM224 はルーター広告を捕捉し、`fd31:15::/64` のステートレス自動設定（SLAAC）アドレスとリンクローカル
既定経路を設定しました。DHCPv4、DNS、NAT44、ルーター広告を外すと、待受は消え、
DNS は拒否、HTTP はタイムアウト、上流の TCP 接続開始パケットは変換前アドレス、8 秒間のルーター広告の捕捉は 0 件でした。
設定を戻すと DHCP 更新、DNS、NAT44、ルーター広告が復旧しました。IPv6 プレフィックス
委任、上流 IPv6 転送、DNS の外部転送、NAT のポート転送とピンホールは未試験です。

Linux IPv6 プレフィックス委任の PVE 試験は `905e0537` に記録しています。VM219 の候補を
VLAN 3132 の VM220 に置いた独立 Kea DHCPv6 サーバーへ接続し、VLAN 3131 の VM224 を
カーネルのルーター広告・ステートレス自動設定クライアントとして接続しました。Kea は
`2001:db8:1500::/60` を委任し、VM219 は `2001:db8:1500::1/64` を構成して広告しました。
VM224 はステートレス自動設定アドレスと既定経路を得て、VM220 の `2001:db8:17::1:8080` への
HTTP 応答には VM224 の送信元アドレスが記録されました。これは VM219 を経由した IPv6 転送の確認です。

この試験で、短い更新時刻に固定の 30 秒前倒しを適用すると更新が直後に反復する不具合を検出しました。
`905e0537` は前倒し時間を各タイマーの半分までに制限します。焦点を絞った Go 試験を通過し、
PVE の Kea 記録でも更新間隔は約 3 秒から約 12 秒へ変化しました。Kea を停止すると、委任プレフィックスは
非推奨となった後に失効しました。VM219 は委任アドレスなしの要求中状態となり、VM224 のアドレス、既定経路、
HTTP 到達性は消失しました。Kea の再開後は、プレフィックス委任、ルーター広告、ステートレス自動設定、
HTTP 転送が自動復旧しました。最後に空の 1.5 設定を `routerctl apply --replace` で適用すると、
VM219 は Healthy・資源数 0 となり、委任アドレスと VM224 の HTTP 到達性は消失しました。
DNS 配布、プレフィックス再採番、`routerd` の再起動・ホスト再起動、WAN 側のルーター広告、
ファイアウォール隔離、FreeBSD はこの試験の対象外です。

現行リビジョン `2bb2a6e3` では、同じ DHCPv6-PD の更新・撤回・復旧を PVE VM219 の
隔離ネットワーク名前空間で再実行しました。独立した Kea、SLAAC クライアント、DNS、HTTP 上流に対して、
初回委任、`2001:db8:1500::/60` から `2001:db8:1501::/60` への再採番、`routerd` 再起動、
上流停止による失効・既定経路と通信の撤回、上流復旧、空設定への撤回を全て確認しました。
これは VM220・VM224 と VLAN 3131・3132 を使う三 VM 構成の再実行ではなく、現行バイナリを
対象にした隔離 VM 内の実通信です。対象、観測、復元、範囲外は
[`routerd15-2bb2a6e3-dhcpv6-pd-isolated-pve-20261102.md`](../../evidence/routerd15-2bb2a6e3-dhcpv6-pd-isolated-pve-20261102.md)
に記録しています。

現行リビジョン `b5ecacd5` では、NAT44 の動的 DNAT ピンホールを隔離 PVE VM219 で確認しました。
別ネットワーク名前空間の LAN クライアントと WAN 側ピアの間で、未要求の逆方向 UDP を遮断し、
許可済みの LAN→WAN UDP 通信に対応する逆方向 UDP だけを動的 DNAT しました。期限満了、
送信元集合の変更、空設定への撤回も確認しました。これは VM220・VM224 と VLAN 3131・3132 を
使う三 VM 構成ではありません。対象、観測、復元、範囲外は
[`routerd15-b5ecacd5-nat44-flow-dnat-isolated-pve-20261102.md`](../../evidence/routerd15-b5ecacd5-nat44-flow-dnat-isolated-pve-20261102.md)
に記録しています。

現行リビジョン `4aa5339e` では、静的 `PortForward` と二バックエンドの `IngressService` を隔離 PVE VM219 で確認しました。外部要求元と LAN クライアントの双方から通常・ヘアピン接続を確認し、バックエンド停止時の切替、復旧、明示的な排出と解除、`routerd` 再起動後の復旧を確認しました。`PortForward` の撤回後は既存 TCP 接続が継続し、新規接続は拒否されました。空設定への撤回後は NAT 表が消え、routerd 所有の防火壁は既定拒否を維持し、新規入口接続を拒否しました。これは VM220・VM224 と VLAN 3131・3132 を使う三 VM 構成ではありません。対象、観測、復元、範囲外は
[`routerd15-4aa5339e-ingress-service-isolated-pve-20261102.md`](../../evidence/routerd15-4aa5339e-ingress-service-isolated-pve-20261102.md)
に記録しています。

現行リビジョン `ed074267` では、IPv6 静的経路を VM219・VM220・VM224 と VLAN 3131・3132 を使う三 VM 構成で再確認しました。VM224 から VM220 の IPv6 ループバックへ、VM219 が導入した指定ゲートウェイ・装置・メトリックの経路を通じて ICMP 3/3 を確認しました。空設定への置換で経路と通信は撤回され、再適用と `routerd` 再起動の後に復旧しました。さらに WAN 側 VLAN インターフェースを削除すると通信は停止し、同名・同一 VLAN の復元後は設定置換や `routerd` 再起動なしに 6 秒で経路と通信が復旧しました。対象、観測、復元、範囲外は
[`routerd15-ed074267-ipv6-static-route-vlan-pve-20261102.md`](../../evidence/routerd15-ed074267-ipv6-static-route-vlan-pve-20261102.md)
に記録しています。

現行リビジョン `81444257` では、DHCPv4 で得たゲートウェイを使う IPv4 方針経路を
VM219、VM220、VM224 と VLAN 3131・3132 の三 VM 構成で確認しました。VM220 の独立 DHCPv4
サーバは候補へ `198.18.16.100/24` とゲートウェイ `198.18.16.1` を払い出しました。候補は
経路表 123 とマーク `0x122` の方針経路を導入し、VM224 の HTTP は NAT44 後の送信元
`198.18.16.100` として VM220 へ到達しました。独立 DHCPv4 サーバだけを停止すると、リース失効後に
WAN アドレス、既定経路、経路表 123 が撤回され、HTTP は失敗しました。サーバだけの再開は候補機の
再起動なしにリース、経路、HTTP を復旧しました。対象、観測、復元、範囲外は
[`routerd15-81444257-dhcpv4-policy-pve-20261102.md`](../../evidence/routerd15-81444257-dhcpv4-policy-pve-20261102.md)
に記録しています。異なるゲートウェイへの切替、VRRP、物理 WAN 障害は未試験です。

Linux DHCPv4 サーバーと予約の PVE 試験は `2c3fe143` に記録しています。VM219 を候補ルーター、
VM224 を予約済み MAC アドレスのクライアント、VM220 を別 MAC アドレスの動的クライアントとして
VLAN 3131 に接続しました。VM224 は予約アドレス `198.18.15.50`、VM220 は同じアドレスを要求しても
動的プールの `198.18.15.107` を取得し、両者は既定ゲートウェイへ 3/3 成功しました。

`routerd` の停止で `routerd-dnsmasq.service` と UDP/67 が消え、再起動後に新しい dnsmasq プロセスで
待受と両リースの更新が復旧しました。DHCPv4 サーバーと予約を除いた設定ではサービスと UDP/67 が
消え、VM220 の 3 回の新規要求はリースなしで失敗しました。設定復元後は予約・動的リースと疎通が
復旧しました。続く `6420b8ba` の PVE 試験では、VM224 が静的に使用する `198.18.15.100` を
VM220 が要求しました。DHCP サーバーは `.107` を払い出し、VM220 は ARP の重複検出後に
120 秒のリースを設定しました。候補を停止すると、118 秒後に VM220 から `.107` と既定経路が
消えました。候補の復旧後は VM220 が `.107` を再取得し、既定ゲートウェイへの ICMP は 3/3 成功しました。

The current PVE BGP result is recorded at `29aa0ee8`. VM219 and VM220 formed
an eBGP session on VLAN 3132. A peer-advertised prefix entered the VM219 kernel
FIB as `proto bgp`, was withdrawn while the TCP session remained established,
and returned when the peer resumed advertisement. This does not qualify
IPv6 BGP, ECMP, a peer implementation other than routerd, or a downstream
endpoint behind the peer.

`bc3a51bf` repeats the isolated VLAN 3132 exchange with an independent
FRRouting peer and an explicit endpoint `198.18.16.1:179`. The candidate
reported the peer as Established, installed only the allowed `10.77.60.0/24`
kernel route, withdrew and restored it on peer advertisement changes, and did
not restart `routerd-bgp`. The setup, observations and rollback are recorded in
[`routerd15-bc3a51bf-bgp-explicit-port-pve-20261102.md`](../../evidence/routerd15-bc3a51bf-bgp-explicit-port-pve-20261102.md).

The BFD result is recorded at `79e947b5`. The Linux systemd unit now grants the
running routerd process `CAP_DAC_OVERRIDE`, allowing its BFD controller to read
FRR's protected `vtysh.conf` and access the `frrvty` control socket. In the same
PVE VLAN 3132 pair, both FRR BFD peers reached `Up`; stopping the independent
VM220 FRR service produced VM219 BFD `Down` and BGP `Reconverging`; restarting
that service restored BFD `Up` and BGP `Established`. The VM219 untrusted-zone
configuration explicitly permits the required inbound UDP/3784 control packets.
This does not qualify a non-routerd BFD peer, IPv6 BGP, ECMP, or physical-network
behavior.

Linux PPPoE の PVE 試験は `6f67e4fb` と `59681ef4` に記録しています。監督側は
設定済みの PPP インターフェース名と既定経路の意図を補助プログラムへ渡し、Linux の
peer 設定は `defaultroute` と `replacedefaultroute` を出力します。VM219 は VLAN
3132 上の独立 VM220 アクセスコンセントレーターと PAP/IPCP を確立しました。VLAN
3131 のクライアント VM224 は VM219 経由で VM220 の HTTP サーバーへ到達し、VM220
はクライアントではなくルーターの PPP アドレスを送信元として記録しました。

アクセスコンセントレーターの停止により PPP インターフェースと既定経路が消え、再開と
routerd 再起動によりクライアント通信まで復旧しました。初回の基準設定への置換では、
終了待ちの PPP 常駐プロセスが既定経路の所有権確認と競合して `Drifted` になりました。
`59681ef4` は、厳密置換の経路制御より先に不要な PPPoE 常駐プロセスを停止し、設定済み
PPP インターフェース名を現在状態へ反映します。修正後の PVE 再試験では、有効な PPPoE
接続から基準設定を一回だけ適用し、世代 46、21 リソースすべて `Healthy`、
`ppp-lab` 不在、`rd15-wan` 経由の基準既定経路復帰を確認しました。IPv6 PPP、CHAP、
物理回線および異なる実装のアクセスコンセントレーターは未検証です。

現行リビジョン `eca0cf08` の再試験では、VM219 が VLAN 3132 の独立 VM220 と PAP/IPCP を
確立し、`ppp-lab` の `198.18.10.2` と PPP 既定経路を設定しました。VLAN 3131 の VM224 は
HTTP を VM220 へ転送し、応答には NAT44 後の `198.18.10.2` が記録されました。アクセス
コンセントレーターの停止で PPP インターフェース、既定経路、HTTP 到達性は消失し、再開後と
`routerd` 再起動後に自動復旧しました。空設定の適用は、試験前から存在した外来 dnsmasq
サービスを停止したクリーンな状態で Healthy となり、PPP インターフェース、既定経路、HTTP は
撤回されました。IPv6 PPP、CHAP、物理回線、異なるアクセスコンセントレーターは未試験です。

現行リビジョン `99d44d33` は、旧設定を読まず新規 1.5 設定と新規状態領域で PVE VLAN 実通信を
再確認しました。VM219 は VM220 の PAP/IPCP アクセスコンセントレーターから `ppp-lab` の IPv4
アドレスと PPP 既定経路を取得しました。VM224 の ICMP 3/3 と HTTP 200 は NAT44 を経由しました。

アクセスコンセントレーター停止では PPP インターフェースと NAT44 が撤回され、HTTP は 000 でした。
再開、PPPoE を含まない基準設定への置換と再適用、routerd 再起動のそれぞれで、PPP、既定経路、
NAT44、HTTP は復旧しました。これは PAP/IPv4 の限定証跡です。

IPv6 PPP、CHAP、認証失敗、資格情報更新、MTU 境界、複数同時セッション、長時間障害、物理回線、
他実装のアクセスコンセントレーターは未検証です。詳細は
`evidence/routerd15-99d44d33-pppoe-pve-qualification-20261102.md` に記録します。

現行リビジョン `ea892a6b` では、VLAN 3132 の独立 VM220 `rp-pppoe` と、PAP、IPCP、IPv6CP を
新規 1.5 構成で再確認しました。VM219 は `ppp-v6` に IPv6CP の対向リンクローカルアドレスを持ち、
明示した非信頼ゾーンから候補自身への ICMPv6 echo-request だけを許可しました。VM219 からの
ICMPv6 は 3/3 応答しました。アクセスコンセントレーター停止で `ppp-v6` は 3 秒以内の観測で消え、
状態は `ERROR` になりました。同じアクセスコンセントレーターの再起動後、21 秒の観測で IPv6CP と
ICMPv6 3/3 が復旧しました。この時間は目標値ではありません。

空の新規構成を適用すると、候補は `HEALTHY`、資源数 0、`ppp-v6` 不在となりました。PPP 用の
ゾーンと許可規則は消え、routerd 所有の防火壁は空の既定遮断表を維持しました。同じ IPv6 PPP
構成を再適用すると、IPv6CP と ICMPv6 3/3 が復旧しました。試験後は VM219/220 を元の
スナップショットへ戻し、ISO、一時配置、スナップショット、残留 CD-ROM を削除しました。詳細は
`evidence/routerd15-ea892a6b-pppoe-ipv6-pve-20261102.md` に記録します。IPv6 プレフィックスの
配布・LAN 転送、別実装のアクセスコンセントレーター、CHAP、資格情報更新、長時間切断、物理回線、
FreeBSD は未検証です。

現行リビジョン `c1aa1422` では、VM219 の既存状態領域を退避して新しい 4 KiB の状態 DB から開始し、
資格情報を変更する新規 1.5 構成を実通信で確認しました。VM220 の PAP 資格情報を同時に変更すると、旧
`ppp-v6` は 3 秒以内の観測で消え、候補は `ERROR` を公開しました。変更後の構成を `routerctl apply --replace`
で適用すると、IPv6CP と ICMPv6 3/3 が復旧しました。空の `ip routerd_nat` 表を孤立資源と誤って警告する
不具合は、Linux NAT 基本表の所有意図を宣言して修正しました。修正版では `HEALTHY`、警告なし、PPP 常駐
プロセス各一つ、空構成への撤回と再適用後の IPv6CP・ICMPv6 3/3 を確認しました。

先の `ea892a6b` の IPv6 PPP 証跡は、基準 VM の状態 DB を使っていました。「空状態」とした記載を訂正し、
空状態での資格情報変更試験を現行の根拠に置き換えました。両試験後に VM219/220 を元の状態へ復元し、
ISO、一時配置、スナップショットは削除済みです。詳細は
`evidence/routerd15-c1aa1422-pppoe-credential-rotation-pve-20261102.md` に記録します。別実装の
アクセスコンセントレーター、長時間切断、IPv6 プレフィックス配布と LAN 転送、CHAP、物理回線、FreeBSD は
未検証です。

現行リビジョン `883dacbd` では、同じ三 VM・独立 VLAN 構成を新しい Linux 候補パッケージで再実行しました。
VM219 は VM220 の rp-pppoe アクセスコンセントレーターと PAP/IPCP を確立し、`ppp-lab` に
`100.64.0.10`、PPP 既定経路、LAN から PPPoE への NAT44 規則を構成しました。VM224 の ICMP は
3/3 成功し、VM220 の HTTP 応答は変換後の送信元 `100.64.0.10` を記録しました。

VM220 のアクセスコンセントレーターとセッション PPP プロセスを停止すると、VM219 は `Failed` と
`Connection terminated.` を公開し、`ppp-lab` は消え、既定経路は `rd15-wan` へ戻りました。VM224 の
HTTP は接続時間切れでした。同じアクセスコンセントレーターの再起動後、候補は自動的に
PAP/IPCP、`100.64.0.10`、PPP 既定経路を回復し、VM224 の ICMP 3/3 と HTTP も回復しました。
試験後は三 VM を元のスナップショットへ戻し、候補 VM219 と VM220 の既存 `routerd` は `active`、
VM224 は停止状態へ復元しました。詳細は
`evidence/routerd15-883dacbd-pppoe-recovery-pve-20261102.md` に記録します。

`409719ba` の PVE 再試験では、VLAN 3132 の VM220 を外部 HTTP 要求元、VLAN 3131 の
VM224 を内部 HTTP サーバーとして、VM219 の TCP 宛先変換を確認しました。外部側から
`198.18.53.2:18080` へ接続すると VM224 の `198.18.51.10:8080` が応答しました。ポート転送を
除いた構成への厳密置換は `Healthy` となり、外部側の接続はタイムアウトしました。ポート転送を
再投入すると同じ HTTP 応答が復旧しました。以前は、存在しない dnsmasq プロセスだけを指す古い
所有記録を停止しようとして、無関係な撤回が `Drifted` になりました。`409719ba` は、生存中の
所有済みプロセスの停止を維持しつつ、この古い記録を外部サービスとして扱い、適用失敗にしません。
この結果は単一 TCP ポート転送の適用・撤回・復旧に限ります。DNS 転送、NAT ピンホール、IPv6 NAT、
再起動後のポート転送、他のプロトコルとセキュリティ機能対照は未検証です。

同じ PVE VLAN で、VM220 の独立 dnsmasq を DNS 上流、VM224 を DNS クライアントとして
`409719ba` の DNS 転送を確認しました。VM224 の UDP と TCP の問い合わせは、VM219 を経由して
`test.upstream.example` の応答を受けました。上流を停止すると、未キャッシュ名は両プロトコルで
`SERVFAIL` になり、再開後に応答を回復しました。DNS 資源を除いた構成への厳密置換は `Healthy` となり、
待受を失った VM224 は UDP/TCP とも接続拒否になりました。DNS 資源の再投入後は両プロトコルで
転送応答が戻りました。この結果は一つの UDP 上流とクライアント側 UDP/TCP に限ります。DNSSEC、
DoT/DoH、否定応答、リース名、再起動、IPv6 および旧構成の全 DNS ポリシー対照は未検証です。

現行リビジョン `477e57a3` では、VM219 を新しい状態 DB から開始し、VLAN 3131 の独立 VM224 と
VLAN 3132 の独立 VM220 を使って IPv6 DNS を確認しました。VM224 は UDP/TCP とも候補のローカル
AAAA 応答と、VM220 上流を経由する AAAA 応答を受けました。`routerd-dns-resolver@lan.service` を再起動し、
PID の交代後にも同じ通信を確認しました。空構成への置換は `HEALTHY`、警告なしで DNS 常駐プロセスと
IPv4/IPv6 の待受を撤回しました。空の既定遮断用 `inet routerd_filter` 表を孤立資源と報告する不具合は、
同表の routerd 所有を明示して修正しました。保存した新規構成の再適用は `HEALTHY` となりました。詳細は
`evidence/routerd15-477e57a3-dns-ipv6-restart-pve-20261102.md` に記録します。

現行リビジョン `7fbafbbc` では、VM219 を新しい状態領域から開始し、VLAN 3131 の独立 VM224、VLAN 3132 の独立 VM220 AFTR を使って DS-Lite を確認しました。VM224 の IPv4 HTTP は、候補の `rd15-dslite` を通り、VM220 では内側送信元 `192.0.0.2` として記録されました。候補の接続追跡は元の `198.18.15.10` を保持しました。AFTR 側で捕捉した TCP SYN の MSS は1414で、候補の NAT44 と TCP MSS 制御に一致しました。

`routerd` の再起動後にも新規 HTTP は成功しました。空構成の適用では DS-Lite、既定経路、NAT44 が撤回され、VM224 の新規 HTTP は失敗しました。直後の一回目は削除処理中の `routerd_mss` を一時的に孤立資源と警告しましたが、表の削除後に同じ空構成を再適用すると警告なしで `Healthy` になりました。保存した新規構成の再適用では世代4、7資源が `Healthy` となり、HTTP は復旧しました。

この証跡は静的 IPv6 送信元の DS-Lite、NAT44、TCP MSS、候補再起動、撤回、再適用に限ります。DHCPv6-PD の送信元失効・再取得、AFTR 名解決・冗長化、FreeBSD の PF、物理ネットワークは未確認です。詳細は `evidence/routerd15-7fbafbbc-dslite-nat-restart-withdrawal-pve-20261102.md` に記録します。

現行リビジョン `4655971e` では、同じ三 VM・分離 VLAN 構成で DS-Lite の外側送信元を `DHCPv6PrefixDelegation/wan-pd` の `2001:db8:1500::/60` から生成しました。候補は LAN に `2001:db8:1500::100/128` を設定し、これを外側送信元として `rd15-dslite` を作成しました。VM224 の IPv4 HTTP は VM220 の独立AFTRとHTTP端点に届き、AFTRは変換後の `192.0.0.2` と MSS 1414 を記録しました。

Keaだけを停止して35秒の試験用リースを失効させると、候補は委任送信元、`rd15-dslite`、DS-Lite既定経路を撤回し、VM224の新規HTTPは失敗しました。Keaだけを戻すと、候補プロセスを再起動せず約3秒で委任、トンネル、既定経路、HTTP通信を自動復旧しました。AFTRのFQDN解決・冗長切替、FreeBSD PF、物理ネットワークは未確認です。詳細は `evidence/routerd15-4655971e-dslite-delegated-source-pve-20261102.md` に記録します。

現行リビジョン `623a9082` では、同じ三 VM・分離 VLAN 構成で AFTR の FQDN 応答変更を確認しました。候補ローカルの DNS リゾルバーは VM220 の独立 DNS へ `aftr.test` を問い合わせ、初期 AAAA 応答 `2001:db8:16::1` から DS-Lite を生成しました。VM224 の IPv4 HTTP は VM220 の独立 AFTR・HTTP端点まで到達しました。

独立 DNS の AAAA 応答を `2001:db8:16::2` だけへ変更し、旧 `::1` AFTR を撤去すると、直後の新規 HTTP は失敗しました。候補プロセスを再起動せず、開始から約12秒後に `rd15-dslite` は外側宛先 `::2` で再生成され、同じ HTTP が復旧しました。これは DNS 応答の撤回・復旧を伴う一端点の再選択であり、複数トンネルの同時経路選択や能動待機の合格ではありません。詳細は `evidence/routerd15-623a9082-dslite-aftr-fqdn-pve-20261102.md` に記録します。

現行リビジョン `81444257` では、同じ委任済み IPv6 送信元を共有する二つの DS-Lite トンネルを、三 VM・分離 VLAN 構成で確認しました。VM219 は `rd15-dsla` と `rd15-dslb` を作成し、VM220 の独立 AFTR `2001:db8:16::1` と `2001:db8:16::2` へ接続しました。優先 EgressRoutePolicy は単一の選択済み宛先をハッシュせず固定マークで記録するよう修正しました。VM224 の新規 HTTP は初期状態で `dslite-a`、マーク `0x110` を通り、VM220 は変換後の送信元 `192.0.0.2` を記録しました。

VM220 で AFTR-A のトンネルだけを撤去すると、`through-dslite-a` は `Unhealthy` へ遷移し、候補は `dslite-b`、マーク `0x111` を選択しました。同じ VM224 の新規 HTTP は成功し、接続追跡と VM220 は送信元 `192.0.0.3` を記録しました。AFTR-A を戻すと、候補プロセスの再起動や設定再適用なしで `through-dslite-a` は `Healthy` へ戻り、選択、HTTP、送信元は `dslite-a`、`0x110`、`192.0.0.2` へ復帰しました。固定の切替時間は要件にしていません。詳細は `evidence/routerd15-81444257-dslite-priority-failover-pve-20261102.md` に記録します。

同じ `81444257` の配布物を PVE VM219 へ新規状態領域で導入し、二つの HealthCheck、優先 EgressRoutePolicy、NAT44、二つの DS-Lite を再生成しました。VM224 の HTTP は VM220 で `192.0.0.2` として観測されました。次に `routerd` を再起動すると主プロセスPIDは交代し、`dslite-a`、重み200、固定マーク `0x110`、HealthCheck、独立HTTP、変換後送信元 `192.0.0.2` はすべて復旧しました。詳細は `evidence/routerd15-81444257-health-policy-package-restart-pve-20261102.md` に記録します。

`e83b639b` の候補実行物では、静的な `IPv4Route/private-10-blackhole` を VLAN 3131 と
VLAN 3132 の独立 VM で確認しました。黒穴経路がある間、VM224 の `10.23.45.67` 宛て ICMP は
すべて失敗し、VM220 の上流捕捉は 0 でした。黒穴経路を撤回すると同じ要求 1 回を上流で捕捉し、
再適用後は再び未捕捉でした。

`e8bfbd3a` の候補実行物で、`EgressRoutePolicy` の `excludeDestinationCIDRs` を確認しました。
通常宛先 `203.0.113.9` は、VLAN 3131 の VM224 から候補 WAN 側と VLAN 3132 の VM220 まで
到達しました。`10.0.0.0/8` を除外すると `10.23.45.67` 宛ての要求は両方で未捕捉でした。
除外 CIDR を外すと同じ私設宛て要求を両方で捕捉し、再適用後は再び未捕捉でした。

`019ca029` の候補実行物では、VLAN 3131 の独立 VM224 から公開宛て相当の
`198.18.93.53:53` と `198.18.93.123:123` へ UDP 要求を送りました。VM219 の
`LocalServiceRedirect` は DNS をローカル UDP 1053、NTP をローカル UDP 1123 へ変換しました。
DNS は `redirect.test` の `198.18.91.42` 応答を返し、NTP は 48 バイトの種別 4・`RD15`
応答を返しました。規則を除く厳密置換後は DNS が到達不能、NTP が 2 秒でタイムアウトしました。
規則の再適用後は両方の UDP 応答が復旧しました。これは DNS/NTP の新規構成による誘導を示す
証跡であり、撤回時の既存通信や他のセキュリティ機能の完了証明ではありません。

Linux DHCPv4 リレーの隔離 PVE 検証は `00fb5f06` と `2fb7a320` に記録しています。
`00fb5f06` は、各受信側の論理インターフェースと実際に割り当てた IPv4 アドレスを
`listeners` へ明示する新しい設定契約に変更しました。このアドレスは dnsmasq が
上流 DHCP サーバーへ送る `giaddr` です。`2fb7a320` は、指定した上流論理
インターフェースから自身の UDP/67 宛て応答を許可するファイアウォール規則を追加しました。

隔離したルーター、上流 DHCP サーバー、LAN クライアントの 3 VM で、新しい 1.5 状態領域から
候補実行物を起動しました。旧状態データは読まずに退避し、検証後に元の状態領域へ復元しました。
生成された dnsmasq の行は
`dhcp-relay=198.18.15.1,198.18.16.1,rd15-wan` です。独立クライアントは
DISCOVER、OFFER、REQUEST、ACK を完了し、`198.18.15.104/24` と既定経路を取得しました。
ルーター `198.18.15.1` への 3 回の ICMP 到達確認はすべて成功しました。

上流 DHCP サーバーを停止した状態では、クライアントの DISCOVER に応答はなく、IPv4
アドレスは取得されませんでした。上流を再起動すると、ルーターを再起動せずにクライアントは
再度 ACK を受け、同じアドレスと既定経路を取得しました。WAN 側の UDP/67 許可規則は
応答 4 パケットを受信し、後続の既定破棄規則は 0 パケットでした。検証後は元の実行物、
設定、状態領域を復元し、検証専用の ISO、VLAN、クライアントリンク、上流サーバーを削除しました。

Linux の二重 WAN 健全性連動ルーティングを、`f4308787` の候補実行物で隔離 PVE にて
確認しました。候補は空の 1.5 状態領域から起動し、systemd の二つの HealthCheck
ワーカー、優先 EgressRoutePolicy、二つの専用 IPv4 経路表、NAT 規則を稼働させました。
VLAN 分離したクライアントから独立 HTTP 宛先へ送った初期通信は WAN-A を通り、上流側では
変換後の送信元 `198.18.16.2` を捕捉しました。

WAN-A 専用の健全性サービスを停止すると、選択規則は WAN-B のマーク `0x120` へ更新され、
クライアント HTTP 通信は継続しました。上流側で捕捉した WAN-B の送信元は
`198.18.18.2` です。WAN-A の健全性サービスを再開すると、手動の再適用やルーター再起動なしで
選択規則は WAN-A のマーク `0x110` へ戻り、送信元 `198.18.16.2` の通信を再確認しました。
本試験は固定の切替時間を要件にしていません。検証後は候補の状態、経路規則、nftables 表、
VLAN、HTTP サーバー、検証メディアを削除し、元の実行物・設定・状態領域を復元しました。

Linux の WireGuard を、`b1ae8709` の候補実行物で隔離 PVE にて確認しました。VM219 の
候補 routerd は空の 1.5 状態領域から WireGuardInterface、WireGuardPeer、IPv4StaticAddress、
IPv4Route、および `wg-pve` の信頼ゾーンを適用しました。VLAN 3132 上の独立 VM220 ピアと
ハンドシェイクし、双方の WireGuard 送受信量が増加しました。`10.210.0.1` と `10.210.0.2`
の間では、両方向の ICMP を各 3 回すべて受信しました。

独立ピアの WireGuard インターフェースを停止すると、候補からの到達確認は失敗しました。
再開後、外部ピア側で停止により消えた試験用経路を復元すると、ハンドシェイクと両方向の
3 回の ICMP 到達確認が復帰しました。候補側のトンネル、IPv4 アドレス、経路および信頼
ゾーンは維持されました。鍵ローテーション、routerd 再起動、設定撤去、および FreeBSD は
未確認です。検証後は VM219/220 の元の実行物・設定・状態領域を復元し、試験用 ISO、VLAN
リンク、インターフェース、秘密鍵を含む一時領域を削除しました。

同じ Linux WireGuard 構成を `8e732fe2` の候補実行物で再試験しました。候補 routerd を
再起動した後も、トンネルの IPv4 アドレスと経路が存在し、独立ピアとの双方向 ICMP は各
3 回すべて成功しました。独立ピアの鍵を新しい鍵へ変更し、候補の WireGuardPeer 公開鍵を
置換すると、新しいハンドシェイクと双方の 3 回 ICMP が成功しました。

最後に WireGuardInterface、WireGuardPeer、WireGuard 用 IPv4 アドレス・経路・信頼ゾーンを
含まない設定へ置換しました。候補は残る 6 リソースを Healthy として適用し、`wg-pve`、
`10.210.0.1/32`、`10.210.0.2/32` 宛て経路は存在せず、独立ピアからの通信は失敗しました。
この確認で Linux の WireGuard に残していた鍵ローテーション、routerd 再起動、設定撤去を
完了しました。FreeBSD は未確認です。検証後は VM219/220 の元の実行物・設定・状態領域を
復元し、試験用 ISO、VLAN リンク、インターフェース、秘密鍵を含む一時領域を削除しました。

Linux VRF の隔離 PVE 試験では、`c6b42c1c` の候補 routerd を VM219 の空の 1.5 状態領域から
起動しました。`FirewallZone` が VRF 名を参照するとき、`when` 条件の選択後にその参照が
誤って空になる不具合を修正しています。候補は VLAN 3131 上で `vrf-pve`、経路表 110、
メンバー `rd15-lan`、`198.18.15.1/24`、および `if_tenant = { "vrf-pve" }` を実際に生成
しました。独立 VM224 は同じ VLAN に `198.18.15.2/24` を持ち、候補への ICMP は 3 回すべて
成功しました。入力規則の `tenant_to_self` カウンターも受信を記録しました。

候補 routerd を再起動しても、VRF、メンバー、IPv4 アドレス、経路表およびゾーン規則は残り、
独立 VM224 の ICMP は再び 3 回すべて成功しました。次に VRF、アドレスおよびゾーンを含まない
構成へ置換すると、それらのカーネル状態と nftables 表は消え、VM224 からの ICMP は 3 回すべて
失敗しました。候補構成を再適用すると状態と規則は戻り、ICMP は 3 回すべて成功しました。
検証後は VM219 の元の実行物、設定、状態領域と VM224 の元のネットワーク状態を復元し、候補
ISO、VLAN 子インターフェースおよび一時領域を削除しました。この結果は単一 IPv4 接続網の
VRF 親子、再起動、設定撤去および復旧を示します。複数 VRF 間の分離・転送、IPv6 および FreeBSD
は未確認です。

続く Linux 環境の仮想ルーティング・転送（VRF）試験では、`d9ed53a` の候補実行物を隔離した PVE 環境で
実行しました。この修正は、仮想ルーティング・転送を参照する防火壁ゾーンを親側だけでなくメンバー側の実際の
インターフェースにも適用します。空の 1.5 状態領域から候補を起動し、候補自身がアドレスを
作成した状態で、VM219 の VLAN 3131 と 3132 に `vrf-a` および `vrf-b` を生成しました。
`if_tenant_a` は `vrf-a` と `rd15-lan` を、`if_tenant_b` は `vrf-b` と `rd15-wan` を含みます。

VLAN 分離した VM224 の `198.18.17.2` と、独立 VM220 の `198.18.15.3` は、同じ `vrf-a` 内で
両方向とも 3 回の ICMP 到達確認に成功しました。VM220 の `vrf-b` 側から同じ仮想ルーティング・転送のゲートウェイへ
送った ICMP も 3 回すべて成功しました。送信元を `vrf-a`、宛先を `vrf-b` の `198.18.16.2` にした ICMP と、
送信元を `vrf-b` に固定した逆方向の `198.18.17.2` 宛て ICMP は、いずれも 3 回すべて失敗しました。
カーネル経路表は各仮想ルーティング・転送の直結網だけを保持し、`vrf-a` 側では `Destination Net Unreachable` を返しました。

候補実行物の再起動後も親、メンバー、アドレス、経路表および両ゾーン集合を再生成し、同一仮想ルーティング・転送
内の 3 回の ICMP は成功し、別の仮想ルーティング・転送宛ての 3 回はすべて失敗しました。次に `vrf-a` とその二つの
アドレス・ゾーンを含まない構成へ置換すると、`vrf-a`、そのアドレスおよび `if_tenant_a` は消え、
VM224 から同じゲートウェイへ送った 3 回の ICMP はすべて失敗しました。`vrf-b` のゲートウェイは
3 回すべて応答しました。完全な候補構成へ戻すと、`vrf-a` の状態とゾーン集合を再作成し、同一仮想ルーティング・転送
内の転送と仮想ルーティング・転送間の分離を再確認しました。試験後は VM219、VM220、VM224、PVE 環境の ISO と一時資材を
すべて元へ戻し、候補資材を削除しました。これは Linux 環境における IPv4 仮想ルーティング・転送の親子と、
複数の仮想ルーティング・転送の転送・分離に関する限定証跡です。再起動、構成撤去および復旧も確認しました。
IPv6、FreeBSD および全機能認定は未確認です。

Linux 環境のブリッジ（Bridge）試験では、`54b27326` の候補実行物を隔離した PVE 環境で実行しました。
空の 1.5 状態領域から、VLAN 3131 の `rd15-lan` と VLAN 3132 の `rd15-wan` をメンバーにする
`br-pve` を作成しました。スパニングツリープロトコルは試験構成で無効にし、二つのメンバーは
ともに転送状態になりました。VM224 の VLAN 3131 側に `198.18.31.2/24`、独立 VM220 の VLAN 3132 側に
`198.18.31.3/24` を設定しました。

最初の ICMP は端点 VM の既存防火壁が VLAN 子インターフェースからの入力を拒否したため失敗しました。
ARP の解決と候補ブリッジの MAC 学習を確認した後、試験中だけ端点の既存 routerd プロセスを停止し、端点側の
生成済み防火壁表を削除しました。候補実行物の防火壁構成は変更していません。この条件で、両端点の
ICMP は各方向 3 回すべて成功しました。候補実行物の再起動後も `br-pve` と両メンバーを再生成し、
両方向の ICMP は各 3 回すべて成功しました。

ブリッジ資源を含まない構成へ置換すると `br-pve` は消え、二つの VLAN 子インターフェースはメンバーでなくなり、
両方向の ICMP は 3 回すべて失敗しました。完全な候補構成を再適用すると、ブリッジ、メンバー状態および
両方向の通信を回復しました。試験後は VM219 の元の実行物、設定、状態領域と三つの IPv4 アドレス、
VM220 の元の routerd、VM224 の停止状態を復元し、候補 ISO と一時資材を削除しました。これは Linux 環境の
独立ブリッジ転送、再起動、構成撤去および復旧に関する限定証跡です。FreeBSD、物理ネットワークおよび
全機能認定は未確認です。

The next Linux work selects the highest remaining product function with a
concrete independent peer or client. It must record actual kernel and packet
effects, withdrawal and recovery where the function requires them. No timing
threshold or compatibility requirement beyond the stated product function is
introduced by this checkpoint.

現行リビジョン `81444257` では、IPv6 の `hostTraffic` 方針を PVE の VM219 と独立 VM220 で
確認しました。候補機自身の HTTP は、優先度 10119 の `main` 経路表抑制規則と、優先度 10120 の
経路表 120 を通り、VM220 は候補の IPv6 送信元を記録しました。空の新規 1.5 構成への置換では
所有規則と既定経路が消え、新規 HTTP は接続不能となりました。同じ構成の再適用後は規則、経路、
HTTP が復旧しました。これは候補機自身が生成する IPv6 通信の限定証跡です。LAN 転送、IPv4 方針、
ハッシュ方式、マーク方式、動的経路条件、VRRP、物理 WAN 障害、FreeBSD および全機能認定は
未確認です。詳細は
[`routerd15-81444257-ipv6-host-policy-pve-20261102.md`](../../evidence/routerd15-81444257-ipv6-host-policy-pve-20261102.md)
に記録しています。

## Work sequence

1. **Health to PBR (initial implementation).** Decode current status at its
   boundary into a small typed observation. Share the threshold/freshness decision
   between advisory selection and priority routing, using one captured time.
   Publish priority routing results after nftables succeeds; report effect and
   status-write errors. Check stale observations, grace failures, fallback and
   application failure locally, then demonstrate two-WAN failover on an isolated
   VM with the product daemon. This first checkpoint retains the existing store
   and IPC boundaries; it does not claim to finish typed end-to-end transport.
2. **Typed observation transport.** Follow Health daemon -> status acquisition ->
   routing through actual callers. Replace string/map round trips with explicit
   messages and a single owner for each current observation. Update both IPC ends,
   API consumers and schemas together, deleting replaced representations. Keep
   validity/restart behavior explicit; do not add old producer support.
3. **Intent and read-only decisions.** Replace YAML cloning in dynamic config.
   Prepare static references with configuration and pass current observations to
   synchronous decisions. Separate effects so plan/dry-run no longer requires a
   cloned writable DB. Handle expiry, changes during waits and dependent withdrawal
   at their responsible operation; avoid a universal new resource engine.
4. **Persistence and recovery.** Choose one new schema for required leases,
   ownership and pending operations. Separate diagnostic history from control
   decisions. Preserve EventRule history semantics and DHCP callback recovery if
   those features remain enabled. Remove discarded readers/migrations and dual
   paths; test a fresh start and a restart using the new format.
5. **Protocol and OS feature paths.** Work through DHCPv4, DHCPv6-PD/RA, DS-Lite,
   PPPoE, DNS, NAT/firewall and the remaining enabled features. Repair packet bounds
   and exchange handling in the owning receive path; retain useful optional data.
   Reuse selected routerd2 fixes after reading their full callers. Remove unused
   old paths only after checking references and affected builds.
6. **Fresh installation and release.** Run the existing feature inventory with
   converted 1.5 configuration, fresh 1.5 state and independent peers. Exercise
   service startup, restart, loss/return of upstream, renewal/expiry,
   managed-resource cleanup and management recovery. Record packet, client and
   kernel results. Do not qualify 1.0-to-1.5 in-place upgrade or data/config
   migration. Publish a version only after its stated scope is demonstrated. No
   production cutover or existing DB deletion occurs implicitly.

Each step is a complete feature path with its obsolete code removed, not an
invitation to finish a new framework before trying actual communication.

## Local and VM checks

`make check-routerd15` runs the affected local Go packages. Ordinary tests must
not mutate the host network. Focused command fixtures can establish ordering and
failure propagation; they cannot establish packet forwarding.

[routerd15-feature-qualification.json](routerd15-feature-qualification.json)
maps every baseline resource kind and example artifact to its feature scope,
and tracks commands, control API, WebConsole and distribution separately.
Inventory presence is not a parity verdict. The pinned historical evidence
does not qualify later changes or turn an unknown feature into a pass.

`make check-routerd15-wireguard-parent` checks the production WireGuard parent
and child ownership boundaries with command fixtures. The explicit Linux target
`make check-routerd15-native-parent-linux` additionally enters fresh user,
network and mount namespaces. It requires iproute2, WireGuard tools, kernel
WireGuard/VXLAN support and enabled user namespaces. It exercises native Bridge
address lifecycle, VXLAN creation/configuration/withdrawal and an independent
WireGuard peer, including SQLite reopen and owned-interface recovery. These
checks establish their stated native paths; they do not qualify other protocols,
FreeBSD, package activation or all WireGuard source integrations.

The VM check needs an isolated Linux VM/netns with CAP_NET_ADMIN/CAP_NET_RAW,
Unix sockets, two upstreams and an independent TCP/UDP peer. Start the actual
routerd/Health processes, preserve the management path, send traffic, interrupt
one upstream and restore it. Observe routes/nftables and the traffic path; repeat
with worker restart and stale observations. Record actual setup and revision.
The audit records executed isolated Linux/FreeBSD VM paths and their revisions.
The two-WAN failover/recovery gate is recorded for the current Linux revision
in `53e54d58`. It used an isolated PVE client and two independent upstream
network namespaces, verified NAT source change from WAN-A to WAN-B during
upstream withdrawal, recovery to WAN-A, and configuration withdrawal/reapply.
It remains scoped to IPv4 priority selection; IPv6 policy forwarding, ECMP and
physical WAN failure remain separate work.

## Repository-wide implementation checkpoint

The scope is now the whole repository, rather than only Health/PBR. The coverage
record in [routerd15-audit.md](routerd15-audit.md) explicitly distinguishes source
inventory, semantic review, implementation and executed checks. Pending packages
are not declared compliant.

Implemented across existing callers:

- Concrete ResourceSpec types at YAML/JSON boundaries, shared generated decoding
  and ordinary deep copies; no JSON fallback to recover internal spec types.
- Typed current Health observation from the worker to persistence and routing;
  one current-status owner; worker restart restores history without asserting
  previous reachability; failure grace requires a previously healthy path.
- Explicit MTU resource handling; removal of unreachable standalone IPv4/Health
  apply paths and obsolete skipped inspection tests.
- One SQLite schema (user_version 1501) shared by state and the ownership ledger;
  no JSON persistence backend, migration chain, or no-op Save contract. Closed
  stores and invalid status encodings report errors.
- Error-returning state-variable writes, propagated through PD observations and
  ingress drain operations; expiry observation does not delete drain intent.
- Read-only apply planning, including dynamic parts and when filtering. Plan does
  not execute controllers, generate simulated events, create a temporary DB, or
  open ownership data for writing. Apply reacquires observations before effects.
- Cancellation passed to policy route observations and delayed derived events.
- Repository-wide command reachability cleanup, shared IPv4 frame bounds,
  DHCPv6-PD request retry/identity/timer fixes, validated RA reception and worker
  stop/signal ownership. The audit lists remaining protocol and state-write gaps.
- Example inventory tests use their own loopback metadata endpoints and fake
  provider CLIs; Azure inventory honors its configured metadata endpoint.

No released version, full semantic audit, or real network qualification is claimed.

The subsequent failure/recovery pass implements full DHCPv4 exchange ownership,
multi-prefix DHCPv6 and information requests, error-returning conditional reads,
serialized partial config updates, truthful nft/PF acknowledgement and withdrawal,
ICMP exchange correlation, DNS reload/cache/log ownership, current console views
and non-destructive installer behavior. The audit records the executed checks and
the remaining feature inventory work. A further pass connects handover release
to all native owners, journals BGP additions before effects, prepares validated
static declarations once per update, and gives PD one typed current observation.
Mutation responses distinguish committed intent from failed or unconfirmed
effects. These are implementation checkpoints, not completion of steps 3–6.

## Validation

Use go.mod's Go 1.25.9 toolchain and the Makefile targets. `check-routerd15`
retains the complete test/vet gate; `check-routerd15-focused` is a separate local
regression gate, never a substitute for full acceptance. Current executed results
and environment restrictions are tracked in the audit record. GitHub Actions
completion is not awaited.

`check-routerd15-recovery` runs the focused ownership, native-effect boundary,
current-declaration, handover and mutation regressions. It is included in the
focused gate. Its test-owned kernel-command observations and loopback services
do not replace the complete native suite or the VM/independent-peer gates.

The second semantic pass extends those same production paths: explicit provider
uncertainty and operator resolution; atomic subscription output/deduplication;
authenticated immutable federation facts; bounded worker/exporter shutdown;
strict configuration and error-bearing source reads; native BFD/VIP ownership;
and truthful console observations. `check-routerd15-robustness`, included in the
focused gate, exercises these paths with owned SQLite/process/loopback fixtures.
The audit record retains the unread scopes, implementation gaps and independent
peer/platform gates; a successful focused gate does not close them.

The next repair batches follow the concrete production findings in
[routerd15-open-findings.md](routerd15-open-findings.md): provider ambiguity and
exact identity, one BGP policy owner, ordinary/native address and route ownership,
current runtime lifetimes, truthful command/control behavior, and durable isolated
build/lab cleanup. Complete each actual owner/caller path and remove superseded
branches; do not expand a generic framework or certify an unread scope.
