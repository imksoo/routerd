---
title: Changelog
---

# Changelog

routerd のリリース履歴です。形式は [Keep a Changelog](https://keepachangelog.com/) に準拠します。
routerd は `vYYYYMMDD.HHmm` 形式の日付と時刻に基づく版番号を使います。
ソフトウェアは v1alpha1 段階のため、リリース間で破壊的変更を含むことがあります。

## Unreleased

### 追加

- `mode: vrrp` の `VirtualIPv4Address` に FreeBSD CARP backend を追加しました。
  runtime controller、rc.d rendering、validation、tests、最小構成の
  `examples/freebsd-vrrp.yaml` を含みます。
- ingress/local router service の listen-port collision validation と、
  Linux nftables 向けの `IngressService` `sourceHash` / `random` backend
  distribution を追加しました。
- FRR BGP の connected/static redistribution、BGP community の send/accept/set
  policy、観測 community の status 解析、
  `examples/lan-advertise-with-community.yaml` を追加しました。
- VRF-backed FRR BGP instance による multi-instance `BGPRouter` support、
  listen-address collision validation、router ごとの observed status、
  `examples/multi-instance-bgp.yaml` を追加しました。
- FRR 管理の BGP peer 向け BFD support、FRR `bfdd` daemon rendering、
  BGP watcher tuning field、BFD status observation、
  `examples/bgp-bfd.yaml` を追加しました。
- transit routing 用の BGP export policy allow-list と、`BGPRouter` がある場合の
  FRR `bgpd` daemon 自動 enable を追加しました。
- Kubernetes の Pod / Service CIDR static route 向け `ClusterNetworkRoute`
  helper と、BGP peer password / VRRP-CARP authentication 用の
  `passwordFrom` / `authenticationFrom` secret source を追加しました。
- 一時的な `IngressService` backend maintenance 用の `routerctl drain` /
  `undrain` と、VRRP production tuning documentation および
  `examples/vrrp-tuning-presets.yaml` を追加しました。
- BGP / VRRP / IngressService の Web Console 運用ページに SSE 更新、
  filter 付き event log、軽量なローカル SVG metric trend を追加しました。
- stateful firewall rule expression として ICMP / ICMPv6 type、送信元 /
  宛先の複数 port match、nftables rate limit、送信元ごとの connection
  limit を追加しました。
- IPv4/IPv6 unicast の dual-stack BGP rendering / observation、
  `VirtualIPv6Address` による VRRPv3/CARP VIP support、AAAA record 自動派生、
  dual-stack BGP / Kubernetes API VIP example を追加しました。
- OTLP environment rendering と stdout / syslog / Loki への内蔵 routerd event
  forwarding 用の `ObservabilityPipeline`、および apply/controller mutation を
  file lease で gate する `RouterdCluster` を追加しました。
- Alpine/OpenRC 向け VRRP apply support を追加しました。`routerd apply --once`
  が keepalived config を render し、OpenRC の `keepalived` service を管理し、
  live address から観測した VRRP role を status に保存します。Alpine 向け
  Kubernetes VIP example も追加しました。
- Alpine Live ISO の経路を改善し、VRRP controller の既定を live にし、
  `routerctl show vrrp` は live address から role を再観測します。version
  output には commit を埋め込めるようにし、FRR reload tooling dependency と、
  非 blocking の setup wizard 動作も追加しました。
- live VRRP reconcile で keepalived の no-op reload/restart を避け、
  最後に keepalived を reload/restart した時刻と理由を controller status に
  出すようにしました。
- `routerd apply --once` の VRRP 処理を daemon mode と同じ controller
  reconcile 経路へ寄せ、keepalived reload/restart status fields が一貫して
  保存されるようにしました。
- IngressService の live nftables apply を独立 NAT44 dry-run mode から分離し、
  hostname の DNSZone coverage は warning に緩和しました。外部 DNS 管理の名前は
  `externalDNS` で自動公開と warning を抑止できます。
- IngressService の同一 interface hairpin SNAT と forwarding 用 runtime
  `ip_forward` sysctl を自動適用し、`routerctl show ingress --verbose` で
  forwarding、nftables、conntrack の dataplane 状態を確認できるようにしました。
- listen interface prefix が YAML に無い Live ISO 風構成でも、
  private `/24` 内の IngressService listen/backend address は
  `hairpin.mode: auto` で hairpin が必要と判定するようにし、verbose ingress 出力は
  期待される nftables SNAT が無い場合に warning を出すようにしました。
- systemd、OpenRC、rc.d、NixOS の service artifact 名と lifecycle command を扱う
  `pkg/servicemgr` abstraction を追加し、service artifact intent generation を
  そこへ寄せて resource ごとの OS switch drift を減らしました。
- すべての checked-in example config について Linux、Alpine/OpenRC、
  FreeBSD/rc.d、NixOS の render snapshot を固定する golden test と、
  netns 側の compatibility wrapper を追加しました。`pkg/servicemgr` には lifecycle
  hook を追加し、FRR の config-check + live reload、keepalived の reload/restart
  分離、signal-based daemon reload が generic restart に潰れないようにしました。
- bespoke lifecycle command の golden test と `make check-bespoke-lifecycle`
  gate を追加しました。FRR live reload、keepalived no-op/reload、dnsmasq
  SIGHUP、DHCP daemon IPC、BFD daemon enablement、IngressService の nftables-only
  backend rotation、VRRP track artifact、DS-Lite dataplane hook、DHCP event daemon
  ordering、FRR graceful-restart observation を固定します。
- nftables / pf の render・diff・reload 経路向けに、挙動変更なしの
  firewall backend abstraction を追加しました。nftables の `ct state`、`jhash`、
  `numgen`、hairpin conntrack expression と、pf の `rdr`、`nat-anchor`、
  hairpin NAT syntax を regression contract で固定します。
- netplan、systemd-networkd drop-in、NixOS module、FreeBSD rc.conf fragment
  向けに、挙動変更なしの network config backend abstraction を追加しました。
  IPv4/IPv6 address と route は共通 declaration として扱います。
- PPPoE、VRRP/CARP、FRR、dnsmasq、DHCPv6 PD、DNS resolver、Tailscale の
  service-backed artifact intent を ServiceManager declaration table に整理し、
  systemd/OpenRC/rc.d/NixOS の ownership が出力変更なしで揃うようにしました。
- firewall hole derivation と OS 別 interface/network artifact の render golden
  coverage を拡張し、Linux の netplan/systemd-networkd output と Alpine の
  nftables snapshot も固定しました。
- abstraction layer regression coverage を強化し、cross-OS semantic test、
  invalid spec check、firewall backend error propagation の status/event、
  edge-case declaration、race-tested reload、80% coverage gate、4 OS の
  bespoke lifecycle command matrix を追加しました。

## v20260519.0743

### 変更

- 公開 documentation と example configuration の名前を整理し、内部 lab の
  hostname、domain、management network address が website や再利用用 example
  ではなく internal notes に残るようにしました。
- internal design / soak note を公開 Docusaurus docs tree から外し、native nDPI
  と RA/DHCPv6-PD coverage の lab validation policy を `internal/notes/` に
  記録しました。

## v20260519.0713

### 修正

- `routerctl show bgp`、`routerctl show vrrp`、`routerctl show ingress` が
  ownership ledger を開かないようにし、明示した status store を使う場合に
  default ledger path が書き込めない環境でも動くようにしました。

## v20260519.0708

### 追加

- Kubernetes edge 用に、FRR backend の `BGPRouter` / `BGPPeer`、
  keepalived backend の `VirtualIPv4Address`、および `IngressService`
  backend health/failover controller を追加しました。
- `routerctl show bgp`、`routerctl show vrrp`、`routerctl show ingress` の
  table view、VIP/ingress の `hostname` field からの DNS record 自動派生、
  BGP/VRRP/Ingress の transition と backend health 用 OTel metrics を追加しました。
- Web Console に BGP、VRRP、IngressService の dedicated view と JSON endpoint を追加しました。

### 変更

- FRR BGP 設定は `vtysh -C -f` で検証し、`frr-reload.py --reload` で
  差分適用します。VRRP は unicast peer と `nopreempt` を既定にし、
  track hysteresis と `preemptDelay` を扱います。BGP、VRRP、IngressService
  listen port の firewall hole も自動派生します。
- BGP reconcile では dry-run の書き込みが後続の live apply を隠さないようにし、
  初回 live 観測時は FRR running-config を比較してから reload するため、
  既に一致している session を no-op reload で reset しません。

## v20260518.1810

### 追加

- native nDPI classification を有効化する host 向けに、別 archive
  `routerd-ndpi-agent-libndpi-linux-amd64` を追加しました。通常の Linux
  release archive は完全な静的 binary のまま維持し、optional な nDPI agent
  override は `CGO_ENABLED=1 -tags libndpi` で build して libndpi self-test で
  検証します。

## v20260518.1431

### 追加

- controller reconcile の runtime status を control API、log、OpenTelemetry
  metrics/traces、Web Console の controller view に追加しました。controller
  status は interval、trigger、実行回数、error 回数、last/average/max duration、
  最新 error を返します。

## v20260518.1301

### 変更

- 現在の controller-chain 設定 path では使われなくなった dead compatibility
  helper と旧 raw systemd unit renderer を削除しました。

## v20260517.2339

### 追加

- 番号付きの構成図、図と YAML の対応 comment、安全上の注意、検証済み sample
  YAML を含む「設定事例集」セクションを追加しました。基本的な IPv4 NAT、
  LAN DHCP/DNS、DS-Lite、PPPoE、port forwarding、guest 分離、multi-WAN
  failover、local DNS redirect、Tailscale、WireGuard、telemetry export の
  パターンを用意しました。
- IPv4 route policy resource から参照される health check は、参照元の route
  candidate または target から socket mark を導出するようにしました。単体 probe
  用の `spec.fwmark` は引き続き利用でき、明示 mark と導出 mark が衝突する設定は
  validation で拒否します。

### 変更

- Linux upgrade では、routerd helper systemd service が削除済みの旧 binary を
  実行している場合、または unit file が helper process の起動後に再生成された場合に
  限って helper を更新するようにしました。installer はその判定前に
  `routerd.service` と routerd 管理 unit file の反映が落ち着くのを待ちます。
- release installer は NixOS で host service manager の変更を行わないようにしました。
  これにより、`/etc/systemd/system` が読み取り専用で service unit を宣言的に管理する
  host でも archive からの binary 更新が失敗しません。
- conntrack procfs file が host に存在しない場合、conntrack observation は interval
  ごとに warning を出す代わりに `Unavailable` status を記録するようにしました。
- FreeBSD の `--skip-service-manager` apply は、生成 helper、管理対象 dnsmasq、
  pf/pflog service activation の rc.d/service 操作を抑止するようにしました。
  その一方で、rc.conf による network state と直接の `pfctl` rule loading は継続します。
  recovery や bootstrapping の経路が base rc boot sequence と競合することを避けます。
- FreeBSD upgrade では、config 管理の `routerd` rc.d script を generic bootstrap
  template で置き換えず保持するようにしました。Linux で config 管理の
  `routerd.service` を保持する挙動と揃えています。
- `routerd serve` は SIGTERM/SIGINT を受けたときに control socket と status socket を
  clean shutdown するようにしました。FreeBSD の `daemon(8)` 配下で rc.d restart する際、
  強制 KILL に進まず停止できます。
- routerd state SQLite database は、既存の busy timeout と併せて WAL mode を使うように
  しました。status reader と controller が重なったときの一時的な `SQLITE_BUSY` を
  減らします。

## v20260517.1808

### 修正

- Debian/Ubuntu 用の release installer は、完全な `dnsmasq` package ではなく
  `dnsmasq-base` を導入するようにしました。distro 側の `dnsmasq.service` が
  有効化され、routerd 管理の dnsmasq instance と競合することを避けます。

## v20260517.1800

### 修正

- controller と helper probe からの一回完結の HTTP-over-Unix 呼び出しは
  keep-alive を無効化し、idle transport を明示的に閉じるようにしました。
  定期的な status polling により、`routerd`、health check helper、DHCP client、
  DNS/DPI helper service に多数の確立済み Unix socket が残り続けることを防ぎます。

## v20260517.1533

### 修正

- release helper は、schema check の前に管理対象の config schema と control API
  schema を再生成するようにしました。API type の変更が release の終盤で失敗する
  代わりに、release commit に含まれます。
- `routerctl` は daemon 起動直後の一時的な Unix socket 接続失敗を、読み取り専用
  control API request に限って retry するようにしました。`routerctl status` は
  既定で別の読み取り専用 status socket を使い、apply と delete は引き続き権限付き
  control socket だけを使い、retry しません。

## v20260517.1510

### 追加

- Web Console の Connections で、`LocalServiceRedirect` により処理された flow
  に印を付けるようにしました。live conntrack tuple と解決済み set status から
  判定できる場合は、redirect rule と宛先 `IPAddressSet` も表示します。
- Web Console の Firewall で、deny log row の宛先 `IPAddressSet` match を表示する
  ようにしました。明示的な `FirewallRule.destinationSetRefs` による match と、
  現在設定済み set に含まれている宛先を区別して表示します。

## v20260517.1401

### 修正

- `syscall.Statfs_t` の block counter が signed integer type になる FreeBSD でも
  Web Console の disk usage collection が compile できるようにしました。

## v20260517.1353

### 修正

- release helper は、最初の release section が `Unreleased` ではない changelog を
  拒否するようにしました。また、以前の helper 実行で残っていた空の release
  見出しを、管理対象の changelog file から削除しました。

## v20260517.1351

### 変更

- `routerd-dpi-classifier` に明示的な classifier engine facade を追加しました。
  既定は built-in parser で、`auto` / `ndpi-agent` mode では将来の
  `routerd-ndpi-agent` Unix socket service に問い合わせ、失敗時は built-in
  parser に fallback できます。
- Web Console の Connections で、DPI が flow を識別できない場合でも、
  TCP port 4317 を OTLP、TCP port 4318 を OTLP/HTTP として表示するようにしました。
- Web Console の Overview に host の CPU、memory、root filesystem 使用率と
  classifier 側の DPI processing latency を表示し、router 内部の負荷悪化を
  routing や DPI の状態と並べて確認できるようにしました。
- Web Console の Clients と Connections を相互に移動しやすくしました。
  client 行からその client の観測アドレスで絞り込んだ Connections を開け、
  connection 詳細から対応する local client identity に戻れます。
- Web Console の Connections で Clients snapshot を作る際に、直近の traffic-flow
  観測も読み込むようにしました。これにより、IPv6 privacy address でも client に
  戻せる可能性が上がります。また、source endpoint では既知の identity にまだ
  統合されていないアドレスでも Clients 検索に移動できます。
- Web Console の検索入力に、文字が入っているときだけ表示されるクリアボタンを
  追加しました。
- release helper は clean な working tree からだけ実行するようにし、空の tag
  見出しを作る代わりに、現在の `Unreleased` の内容を release tag へ昇格するように
  しました。

### 追加

- `IPAddressSet` と `LocalServiceRedirect` を追加しました。`IPAddressSet` は
  直接指定した IPv4/IPv6 address と FQDN の `A`/`AAAA` record を、再利用可能な nftables
  named set に解決できます。`LocalServiceRedirect` は、その set 宛てに LAN
  client から出る平文 DNS/NTP 通信を router の local service へ redirect できます。
  DoH/DoT や router 自身が発信する health check は対象にしません。
- `FirewallRule`、`NAT44Rule`、`IPv4PolicyRoute`、`IPv4PolicyRouteSet` が
  `destinationSetRefs` と `excludeDestinationSetRefs` で `IPAddressSet` を参照できる
  ようになりました。FQDN-backed な address set を firewall filtering、NAT の適用範囲、
  IPv4 policy routing の条件として再利用できます。
- runtime の `IPAddressSet` refresh controller を追加しました。参照されている
  nftables set は DNS TTL に基づいてその場で更新します。観測した最小 TTL の半分を
  基本にし、60 秒より短くせず、必要に応じて `refreshInterval` で上限を指定できます。
  firewall、NAT、policy table 全体を reload せず、FQDN-backed set を新しい状態に保てます。
- optional command として、初期版の `routerd-ndpi-agent` service boundary を追加しました。
  既定の build は libndpi backend が利用不可であることを報告し、`-tags libndpi`
  build では同じ IPC surface の背後で native library に link します。
- `routerd-ndpi-agent` が flow ごとの観測 state を持つようにしました。
  flow TTL、flow 数上限、先頭 payload packet 数上限と、observed、classified、
  unknown、skipped、error、pruned packet の status counter を持ちます。
- `routerd-ndpi-agent` 向けの初期 libndpi backend を追加しました。`libndpi`
  build tag で opt-in し、native flow state を agent 内に閉じ込めたまま、
  firewall logger から届く full packet observation を分類できます。
- libndpi development files が入っている環境で optional native backend を build
  するための `make build-ndpi-agent-libndpi` target を追加しました。
- `routerd-dpi-classifier` が `--engine auto` または `--engine ndpi-agent`
  で設定されている場合に、systemd、OpenRC、FreeBSD rc.d、NixOS で
  `routerd-ndpi-agent` を render するようにしました。
- DPI flow と traffic flow の record が、従来の app label に加えて、
  detected protocol、application protocol、category、confidence、risk、
  metadata などの typed classifier field を保存するようにしました。
- `routerd-dpi-classifier` の status が、daemon で処理した classify request の
  average latency と maximum latency を報告するようにしました。

### 修正

- Linux の upgrade 時に、差し替え前の削除済み binary を実行し続けている
  routerd helper の systemd service があれば、`install.sh` が再起動するようにしました。
- nDPI agent の結果が application を識別していても TLS SNI、HTTP Host、DNS query
  などの detail を持っていない場合、`routerd-dpi-classifier` が built-in parser
  の有用な hint を保持するようにしました。
- DPI helper daemon が Unix socket を bind するとき、socket ではない path を
  誤って unlink しないようにしました。また `routerd-ndpi-agent` は native
  libndpi state を明示的に close します。
- Web Console の traffic-flow 読み取りは、writer が schema migration を行う前の
  legacy SQLite file に新しい DPI column がない場合でも成功するようにしました。

## v20260516.2302

### 変更

- Web Console の Connections で、source から destination への経路を固定幅の
  route column に揃え、state、protocol、provider、traffic、timeout などの
  metadata を別の badge 領域に分けました。
- Web Console の connection label は、transport/application identity と
  destination provider を分けて表示するようにしました。
  `google-https` のような旧 provider 固有 label は `TLS` に正規化し、
  Google、AWS、Microsoft、Apple、Cloudflare は別の destination provider badge
  として表示します。
- `https` などの destination service 名は、connection row に追加情報を与える場合、
  protocol badge として表示するようにしました。

### 修正

- 展開した connection detail で、destination service と provider の badge が
  detail column 全体に伸びず、内容幅のまま表示されるようにしました。
- 展開した connection detail で、source と destination の identity text が
  compact row 用の幅で省略されず、利用可能な幅を使って折り返すようにしました。
- Connections の `Showing` metric で、API の取得上限により row が打ち切られた場合に、
  filtered rows、loaded rows、総 conntrack count を区別して表示するようにしました。

## v20260516.2155

### 変更

- Web Console の Connections は、観測された転送 byte 数の降順を既定の並び順にしました。
  Connections の sort menu に `Traffic` を追加し、connection card には合計 byte 数を、
  詳細表示には conntrack accounting が使える場合の outbound、inbound、total counter を表示します。
- Web Console の connection 件数上限を適用するとき、conntrack observer は
  family/protocol group ごとに byte 数の大きい entry を優先します。
  低 traffic の entry に押し出されて、大きな active flow が隠れにくくなります。

## v20260516.1413

### 修正

- `routerd apply --dry-run` と関連する planning path で、存在しない SQLite ownership
  ledger を空の in-memory ledger として扱うようにしました。
  権限のない CI runner 上で `/var/lib/routerd` を作成しようとして失敗しません。

## v20260516.1405

### 追加

- `firewall.routerd.net/v1alpha1` に `PortForward` と単一 backend の
  `IngressService` を追加しました。WAN 側 IPv4 TCP/UDP ingress DNAT を表せます。
- Linux nftables と FreeBSD pf の rendering で、これらの ingress service を公開できるようにしました。
  任意の hairpin NAT も生成でき、LAN クライアントが WAN アドレス経由で同じ port forward
  service へ到達できます。
- 新しい ingress NAT resource 向けに、生成 JSON Schema、CLI alias、API documentation、
  resource ownership documentation を追加しました。

## v20260516.0804

### 変更

- Web Console の Connections は、DPI application ごとに表を分けるのではなく、
  IP family と transport protocol の固定 bucket で active flow をまとめるようにしました。
  TLS、DNS、QUIC などの app label は各 group 内の表示として残ります。

## v20260514.1433

## v20260514.0813

### 修正

- Web Console の Clients で、IP address ベースの DNS、traffic、firewall、DPI、
  DHCP fingerprint 情報を、現在の DHCP lease と突き合わせる前に直近 1 時間の
  observation window に揃えるようにしました。
- client inventory では sticky DHCP lease annotation に active hold だけを使うようにし、
  古い lease history が現在の endpoint identity 判定に混ざらないようにしました。

## v20260514.0743

### 修正

- Web Console の Clients で、期限切れの dnsmasq lease を無視するようにしました。
  古い host が無期限に残り続けません。
- DHCP lease の統合では、まず有効期限が新しい lease を優先し、lease file の
  設定順は同条件の場合の tie-breaker としてだけ使います。
- routerd は controller-chain の dnsmasq lease file を Web Console に先頭候補として渡します。
  これにより、管理対象 dnsmasq が実際に使う lease file に沿って表示します。

## v20260514.0654

### 修正

- Web Console の Overview で、初回の軽量 snapshot を 0 値の metric sample として
  記録しないようにしました。
- Overview の遅延 refresh は、必要な resource、event、conntrack、DNS、最近の
  traffic flow を取得します。一方で、重い firewall、VPN、client inventory の処理は
  引き続き避けます。
- Overview card は、まだ取得していない flow / connection data を 0 と見せず、
  loading state として表示します。

## v20260514.0037

### 修正

- DHCPv4 の LAN domain rendering で、明示的な domain-search option がない場合は `domain` / `domainFrom` から domain-name と domain-search の両方を生成するようにしました。

## v20260514.0025

### 追加

- `domainFrom`、`dnsslFrom`、`domainSearchFrom` を追加しました。
  DHCPv4、IPv6 RA、DHCPv6 で LAN の suffix を広告するとき、
  ローカルドメイン文字列を重複して書かず `DNSZone/<name>.zone` を参照できます。

## v20260513.2358

### 変更

- 長時間動き続けるイベント処理を堅牢化しました。
  `EventRule` と `DerivedEvent` の timer は発火後に map から取り除かれ、
  古い timer callback を無視し、共有状態を controller の lock で保護します。
- `EventRule` の相関状態に上限を設けました。
  高カーディナリティのイベント列でも、メモリ使用量が無制限に増え続けません。
- daemon の `events.jsonl` は追記し続けるのではなく、一定サイズで
  ローテーションするようにしました。
- local control、daemon event、DNS resolver、DoH、classifier の経路に
  request / response サイズ上限を追加しました。
  local daemon server と Web Console には HTTP header timeout も追加しています。

### 修正

- `DerivedEvent` の hysteresis 中に、timer callback と reconcile が
  pending transition state を同時に更新し得る race を修正しました。

## v20260513.2317

### 変更

- `v20260513.2252` の堅牢化に合わせて、本番環境での reconcile に関する
  ドキュメントを更新しました。
  operations、upgrade、state ownership、各言語の changelog で、実機状態の
  drift 確認、管理対象構成物の掃除、nftables named set の更新、
  設定で管理される `routerd.service` の upgrade 時の扱いを説明しています。

## v20260513.2252

### 変更

- 本番環境での reconcile を堅牢化しました。
  controller は処理を省略する前に、状態データベースだけでなく実機状態も確認します。
  対象には systemd unit、dnsmasq、DHCPv4 lease アドレス、
  route-policy の nftables table、NAT44、関連する管理対象構成物が含まれます。
- health check の `fwmark` を、生成する systemd unit、socket 設定、status の観測値、
  OpenTelemetry 属性まで通すようにしました。
  probe が、検査対象の経路と同じ policy-route mark を使えます。
- Linux firewall の rendering で、routerd が管理する named set を再定義前に
  消すようにしました。
  zone interface や client-policy の MAC アドレスを削除したときに nftables 上へ残らず、
  filter table 全体を destroy せずに再読み込みします。
- release installer は、設定で管理されている `routerd.service` を
  archive の template で上書きせず保持します。
  routerd が自分自身の unit を管理している場合、unit file の変更時は
  `systemd-run` で少し遅らせた self-restart を予約します。

### 修正

- YAML から消えた `HealthCheck` に対応する古い `routerd-healthcheck@*.service` を
  削除するようにしました。
- NAT rule が 0 件になったとき、管理対象の NAT44 table または pf anchor を
  空にするようにしました。
- status では DHCPv4 lease アドレスが存在すると見えていても、実際の
  インターフェースから消えている場合は再適用するようにしました。
- 設定内容が空の `WireGuardPeer` は、誤解を招く Pending ではなく
  `NotConfigured` として表示するようにしました。

## v20260513.1931

### 修正

- health check による経路切替の挙動を安定化しました。

## v20260513.1153

### 修正

- controller reconcile の冪等性を安定化しました。

## v20260513.0836

### 追加

- WireGuard mesh controller を追加しました。

## v20260513.0727

### 変更

- home-router の UDP conntrack timeout 設定を引き上げました。

## v20260512.0037

### 追加

- conntrack observer から DPI flow metrics を出力するようにしました。

## v20260512.0032

### 追加

- Web Console Overview に DPI summary card を追加しました。

## v20260512.0027

### 追加

- Web Console Clients ページに DPI activity summary を追加しました。

## v20260512.0008

### 追加

- Web Console Connections ページに DPI classification を表示するようにしました。

## v20260511.2357

### 変更

- forward flow へ DPI enrichment を広げました。

## v20260511.2307

### 修正

- Web Console の横方向 overscroll を抑制しました。

## v20260511.2300

### 修正

- Firewall timeline の横スクロールを修正しました。

## v20260511.2253

### 変更

- Web Console を content-driven な layout section へ整理しました。

## v20260511.2217

### 検証

- mobile Web Console layout を検証しました。

## v20260511.2211

### 変更

- Web Console の page state を画面遷移後も保持するようにしました。

## v20260511.2154

### 変更

- Clients inventory view を整理しました。

## v20260511.2145

### 追加

- Web Console SSE reconciliation を追加しました。

## v20260511.2130

### 追加

- client fingerprint inference を追加しました。

## v20260511.2106

### 変更

- 期限切れ conntrack return flow の相関を取るようにしました。

## v20260511.2045

### 変更

- firewall deny event に DPI context を付与するようにしました。

## v20260511.2018

### 検証

- DPI classifier の OS parity を検証しました。

## v20260511.1846

### 修正

- Web Console の時刻 locale を英語に固定しました。

## v20260511.1840

### 追加

- 分離した DPI classifier proof of concept を追加しました。

## v20260511.1820

### 追加

- Connections protocol summary を追加しました。

## v20260511.1709

### 修正

- release artifact の checksum を修正しました。

## v20260511.1428

### 変更

- Web Console の navigation section を改善しました。

## v20260511.1240

### 変更

- controller mode reason の表現を調整しました。

## v20260511.1041

### 追加

- dry-run controller の可視性を高めました。

## v20260511.1017

### 変更

- controller の dry-run mode を明示的に表示するようにしました。

## v20260510.1956

### 変更

- `NetworkAdoption` が resolved DNS を管理できるようにしました。

## v20260510.1811

### 追加

- PVE live ISO のシリアルコンソール検証ログを `internal/notes/` に追加しました。
  walkthrough の画面キャプチャと実行ログを、test evidence として同じ release に残します。

## v20260510.1802

### 変更

- 日本語、簡体字中国語、繁体字中国語のディスクレス mini PC walkthrough に、
  PVE live ISO boot test で取得した実際の画面キャプチャを埋め込みました。
- ディスクレス mini PC walkthrough に残っていた古い placeholder 画像参照を削除しました。

## v20260510.1750

### 追加

- ディスクレス mini PC walkthrough に、PVE live ISO 実機検証で取得した
  画面キャプチャを追加しました。
- 簡体字中国語版と繁体字中国語版に、位置づけ、USB 永続化、
  法務と再配布の不足ページを追加しました。

### 変更

- website footer の著作権表示を、著作権表示を先に置く慣習的な形式へ変更しました。
- ディスクレス mini PC walkthrough の PVE 例を、VGA と serial console の
  両方を有効にする構成へ更新しました。これにより、QEMU screenshot と
  `qm terminal` 検証を同じ実行で取得できます。

### 修正

- live ISO の設定ウィザードで、DHCPv4 pool の既定値を選択した LAN
  アドレスの prefix から導出するように修正しました。
- PVE live ISO boot test を再実行し、
  `/tmp/iso-boot-test-20260510-1742.log`、QEMU screenshot、routerd apply、
  Healthy status、USB persistence flush まで確認しました。

## v20260510.1722

### 追加

- routerd の Go ソース、インストーラースクリプト、プラグインスクリプト、
  Web Console ソースに BSD 3-Clause の SPDX 識別子を追加しました。
- README にライセンスバッジを追加し、英語版と日本語版 README から
  BSD 3-Clause License へリンクしました。
- 公開ドキュメントに貢献ガイドを追加し、ドキュメントの sidebar から
  辿れるようにしました。
- SECURITY にメールと GitHub Security Advisories の報告先を明記しました。

### 変更

- repository root の `LICENSE` にある著作権表示を
  `Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors`
  に統一しました。
- SPDX ヘッダーが routerd ソースファイルだけに適用されることを
  法務ドキュメントに明記しました。同梱する第三者ソフトウェアは
  `THIRD_PARTY_LICENSES.md` に記載された個別ライセンスに従います。
- README から製品比較表を削除し、routerd 自身の対象範囲と特徴を説明する
  記述に整理しました。

## v20260510.1626

### 追加

- 公開ドキュメントに法務と再配布ページを追加し、release checklist を整理しました。
- 生成される第三者ライセンス一覧に Go module source URL を追加しました。
- BSD routerd binary と aggregate live ISO distribution model の内部 license audit note を記録しました。

## v20260510.1612

### 追加

- Go module とライブ ISO で使う Alpine package の第三者ライセンス一覧を自動生成できるようにしました。
- release archive とライブ ISO にライセンス通知を同梱する場所を追加しました。
- routerd 本体の BSD 3-Clause License と、ライブ ISO の aggregate distribution としての扱いを文書化しました。

## v20260510.1547

### 追加

- routerd 自身の対象範囲と deployment spectrum を中心に、公開向けの位置づけ説明を広げました。
- Intel NUC、N100 mini PC、Raspberry Pi 5、thin client、Proxmox VM の hardware compatibility を拡充しました。
- 中国語の hardware compatibility ページを追加し、ライブ ISO と USB 永続化の流れを明確にしました。

## v20260510.1534

### 追加

- ディスクレス mini PC walkthrough の図、tutorial index、field-note blog post を追加しました。

## v20260510.1508

### 追加

- USB persistence の運用ドキュメントと live ISO の USB persistence support を追加しました。

## v20260510.1451

### 追加

- contribution、security、license、positioning、hardware compatibility、diskless mini PC の各ドキュメントを追加しました。

## v20260510.1429

### 追加

- Alpine live ISO build と install documentation を追加しました。

## v20260510.1412

### 追加

- live ISO validation note と、live ISO 経路の installer documentation を追加しました。

## v20260510.1354

### 修正

- Alpine 上の live ISO runtime apply を修正しました。

## v20260510.1310

### 追加

- live ISO の serial console support を有効にしました。

## v20260510.1301

### 変更

- release tag を JST timestamp 形式へ切り替えました。

## 20260510.4

### 修正

- live ISO overlay archive path を修正しました。

## 20260510.3

### 修正

- Alpine live ISO の release discovery を修正しました。

## 20260510.2

### 追加

- Alpine ベースの live ISO packaging を追加しました。

## 20260510.1

### 追加

- installer configuration wizard を追加しました。

## 20260510.0

### 変更

- fixed-download-asset release の後続として、20260510 release series を開始しました。

## 20260509.16

### 追加

- 版番号付きアーカイブに加えて、`routerd-linux-amd64.tar.gz` のような固定名 alias をリリースアーカイブに追加しました。
- 固定名アーカイブと `.sha256` ファイルを GitHub Releases に配置します。これにより、ドキュメントで `releases/latest/download/...` URL を使えます。

### 変更

- クイックスタートのドキュメントを、固定された latest download URL に変更しました。
- release workflow で、対応している GitHub JavaScript actions が Node.js 24 runtime を使うようにしました。

## 20260509.15

### 追加

- branch push と pull request 用の `CI` GitHub Actions workflow を追加しました。
- CI workflow は Ubuntu 上で `go test ./...`、schema 確認、example 検証、Web サイト生成を実行します。
- ローカル commit の前に Go テストと schema 確認を実行する任意の `scripts/pre-commit.sh` hook を追加しました。
- CI、pre-commit 確認、tag で起動する release workflow の役割分担を説明する開発ドキュメントを追加しました。

## 20260509.14

### 検証

- Ubuntu lab ルーターで `ClientPolicy` ゲストモードを検証しました。
- Linux nftables で、include mode のゲスト MAC アドレス集合、ゲスト向け DNS/DHCP/NTP 許可、自己隔離、RFC 1918 / ULA 拒否規則が生成されることを確認しました。
- exclude mode は、nftables 生成テストで確認しました。

## 20260509.13

### Added

- ゲストモードガイドを詳細化しました。ユースケース、内部実装、`ClientPolicy` の全フィールド、確認手順、トラブルシューティング、セキュリティ上の限界を追加しました。
- include mode、exclude mode、複数ゲスト端末、カスタム拒否・許可リスト、ローカル探索サービス、IoT 固定割り当ての例を追加しました。
- `ClientPolicy.spec.guestServices` で、`dhcp`、`dns`、`ntp` に加えて `mdns` と `ssdp` を指定できるようにしました。

## 20260509.12

### Added

- `ClientPolicy` を追加しました。Linux nftables で LAN 端末を MAC アドレスごとに分類するゲストモードです。
- ゲスト端末は DNS、DHCP、NTP を使えます。プライベート IPv4 宛てと ULA IPv6 宛ての通信は既定で拒否します。
- `examples/guest-mode.yaml` と、include mode / exclude mode の分類方法を説明するドキュメントを追加しました。

### Changed

- FreeBSD pf では `ClientPolicy` を明示的に未対応として扱います。pf は同じ MAC ベースの routed filtering モデルを持たないためです。

## 20260509.11

### Added

- 最小 Tailscale mesh 参加、WireGuard hub-spoke 経路、VRF lab、multi-WAN home fallback の用途別 example を追加しました。
- 各 example の用途を説明する `examples/README.md` を追加しました。

### Changed

- `make validate-example` が `examples/` 配下の全 YAML ファイルを検証するようにしました。

## 20260509.10

### Added

- Web Console の Overview に、世代、リソース phase、HealthCheck 状態の簡易時系列チャートを追加しました。
- Config 画面で、現在の YAML ファイルと最新適用世代を比較できるようにしました。`routerd apply` の前に差分を確認できます。
- Resource テーブルで、kind、name、phase、詳細の検索、phase 絞り込み、検索結果の強調表示ができるようにしました。
- VPN 画面に Tailscale と WireGuard の peer 状態を示す視覚サマリーを追加しました。

## 20260509.9

### Added

- リリースアーカイブに `share/doc/TARGET` を含め、`install.sh` がホストの OS と CPU アーキテクチャーを確認するようにしました。
- GitHub Actions で Linux と FreeBSD の `amd64` / `arm64` アーカイブを生成するようにしました。
- release CI で `install.sh` と `uninstall.sh` に `shellcheck` を実行します。

### Changed

- `install.sh --list-deps` の出力を、OS、CPU アーキテクチャー、パッケージマネージャー、パッケージ、確認対象コマンドが分かる形に整理しました。
- PPPoE、RA、IPsec、パケット取得、経路制御、ファイアウォールで使う実用パッケージを依存リストへ追加しました。

## 20260509.8

### Fixed

- zh-Hant と zh-Hans のドキュメントリンクを修正し、翻訳ページが未翻訳のロケール内ページを指さないようにしました。
- 翻訳がそろうまで、概要ページから英語版の正準リファレンスへリンクする形にしました。

## 20260509.7

### Added

- `EgressRoutePolicy` で、DS-Lite 主経路、RA 由来 DS-Lite、PPPoE、WAN 直結の多段フォールバックを表現できるようにしました。
- 宣言的な `Telemetry` リソースと OTLP 環境変数の伝播により、ルーター群へ OpenTelemetry 設定を展開しました。
- DS-Lite の例は、RFC 6333 の B4-AFTR link prefix `192.0.0.0/29` を tunnel 内側 IPv4 送信元として使う形にしました。
- `PPPoEInterface.disabled` と無効化された経路候補により、PPPoE フォールバック定義を YAML に残しつつ、本番 PPPoE セッションの漏れを防げるようにしました。

### Changed

- リリース版番号を `0.x.y` から日付ベースの値へ変更しました。
- `routerd --version`、`routerctl --version`、リリースアーカイブで同じ release tag の値を使うようにしました。
- Linux nftables と FreeBSD pf の NAT44 生成を、インターフェース単位のルールへ寄せました。
- 3-role firewall モデルを Linux と FreeBSD で確認し、service hole を広い zone 全体ではなく、所有する受信インターフェースへ束縛しました。
- FreeBSD pf で `PathMTUPolicy` の TCP MSS clamp を生成できるようにし、Linux nftables とそろえました。
- dnsmasq の RA 生成で、IPv6 RA MTU option により path MTU を配布できるようにしました。

### Fixed

- FreeBSD pf で DHCPv6、WireGuard、VXLAN の service hole が `wan` zone の全インターフェースへ広がる問題を修正しました。
- FreeBSD の NAT artifact を nftables ではなく `pf.anchor/routerd_nat` として報告するようにしました。
- NAT 生成の前に、PPPoE のリソース名を実 OS インターフェース名へ解決するようにしました。

## 0.4.0

### Added

- nftables の暗黙拒否ログを `routerd-firewall-logger` で取り込み、`firewall-logs.db` に保存するようになりました。Linux では `nfnetlink` を直接読み取り、FreeBSD では `pflog` を `tcpdump` 経由で取り込みます。
- Web Console に Connections タブ (実時間 conntrack / pf state)、Clients タブ (DHCP リース + トラフィック統合)、Firewall タブ (拒否ランキング + 時系列テーブル) を追加しました。
- `TailscaleNode` で Tailscale の exit node と subnet router を広告できるようにしました。生成した systemd ユニットで `tailscale up` を実行します。NixOS 向け生成では `services.tailscale` を有効化し、ユニットの `path` も設定します。
- `WebConsole.spec.listenAddressFrom` と `DNSResolver` 系のリスニングアドレスを `Interface/<name>.status.ipv4Addresses` から導出できるようにしました。即値の代わりに参照で書けます。
- conntrack accounting (`net.netfilter.nf_conntrack_acct=1`) を `SysctlProfile/router-linux` 既定値に追加し、`TrafficFlowLog` で `bytesOut` / `bytesIn` を集計できるようにしました。

### Changed

- 実時間コネクション表示の API / CLI を `connections` に統一しました (旧称 `conntrack-snapshot`)。`/api/v1/connections`、`routerctl connections` を使います。IPv6 を含む全ファミリを同じ表で扱います。
- NixOS 向けの宣言的レンダリングを拡張しました。`Package` (NixOS パッケージ宣言)、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` を `routerd render nixos` の出力に統合します。NixOS 上の `Package` は実行時に導入せず、生成された NixOS 設定で管理します。
- `SystemdUnit` から FreeBSD `rc.d` スクリプトを生成できるようになりました (`routerd render freebsd --out-dir`)。

### Fixed

- `IPv6DelegatedAddress` controller が `Link/<name>` の status が空のとき、PD 由来アドレスをホストインターフェースに付与しない問題を修正しました。
- `SystemdUnit` controller が変更のない active unit を毎回再起動する問題を修正しました。

## 0.3.0

### Added

- 宣言的な OS bootstrap リソースとして `Package` と `SysctlProfile` を追加しました。apt、dnf、nix、pkg のパッケージ宣言と、ルーター用途向けの sysctl 推奨値 (`nf_conntrack_max`、socket buffer、TCP/UDP timeout、`ip_forward` など) を 1 つのリソースで適用します。
- `NetworkAdoption` で systemd-networkd の DHCP / RA を YAML から無効化できます。`SystemdUnit` で routerd 自身が unit を render + install + enable できます。
- `routerctl events --limit N --topic X --resource K/N -o json` で sqlite3 不要に bus event を確認できます。
- `routerd plan --diff` で apply 前差分を表示します。
- `DNSResolver` に bootstrap forwarder (RFC1918 内部 DNS を優先しつつ public DNS を予備にする) を追加しました。

### Changed

- 設定ファイル中の `${...status.field}` 文字列参照を、型付きの `*From` フィールドへ整理しました (`addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`dependsOn`)。互換別名はありません。
- controller chain を pure event-loop 型に再構築しました。共通 `framework.FuncController` (Subscriptions + Bootstrap + PeriodicFunc) と `eventedStore` で、状態保存時に必ず `routerd.resource.status.changed` を発行し、下流が再評価する設計です。
- bus event を `slog` 経由で systemd journal へ出力します (`journalctl -u routerd.service -f | grep "routerd event"` で controller の意思決定を追跡できます)。高頻度イベントは debug レベルです。
- 全バイナリを静的ビルドにしました (`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`)。OS 別の依存パッケージ (`dnsmasq-base`、`nftables`、`conntrack`、`iproute2`、`ppp`、`wireguard-tools`、`strongswan-swanctl`、`radvd`、`tcpdump` など) を Ubuntu / NixOS / FreeBSD ごとに整理しました。
- `HealthCheck.sourceInterface` を YAML 上ではリソース名で書き、実行時に OS の interface 名に解決します。

