---
title: Changelog
---

# Changelog

routerd is currently pre-release software. This changelog records the
behavior changes and new resource shapes as the model takes shape.

## Unreleased

- **取り下げ**: 2026-04-30 に発見した「NTT NGN PR-400NE HGW で Solicit が
  drop される時、直接 Request が取得 fallback として有効」という主張は
  もう保持しない。2026-05-01 のラボ検証で、事前 Reply 無しの Active
  Request は HGW PD table に entry を作るが binding は不完全 (HGW が
  以後の Renew / Rebind / Request を全て silent drop する "phantom" 状態)
  であることを観測した。修正後の方針は
  `docs/knowledge-base/ntt-ngn-pd-acquisition.md` の Section A.4 / B.4 と
  `docs/design-notes.md` Section 5.2 訂正に記録した。canonical な RFC 8415
  Solicit/Advertise/Request/Reply を実行する OS DHCPv6 client が当該 HGW
  での主要な取得 path。routerd の active controller は HGW server context
  が完成している binding に対する maintenance (Renew / Rebind / Release /
  Information-Request) に限定する。
- routerd の active sender の packet 形を IX2215 の動作確認済 packet に
  揃えた: IPv6 hop limit 255 (旧 1。Proxmox VE 仮想ネット経路で drop される)、
  DHCPv6 option 順序を Client-ID / Server-ID / IA_PD / Elapsed-Time /
  Reconfigure-Accept (旧 Elapsed-Time / ORO / IA_PD)、ORO 無し、Solicit
  IA_PD を IAID-only で T1/T2 ゼロ (旧 Renew 同等値)。
- `routerd dhcp6` CLI に `--iaid` を追加。`IPv6PrefixDelegation.spec.iaid`
  を編集せずに 1 発限りの IAID 上書きが可能に。
- `routerd dhcp6 request` は Reply 記録の無いリソースで呼び出すと警告を
  出すようになった (direct-claim Request は当該 HGW で phantom binding を
  作るため)。
- `IPv6PrefixDelegation` の observability 判定が DHCPv6 transaction evidence
  必須になった (`LastReplyAt` + 非ゼロ未失効 `VLTime`)。LAN 委譲アドレスが
  載っているだけでは `Currently observable: yes` にならない。stale 状態に
  drift した時は dnsmasq の IPv6 LAN service / RA / 委譲 LAN アドレスの
  描画を停止し、壊れた IPv6 を下流 client に提供しない。

## 0.1.0 (2026-05-01)

最初のタグ付きリリース。Linux/NixOS/FreeBSD 混在の 5 ノードラボで、
1 本の VXLAN オーバーレイが 25/25 のオーバーレイ ping を冷状態でも
router 再起動後でも維持することを確認した状態を v0.1.0 と呼ぶ。
`v1alpha1` API (router、net、system、firewall) はその体験の作業基準で
あり、明確さ優先のフェーズ中はこれらのシェイプを互換シムなしに名前変更や
差し替えしてもよい。

- apply が `inet routerd_filter` に VXLAN underlay UDP と bridge ICMP の
  accept を自動で出すようになった。default-deny FirewallPolicy 配下でも
  オーバーレイの制御経路が落ちない。bridge L2 フィルタには nftables の
  `counter` を付け、DHCP/RA/ND の drop hit を観測できるようにした。
- apply は新規に書いた systemd-networkd の `.netdev` を検出すると、
  `networkctl reload` ではなく `systemctl restart systemd-networkd` を
  実行する。reload では新 VXLAN/bridge netdev が常に materialize しない
  ディストリビューションがあるため。
- `routerd render nixos` は VXLANSegment の underlay UDP ポートを
  `networking.firewall.allowedUDPPorts` に、VXLANSegment が
  `spec.bridge` で明示的に紐付けた bridge を
  `networking.firewall.trustedInterfaces` に出すようになった。VXLAN を
  接続していない bridge は既定の firewall ポリシーに従う。
- NixOS の VXLAN netdev が `Independent = true` と、各ホストで
  `00:00:00:00:00:00 dst <peer>` の flood FDB エントリを全 remote 分
  追記する `routerd-vxlan100-fdb` 系の oneshot サービスを生成するように
  なった。これで multi-peer VXLAN は `nixos-rebuild` や再起動を超えて
  維持される。
- `routerd apply` は NixOS では live network ステージをスキップし、
  `routerd render nixos` + `nixos-rebuild switch` が真の永続経路で
  あることを info ログで案内するようになった。nft や dnsmasq などの
  他ステージはそのまま動く。
