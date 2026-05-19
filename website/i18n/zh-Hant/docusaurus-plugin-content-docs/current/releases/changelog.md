---
title: 變更記錄
---

# 變更記錄

routerd 的版本歷程。格式遵循 [Keep a Changelog](https://keepachangelog.com/)。
routerd 使用 `vYYYYMMDD.HHmm` 格式的日期與時間型版號。
本軟體仍在 v1alpha1 階段，版本之間可能含有破壞性異動。

## Unreleased

### 新增

- 為 `mode: vrrp` 的 `VirtualIPv4Address` 新增 FreeBSD CARP 後端，包括
  runtime controller、rc.d rendering、validation、tests，以及最小範例
  `examples/freebsd-vrrp.yaml`。
- 新增 ingress/local router service 的 listen-port collision validation，
  以及 Linux nftables 的 `IngressService` `sourceHash` / `random` backend
  distribution。
- 新增 FRR BGP connected/static redistribution、BGP community send/accept/set
  policy、observed community status parsing，以及
  `examples/lan-advertise-with-community.yaml`。
- 新增基於 VRF-backed FRR BGP instance 的 multi-instance `BGPRouter` support、
  listen-address collision validation、per-router observed status，以及
  `examples/multi-instance-bgp.yaml`。
- 新增面向 FRR-managed BGP peer 的 BFD support、FRR `bfdd` daemon rendering、
  BGP watcher tuning fields、BFD status observation，以及
  `examples/bgp-bfd.yaml`。
- 新增面向 Kubernetes Pod / Service CIDR static route 的
  `ClusterNetworkRoute` helper，並為 BGP peer password 與 VRRP/CARP
  authentication 增加 `passwordFrom` / `authenticationFrom` secret source。
- 新增用於暫時性 `IngressService` backend maintenance 的 `routerctl drain` /
  `undrain`，以及 VRRP production tuning 文件和
  `examples/vrrp-tuning-presets.yaml`。
- 改善 Alpine Live ISO 路徑：VRRP controller 預設為 live，
  `routerctl show vrrp` 會從 live address 重新觀測 role，version output 可嵌入
  commit，並補上 FRR reload tooling dependency 與非阻塞 setup wizard 行為。
- live VRRP reconcile 會避免 keepalived 的 no-op reload/restart，並在
  controller status 中暴露最近一次 keepalived reload/restart 的時間與原因。
- `routerd apply --once` 的 VRRP 處理現在復用與 daemon mode 相同的
  controller reconcile 路徑，因此 keepalived reload/restart status fields
  會被一致寫入。
- 將 IngressService 的 live nftables apply 與獨立 NAT44 dry-run mode 解耦；
  hostname 的 DNSZone coverage 現在降級為 warning，並可用 `externalDNS`
  標記外部 DNS 管理的名稱。
- 自動處理 IngressService 的同一 interface hairpin SNAT 和轉發所需的 runtime
  `ip_forward` sysctl，並在 `routerctl show ingress --verbose` 中顯示
  forwarding、nftables、conntrack 的 dataplane 狀態。
- 修復沒有宣告 listen-interface prefix 的 Live ISO 風格設定中的
  IngressService `hairpin.mode: auto`：同一 private `/24` 內的 listen/backend
  address 會被視為需要 hairpin，並在 verbose ingress 輸出中提示預期的 nftables SNAT
  是否缺失。
- 新增 `pkg/servicemgr` 抽象，統一 systemd、OpenRC、rc.d、NixOS 的 service
  artifact 命名和 lifecycle command，並讓 service artifact intent generation
  通過該層，減少每個 resource 中分散的 OS switch drift。
- 為所有 checked-in example config 增加 Linux、Alpine/OpenRC、FreeBSD/rc.d、
  NixOS render snapshot golden test，並增加 netns compatibility wrapper。
  `pkg/servicemgr` 也新增 lifecycle hook，使 FRR config-check + live reload、
  keepalived reload/restart 區分、signal-based daemon reload 不會退化成 generic
  restart。
- 新增 bespoke lifecycle command golden test 與 `make check-bespoke-lifecycle`
  gate，固定 FRR live reload、keepalived no-op/reload、dnsmasq SIGHUP、DHCP
  daemon IPC、BFD daemon enablement、IngressService nftables-only backend
  rotation、VRRP track artifact、DS-Lite dataplane hook、DHCP event daemon
  ordering，以及 FRR graceful-restart observation。
- 為 nftables / pf 的 render、diff、reload 路徑新增無行為變化的 firewall
  backend abstraction，並用 regression contract 固定 nftables 的 `ct state`、
  `jhash`、`numgen`、hairpin conntrack expression，以及 pf 的 `rdr`、
  `nat-anchor`、hairpin NAT syntax。
- 為 netplan、systemd-networkd drop-in、NixOS module、FreeBSD rc.conf
  fragment 新增無行為變化的 network config backend abstraction，並以通用
  IPv4/IPv6 address 與 route declaration 表示網路設定。
- 將 PPPoE、VRRP/CARP、FRR、dnsmasq、DHCPv6 PD、DNS resolver、Tailscale 的
  service-backed artifact intent 整理為 ServiceManager declaration table，使
  systemd/OpenRC/rc.d/NixOS ownership 在不改變輸出的前提下保持一致。
- 擴展 render golden coverage，涵蓋 firewall hole derivation 與 OS-specific
  interface/network artifacts，並固定 Linux netplan/systemd-networkd output 與
  Alpine nftables snapshot。
- 強化 abstraction layer regression coverage，新增 cross-OS semantic test、
  invalid spec check、firewall backend error propagation status/event、
  edge-case declaration、race-tested reload、80% coverage gate，以及 4 OS 的
  bespoke lifecycle command matrix。

## v20260519.0743

### 變更

- 整理公開 documentation 與 example configuration 的命名，避免內部 lab
  hostname、domain、management network address 出現在 website 或可複用 example
  中，而是保留在 internal notes。
- 將 internal design / soak note 移出公開 Docusaurus docs tree，並在
  `internal/notes/` 記錄 native nDPI 與 RA/DHCPv6-PD coverage 的 lab
  validation policy。

## v20260519.0713

### 修正

- `routerctl show bgp`、`routerctl show vrrp`、`routerctl show ingress` 不再開啟
  ownership ledger，因此在指定 status store 且 default ledger path 不可寫的環境中
  也能正常執行。

## v20260519.0708

### 新增

- 新增面向 Kubernetes edge 使用情境的 FRR backend `BGPRouter` / `BGPPeer`、
  keepalived backend `VirtualIPv4Address`，以及 `IngressService` backend
  health/failover controller。
- 新增 `routerctl show bgp`、`routerctl show vrrp`、`routerctl show ingress`
  table view，從 VIP/ingress `hostname` field 自動推導 DNS record，並新增
  BGP/VRRP/Ingress transition 與 backend health 的 OTel metrics。
- Web Console 新增 BGP、VRRP、IngressService dedicated view 與 JSON endpoint。

### 變更

- FRR BGP 設定現在會先用 `vtysh -C -f` 驗證，再透過 `frr-reload.py --reload`
  差分套用。VRRP 預設使用 unicast peer 與 `nopreempt`，並支援 track hysteresis
  和 `preemptDelay`。BGP、VRRP、IngressService listen port 的 firewall hole
  也會自動推導。
- BGP reconcile 不再讓 dry-run 寫入遮蔽後續 live apply；初次 live 觀測時會先比較
  FRR running-config，再決定是否 reload，避免已一致的 session 被 no-op reload reset。

## v20260518.1810

### 新增

- 新增獨立的 `routerd-ndpi-agent-libndpi-linux-amd64` release archive，
  供需要啟用 native nDPI classification 的主機使用。一般 Linux release
  archive 仍維持完全靜態連結，optional nDPI agent override 使用
  `CGO_ENABLED=1 -tags libndpi` 建置，並透過 libndpi self-test 驗證。

## v20260518.1431

### 新增

- 在 control API、log、OpenTelemetry metrics/traces，以及 Web Console 的
  controller view 中新增 controller reconcile runtime status。controller status
  現在會回傳 interval、trigger、執行次數、錯誤次數、last/average/max duration，
  以及最新錯誤。

## v20260518.1301

### 變更

- 移除了目前 controller runtime 設定路徑已不再使用的 dead compatibility helper
  和舊 raw systemd unit renderer。

## v20260517.2339

### 新增

- 新增 Configuration examples 文件區段，包含編號 topology diagrams、diagram-to-YAML
  對應註解、安全注意事項，以及基本 IPv4 NAT、LAN DHCP/DNS、DS-Lite、PPPoE、
  port forwarding、guest isolation、multi-WAN failover、local DNS redirect、
  Tailscale、WireGuard、telemetry export 等已驗證 sample YAML。
- IPv4 route policy resource 參照的 health check 現在會從參照來源的 route
  candidate 或 target 推導 socket mark。單獨 probe 仍可使用 `spec.fwmark`，
  validation 會拒絕明確 mark 與推導 mark 衝突的設定。

### 變更

- Linux upgrade 現在只會在 routerd helper systemd service 仍執行已刪除的舊 binary，
  或 unit file 在 helper process 啟動後重新產生時，才重新啟動該 helper。installer
  會先等待 `routerd.service` 與 routerd 管理的 unit file 穩定後再判斷。
- release installer 現在會在 NixOS 上略過 host service manager 變更，因此
  `/etc/systemd/system` 為唯讀且 service unit 由宣告式設定管理的 host，也能用 archive
  更新 binary。
- 當 host 沒有 conntrack procfs file 時，conntrack observation 會記錄 `Unavailable`
  status，而不是每個 interval 都寫出 warning。
- FreeBSD `--skip-service-manager` apply 現在會抑制 generated helper、managed dnsmasq、
  以及 pf/pflog service activation 的 rc.d/service 操作，同時仍允許寫入 rc.conf-backed
  network state 並直接透過 `pfctl` 載入 rule。這可避免 recovery 與 bootstrapping path
  和 base rc boot sequence 競爭。
- FreeBSD upgrade 現在會保留 config-managed `routerd` rc.d script，不再用 generic
  bootstrap template 覆蓋；這與 Linux 保留 config-managed `routerd.service` 的行為一致。
- `routerd serve` 現在會在收到 SIGTERM/SIGINT 時 cleanly shutdown control 與 status
  socket，讓 FreeBSD rc.d 在 `daemon(8)` 下 restart 時能正常停止，不會落到 forced KILL。
- routerd state SQLite database 現在搭配既有 busy timeout 使用 WAL mode，減少 status
  reader 與 controller 重疊時短暫發生的 `SQLITE_BUSY`。

## v20260517.1808

### 修正

- Debian/Ubuntu release installer 現在會安裝 `dnsmasq-base`，而不是完整的
  `dnsmasq` package，避免 distro 的 `dnsmasq.service` 被啟用並與 routerd 管理的
  dnsmasq instance 競爭。

## v20260517.1800

### 修正

- controller 與 helper probe 發出的單次 HTTP-over-Unix 呼叫現在會停用
  keep-alive，並明確關閉 idle transport。這可避免週期性的 status polling 在
  `routerd`、health check helper、DHCP client、DNS/DPI helper service 中留下大量
  已建立的 Unix socket。

## v20260517.1533

### 修正

- release helper 現在會在 schema check 前重新產生受管理的 config schema 與
  control API schema。API type 變更會包含在 release commit 中，而不是到 release
  後段才失敗。
- `routerctl` 現在只會針對唯讀 control API request，retry daemon 啟動期間暫時性的
  Unix socket 連線失敗。`routerctl status` 預設會使用獨立的唯讀 status socket，
  而 apply 與 delete 仍只使用具權限的 control socket，且不會 retry。

## v20260517.1510

### 新增

- Web Console Connections 現在會標示由 `LocalServiceRedirect` 處理的 flow。
  當 live conntrack tuple 與已解析的 set status 能辨識 match 時，也會顯示
  redirect rule 與目的地 `IPAddressSet`。
- Web Console Firewall 現在會在 deny log row 顯示目的地 `IPAddressSet` match，
  並區分明確的 `FirewallRule.destinationSetRefs` match，以及目前存在於已設定
  set 內的目的地。

## v20260517.1401

### 修正

- 修正 Web Console disk usage collection，使其在 `syscall.Statfs_t` block counter
  使用 signed integer type 的 FreeBSD 上也能編譯。

## v20260517.1353

### 修正

- release helper 現在會拒絕第一個 release section 不是 `Unreleased` 的
  changelog，並從維護中的 changelog 檔案移除了舊 helper 執行留下的空 release
  標題。

## v20260517.1351

### 變更

- `routerd-dpi-classifier` 現在有明確的 classifier engine facade。預設 engine 是
  built-in parser，`auto` / `ndpi-agent` mode 可以查詢未來的
  `routerd-ndpi-agent` Unix socket service，失敗時會 fallback 到 built-in parser。
- Web Console Connections 現在會在 DPI 尚未識別 flow 時，將 TCP port 4317
  標示為 OTLP，將 TCP port 4318 標示為 OTLP/HTTP。
- Web Console Overview 現在會顯示 host CPU、memory、root filesystem 使用率，
  以及 classifier 端的 DPI processing latency，方便把 router 本機負載惡化與
  routing、DPI 健康狀態一起觀察。
- Web Console Clients 與 Connections 現在可以互相跳轉。client row 可以開啟依該
  client 觀測位址篩選的 Connections，connection 詳細也可以回到對應的 local
  client identity。
- Web Console Connections 現在建立 Clients snapshot 時也會讀取近期
  traffic-flow observation，讓近期的 IPv6 privacy address 更有機會對應回 client。
  source endpoint 即使尚未合併到已知 identity，也會提供前往 Clients 搜尋的動作。
- Web Console 的搜尋輸入框現在會在有文字時顯示內嵌清除按鈕。
- release helper 現在要求 working tree 處於 clean 狀態，並會把目前
  `Unreleased` 的內容提升到 release tag，而不是建立空的 tag 標題。

### 新增

- 新增 `IPAddressSet` 與 `LocalServiceRedirect`。`IPAddressSet` 可以把直接指定的
  IPv4/IPv6 address 與 FQDN 的 `A`/`AAAA` record 解決成可重用的 nftables named
  set；`LocalServiceRedirect` 可以把 LAN client 送往這些 set 的明文 DNS/NTP
  通信 redirect 到 router 的 local service，且不會碰 DoH/DoT 或 router 自身發出的
  health check。
- `FirewallRule`、`NAT44Rule`、`IPv4PolicyRoute` 與 `IPv4PolicyRouteSet` 現在可以透過
  `destinationSetRefs` 與 `excludeDestinationSetRefs` 使用 `IPAddressSet`，讓
  FQDN-backed address set 可重用於 firewall filtering、NAT 範圍與 IPv4 policy routing 條件。
- 新增 runtime `IPAddressSet` refresh controller。被參照的 nftables set 會根據 DNS
  TTL 原地更新，使用觀測到的最小 TTL 的一半、60 秒 floor，以及可選的
  `refreshInterval` cap，讓 FQDN-backed set 不必 reload 整個 firewall、NAT 或 policy table 也能保持新狀態。
- 新增初始版 `routerd-ndpi-agent` service boundary 作為 optional command。預設
  build 會回報 libndpi backend 不可用，而 `-tags libndpi` build 會在同一個
  IPC surface 後方連結 native library。
- `routerd-ndpi-agent` 現在會持有 per-flow observation state，包括 flow TTL、flow
  數量上限、起始 payload packet 數量上限，以及 observed、classified、unknown、
  skipped、error、pruned packet 的 status counter。
- 新增 `routerd-ndpi-agent` 的初始 libndpi backend。它透過 `libndpi` build tag
  opt-in，將 native flow state 保留在 agent 內，並可分類 firewall logger 傳來的
  full packet observation。
- 新增 `make build-ndpi-agent-libndpi` target，可在已安裝 libndpi development files
  的環境中建置 optional native backend。
- 當 `routerd-dpi-classifier` 設定為 `--engine auto` 或 `--engine ndpi-agent`
  時，systemd、OpenRC、FreeBSD rc.d 與 NixOS 現在會 render `routerd-ndpi-agent`。
- DPI flow 與 traffic flow record 現在除了既有 app label 欄位外，也會保存 typed
  classifier fields，例如 detected protocol、application protocol、category、
  confidence、risk 與 metadata。
- `routerd-dpi-classifier` status 現在會回報 daemon 處理 classify request 的
  average latency 與 maximum latency。

### 修正

- Linux 升級時，如果有 routerd helper 的 systemd service 仍在執行升級前已刪除的
  binary，`install.sh` 現在會重新啟動該 service。
- 當 nDPI agent 結果已識別 application、但缺少 TLS SNI、HTTP Host 或 DNS query
  等 detail 時，`routerd-dpi-classifier` 現在會保留 built-in parser 提供的有用 hint。
- DPI helper daemon bind Unix socket 時，現在會拒絕 unlink 非 socket path；
  `routerd-ndpi-agent` 也會明確 close native libndpi state。
- Web Console 讀取 traffic-flow 時，現在可容忍 writer 尚未完成 schema migration、
  因而缺少最新 DPI column 的 legacy SQLite file。

## v20260516.2302

### 變更

- Web Console Connections 現在會將 source 到 destination 的路徑對齊在固定的
  route column，並把 state、protocol、provider、traffic 與 timeout 等 metadata
  移到獨立的 badge 區域。
- Web Console 的 connection label 現在會分開顯示 transport/application identity
  與 destination provider。像 `google-https` 這類舊的 provider-specific label
  會正規化為 `TLS`，而 Google、AWS、Microsoft、Apple 與 Cloudflare 會以獨立的
  destination provider badge 顯示。
- `https` 等 destination service 名稱現在會在能補充 connection row 資訊時，
  以 protocol badge 顯示。

### 修正

- 修正展開後的 connection detail，destination service 與 provider badge 會維持
  內容寬度，不再撐滿整個 detail column。
- 修正展開後的 connection detail，source 與 destination identity text 會使用可用寬度
  並在需要時換行，不再套用 compact row 的寬度後以省略號截斷。
- 修正 Connections 的 `Showing` metric，當 API 結果因要求的 row limit 被截斷時，
  會分開顯示 filtered rows、loaded rows 與總 conntrack count。

## v20260516.2155

### 變更

- Web Console Connections 現在預設依觀測到的傳輸 byte 數降冪排序。
  Connections 的 sort menu 新增 `Traffic` 選項，connection card 會顯示總 byte 數，
  展開詳細資訊時會在 conntrack accounting 可用時顯示 outbound、inbound 與 total counter。
- 套用 Web Console connection 數量上限時，conntrack observer 現在會在每個
  family/protocol group 內優先保留 byte 數較大的 entry。
  這可降低大型 active flow 被低 traffic entry 擠出清單的機率。

## v20260516.1413

### 修正

- 修正 `routerd apply --dry-run` 與相關 planning path，當 SQLite ownership ledger
  不存在時會視為空的 in-memory ledger，不再嘗試於無權限的 CI runner 上建立
  `/var/lib/routerd`。

## v20260516.1405

### 新增

- 在 `firewall.routerd.net/v1alpha1` 新增 `PortForward` 與單一 backend 的
  `IngressService`，用於描述 WAN 側 IPv4 TCP/UDP ingress DNAT。
- Linux nftables 與 FreeBSD pf rendering 現在可以發布這些 ingress service。
  也可選擇產生 hairpin NAT，讓 LAN client 透過 WAN address 存取同一個
  port-forwarded service。
- 為新的 ingress NAT resource 新增 generated JSON Schema、CLI alias、API
  documentation 與 resource ownership documentation。

## v20260516.0804

### 變更

- Web Console Connections 現在以固定的 IP family 與 transport protocol
  bucket 彙整 active flow，不再依 DPI application 拆成多個表格。
  TLS、DNS、QUIC 等 app label 仍會顯示在各 group 內。

## v20260514.1433

## v20260514.0813

### 修正

- 修正 Web Console Clients，在與目前 DHCP lease 關聯之前，將以 IP address 為基礎的
  DNS、traffic、firewall、DPI 與 DHCP fingerprint 證據限制在相同的最近一小時
  observation window 內。
- client inventory 的 sticky DHCP lease annotation 現在只使用 active hold，
  避免舊 lease history 混入目前的 endpoint identity 判定。

## v20260514.0743

### 修正

- 修正 Web Console Clients，忽略已過期的 dnsmasq lease，避免舊 host 無限期留在清單中。
- DHCP lease 合併現在會優先採用最新的有效 lease，只有在條件相同時才以 lease file 設定順序作為 tie-breaker。
- routerd 現在會把 controller runtime dnsmasq lease file 作為第一候選傳給 Web Console，
  讓 console 依照受管理 dnsmasq 實際使用的 lease file 顯示。

## v20260514.0654

### 修正

- 修正 Web Console Overview，避免把首次輕量 snapshot 記錄成數值為 0 的 metric sample。
- Overview 的延遲 refresh 現在會載入所需的 resource、event、conntrack、DNS
  與近期 traffic flow 資料，同時仍避開較重的 firewall、VPN 與 client inventory 工作。
- Overview card 會將尚未取得的 flow / connection data 顯示為 loading state，
  不再把不可用的值呈現為 0。

## v20260514.0037

### 修正

- DHCPv4 LAN domain rendering 現在會在未明確設定 domain-search option 時，從 `domain` / `domainFrom` 同時產生 domain-name 與 domain-search。

## v20260514.0025

### 新增

- 新增 `domainFrom`、`dnsslFrom` 與 `domainSearchFrom`，讓 DHCPv4、
  IPv6 RA 與 DHCPv6 的 LAN suffix 宣告可參照 `DNSZone/<name>.zone`，
  不必重複寫入本地域名字串。

## v20260513.2358

### 變更

- 強化長期運行的事件處理。`EventRule` 與 `DerivedEvent` 的 timer 觸發後會清理 map entry，忽略過期的 timer callback，並以 controller lock 保護共享狀態。
- 為 `EventRule` 的 correlation state 設定上限，避免高基數事件流讓記憶體用量無限制成長。
- daemon 的 `events.jsonl` 不再無限追加，而是在固定大小後輪替。
- 為 local control、daemon event、DNS resolver、DoH 與 classifier 路徑加入 request / response 大小限制，並為 local daemon server 與 Web Console 加入 HTTP header timeout。

### 修正

- 修正 `DerivedEvent` hysteresis 處理中 timer callback 與 reconcile 可能同時更新 pending transition state 的 race。

## v20260513.2317

### 變更

- 配合 `v20260513.2252` 的穩健化工作，更新 production reconcile 文件。operations、upgrade、state ownership 與各語言 changelog 現在說明主機狀態 drift 檢查、受管理 artifact 清理、nftables named set 更新，以及由設定管理的 `routerd.service` 在 upgrade 時的處理方式。

## v20260513.2252

### 變更

- 強化 production reconcile。controller 在略過工作前，會同時檢查 status database 與實際主機狀態；範圍包含 systemd unit、dnsmasq、DHCPv4 lease 位址、route-policy nftables table、NAT44，以及相關的受管理 artifact。
- Health check 的 `fwmark` 現在會傳遞到產生的 systemd unit、socket 設定、status 觀測值與 OpenTelemetry attributes。probe 可以使用與被檢查路徑相同的 policy-route mark。
- Linux firewall rendering 會在重新定義 routerd-managed named set 前先清除它們。已移除的 zone interface 或 client-policy MAC 位址不會殘留在 nftables 中，同時仍會保留整個 managed filter table，不會 destroy/recreate table。
- release installer 會保留由設定管理的 `routerd.service`，不再以 archive template 覆寫。當 routerd 管理自己的 unit 時，unit file 變更會透過 `systemd-run` 排程延遲 self-restart。

### 修正

- 當 `HealthCheck` resource 從 YAML 消失時，會移除對應的舊 `routerd-healthcheck@*.service` unit。
- 移除最後一條 NAT rule 後，會清空受管理的 NAT44 table 或 pf anchor。
- status 顯示 DHCPv4 lease 位址存在，但介面上實際缺少該位址時，會重新套用位址。
- 空的 `WireGuardPeer` resource 現在標示為 `NotConfigured`，避免停留在容易誤解的 Pending 狀態。

## v20260513.1931

### 修正

- 穩定 health check 路徑切換行為。

## v20260513.1153

### 修正

- 穩定 controller reconcile 的冪等性。

## v20260513.0836

### 新增

- 新增 WireGuard mesh controller。

## v20260513.0727

### 變更

- 提高 home-router 的 UDP conntrack timeout 設定。

## v20260512.0037

### 新增

- 從 conntrack observer 匯出 DPI flow metrics。

## v20260512.0032

### 新增

- 在 Web Console Overview 頁面新增 DPI summary card。

## v20260512.0027

### 新增

- 在 Web Console Clients 頁面新增 DPI activity summary。

## v20260512.0008

### 新增

- 在 Web Console Connections 頁面顯示 DPI classification。

## v20260511.2357

### 變更

- 將 DPI enrichment 擴展到 forward flow。

## v20260511.2307

### 修正

- 抑制 Web Console 的水平 overscroll。

## v20260511.2300

### 修正

- 修正 Firewall timeline 的水平捲動。

## v20260511.2253

### 變更

- 將 Web Console 整理為 content-driven layout section。

## v20260511.2217

### 驗證

- 驗證 mobile Web Console layout。

## v20260511.2211

### 變更

- Web Console 在頁面切換後會保留 page state。

## v20260511.2154

### 變更

- 整理 Clients inventory view。

## v20260511.2145

### 新增

- 新增 Web Console SSE reconciliation。

## v20260511.2130

### 新增

- 新增 client fingerprint inference。

## v20260511.2106

### 變更

- 關聯 expired conntrack return flow。

## v20260511.2045

### 變更

- 為 firewall deny event 加上 DPI context。

## v20260511.2018

### 驗證

- 驗證 DPI classifier OS parity。

## v20260511.1846

### 修正

- 將 Web Console time locale 固定為 English。

## v20260511.1840

### 新增

- 新增 isolated DPI classifier proof of concept。

## v20260511.1820

### 新增

- 新增 Connections protocol summary。

## v20260511.1709

### 修正

- 修正 release artifact checksum。

## v20260511.1428

### 變更

- 改善 Web Console navigation section。

## v20260511.1240

### 變更

- 調整 controller mode reason 的呈現。

## v20260511.1041

### 新增

- 提高 dry-run controller 的可見度。

## v20260511.1017

### 變更

- 明確顯示 controller dry-run mode。

## v20260510.1956

### 變更

- 讓 `NetworkAdoption` 管理 resolved DNS。

## v20260510.1811

### 新增

- 將 PVE live ISO serial-console 驗證日誌加入 `internal/notes/`，讓 walkthrough 截圖與執行日誌作為測試證據保存在同一 release 中。

## v20260510.1802

### 變更

- 在日文、簡體中文和繁體中文的 diskless mini PC walkthrough 中嵌入 PVE live ISO boot test 的實際截圖。
- 移除 diskless mini PC walkthrough 中殘留的舊 placeholder 圖片引用。

## v20260510.1750

### 新增

- 在 diskless mini PC walkthrough 中加入 PVE live ISO 實機驗證截圖。
- 為簡體中文和繁體中文補充 positioning、USB persistence 與 legal redistribution 頁面。

### 變更

- 將 website footer 的 copyright 文字改為先寫著作權聲明的慣用形式。
- 更新 diskless mini PC walkthrough 的 PVE 範例，同時啟用 VGA 與 serial console，方便在同一次驗證中取得 QEMU screenshot 和 `qm terminal` 日誌。

### 修正

- 修正 live ISO configure wizard，使 DHCPv4 pool 預設值從選擇的 LAN address prefix 推導。
- 重新執行 PVE live ISO boot test，並確認 `/tmp/iso-boot-test-20260510-1742.log`、QEMU screenshots、routerd apply、Healthy status 與 USB persistence flush。

## v20260510.1722

### 新增

- 為 routerd Go source、installer scripts、plugin scripts 與 Web Console source 增加 BSD 3-Clause SPDX identifiers。
- 在 README 中加入 license badge，並從英文與日文 README 連結到 BSD 3-Clause License。
- 新增公開 contributing 文件，並從 docs sidebar 連結。
- 在 SECURITY 中補充 email 與 GitHub Security Advisories 報告路徑。

### 變更

- 將 repository root 的 `LICENSE` copyright notice 統一為 `Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors`。
- 在 legal 文件中說明 SPDX headers 只適用於 routerd source files；bundled third-party software 繼續遵循 `THIRD_PARTY_LICENSES.md` 中的各自 license。
- 從 README 移除產品比較表，改為說明 routerd 自身的範圍與特點。

## v20260510.1626

### 新增

- 新增公開 legal 與 redistribution 頁面，整理 release checklist。
- 在產生的第三方授權清單中加入 Go module source URL。
- 記錄 BSD routerd binary 與 aggregate live ISO distribution model 的內部 license audit note。

## v20260510.1612

### 新增

- 新增 Go module 與 live ISO Alpine package 的第三方授權清單自動產生流程。
- 新增 release archive 與 live ISO 內的授權通知安裝位置。
- 文件補充 routerd 本體 BSD 3-Clause License，以及 live ISO 作為 aggregate distribution 的處理方式。

## v20260510.1547

### 新增

- 擴充公開定位說明，重點說明 routerd 自身的範圍與 deployment spectrum。
- 擴充 Intel NUC、N100 mini PC、Raspberry Pi 5、thin client 與 Proxmox VM 的硬體相容性說明。
- 新增中文硬體相容性頁面，並補充 live ISO 與 USB persistence 的使用路徑。

## v20260510.1534

### 新增

- 新增 diskless mini PC walkthrough 圖、tutorial index 更新與 field-note blog post。

## v20260510.1508

### 新增

- 新增 USB persistence 操作文件與 live ISO USB persistence 支援。

## v20260510.1451

### 新增

- 新增 contribution、security、license、positioning、hardware compatibility 與 diskless mini PC 文件。

## v20260510.1429

### 新增

- 新增 Alpine live ISO build 與 install documentation。

## v20260510.1412

### 新增

- 新增 live ISO validation note 與 live ISO 路徑的 installer documentation。

## v20260510.1354

### 修正

- 修正 Alpine 上的 live ISO runtime apply。

## v20260510.1310

### 新增

- 啟用 live ISO serial console support。

## v20260510.1301

### 變更

- 將 release tag 切換為 JST timestamp 格式。

## 20260510.4

### 修正

- 修正 live ISO overlay archive path。

## 20260510.3

### 修正

- 修正 Alpine live ISO release discovery。

## 20260510.2

### 新增

- 新增 Alpine-based live ISO packaging。

## 20260510.1

### 新增

- 新增 installer configuration wizard。

## 20260510.0

### 變更

- 在 fixed-download-asset release 之後，開始 20260510 release series。

## 20260509.16

### 新增

- Release archive 現在除了 versioned archive，也包含 `routerd-linux-amd64.tar.gz` 這類固定名稱 alias。
- 固定名稱 archive 與 `.sha256` 檔會上傳到 GitHub Releases，因此文件可以使用 `releases/latest/download/...` URL。

### 異動

- Quick start 文件改用 stable latest-download URL，不再硬編特定 release version。
- release workflow 會在支援時讓 GitHub JavaScript actions 使用 Node.js 24 runtime。

## 20260509.15

### 新增

- 新增 branch push 與 pull request 用的 `CI` GitHub Actions workflow。
- CI workflow 會在 Ubuntu 上執行 `go test ./...`、schema 檢查、example 驗證與網站建置。
- 新增可選的 `scripts/pre-commit.sh` hook，在本機 commit 前執行 Go test 與 schema 檢查。
- 新增 development 文件，說明 CI、pre-commit check 與 tag-driven release publishing 的分工。

## 20260509.14

### 驗證

- 在 Ubuntu lab router 上驗證 `ClientPolicy` guest mode。
- 確認 Linux nftables 會產生 include mode guest MAC set、guest DNS/DHCP/NTP allow、自我隔離，以及 RFC 1918 / ULA deny 規則。
- exclude mode 已透過 focused nftables renderer test 驗證。

## 20260509.13

### 新增

- 擴充 guest mode guide，加入使用情境、內部實作、完整 `ClientPolicy` field reference、驗證步驟、troubleshooting 與安全限制。
- 新增 include mode、exclude mode、多個 guest device、自訂 deny/allow list、local discovery service 與 IoT reservation 範例。
- `ClientPolicy.spec.guestServices` 現在除了 `dhcp`、`dns`、`ntp`，也接受 `mdns` 與 `ssdp`。

## 20260509.12

### 新增

- 新增 `ClientPolicy`。它是由 Linux nftables 支援的 guest mode，可依 MAC 位址分類 LAN client。
- guest client 可使用 DNS、DHCP、NTP，但預設會拒絕前往 private IPv4 與 ULA IPv6 目的地的轉送。
- 新增 `examples/guest-mode.yaml` 與 include mode / exclude mode 文件。

### 異動

- FreeBSD pf 會明確拒絕 `ClientPolicy`，因為 pf 沒有相同的 MAC-based routed filtering 模型。

## 20260509.11

### 新增

- 新增最小 Tailscale mesh、WireGuard hub-spoke、VRF lab 與 multi-WAN home fallback 的用途別範例。
- 新增 `examples/README.md`，說明各範例適合的使用情境。

### 異動

- `make validate-example` 現在會驗證 `examples/` 目錄下的所有 YAML 檔案。

## 20260509.10

### 新增

- Web Console Overview 會顯示 generation、resource phase、HealthCheck 狀態的簡易趨勢圖。
- Config 頁可比較目前 YAML 檔案與最新已套用 generation，方便在執行 `routerd apply` 前確認差異。
- Resource 表格支援 kind、name、phase、詳細內容搜尋、phase 篩選與結果標示。
- VPN 頁面新增 Tailscale 與 WireGuard peer 狀態的視覺摘要。

## 20260509.9

### 新增

- release archive 現在包含 `share/doc/TARGET`，`install.sh` 會檢查 archive 的 OS / CPU 架構是否符合主機。
- GitHub Actions 會產生 Linux 與 FreeBSD 的 `amd64` / `arm64` archive。
- release CI 會對 `install.sh` 與 `uninstall.sh` 執行 `shellcheck`。

### 異動

- `install.sh --list-deps` 改為結構化輸出，列出 OS、CPU 架構、套件管理器、套件與檢查命令。
- 依賴清單加入 PPPoE、RA、IPsec、封包擷取、路由與 firewall 工具。

## 20260509.8

### 修正

- 修正 zh-Hant 與 zh-Hans 文件連結，翻譯頁不再指向尚未翻譯的同語系頁面。
- 在完整翻譯完成前，總覽頁會連到英文正準參考頁。

## 20260509

### 新增

- `EgressRoutePolicy` 現在可以表達 DS-Lite 主路徑、RA 來源 DS-Lite、PPPoE 與 WAN 直連的多階段備援。
- 透過宣告式 `Telemetry` 資源與 OTLP 環境變數傳遞，將 OpenTelemetry 設定擴展到路由器群。
- DS-Lite 範例改用 RFC 6333 的 B4-AFTR link prefix `192.0.0.0/29` 作為隧道內側 IPv4 來源位址。
- `PPPoEInterface.disabled` 與停用的路徑候選允許在 YAML 中保留 PPPoE 備援定義，同時避免正式環境 PPPoE session 外洩。

### 異動

- 版號從 `0.x.y` 改為 `20260509` 這類日期字串。
- Linux nftables 與 FreeBSD pf 的 NAT44 產生方式收斂為按介面產生規則。
- 已在 Linux 與 FreeBSD 驗證 3-role firewall；service hole 會綁定到擁有它的接收入介面。
- FreeBSD pf 支援為 `PathMTUPolicy` 產生 TCP MSS clamp；dnsmasq RA 也會發布 MTU option。

### 修正

- FreeBSD pf 不再將 DHCPv6、WireGuard、VXLAN 的 service hole 擴展到 `wan` zone 的所有介面。
- FreeBSD NAT artifact 現在回報為 `pf.anchor/routerd_nat`。
- NAT 產生前會將 PPPoE 資源名解析為實際 OS 介面名。

## 0.4.0

### 新增

- nftables 的隱含拒絕封包紀錄會由 `routerd-firewall-logger` 接收，並寫入 `firewall-logs.db`。Linux 直接讀取 `nfnetlink`，FreeBSD 透過 `tcpdump` 讀取 `pflog`。
- Web Console 新增「Connections」分頁（即時 conntrack / pf state）、「Clients」分頁（DHCP 租約與流量整合）以及「Firewall」分頁（拒絕排行 + 時間序列）。
- `WebConsole.spec.listenAddressFrom` 與 `DNSResolver` 系列的待聽位址，可由 `Interface/<name>.status.ipv4Addresses` 推導。允許以參考代替字面值。
- 預設啟用 conntrack 計數（`net.netfilter.nf_conntrack_acct=1`），`SysctlProfile/router-linux` 將其納入；`TrafficFlowLog` 因此能聚合 `bytesOut` / `bytesIn`。

### 異動

- 即時連線檢視的 API / CLI 統一命名為 `connections`（舊稱 `conntrack-snapshot`）。請改用 `/api/v1/connections`、`routerctl connections`。IPv6 也納入同一張表。
- NixOS 的宣告式渲染擴充。`Package`（NixOS 套件宣告）、`SysctlProfile`、`NetworkAdoption`、`generated service artifacts` 皆會輸出至 `routerd render nixos`。NixOS 上的 `Package` 不再於執行期安裝，而由產生的 NixOS 設定接管。
- `generated service artifacts` 可產生 FreeBSD `rc.d` 腳本（`routerd render freebsd --out-dir`）。

### 修正

- 當 `Link/<name>` 狀態為空時，`IPv6DelegatedAddress` 不再略過將 PD 派生位址掛上實體介面的步驟。
- `generated service artifacts` 不再對未變動的 active unit 做不必要的重啟。

## 0.3.0

### 新增

- 宣告式 OS bootstrap 資源 `Package` 與 `SysctlProfile`。涵蓋 apt、dnf、nix、pkg 的套件宣告，以及路由器導向的 sysctl 推薦值（`nf_conntrack_max`、socket buffer、TCP/UDP timeout、`ip_forward` 等）。
- `NetworkAdoption` 可由 YAML 關閉 systemd-networkd 的 DHCP / RA。`generated service artifacts` 由 routerd 自身渲染、安裝、啟用 unit 檔案。
- `routerctl events --limit N --topic X --resource K/N -o json` 不再依賴 `sqlite3` 即可檢視 bus event。
- `routerd plan --diff` 提供 apply 前的差異預覽。
- `DNSResolver` 支援 bootstrap forwarder（內部 DNS 為主，公用 DNS 為備援）。

### 異動

- 設定檔的 `${...status.field}` 字串參考改為型別化 `*From` 欄位（`addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`dependsOn`）。沒有相容別名。
- controller chain 重構為純 event-loop。共用 `framework.FuncController`（Subscriptions + Bootstrap + PeriodicFunc）與 `eventedStore`，狀態保存時必發 `routerd.resource.status.changed`，由下游 controller 觸發再評估。
- bus event 透過 `slog` 輸出至 systemd journal（`journalctl -u routerd.service -f | grep "routerd event"` 即可追蹤 controller 行為）。高頻事件為 debug 等級。
- 全部 binary 改為靜態連結（`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`）。OS 別套件清單（`dnsmasq-base`、`nftables`、`conntrack`、`iproute2`、`ppp`、`wireguard-tools`、`strongswan-swanctl`、`radvd`、`tcpdump` 等）按 Ubuntu / NixOS / FreeBSD 整理。
- `HealthCheck.sourceInterface` 在 YAML 上以資源名表示，於執行期解析為 OS 介面名。

### 修正

- `generated service artifacts` 之間的 `RuntimeDirectory` 衝突會在重啟時刪除 socket，已透過 `runtimeDirectoryPreserve` 宣告式解決。
- `state: absent` 的 `generated service artifacts` 現可正確判定為 Drifted，並列入 plan 中刪除。
- `SysctlProfile` 觀測時的型別漂移誤判已抑制。

## 0.2.0

### 新增

- 狀態化 firewall：`FirewallZone`、`FirewallPolicy`、`FirewallRule` 產生 nftables 的 `inet routerd_filter` table。
- `EgressRoutePolicy`（原名 `WANEgressPolicy`）新增 `destinationCIDRs`、`gateway`、`gatewaySource`。`HealthCheck` 可透過 `via`、`sourceInterface`、`sourceAddress` 指定 probe 路徑。
- DNS 子系統重構：`DNSZone`（權威區）與 `DNSResolver`（轉發 / 快取）分離。涵蓋本地區、條件式轉發、DoH / DoT / DoQ、明文 UDP DNS。dnsmasq 限定為 DHCPv4 / DHCPv6 / RA / 中繼。
- DS-Lite（`DSLiteTunnel`）、PPPoE（`PPPoESession`、`routerd-pppoe-client`）、DHCPv4 client（`routerd-dhcpv4-client`、`DHCPv4Lease`）。
- NAT44（`NAT44Rule`）與 conntrack 觀測。在無 `/proc/net/nf_conntrack` 環境會退回 sysctl 統計。

### 異動

- `WANEgressPolicy` 改名為 `EgressRoutePolicy`。沒有相容別名。
- DHCP 相關 Kind 與 binary 名稱對齊 RFC 表記法（`routerd-dhcpv4-client`、`routerd-dhcpv6-client`）。沒有相容別名。

## 0.1.0

最初的 v1alpha1 實作。

- 引入 DHCPv6-PD client、daemon contract、event bus、controller framework。
- 實作從 DHCPv6-PD 到 LAN 位址推導再到 DNS 回應的 controller chain。
- 新增 DHCPv6 information-request、DS-Lite（試作）、IPv4 路由、RA、DHCPv6 server、`HealthCheck`、`EventRule`、`DerivedEvent`。

之後出貨前整理過程中，API 名稱與實作策略做了大幅調整。請參考上方 `Unreleased` 與 `examples/` 取得最新使用方式。
