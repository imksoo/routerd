---
title: 变更记录
---

# 变更记录

routerd 的版本历程。格式遵循 [Keep a Changelog](https://keepachangelog.com/)。
routerd 使用 `vYYYYMMDD.HHmm` 格式的日期和时间型版本号。
本软件仍处于 v1alpha1 阶段，版本之间可能含有破坏性改动。

## Unreleased

## v20260521.0731

### 修正

- standard release archive 只包含 static fallback 版 `routerd-ndpi-agent`
  时，如果已有 native `routerd-ndpi-agent` 的 `selftest` 返回
  `libndpiLoaded: true`，installer 会保留该 native agent。`install.sh
  --with-ndpi` 现在也会在最终 agent 未返回 `libndpiLoaded: true` 时失败。
- 当 `spec.includeApplicationLayer: true` 但 nDPI agent 未加载 native
  `libndpi` backend 时，`TrafficFlowLog` 会以
  `TrafficFlowApplicationLayerUnavailable` reason 显示为 `Pending`。
- 将派生的 `routerd_mss` nftables table 注册为 router-owned artifact，避免
  routerd 仍会重新生成该 table 时却把它误报为 orphan。
- `routerctl show derived-resources` 默认隐藏 stale 派生 state，并新增
  `--include-stale` 供 audit/debug 使用；同时新增 `routerctl delete --force`，
  让已删除或重命名 kind 的 state DB 行可以不经手动 SQLite 编辑而删除。
- TCP MSS clamp 现在会感知 source path，且只向下调整。可以用
  `Interface.spec.mtu` 描述 `tailscale0` 等低 MTU source interface；routerd 会按
  source/destination path 使用 `min(source MTU, destination path MTU)`，nftables
  只改写 advertised MSS 高于派生值的 SYN packet。

## v20260521.0039

### 修正

- 针对已删除的 `PPPoESession`，现在会 garbage collect ownership ledger 中
  留下的生成 artifact，包括 PPP peer file、runtime socket、runtime
  directory、state directory，以及已停止/停用的 systemd unit。
- Live ISO 现在也可以从以 CD-ROM 连接的 read-only ISO9660/UDF config media
  import router config，包含 Proxmox `media=cdrom` 且 label 为
  `ROUTERD_CONFIG` 的 config ISO。

## v20260520.2307

### 修正

- 只有在 router config 含有 FRR/keepalived 集成时，才会在生成的
  `routerd.service` 加入 `CAP_DAC_OVERRIDE`。Ubuntu FRR 常见 `/run/frr`
  为 `frr:frr` 且 mode `0755`，仅有 `frrvty` group 不足以让
  `frr-reload.py` 创建 `/var/run/frr/reload-*.txt`。
- 将 `frr-reload.py` 的 permission failure 分类为
  `FRRReloadPermissionDenied`，不再只落入 generic 的 `FRRReloadFailed`。
- 当 `WireGuardInterface` / `WireGuardPeer` 从 config 消失时，routerd 会移除
  routerd-managed 的旧 WireGuard interface 与 peer status，避免需要手动编辑
  state DB。

### 变更

- 更新 Kubernetes BGP examples，改为 import MetalLB LoadBalancer pool
  `10.250.0.0/24`，并调整 home-router sample 让它分别与两台 k8s route node
  建立 peer。

## v20260520.2227

### 修正

- 修正加入 OpenRC `routerd` service script 后的 Live ISO build。现在会先建立
  overlay `/etc/init.d` directory，再写入 script。

## v20260520.2222

### 新增

- 在 BGP prefix status 与 `routerctl show bgp` 加入 route selection diagnostics；
  FRR 有提供字段时，可看到 select-deferred、no-best-path 与
  not-installed-to-zebra 状态。
- 新增面向 Kubernetes/edge router 的 `BGPRouter.spec.convergenceProfile: fast`。
  fast profile 会派生较短的 BGP timers，并默认停用 graceful restart，以避免 fresh
  boot 时的 stale-path selection defer。
- Live ISO 现在可从 label 为 `ROUTERD_CONFIG` 的 USB partition 导入 config。
  boot helper 会选择 `/routerd/hosts/<hostname>.yaml`、
  `/routerd/hosts/<mac>.yaml` 或 `/routerd/router.yaml`，并将 source 与 SHA256
  记录在 `/run/routerd/`。

## v20260520.2107

### 修正

- BGP controller 现在会在每次 reconcile 检查 FRR service state。若
  Alpine/OpenRC 或 systemd host 上的 FRR 为 stopped/failed，routerd 会先
  start/restart service，再执行 `vtysh` probe 与 `frr-reload.py`。
- 收紧 BGPRouter Healthy 判定：service state、`vtysh` round-trip、
  `tcp/179` listen，以及 rendered `router bgp <asn>` stanza 必须全部存在，
  才会回报 Healthy。
- `routerctl status` 现在由 resource phases 聚合，避免 Pending/Error 的 BGP
  resource 被 controller runtime 的 success update 隐藏。

### 文档

- 新增 BGP / FRR control-plane design note，记录 readiness、reload、
  verification、failure status，以及 Live ISO acceptance scenarios。

## v20260520.2007

### 修正

- 从 BGP controller 的 FRR readiness 判定移除 TCP VTY gate，改用
  `vtysh -c "show running-config"` 作为 control-plane probe 与 running config
  diff 来源。这让禁用 TCP VTY 的 Alpine FRR build 也能在首次收敛时执行
  `frr-reload.py`。
- 在 status 中明确呈现 FRR control 不可用、权限不足、reload 尝试，以及 reload
  后验证未完整反映的状态。
- Alpine Live ISO autostart 在已经有 `routerd serve` 运行时，不再启动第二个
  `routerd serve`。

## v20260520.1904

### 修正

- 在 BGP controller reconcile 期间重试临时性的 FRR reload lock 失败，让首次
  boot 也能不靠手动 `frr-reload.py` 到达 `bgpd` config。
- 让 Alpine Live ISO 的 DHCP client 在取得初始 lease 后持续常驻，为 live
  router 派生稳定 DHCP hostname，且默认不发送 DHCP option 61，让 Windows DHCP
  reservation 继续按 Ethernet MAC 匹配。

## v20260520.1737

### 新增

- 为 `mode: vrrp` 的 `VirtualAddress` 新增 FreeBSD CARP 后端，包括
  runtime controller、rc.d rendering、validation、tests，以及最小示例
  `examples/freebsd-vrrp.yaml`。
- 新增 ingress/local router service 的 listen-port collision validation，
  以及 Linux nftables 的 `IngressService` `sourceHash` / `random` backend
  distribution。
- 新增 FRR BGP connected/static redistribution、BGP community send/accept/set
  policy、observed community status parsing，以及
  `examples/lan-advertise-with-community.yaml`。
- 新增基于 VRF-backed FRR BGP instance 的 multi-instance `BGPRouter` support、
  listen-address collision validation、per-router observed status，以及
  `examples/multi-instance-bgp.yaml`。
- 新增面向 FRR-managed BGP peer 的 BFD support、FRR `bfdd` daemon rendering、
  BGP watcher tuning fields、BFD status observation，以及
  `examples/bgp-bfd.yaml`。
- 新增面向 Kubernetes Pod / Service CIDR static route 的
  `ClusterNetworkRoute` helper，并为 BGP peer password 与 VRRP/CARP
  authentication 增加 `passwordFrom` / `authenticationFrom` secret source。
- 新增用于临时 `IngressService` backend maintenance 的 `routerctl drain` /
  `undrain`，以及 VRRP production tuning 文档和
  `examples/vrrp-tuning-presets.yaml`。
- 改善 Alpine Live ISO 路径：VRRP controller 默认为 live，
  `routerctl show vrrp` 会从 live address 重新观测 role，version output 可嵌入
  commit，并补上 FRR reload tooling dependency 与非阻塞 setup wizard 行为。
- live VRRP reconcile 会避免 keepalived 的 no-op reload/restart，并在
  controller status 中暴露最近一次 keepalived reload/restart 的时间与原因。
- `routerd apply --once` 的 VRRP 处理现在复用与 daemon mode 相同的
  controller reconcile 路径，因此 keepalived reload/restart status fields
  会被一致写入。
- 将 IngressService 的 live nftables apply 与独立 NAT44 dry-run mode 解耦；
  hostname 的 DNSZone coverage 现在降级为 warning，并可用 `externalDNS`
  标记外部 DNS 管理的名称。
- 自动处理 IngressService 的同一 interface hairpin SNAT 和转发所需的 runtime
  `ip_forward` sysctl，并在 `routerctl show ingress --verbose` 中显示
  forwarding、nftables、conntrack 的 dataplane 状态。
- 修复没有声明 listen-interface prefix 的 Live ISO 风格配置中的
  IngressService `hairpin.mode: auto`：同一 private `/24` 内的 listen/backend
  address 会被视为需要 hairpin，并在 verbose ingress 输出中提示预期的 nftables SNAT
  是否缺失。
- 新增 `pkg/servicemgr` 抽象，统一 systemd、OpenRC、rc.d、NixOS 的 service
  artifact 命名和 lifecycle command，并让 service artifact intent generation
  通过该层，减少每个 resource 中分散的 OS switch drift。
- 为所有 checked-in example config 增加 Linux、Alpine/OpenRC、FreeBSD/rc.d、
  NixOS render snapshot golden test，并增加 netns compatibility wrapper。
  `pkg/servicemgr` 也新增 lifecycle hook，使 FRR config-check + live reload、
  keepalived reload/restart 区分、signal-based daemon reload 不会退化成 generic
  restart。
- 新增 bespoke lifecycle command golden test 与 `make check-bespoke-lifecycle`
  gate，固定 FRR live reload、keepalived no-op/reload、dnsmasq SIGHUP、DHCP
  daemon IPC、BFD daemon enablement、IngressService nftables-only backend
  rotation、VRRP track artifact、DS-Lite dataplane hook、DHCP event daemon
  ordering，以及 FRR graceful-restart observation。
- 为 nftables / pf 的 render、diff、reload 路径新增无行为变化的 firewall
  backend abstraction，并用 regression contract 固定 nftables 的 `ct state`、
  `jhash`、`numgen`、hairpin conntrack expression，以及 pf 的 `rdr`、
  `nat-anchor`、hairpin NAT syntax。
- 为 netplan、systemd-networkd drop-in、NixOS module、FreeBSD rc.conf
  fragment 新增无行为变化的 network config backend abstraction，并以通用
  IPv4/IPv6 address 与 route declaration 表示网络配置。
- 将 PPPoE、VRRP/CARP、FRR、dnsmasq、DHCPv6 PD、DNS resolver、Tailscale 的
  service-backed artifact intent 整理为 ServiceManager declaration table，使
  systemd/OpenRC/rc.d/NixOS ownership 在不改变输出的前提下保持一致。
- 扩展 render golden coverage，覆盖 firewall hole derivation 与 OS-specific
  interface/network artifacts，并固定 Linux netplan/systemd-networkd output 与
  Alpine nftables snapshot。
- 强化 abstraction layer regression coverage，新增 cross-OS semantic test、
  invalid spec check、firewall backend error propagation status/event、
  edge-case declaration、race-tested reload、80% coverage gate，以及 4 OS 的
  bespoke lifecycle command matrix。

## v20260519.0743

### 变更

- 整理公开 documentation 和 example configuration 的命名，避免内部 lab
  hostname、domain、management network address 出现在 website 或可复用 example
  中，而是保留在 internal notes。
- 将 internal design / soak note 移出公开 Docusaurus docs tree，并在
  `internal/notes/` 记录 native nDPI 与 RA/DHCPv6-PD coverage 的 lab
  validation policy。

## v20260519.0713

### 修正

- `routerctl show bgp`、`routerctl show vrrp`、`routerctl show ingress` 不再打开
  ownership ledger，因此在指定 status store 且 default ledger path 不可写的环境中
  也能正常执行。

## v20260519.0708

### 新增

- 新增面向 Kubernetes edge 使用场景的 FRR backend `BGPRouter` / `BGPPeer`、
  keepalived backend `VirtualAddress`，以及 `IngressService` backend
  health/failover controller。
- 新增 `routerctl show bgp`、`routerctl show vrrp`、`routerctl show ingress`
  table view，从 VIP/ingress `hostname` field 自动推导 DNS record，并新增
  BGP/VRRP/Ingress transition 与 backend health 的 OTel metrics。
- Web Console 新增 BGP、VRRP、IngressService dedicated view 与 JSON endpoint。

### 变更

- FRR BGP 配置现在会先用 `vtysh -C -f` 验证，再通过 `frr-reload.py --reload`
  差分应用。VRRP 默认使用 unicast peer 与 `nopreempt`，并支持 track hysteresis
  和 `preemptDelay`。BGP、VRRP、IngressService listen port 的 firewall hole
  也会自动推导。
- BGP reconcile 不再让 dry-run 写入遮蔽后续 live apply；首次 live 观测时会先比较
  FRR running-config，再决定是否 reload，避免已一致的 session 被 no-op reload reset。

## v20260518.1810

### 新增

- 新增单独的 `routerd-ndpi-agent-libndpi-linux-amd64` release archive，
  供需要启用 native nDPI classification 的主机使用。普通 Linux release
  archive 仍保持完全静态链接，optional nDPI agent override 使用
  `CGO_ENABLED=1 -tags libndpi` 构建，并通过 libndpi self-test 验证。

## v20260518.1431

### 新增

- 在 control API、日志、OpenTelemetry metrics/traces，以及 Web Console 的
  controller view 中新增 controller reconcile runtime status。controller status
  现在会返回 interval、trigger、运行次数、错误次数、last/average/max duration，
  以及最新错误。

## v20260518.1301

### 变更

- 移除了当前 controller runtime 配置路径已不再使用的 dead compatibility helper
  和旧 raw systemd unit renderer。

## v20260517.2339

### 新增

- 新增 Configuration examples 文档区段，包含编号 topology diagrams、diagram-to-YAML
  对应注释、安全注意事项，以及基本 IPv4 NAT、LAN DHCP/DNS、DS-Lite、PPPoE、
  port forwarding、guest isolation、multi-WAN failover、local DNS redirect、
  Tailscale、WireGuard、telemetry export 等已验证 sample YAML。
- IPv4 route policy resource 引用的 health check 现在会从引用来源的 route candidate
  或 target 推导 socket mark。单独 probe 仍可使用 `spec.fwmark`，validation 会拒绝
  明确 mark 与推导 mark 冲突的配置。

### 变更

- Linux upgrade 现在只会在 routerd helper systemd service 仍执行已删除的旧 binary，
  或 unit file 在 helper process 启动后重新生成时，才重启该 helper。installer 会先等待
  `routerd.service` 与 routerd 管理的 unit file 稳定后再判断。
- release installer 现在会在 NixOS 上跳过 host service manager 变更，因此
  `/etc/systemd/system` 为只读且 service unit 由声明式配置管理的 host，也能用 archive
  更新 binary。
- 当 host 没有 conntrack procfs file 时，conntrack observation 会记录 `Unavailable`
  status，而不是每个 interval 都写出 warning。
- FreeBSD `--skip-service-manager` apply 现在会抑制 generated helper、managed dnsmasq、
  以及 pf/pflog service activation 的 rc.d/service 操作，同时仍允许写入 rc.conf-backed
  network state 并直接通过 `pfctl` 加载 rule。这可避免 recovery 与 bootstrapping path
  和 base rc boot sequence 竞争。
- FreeBSD upgrade 现在会保留 config-managed `routerd` rc.d script，不再用 generic
  bootstrap template 覆盖；这与 Linux 保留 config-managed `routerd.service` 的行为一致。
- `routerd serve` 现在会在收到 SIGTERM/SIGINT 时 cleanly shutdown control 与 status
  socket，让 FreeBSD rc.d 在 `daemon(8)` 下 restart 时能正常停止，不会落到 forced KILL。
- routerd state SQLite database 现在搭配既有 busy timeout 使用 WAL mode，减少 status
  reader 与 controller 重叠时短暂发生的 `SQLITE_BUSY`。

## v20260517.1808

### 修复

- Debian/Ubuntu release installer 现在会安装 `dnsmasq-base`，而不是完整的
  `dnsmasq` package，避免 distro 的 `dnsmasq.service` 被启用并与 routerd 管理的
  dnsmasq instance 竞争。

## v20260517.1800

### 修复

- controller 与 helper probe 发出的单次 HTTP-over-Unix 调用现在会停用
  keep-alive，并明确关闭 idle transport。这可避免周期性的 status polling 在
  `routerd`、health check helper、DHCP client、DNS/DPI helper service 中留下大量
  已建立的 Unix socket。

## v20260517.1533

### 修复

- release helper 现在会在 schema check 前重新生成受管理的 config schema 与
  control API schema。API type 变更会包含在 release commit 中，而不是到 release
  后段才失败。
- `routerctl` 现在只会针对只读 control API request，retry daemon 启动期间临时性的
  Unix socket 连接失败。`routerctl status` 默认会使用独立的只读 status socket，
  而 apply 与 delete 仍只使用有权限的 control socket，且不会 retry。

## v20260517.1510

### 新增

- Web Console Connections 现在会标记由 `LocalServiceRedirect` 处理的 flow。
  当 live conntrack tuple 与已解析的 set status 能识别 match 时，也会显示
  redirect rule 和目的地 `IPAddressSet`。
- Web Console Firewall 现在会在 deny log row 显示目的地 `IPAddressSet` match，
  并区分明确的 `FirewallRule.destinationSetRefs` match，以及当前存在于已配置
  set 内的目的地。

## v20260517.1401

### 修复

- 修复 Web Console disk usage collection，使其在 `syscall.Statfs_t` block counter
  使用 signed integer type 的 FreeBSD 上也能编译。

## v20260517.1353

### 修复

- release helper 现在会拒绝第一个 release section 不是 `Unreleased` 的
  changelog，并从维护中的 changelog 文件移除了旧 helper 运行留下的空 release
  标题。

## v20260517.1351

### 变更

- `routerd-dpi-classifier` 现在有明确的 classifier engine facade。默认 engine 是
  built-in parser，`auto` / `ndpi-agent` mode 可以查询未来的
  `routerd-ndpi-agent` Unix socket service，失败时会 fallback 到 built-in parser。
- Web Console Connections 现在会在 DPI 尚未识别 flow 时，将 TCP port 4317
  标示为 OTLP，将 TCP port 4318 标示为 OTLP/HTTP。
- Web Console Overview 现在会显示 host CPU、memory、root filesystem 使用率，
  以及 classifier 侧的 DPI processing latency，方便把 router 本机负载恶化与
  routing、DPI 健康状态一起观察。
- Web Console Clients 与 Connections 现在可以互相跳转。client row 可以打开按该
  client 观测地址筛选的 Connections，connection 详情也可以回到对应的 local
  client identity。
- Web Console Connections 现在建立 Clients snapshot 时也会读取近期
  traffic-flow observation，让近期的 IPv6 privacy address 更有机会对应回 client。
  source endpoint 即使尚未合并到已知 identity，也会提供前往 Clients 搜索的动作。
- Web Console 的搜索输入框现在会在有文字时显示内嵌清除按钮。
- release helper 现在要求 working tree 处于 clean 状态，并会把当前
  `Unreleased` 的内容提升到 release tag，而不是创建空的 tag 标题。

### 新增

- 新增 `IPAddressSet` 与 `LocalServiceRedirect`。`IPAddressSet` 可以把直接指定的
  IPv4/IPv6 address 与 FQDN 的 `A`/`AAAA` record 解析成可重用的 nftables named
  set；`LocalServiceRedirect` 可以把 LAN client 送往这些 set 的明文 DNS/NTP
  通信 redirect 到 router 的 local service，且不会碰 DoH/DoT 或 router 自身发出的
  health check。
- `FirewallRule`、`NAT44Rule`、`IPv4PolicyRoute` 和 `IPv4PolicyRouteSet` 现在可以通过
  `destinationSetRefs` 和 `excludeDestinationSetRefs` 使用 `IPAddressSet`，让
  FQDN-backed address set 可重用于 firewall filtering、NAT 范围和 IPv4 policy routing 条件。
- 新增 runtime `IPAddressSet` refresh controller。被引用的 nftables set 会根据 DNS
  TTL 原地更新，使用观测到的最小 TTL 的一半、60 秒 floor，以及可选的
  `refreshInterval` cap，让 FQDN-backed set 不必 reload 整个 firewall、NAT 或 policy table 也能保持新状态。
- 新增初始版 `routerd-ndpi-agent` service boundary 作为 optional command。默认
  build 会报告 libndpi backend 不可用，而 `-tags libndpi` build 会在同一个
  IPC surface 后方链接 native library。
- `routerd-ndpi-agent` 现在会持有 per-flow observation state，包括 flow TTL、flow
  数量上限、起始 payload packet 数量上限，以及 observed、classified、unknown、
  skipped、error、pruned packet 的 status counter。
- 新增 `routerd-ndpi-agent` 的初始 libndpi backend。它通过 `libndpi` build tag
  opt-in，将 native flow state 保留在 agent 内，并可分类 firewall logger 传来的
  full packet observation。
- 新增 `make build-ndpi-agent-libndpi` target，可在已安装 libndpi development files
  的环境中构建 optional native backend。
- 当 `routerd-dpi-classifier` 配置为 `--engine auto` 或 `--engine ndpi-agent`
  时，systemd、OpenRC、FreeBSD rc.d 和 NixOS 现在会 render `routerd-ndpi-agent`。
- DPI flow 和 traffic flow record 现在除了既有 app label 字段外，也会保存 typed
  classifier fields，例如 detected protocol、application protocol、category、
  confidence、risk 和 metadata。
- `routerd-dpi-classifier` status 现在会报告 daemon 处理 classify request 的
  average latency 和 maximum latency。

### 修正

- Linux 升级时，如果有 routerd helper 的 systemd service 仍在运行升级前已删除的
  binary，`install.sh` 现在会重启该 service。
- 当 nDPI agent 结果已识别 application、但缺少 TLS SNI、HTTP Host 或 DNS query
  等 detail 时，`routerd-dpi-classifier` 现在会保留 built-in parser 提供的有用 hint。
- DPI helper daemon bind Unix socket 时，现在会拒绝 unlink 非 socket path；
  `routerd-ndpi-agent` 也会明确 close native libndpi state。
- Web Console 读取 traffic-flow 时，现在可容忍 writer 尚未完成 schema migration、
  因而缺少最新 DPI column 的 legacy SQLite file。

## v20260516.2302

### 变更

- Web Console Connections 现在会将 source 到 destination 的路径对齐在固定的
  route column，并把 state、protocol、provider、traffic 和 timeout 等 metadata
  移到独立的 badge 区域。
- Web Console 的 connection label 现在会分开显示 transport/application identity
  和 destination provider。像 `google-https` 这类旧的 provider-specific label
  会规范化为 `TLS`，而 Google、AWS、Microsoft、Apple 和 Cloudflare 会以独立的
  destination provider badge 显示。
- `https` 等 destination service 名称现在会在能补充 connection row 信息时，
  以 protocol badge 显示。

### 修复

- 修复展开后的 connection detail，destination service 和 provider badge 会保持内容宽度，
  不再撑满整个 detail column。
- 修复展开后的 connection detail，source 和 destination identity text 会使用可用宽度
  并在需要时换行，不再套用 compact row 的宽度后以省略号截断。
- 修复 Connections 的 `Showing` metric，当 API 结果因请求的 row limit 被截断时，
  会分开显示 filtered rows、loaded rows 和总 conntrack count。

## v20260516.2155

### 变更

- Web Console Connections 现在默认按观测到的传输 byte 数降序排序。
  Connections 的 sort menu 新增 `Traffic` 选项，connection card 会显示总 byte 数，
  展开详情时会在 conntrack accounting 可用时显示 outbound、inbound 和 total counter。
- 应用 Web Console connection 数量上限时，conntrack observer 现在会在每个
  family/protocol group 内优先保留 byte 数较大的 entry。
  这会降低大型 active flow 被低 traffic entry 挤出列表的概率。

## v20260516.1413

### 修复

- 修复 `routerd apply --dry-run` 和相关 planning path，当 SQLite ownership ledger
  不存在时会视为空的 in-memory ledger，不再尝试在无权限的 CI runner 上创建
  `/var/lib/routerd`。

## v20260516.1405

### 新增

- 在 `firewall.routerd.net/v1alpha1` 新增 `PortForward` 和单一 backend 的
  `IngressService`，用于描述 WAN 侧 IPv4 TCP/UDP ingress DNAT。
- Linux nftables 和 FreeBSD pf rendering 现在可以发布这些 ingress service。
  也可以选择生成 hairpin NAT，让 LAN client 通过 WAN address 访问同一个
  port-forwarded service。
- 为新的 ingress NAT resource 新增 generated JSON Schema、CLI alias、API
  documentation 和 resource ownership documentation。

## v20260516.0804

### 变更

- Web Console Connections 现在按固定的 IP family 和 transport protocol
  bucket 汇总 active flow，不再按 DPI application 拆成多个表格。
  TLS、DNS、QUIC 等 app label 仍会显示在各 group 内。

## v20260514.1433

## v20260514.0813

### 修复

- 修复 Web Console Clients，在与当前 DHCP lease 关联之前，将基于 IP address 的
  DNS、traffic、firewall、DPI 和 DHCP fingerprint 证据限制在同一个最近一小时
  observation window 内。
- client inventory 的 sticky DHCP lease annotation 现在只使用 active hold，
  避免旧 lease history 混入当前的 endpoint identity 判断。

## v20260514.0743

### 修复

- 修复 Web Console Clients，忽略已过期的 dnsmasq lease，避免旧 host 无限期留在列表中。
- DHCP lease 合并现在会优先采用最新的有效 lease，只有在条件相同时才以 lease file 配置顺序作为 tie-breaker。
- routerd 现在会把 controller runtime dnsmasq lease file 作为第一候选传给 Web Console，
  让 console 按照受管理 dnsmasq 实际使用的 lease file 显示。

## v20260514.0654

### 修复

- 修复 Web Console Overview，避免把首次轻量 snapshot 记录成数值为 0 的 metric sample。
- Overview 的延迟 refresh 现在会加载所需的 resource、event、conntrack、DNS
  与近期 traffic flow 数据，同时仍避开较重的 firewall、VPN 和 client inventory 工作。
- Overview card 会将尚未取得的 flow / connection data 显示为 loading state，
  不再把不可用的值呈现为 0。

## v20260514.0037

### 修复

- DHCPv4 LAN domain rendering 现在会在未显式设置 domain-search option 时，从 `domain` / `domainFrom` 同时生成 domain-name 和 domain-search。

## v20260514.0025

### 新增

- 新增 `domainFrom`、`dnsslFrom` 和 `domainSearchFrom`，让 DHCPv4、
  IPv6 RA 与 DHCPv6 的 LAN suffix 通告可引用 `DNSZone/<name>.zone`，
  不必重复写入本地域名字符串。

## v20260513.2358

### 变更

- 强化长期运行的事件处理。`EventRule` 和 `DerivedEvent` 的 timer 触发后会清理 map entry，忽略过期的 timer callback，并用 controller lock 保护共享状态。
- 为 `EventRule` 的 correlation state 设置上限，避免高基数事件流让内存使用量无限增长。
- daemon 的 `events.jsonl` 不再无限追加，而是在固定大小后轮转。
- 为 local control、daemon event、DNS resolver、DoH 与 classifier 路径加入 request / response 大小限制，并为 local daemon server 与 Web Console 加入 HTTP header timeout。

### 修复

- 修复 `DerivedEvent` hysteresis 处理中 timer callback 与 reconcile 可能同时更新 pending transition state 的 race。

## v20260513.2317

### 变更

- 配合 `v20260513.2252` 的稳健化工作，更新 production reconcile 文档。operations、upgrade、state ownership 与各语言 changelog 现在说明主机状态 drift 检查、受管理 artifact 清理、nftables named set 更新，以及由配置管理的 `routerd.service` 在 upgrade 时的处理方式。

## v20260513.2252

### 变更

- 强化 production reconcile。controller 在跳过工作前，会同时检查 status database 与实际主机状态；范围包括 systemd unit、dnsmasq、DHCPv4 lease 地址、route-policy nftables table、NAT44，以及相关的受管理 artifact。
- Health check 的 `fwmark` 现在会传递到生成的 systemd unit、socket 设置、status 观测值与 OpenTelemetry attributes。probe 可以使用与被检查路径相同的 policy-route mark。
- Linux firewall rendering 会在重新定义 routerd-managed named set 前先清除它们。已移除的 zone interface 或 client-policy MAC 地址不会残留在 nftables 中，同时仍会保留整个 managed filter table，不会 destroy/recreate table。
- release installer 会保留由配置管理的 `routerd.service`，不再用 archive template 覆盖。routerd 管理自己的 unit 时，unit file 变更会通过 `systemd-run` 调度延迟 self-restart。

### 修复

- 当 `HealthCheck` resource 从 YAML 消失时，会移除对应的旧 `routerd-healthcheck@*.service` unit。
- 移除最后一条 NAT rule 后，会清空受管理的 NAT44 table 或 pf anchor。
- status 显示 DHCPv4 lease 地址存在，但接口上实际缺少该地址时，会重新应用地址。
- 空的 `WireGuardPeer` resource 现在标记为 `NotConfigured`，避免停留在容易误解的 Pending 状态。

## v20260513.1931

### 修复

- 稳定 health check 路径切换行为。

## v20260513.1153

### 修复

- 稳定 controller reconcile 的幂等性。

## v20260513.0836

### 新增

- 新增 WireGuard mesh controller。

## v20260513.0727

### 变更

- 提高 home-router 的 UDP conntrack timeout 设置。

## v20260512.0037

### 新增

- 从 conntrack observer 导出 DPI flow metrics。

## v20260512.0032

### 新增

- 在 Web Console Overview 页面新增 DPI summary card。

## v20260512.0027

### 新增

- 在 Web Console Clients 页面新增 DPI activity summary。

## v20260512.0008

### 新增

- 在 Web Console Connections 页面显示 DPI classification。

## v20260511.2357

### 变更

- 将 DPI enrichment 扩展到 forward flow。

## v20260511.2307

### 修复

- 抑制 Web Console 的水平 overscroll。

## v20260511.2300

### 修复

- 修复 Firewall timeline 的水平滚动。

## v20260511.2253

### 变更

- 将 Web Console 整理为 content-driven layout section。

## v20260511.2217

### 验证

- 验证 mobile Web Console layout。

## v20260511.2211

### 变更

- Web Console 在页面切换后会保留 page state。

## v20260511.2154

### 变更

- 整理 Clients inventory view。

## v20260511.2145

### 新增

- 新增 Web Console SSE reconciliation。

## v20260511.2130

### 新增

- 新增 client fingerprint inference。

## v20260511.2106

### 变更

- 关联 expired conntrack return flow。

## v20260511.2045

### 变更

- 为 firewall deny event 加上 DPI context。

## v20260511.2018

### 验证

- 验证 DPI classifier OS parity。

## v20260511.1846

### 修复

- 将 Web Console time locale 固定为 English。

## v20260511.1840

### 新增

- 新增 isolated DPI classifier proof of concept。

## v20260511.1820

### 新增

- 新增 Connections protocol summary。

## v20260511.1709

### 修复

- 修复 release artifact checksum。

## v20260511.1428

### 变更

- 改善 Web Console navigation section。

## v20260511.1240

### 变更

- 调整 controller mode reason 的呈现。

## v20260511.1041

### 新增

- 提高 dry-run controller 的可见度。

## v20260511.1017

### 变更

- 明确显示 controller dry-run mode。

## v20260510.1956

### 变更

- 让 `NetworkAdoption` 管理 resolved DNS。

## v20260510.1811

### 新增

- 将 PVE live ISO serial-console 验证日志加入 `internal/notes/`，让 walkthrough 截图与执行日志作为测试证据保存在同一 release 中。

## v20260510.1802

### 变更

- 在日文、简体中文和繁体中文的 diskless mini PC walkthrough 中嵌入 PVE live ISO boot test 的实际截图。
- 移除 diskless mini PC walkthrough 中残留的旧 placeholder 图片引用。

## v20260510.1750

### 新增

- 在 diskless mini PC walkthrough 中加入 PVE live ISO 实机验证截图。
- 为简体中文和繁体中文补充 positioning、USB persistence 与 legal redistribution 页面。

### 变更

- 将 website footer 的 copyright 文本改为先写版权声明的惯用形式。
- 更新 diskless mini PC walkthrough 的 PVE 示例，同时启用 VGA 与 serial console，方便在同一次验证中取得 QEMU screenshot 和 `qm terminal` 日志。

### 修复

- 修复 live ISO configure wizard，使 DHCPv4 pool 默认值从选择的 LAN address prefix 推导。
- 重新执行 PVE live ISO boot test，并确认 `/tmp/iso-boot-test-20260510-1742.log`、QEMU screenshots、routerd apply、Healthy status 与 USB persistence flush。

## v20260510.1722

### 新增

- 为 routerd Go source、installer scripts、plugin scripts 与 Web Console source 增加 BSD 3-Clause SPDX identifiers。
- 在 README 中加入 license badge，并从英文与日文 README 链接到 BSD 3-Clause License。
- 新增公开 contributing 文档，并从 docs sidebar 链接。
- 在 SECURITY 中补充 email 与 GitHub Security Advisories 报告路径。

### 变更

- 将 repository root 的 `LICENSE` copyright notice 统一为 `Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors`。
- 在 legal 文档中说明 SPDX headers 只适用于 routerd source files；bundled third-party software 继续遵循 `THIRD_PARTY_LICENSES.md` 中的各自 license。
- 从 README 移除产品比较表，改为说明 routerd 自身的范围与特点。

## v20260510.1626

### 新增

- 新增公开 legal 与 redistribution 页面，整理 release checklist。
- 在生成的第三方授权清单中加入 Go module source URL。
- 记录 BSD routerd binary 与 aggregate live ISO distribution model 的内部 license audit note。

## v20260510.1612

### 新增

- 新增 Go module 与 live ISO Alpine package 的第三方授权清单自动生成流程。
- 新增 release archive 与 live ISO 内的授权通知安装位置。
- 文档补充 routerd 本体 BSD 3-Clause License，以及 live ISO 作为 aggregate distribution 的处理方式。

## v20260510.1547

### 新增

- 扩充公开定位说明，重点说明 routerd 自身的范围与 deployment spectrum。
- 扩充 Intel NUC、N100 mini PC、Raspberry Pi 5、thin client 和 Proxmox VM 的硬件兼容性说明。
- 新增中文硬件兼容性页面，并补充 live ISO 与 USB persistence 的使用路径。

## v20260510.1534

### 新增

- 新增 diskless mini PC walkthrough 图、tutorial index 更新与 field-note blog post。

## v20260510.1508

### 新增

- 新增 USB persistence 操作文档与 live ISO USB persistence 支持。

## v20260510.1451

### 新增

- 新增 contribution、security、license、positioning、hardware compatibility 与 diskless mini PC 文档。

## v20260510.1429

### 新增

- 新增 Alpine live ISO build 与 install documentation。

## v20260510.1412

### 新增

- 新增 live ISO validation note 与 live ISO 路径的 installer documentation。

## v20260510.1354

### 修复

- 修复 Alpine 上的 live ISO runtime apply。

## v20260510.1310

### 新增

- 启用 live ISO serial console support。

## v20260510.1301

### 变更

- 将 release tag 切换为 JST timestamp 格式。

## 20260510.4

### 修复

- 修复 live ISO overlay archive path。

## 20260510.3

### 修复

- 修复 Alpine live ISO release discovery。

## 20260510.2

### 新增

- 新增 Alpine-based live ISO packaging。

## 20260510.1

### 新增

- 新增 installer configuration wizard。

## 20260510.0

### 变更

- 在 fixed-download-asset release 之后，开始 20260510 release series。

## 20260509.16

### 新增

- Release archive 现在除了 versioned archive，也包含 `routerd-linux-amd64.tar.gz` 这类固定名称 alias。
- 固定名称 archive 与 `.sha256` 文件会上传到 GitHub Releases，因此文档可以使用 `releases/latest/download/...` URL。

### 变更

- Quick start 文档改用 stable latest-download URL，不再硬编码特定 release version。
- release workflow 会在支持时让 GitHub JavaScript actions 使用 Node.js 24 runtime。

## 20260509.15

### 新增

- 新增 branch push 与 pull request 用的 `CI` GitHub Actions workflow。
- CI workflow 会在 Ubuntu 上执行 `go test ./...`、schema 检查、example 验证与网站构建。
- 新增可选的 `scripts/pre-commit.sh` hook，在本地 commit 前执行 Go test 与 schema 检查。
- 新增 development 文档，说明 CI、pre-commit check 与 tag-driven release publishing 的分工。

## 20260509.14

### 验证

- 在 Ubuntu lab router 上验证 `ClientPolicy` guest mode。
- 确认 Linux nftables 会生成 include mode guest MAC set、guest DNS/DHCP/NTP allow、自我隔离，以及 RFC 1918 / ULA deny 规则。
- exclude mode 已通过 focused nftables renderer test 验证。

## 20260509.13

### 新增

- 扩充 guest mode guide，加入使用场景、内部实现、完整 `ClientPolicy` field reference、验证步骤、troubleshooting 与安全限制。
- 新增 include mode、exclude mode、多个 guest device、自定义 deny/allow list、local discovery service 与 IoT reservation 示例。
- `ClientPolicy.spec.guestServices` 现在除了 `dhcp`、`dns`、`ntp`，也接受 `mdns` 与 `ssdp`。

## 20260509.12

### 新增

- 新增 `ClientPolicy`。它是由 Linux nftables 支持的 guest mode，可按 MAC 地址分类 LAN client。
- guest client 可使用 DNS、DHCP、NTP，但默认会拒绝转发到 private IPv4 与 ULA IPv6 目的地的流量。
- 新增 `examples/guest-mode.yaml` 与 include mode / exclude mode 文档。

### 变更

- FreeBSD pf 会明确拒绝 `ClientPolicy`，因为 pf 没有相同的 MAC-based routed filtering 模型。

## 20260509.11

### 新增

- 新增最小 Tailscale mesh、WireGuard hub-spoke、VRF lab 和 multi-WAN home fallback 的用途示例。
- 新增 `examples/README.md`，说明各示例适合的使用场景。

### 变更

- `make validate-example` 现在会验证 `examples/` 目录下的所有 YAML 文件。

## 20260509.10

### 新增

- Web Console Overview 会显示 generation、resource phase、HealthCheck 状态的简易趋势图。
- Config 页可比较当前 YAML 文件与最新已应用 generation，便于执行 `routerd apply` 前确认差异。
- Resource 表格支持 kind、name、phase、详细内容搜索、phase 筛选与结果标记。
- VPN 页面新增 Tailscale 与 WireGuard peer 状态的视觉摘要。

## 20260509.9

### 新增

- release archive 现在包含 `share/doc/TARGET`，`install.sh` 会检查 archive 的 OS / CPU 架构是否匹配主机。
- GitHub Actions 会生成 Linux 与 FreeBSD 的 `amd64` / `arm64` archive。
- release CI 会对 `install.sh` 与 `uninstall.sh` 运行 `shellcheck`。

### 变更

- `install.sh --list-deps` 改为结构化输出，列出 OS、CPU 架构、包管理器、包与检查命令。
- 依赖清单加入 PPPoE、RA、IPsec、包捕获、路由与 firewall 工具。

## 20260509.8

### 修复

- 修复 zh-Hant 与 zh-Hans 文档链接，翻译页不再指向尚未翻译的同语言页面。
- 在完整翻译完成前，概览页会链接到英文正准参考页。

## 20260509

### 新增

- `EgressRoutePolicy` 现在可以表达 DS-Lite 主路径、RA 来源 DS-Lite、PPPoE 和 WAN 直连的多级回退。
- 通过声明式 `Telemetry` 资源和 OTLP 环境变量传播，将 OpenTelemetry 配置扩展到路由器群。
- DS-Lite 示例改用 RFC 6333 的 B4-AFTR link prefix `192.0.0.0/29` 作为隧道内侧 IPv4 源地址。
- `PPPoESession.disabled` 和禁用的路径候选允许在 YAML 中保留 PPPoE 回退定义，同时避免生产 PPPoE 会话泄漏。

### 变更

- 版本号从 `0.x.y` 改为 `20260509` 这样的日期字符串。
- Linux nftables 与 FreeBSD pf 的 NAT44 生成收敛到按接口生成规则。
- 在 Linux 与 FreeBSD 上验证了 3-role firewall；service hole 绑定到拥有它的接收入接口。
- FreeBSD pf 支持为 `PathMTUPolicy` 生成 TCP MSS clamp；dnsmasq RA 也会发布 MTU option。

### 修复

- FreeBSD pf 不再把 DHCPv6、WireGuard、VXLAN 的 service hole 扩展到 `wan` zone 的所有接口。
- FreeBSD NAT artifact 现在报告为 `pf.anchor/routerd_nat`。
- NAT 生成前会把 PPPoE 资源名解析为实际 OS 接口名。

## 0.4.0

### 新增

- nftables 的隐式拒绝包记录由 `routerd-firewall-logger` 接收并写入 `firewall-logs.db`。Linux 直接读取 `nfnetlink`，FreeBSD 通过 `tcpdump` 读取 `pflog`。
- Web Console 新增「Connections」选项卡（实时 conntrack / pf state）、「Clients」选项卡（DHCP 租约与流量整合）以及「Firewall」选项卡（拒绝排行 + 时间序列）。
- `WebConsole.spec.listenAddressFrom` 与 `DNSResolver` 系列的监听地址，可由 `Interface/<name>.status.ipv4Addresses` 推导。允许用引用代替字面值。
- 默认启用 conntrack 计数（`net.netfilter.nf_conntrack_acct=1`），`SysctlProfile/router-linux` 已纳入；`TrafficFlowLog` 因此能聚合 `bytesOut` / `bytesIn`。

### 变更

- 实时连接视图的 API / CLI 统一命名为 `connections`（旧称 `conntrack-snapshot`）。请使用 `/api/v1/connections`、`routerctl connections`。IPv6 也并入同一张表。
- 扩展了 NixOS 的声明式渲染。`Package`（NixOS 包声明）、`SysctlProfile`、`NetworkAdoption`、`generated service artifacts` 都会输出到 `routerd render nixos`。NixOS 上的 `Package` 不再于运行时安装，由生成的 NixOS 配置接管。
- `generated service artifacts` 可生成 FreeBSD `rc.d` 脚本（`routerd render freebsd --out-dir`）。

### 修复

- 当 `Link/<name>` 状态为空时，`IPv6DelegatedAddress` 不再跳过将 PD 派生地址绑定到主机接口的步骤。
- `generated service artifacts` 不再对未变动的 active unit 进行不必要的重启。

## 0.3.0

### 新增

- 声明式 OS bootstrap 资源 `Package` 与 `SysctlProfile`。覆盖 apt、dnf、nix、pkg 的包声明，以及面向路由器场景的 sysctl 推荐值（`nf_conntrack_max`、socket buffer、TCP/UDP timeout、`ip_forward` 等）。
- `NetworkAdoption` 可由 YAML 关闭 systemd-networkd 的 DHCP / RA。`generated service artifacts` 由 routerd 自身渲染、安装、启用 unit 文件。
- `routerctl events --limit N --topic X --resource K/N -o json` 不再依赖 `sqlite3` 即可查看 bus event。
- `routerd plan --diff` 提供 apply 前的差异预览。
- `DNSResolver` 支持 bootstrap forwarder（内部 DNS 优先，公共 DNS 作为兜底）。

### 变更

- 配置文件中的 `${...status.field}` 字符串引用改为类型化 `*From` 字段（`addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`dependsOn`）。没有兼容别名。
- controller chain 重构为纯 event-loop。共用 `framework.FuncController`（Subscriptions + Bootstrap + PeriodicFunc）与 `eventedStore`，状态保存时必发 `routerd.resource.status.changed`，由下游 controller 触发再评估。
- bus event 通过 `slog` 输出到 systemd journal（`journalctl -u routerd.service -f | grep "routerd event"` 即可追踪 controller 行为）。高频事件为 debug 级别。
- 全部 binary 改为静态链接（`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`）。OS 别包清单（`dnsmasq-base`、`nftables`、`conntrack`、`iproute2`、`ppp`、`wireguard-tools`、`strongswan-swanctl`、`radvd`、`tcpdump` 等）按 Ubuntu / NixOS / FreeBSD 整理。
- `HealthCheck.sourceInterface` 在 YAML 上以资源名书写，运行时解析为 OS 接口名。

### 修复

- `generated service artifacts` 之间的 `RuntimeDirectory` 冲突会在重启时删除 socket，已通过 `runtimeDirectoryPreserve` 声明式解决。
- `state: absent` 的 `generated service artifacts` 现可正确判定为 Drifted，并加入 plan 中删除。
- `SysctlProfile` 观测时的类型漂移误判已抑制。

## 0.2.0

### 新增

- 状态化 firewall：`FirewallZone`、`FirewallPolicy`、`FirewallRule` 生成 nftables 的 `inet routerd_filter` table。
- `EgressRoutePolicy`（原名 `WANEgressPolicy`）新增 `destinationCIDRs`、`gateway`、`gatewaySource`。`HealthCheck` 可通过 `via`、`sourceInterface`、`sourceAddress` 指定 probe 路径。
- DNS 子系统重构：`DNSZone`（权威区）与 `DNSResolver`（转发 / 缓存）分离。覆盖本地区、条件转发、DoH / DoT / DoQ、明文 UDP DNS。dnsmasq 仅限 DHCPv4 / DHCPv6 / RA / 中继。
- DS-Lite（`DSLiteTunnel`）、PPPoE（`PPPoESession`、`routerd-pppoe-client`）、DHCPv4 client（`routerd-dhcpv4-client`、`DHCPv4Client`）。
- NAT44（`NAT44Rule`）与 conntrack 观测。在无 `/proc/net/nf_conntrack` 环境中回退到 sysctl 统计。

### 变更

- `WANEgressPolicy` 改名为 `EgressRoutePolicy`。没有兼容别名。
- DHCP 相关 Kind 与 binary 名称对齐 RFC 表记法（`routerd-dhcpv4-client`、`routerd-dhcpv6-client`）。没有兼容别名。

## 0.1.0

最初的 v1alpha1 实现。

- 引入 DHCPv6-PD client、daemon contract、event bus、controller framework。
- 实现从 DHCPv6-PD 到 LAN 地址推导再到 DNS 响应的 controller chain。
- 新增 DHCPv6 information-request、DS-Lite（试做）、IPv4 路由、RA、DHCPv6 server、`HealthCheck`、`EventRule`、`DerivedEvent`。

之后出货前整理过程中，API 名称与实现策略做了大幅调整。请参考上方 `Unreleased` 与 `examples/` 获取最新使用方式。