- `VXLANSegment`、`Bridge`、`IPv4StaticRoute`、`IPv6StaticRoute`、
  `DHCPv4HostReservation` が ownership intent を出すようになり
  (`net.link`、`net.ipv4.route`、`net.ipv6.route`、
  `nft.table routerd_l2_filter`、`dnsmasq.dhcpv4.host`)、orphan チェック
  でこれらの管理対象が無所有として報告されなくなった。
  `l2Filter: none` の VXLANSegment は L2 フィルタテーブルを所有しない。
- FreeBSD の VXLAN レンダラは、リモートが複数指定された場合でも
  ハードエラーで止まらず、先頭を seed として採用して残りは
  `FreeBSDConfig.Warnings` に降格する。`result.Warnings` と event log にも
  伝搬する。
- `make remote-install` は FreeBSD リモートで展開後に
  `sysrc routerd_enable=YES` を実行するようになった。これで
  `service routerd ...` の rc.d enable 警告が出ない。
- `make check-remote-deps` は `mstpd` / `mstpctl` 不在を警告として扱う
  ように変更した。systemd-networkd はカーネル STP に degrade するので
  Ubuntu noble など mstpd を提供しないディストリビューションでも
  デプロイが通るようになる。
- FreeBSD apply は書き込んだ routerd 管理の sysrc キー集合を
  routerd state store に保存し、次回 apply で前回集合に存在し現在の
  render に存在しないキーを `sysrc -x` で reclaim するようになった。
  VXLAN/bridge のインターフェイス名を変えても古い `ifconfig_<old>` が
  `/etc/rc.conf` に残らない。
- `DnsmasqConfig` は dnsmasq の rule が未観測の DHCP リース由来の
  サーバを必要とする場合 (`DNSConditionalForwarder.upstreamSource:
  dhcp4|dhcp6`、`IPv4DHCPScope.dnsSource: dhcp4`、
  `IPv4DHCPServer.dns.upstreamSource: dhcp4`) に、エラーで止まらず
  warnings を返すようになった。該当 rule は次回 apply の観測で出る。
- nixos-getting-started に 3.4 節を追加し、NixOS の
  `networking.firewall` (`nixos-fw` chain) が routerd の nftables と
  並行で動作し routerd では bypass できないこと、必要となる
  `allowedUDPPorts` / `trustedInterfaces` の設定、underlay パケットが
  tcpdump では見えるのに routerd の input chain に到達しないときの
  診断手順を記載した。
- Added a `VXLANSegment` resource with Linux systemd-networkd, FreeBSD, and
  NixOS render paths. Linux also renders a default bridge-family nftables
  L2 filter that blocks DHCPv4, DHCPv6, RA, and neighbor discovery on VXLAN
  ports unless `spec.l2Filter: none` is set.
- Added `role: server|transit` to DHCPv4 and DHCPv6 server resources so
  shared L2 segments can name one designated DHCP/RA server while other
  routerd hosts remain transit-only.
- Added a `Bridge` resource with conservative STP/RSTP defaults and
  multicast-snooping disabled by default for virtualized IPv6 labs.
- Added `IPv4StaticRoute` and `IPv6StaticRoute` resources for explicit static
  routes on Linux systemd-networkd, FreeBSD, and NixOS render paths.
- Added `DHCPv4HostReservation` for dnsmasq-backed fixed IPv4 leases inside
  an existing `IPv4DHCPScope`.
- SQLite state objects now include `last_applied_path` metadata. This prepares
  routerd for kubectl-style additive apply and explicit delete workflows.
- Successful apply runs now populate `last_applied_path` for each resource in
  the SQLite state database.
- `routerd apply` and `routerctl apply` are additive: they update submitted
  resources and leave omitted, previously managed resources in place.
- `routerd delete <kind>/<name>` and `routerd delete -f <router.yaml>` now
  remove the selected resource objects from state and clean up matching
  routerd-owned artifacts from the ownership ledger.
- `routerctl delete <kind>/<name>` now calls the daemon delete endpoint, and
  `routerctl describe orphans` lists routerd-owned orphaned artifacts without
  removing them.
- `routerd serve` now observes WAN Router Advertisements for
  `IPv6PrefixDelegation`, accepts DHCPv6 client hook events over the local
  control API, tracks acquisition phase and stalled-renewal suspicion, and
  exposes those details in `routerctl describe ipv6pd/<name>`.
- Documentation now clarifies that `acquisitionStrategy: hybrid` observes the
  OS client's first Solicit path and only escalates to routerd's raw
  Request-with-claim helper after the retry budget is exhausted.
- `make check-remote-deps` now uses `CONFIG` or the remote router.yaml to make
  optional dependency checks resource-aware, so `pppd` is required only when a
  `PPPoEInterface` is configured and Linux `dhcp6c` is required only when that
  fallback client is selected.
