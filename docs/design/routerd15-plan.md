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

The current PVE BGP result is recorded at `29aa0ee8`. VM219 and VM220 formed
an eBGP session on VLAN 3132. A peer-advertised prefix entered the VM219 kernel
FIB as `proto bgp`, was withdrawn while the TCP session remained established,
and returned when the peer resumed advertisement. This does not qualify
IPv6 BGP, ECMP, a peer implementation other than routerd, or a downstream
endpoint behind the peer.

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

The next Linux work selects the highest remaining product function with a
concrete independent peer or client. It must record actual kernel and packet
effects, withdrawal and recovery where the function requires them. No timing
threshold or compatibility requirement beyond the stated product function is
introduced by this checkpoint.

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
The full two-WAN failover/recovery gate remains separate from the completed
single-upstream IPv4 and delegated IPv6 client checks.

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