### Fixed

- `SystemdUnit` 同士の `RuntimeDirectory` 競合で再起動時に socket が消える問題を、`runtimeDirectoryPreserve` で declarative に解消しました。
- `SystemdUnit` の `state: absent` を正しく Drifted として検出し、unit 削除を plan に含めるようにしました。
- `SysctlProfile` の observe で型ゆらぎによる不要な drift を抑えました。

## 0.2.0

### Added

- Stateful firewall を導入しました。`FirewallZone`、`FirewallPolicy`、`FirewallRule` で nftables の `inet routerd_filter` table を生成します。
- `EgressRoutePolicy` (旧 `WANEgressPolicy`) に `destinationCIDRs`、`gateway`、`gatewaySource` を追加しました。`HealthCheck` は `via`、`sourceInterface`、`sourceAddress` で probe の送信経路を指定できます。
- DNS サブシステムを再構成しました。`DNSZone` (権威ゾーン定義) と `DNSResolver` (フォワーダー / キャッシュ) に分離し、ローカルゾーン、条件付き転送、DoH / DoT / DoQ、平文 UDP DNS をサポートします。dnsmasq は DHCPv4 / DHCPv6 / RA / 中継に専念します。
- DS-Lite (`DSLiteTunnel`)、PPPoE (`PPPoESession`、`routerd-pppoe-client`)、DHCPv4 client (`routerd-dhcpv4-client`、`DHCPv4Lease`) を追加しました。
- NAT44 (`NAT44Rule`) と conntrack 観測を追加しました。`/proc/net/nf_conntrack` がない環境では sysctl 由来の集計に縮退します。

### Changed

- `WANEgressPolicy` を `EgressRoutePolicy` に改名しました。互換別名はありません。
- DHCP 関連 Kind とバイナリ名を RFC 表記に統一しました (`routerd-dhcpv4-client`、`routerd-dhcpv6-client`)。旧名の互換別名はありません。

## 0.1.0

最初の v1alpha1 実装です。

- DHCPv6-PD クライアント、daemon contract、event bus、controller framework を導入しました。
- DHCPv6-PD → LAN アドレス導出 → DNS 応答までの controller chain を実装しました。
- DHCPv6 情報要求、DS-Lite (試作)、IPv4 経路、RA、DHCPv6 サーバー、`HealthCheck`、`EventRule`、`DerivedEvent` を追加しました。

このバージョン以降、出荷前の整理として API 名や実装方針に大きな変更が入っています。最新の利用方法は `Unreleased` の項目と `examples/` を参照してください。