- The NixOS renderer now rejects explicit `client: dhcp6c` because nixpkgs does
  not provide a built-in WIDE dhcp6c package path; NixOS NTT-profile examples
  use the `dhcpcd` default instead.
- `routerctl describe ipv6pd/<name>` now shows DHCPv6 identity, last
  Solicit/Request/Renew/Rebind/Release timestamps, T1/T2, preferred and valid
  lifetimes, and calculated lease deadlines.
- `routerd dhcp6` now supports `solicit` and `rebind` in addition to
  `request`, `renew`, and `release`. Solicit can be sent without a prior
  prefix or server identifier; Rebind omits Server Identifier while preserving
  non-zero IA_PD lifetimes.
- DHCPv6 active-control packets sent by routerd are now summarized into
  `IPv6PrefixDelegation` status as recent transactions so operators can see
  exactly which message, transaction ID, IAID, lifetimes, and warning markers
  were used.
- `routerd serve` now starts a passive DHCPv6 packet recorder for
  `IPv6PrefixDelegation` on supported platforms. The Linux implementation uses
  AF_PACKET to observe UDP 546/547 without binding those ports, and records
  observed transactions into the same status history.
- The passive DHCPv6 recorder now ignores DHCPv6 packets whose Client DUID
  does not match the resource's observed or expected DUID, keeping neighboring
  routers' traffic out of `routerctl describe ipv6pd/<name>`.
- The passive DHCPv6 packet recorder now has a FreeBSD BPF backend, so
  FreeBSD routers can record DHCPv6 transactions without binding UDP 546/547.
- WAN RA observation now uses the FreeBSD BPF backend as well, allowing
  FreeBSD routers to populate `wanObserved.*` and derived HGW Server ID state.
- `IPv6PrefixDelegation.spec.recovery.mode` now controls daemon-side hung
  recovery. The default `manual` mode records warnings only; `auto-request`
  and `auto-rebind` send rate-limited active DHCPv6 packets after hung
  detection and stop after three failed attempts.
- NixOS rendering now uses the same effective IPv6PrefixDelegation client
  default as apply, so omitted NTT-profile clients render `dhcpcd` packages
  and avoid enabling systemd-networkd DHCPv6-PD.
- Switching IPv6PrefixDelegation away from systemd-networkd now writes
  neutralizing networkd drop-ins for the WAN and delegated LAN interfaces, so
  stale `90-routerd-dhcp6-pd.conf` files cannot keep networkd sending DHCPv6-PD
  packets in parallel with `dhcp6c` or `dhcpcd`.
- The systemd-networkd renderer now resolves the same effective
  `IPv6PrefixDelegation` client default as apply. NTT-profile resources with an
  omitted client no longer render networkd DHCPv6-PD blocks; only the
  neutralizing drop-in remains as a stale-file guard.
- `routerd apply` now clears observed DHCPv6 identity fields in
  `IPv6PrefixDelegation` status when the effective client changes, preventing
  stale networkd IAID/DUID values from appearing after a move to `dhcpcd` or
  `dhcp6c`.
- Linux NTT-profile `IPv6PrefixDelegation` now defaults to `client: dhcpcd`,
  including on NixOS. `client: dhcp6c` remains a supported explicit fallback
  for migration and controlled comparison, but new examples should not select
  it by default.
- Breaking: routerd now uses `apply` as the user-facing verb. The old
  `reconcile` CLI and control API actions were replaced by `routerd apply`,
  `routerctl apply`, and `/apply`; the YAML `spec.reconcile` policy name stays
  unchanged.
- Breaking: removed obsolete pre-release DHCPv6-PD workaround fields. DHCPv6
  Renew/Rebind and Release behavior is delegated to the OS client.
- FreeBSD NTT-profile rendering now starts KAME `dhcp6c` with `-n` so service
  restarts do not send DHCPv6 Release while Renew/Rebind timing remains
  delegated to `dhcp6c`.
- Linux `IPv6PrefixDelegation` can now use `client: dhcp6c`. This renders a
  managed WIDE/KAME-style `dhcp6c.conf` and systemd unit so NTT home-gateway
  profiles can avoid systemd-networkd Renew/Rebind packets with zero IA Prefix
  lifetimes.
- `IPv6PrefixDelegation` can now use `client: dhcpcd`. It is the Linux
  NTT-profile default and remains an explicit lab path on FreeBSD. routerd
  renders a per-resource `dhcpcd.conf`, hook placeholder, and either a systemd
  unit or FreeBSD rc.d script.
- Linux DHCPv6-PD client switching now stops stale managed units for the
  previous client, and the generated dhcpcd hook is file-global so dhcpcd 10
  actually invokes routerd's local event reporter.
- Documentation now includes Mermaid diagrams for the NTT HGW state model,
  the routerd DHCPv6-PD acquisition strategy, and the OS/client selection
  matrix, plus updated dhcpcd lab notes.
- `routerd apply` now resolves an omitted `IPv6PrefixDelegation.spec.client`
  from the host OS and profile, supports `--override-client` and
  `--override-profile` for one-shot lab runs, and records known-bad
  OS/client/profile combinations as warnings instead of validation failures.
- `routerd dhcp6 request|renew` can now override requested T1/T2 and IA Prefix
  lifetimes for lab packets. This is used to test whether an upstream DHCPv6-PD
  server honours shorter leases before waiting for a full production T1 cycle.
- `IPv6PrefixDelegation` now has manual `serverID`, `priorPrefix`, and
  `acquisitionStrategy` fields for the DHCPv6 active-controller path. Renderers
  can receive the resource status and prefer explicit spec overrides before
  falling back to observed lease state.
- Added the first DHCPv6 active-controller command path: `routerd dhcp6
  request|renew|release --resource <name>`. Request/Renew packets use fresh
  transaction IDs, non-zero T1/T2 and IA Prefix lifetimes, and Reconfigure
  Accept; Release sends zero lifetimes without Reconfigure Accept.
- FreeBSD apply no longer rewrites `dhcp6c_flags="-n"` on every loop. This
  prevents unnecessary `dhcp6c` restarts and preserves the DHCPv6 client's
  in-memory lease state for natural Renew/Rebind.
- `routerctl` now has kubectl-style `get`, `describe`, and `show` verbs.
  `show` combines desired config, observed host state, ownership ledger data,
  state history, and events; NAPT/conntrack inspection is reported under
  `IPv4SourceNAT` observed state.
- Local state and ownership storage moved to SQLite with Kubernetes-style
  generations, objects, artifacts, events, and reserved access logs. Apply
  generations and events are first-class records, and `routerctl describe
  inventory/host` shows collected OS inventory.
- DHCPv6-PD state is stored in the structured
  `ipv6PrefixDelegation.<name>.lease` object. NTT profiles use MAC-derived
  DUID-LL by default, omit exact prefix hints, suppress DHCPv6 hostname
  sending, and keep `duidRawData` / `iaid` only as explicit operator
  overrides for migration or HA cases. NTT profiles also disable networkd
  DHCPv6 option-use knobs that are not needed for PD where networkd exposes
  them.
- `IPv6RAAddress` now models WAN-side RA/SLAAC separately from DHCPv6-PD so
  DS-Lite AFTR DNS lookups can rely on an upstream IPv6 address and RA default
  route.
- Router diagnostics are now part of the expected host toolset: Linux remote
  checks require `dig`, `ping`, `tcpdump`, and `tracepath`; FreeBSD checks
  require `dig` alongside the base `ping`, `ping6`, `tcpdump`, and
  `traceroute` tools. Host inventory records additional troubleshooting
  commands when present.
- dnsmasq conditional forwarding now renders IPv6 upstream DNS addresses in the
  dnsmasq `server=/domain/addr` form without URL-style brackets.
- Apply now derives delegated LAN IPv6 addresses and DS-Lite tunnel source
  addresses from the current PD state object when available, and removes
  stale routerd-derived IPv6 addresses that share managed suffixes after a PD
  change.
- FreeBSD groundwork now uses KAME `dhcp6c` for DHCPv6-PD, `dhclient` or the
  configured IPv4 DHCP client for IPv4, `mpd5` for PPPoE, and rc.d-managed
  dnsmasq for LAN services. FreeBSD remote install builds the proper target
  binaries and checks required tools.
- Resource ownership and adoption now have a common foundation: resource kinds
  emit artifact intents, the local ledger records owned artifacts,
  `routerd adopt --candidates` lists adoption candidates, `routerd adopt
  --apply` records matching candidates, and apply reports or cleans known
  orphaned artifacts.
- Networking features added in the current model include DS-Lite tunnels,
  PPPoE, IPv4 source NAT, IPv4 default route policy with route-set candidates,
  path MTU and TCP MSS policy, reverse-path filter resources, health check
  roles, minimal firewall resources, NTP client configuration, log sinks,
  dnsmasq-backed DHCP/DNS with explicit listen interfaces, and safer DHCPv6
  client firewall handling.
- NixOS rendering groundwork can emit host settings, systemd-networkd links,
  packages, persistent sysctl values, reverse-path firewall relaxation for
  router hosts, and an optional local `routerd.service`.
- Added the Docusaurus documentation site for routerd.net with English and
  Japanese content.

## 0.1.0 planning baseline

- Initial resource model for interfaces, static IPv4, DHCP stubs,
  plugins, dry-run, status JSON, and the systemd service layout.
